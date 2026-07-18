package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/inherentescapade/viaduct/auth"
	"github.com/inherentescapade/viaduct/cfg"
	"github.com/inherentescapade/viaduct/engine"
	"github.com/inherentescapade/viaduct/logstats"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// Version is reported to clients in ping responses.
const Version = "viaduct-server/1"

// maxEnvelopeBytes caps the request body so a hostile peer can't exhaust memory.
// Requests are small (a credential blob or a job id), so this stays modest no
// matter how large a response we're willing to send back.
const maxEnvelopeBytes = 4 << 20 // 4 MiB

// maxResponseBytes caps how much of a server reply the client will buffer. It
// must comfortably exceed maxExportBytes (jobs.go) plus envelope and JSON
// escaping overhead so a full-size log export is never truncated on the way back.
const maxResponseBytes = 2 << 30 // 2 GiB

// Options configures a Server.
type Options struct {
	Identity          *auth.Identity                                         // the server's own X25519 key
	AuthorizedKeys    []string                                               // client keys allowed to connect
	InitialAccounts   map[string]Credentials                                 // persisted account tokens, keyed by account key (see AccountKey)
	InitialClientKeys map[string]string                                      // persisted client pubkey -> account key routing
	MonitorsPath      string                                                 // base path for per-account monitor stores
	LogDir            string                                                 // base dir for per-account deletion logs (empty = cfg.LogDir())
	SaveAccount       func(acctKey string, c Credentials) error              // persists one account's pushed token
	LinkClientKey     func(clientKey, acctKey string) error                  // persists a client key's routing to its account
	AuthorizeKey      func(pubKey string) error                              // persists a newly paired client key
	OnPairingCode     func(code string, expires time.Time, requester string) // shows a pairing code on demand
	Logf              func(string, ...any)                                   // optional logger
}

// Server is the self-hosting job owner. It decrypts incoming ECIES envelopes,
// authorizes the sender, dispatches the operation, and seals the reply back.
type Server struct {
	identity      *auth.Identity
	keys          *auth.KeySet
	replay        *auth.ReplayGuard
	authzKey      func(string) error
	pairing       *pairingManager
	onPairingCode func(string, time.Time, string)
	guard         *ipGuard
	logf          func(string, ...any)

	// Per-account segregated state. clients maps an account key (a token hash,
	// or a client pubkey for a not-yet-authenticated bucket — see AccountKey)
	// to its own token, jobs, and monitors. keyToAccount remembers which
	// account each paired client key currently routes to, so requests that
	// omit credentials still land in the right place. byDiscordID maps a
	// verified Discord user ID to whichever account key was first confirmed for
	// it — since a token's bytes aren't stable across devices/re-logins,
	// linkDiscordID uses this to merge a second, differently-hashed account for
	// the same verified person back into the first. saveAccount persists an
	// account's token; linkClientKey persists that routing; monitorsBase is the
	// template path for per-account monitor stores; schedCtx, once set by
	// MonitorScheduler, makes new accounts start their monitor loop.
	clientsMu     sync.Mutex
	clients       map[string]*clientState
	keyToAccount  map[string]string
	byDiscordID   map[string]string
	saveAccount   func(string, Credentials) error
	linkClientKey func(string, string) error
	monitorsBase  string
	logBase       string
	schedCtx      context.Context
}

// New builds a Server from Options.
func New(opts Options) (*Server, error) {
	if opts.Identity == nil {
		return nil, fmt.Errorf("server identity is required")
	}
	keys, err := auth.NewKeySet(opts.AuthorizedKeys)
	if err != nil {
		return nil, err
	}
	logf := opts.Logf
	if logf == nil {
		logf = log.Printf
	}
	s := &Server{
		identity:      opts.Identity,
		keys:          keys,
		replay:        auth.NewReplayGuard(),
		authzKey:      opts.AuthorizeKey,
		pairing:       newPairingManager(),
		onPairingCode: opts.OnPairingCode,
		guard:         newIPGuard(defaultRateLimit(), logf),
		logf:          logf,
		clients:       make(map[string]*clientState),
		keyToAccount:  make(map[string]string),
		byDiscordID:   make(map[string]string),
		saveAccount:   opts.SaveAccount,
		linkClientKey: opts.LinkClientKey,
		monitorsBase:  opts.MonitorsPath,
		logBase:       opts.LogDir,
	}
	if s.logBase == "" {
		s.logBase = cfg.LogDir()
	}
	// Resume each known account so its monitors run again after a restart, and
	// restore each paired client key's routing to its account.
	for acctKey, cred := range opts.InitialAccounts {
		s.clients[acctKey] = s.newClientState(acctKey, cred)
	}
	for clientKey, acctKey := range opts.InitialClientKeys {
		s.keyToAccount[clientKey] = acctKey
	}
	return s, nil
}

// engineFor builds a Discord engine for the monitor manager (which builds its own
// resolver). It mirrors buildEngine but omits the resolver the job manager needs.
func (s *Server) engineFor(creds Credentials) (*engine.Engine, error) {
	if creds.Token == "" {
		return nil, fmt.Errorf("the server has no Discord token yet — run `viaduct remote connect` from your client first")
	}
	return engine.New(creds.Token, creds.BotMode), nil
}

// buildEngine constructs a Discord engine + resolver for the given credentials.
func (s *Server) buildEngine(creds Credentials) (*engine.Engine, *resolver, error) {
	if creds.Token == "" {
		return nil, nil, fmt.Errorf("the server has no Discord token yet — run `viaduct remote connect` from your client first")
	}
	eng := engine.New(creds.Token, creds.BotMode)
	res, err := newResolver(eng)
	if err != nil {
		return nil, nil, err
	}
	return eng, res, nil
}

// Handler returns the HTTP handler serving the sealed /rpc endpoint and the
// plaintext /pair endpoint used to bootstrap trust with a new client.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(rpcPath, s.handleRPC)
	mux.HandleFunc(pairPath, s.handlePair)
	return mux
}

// ArmPairing mints a fresh pairing code and returns it with its expiry, for the
// operator to read off the terminal. Calling it again replaces the old code.
func (s *Server) ArmPairing() (string, time.Time, error) {
	return s.pairing.arm()
}

// MonitorScheduler runs every client's monitor loop until ctx is cancelled, and
// arranges for clients that connect later to start theirs too. Run it in a
// goroutine alongside the HTTP server.
func (s *Server) MonitorScheduler(ctx context.Context) {
	s.clientsMu.Lock()
	s.schedCtx = ctx
	for _, cs := range s.clients {
		go cs.monitors.Run(ctx)
	}
	s.clientsMu.Unlock()
	<-ctx.Done()
}

// handleRPC is the heart of the server: decrypt, authorize, dispatch, seal.
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	// Turn away a proven abuser cheaply. Authenticated clients never get banned,
	// so this is the only guard their (possibly frequent) polling ever sees.
	if s.guard.rejectBanned(w, clientIP(r)) {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxEnvelopeBytes))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	var env auth.Envelope
	if err := env.UnmarshalBinary(raw); err != nil {
		s.guard.strike(clientIP(r))
		http.Error(w, "malformed envelope", http.StatusBadRequest)
		return
	}
	plaintext, sender, err := auth.Open(&env, s.identity)
	if err != nil {
		// Not decryptable: either not for us or forged. Give nothing away.
		s.guard.strike(clientIP(r))
		http.Error(w, "rejected", http.StatusBadRequest)
		return
	}
	if !s.keys.Authorized(sender) {
		s.guard.strike(clientIP(r))
		s.logf("rejected request from unauthorized key %s", auth.Fingerprint(sender))
		http.Error(w, "unauthorized key", http.StatusForbidden)
		return
	}

	var req rpcRequest
	if err := json.Unmarshal(plaintext, &req); err != nil {
		s.sealError(w, sender, "malformed request")
		return
	}
	if err := s.replay.Check(req.Nonce, req.Timestamp); err != nil {
		s.guard.strike(clientIP(r))
		s.logf("rejected %s from %s: %v", req.Op, auth.Fingerprint(sender), err)
		s.sealError(w, sender, err.Error())
		return
	}

	client := s.clientFor(sender, inlineCredentials(req.Op, req.Body))
	body, err := s.dispatch(client, req.Op, req.Body)
	if err != nil {
		s.logf("rpc %s from %s failed: %v", req.Op, auth.Fingerprint(sender), err)
		s.sealError(w, sender, err.Error())
		return
	}
	s.sealOK(w, sender, body)
}

// inlineCredentials peeks an operation's body for credentials it may carry, so
// the request that's actually arriving — not just past visits — can determine
// which account it belongs to. Returns nil if the operation has no
// credentials field or none were sent.
func inlineCredentials(op Op, body json.RawMessage) *Credentials {
	switch op {
	case OpCredentials:
		var c Credentials
		if err := json.Unmarshal(body, &c); err == nil && c.Token != "" {
			return &c
		}
	case OpPreview, OpPreviewStart, OpSubmitJob, OpSetMonitor, OpPreviewMon:
		var req struct {
			Credentials *Credentials `json:"credentials,omitempty"`
		}
		if err := json.Unmarshal(body, &req); err == nil && req.Credentials != nil && req.Credentials.Token != "" {
			return req.Credentials
		}
	}
	return nil
}

// dispatch routes an operation to its handler, scoped to the calling client's
// own state, and returns the JSON body to seal.
func (s *Server) dispatch(client *clientState, op Op, body json.RawMessage) (any, error) {
	switch op {
	case OpPing:
		return s.opPing(client), nil
	case OpCredentials:
		return s.opCredentials(client, body)
	case OpPreview:
		return s.opPreview(client, body)
	case OpPreviewStart:
		return s.opPreviewStart(client, body)
	case OpPreviewStatus:
		return s.opPreviewStatus(client, body)
	case OpSubmitJob:
		return s.opSubmitJob(client, body)
	case OpListJobs:
		return JobListResponse{Jobs: client.jobs.list()}, nil
	case OpGetJob:
		return s.opGetJob(client, body)
	case OpCancelJob:
		return s.opCancelJob(client, body)
	case OpRemoveJob:
		return s.opRemoveJob(client, body)
	case OpRetryJob:
		return s.opRetryJob(client, body)
	case OpExportJob:
		return s.opExportJob(client, body)
	case OpExportChunk:
		return s.opExportChunk(client, body)
	case OpSetMonitor:
		return s.opSetMonitor(client, body)
	case OpListMonitor:
		return MonitorListResponse{Monitors: client.monitors.List()}, nil
	case OpDelMonitor:
		return s.opDelMonitor(client, body)
	case OpPreviewMon:
		return s.opPreviewMonitor(client, body)
	case OpLogStats:
		return s.opLogStats(client)
	case OpListRuns:
		return s.opListRuns(client)
	case OpSearchLogs:
		return s.opSearchLogs(client, body)
	case OpExportSearch:
		return s.opExportSearch(client, body)
	case OpPurgeLogs:
		return s.opPurgeLogs(client, body)
	case OpExportRunChunk:
		return s.opExportRunChunk(client, body)
	case OpDeleteRun:
		return s.opDeleteRun(client, body)
	default:
		return nil, fmt.Errorf("unknown operation %q", op)
	}
}

// sealOK marshals body, wraps it in an OK rpcResponse, seals it to the recipient,
// and writes it.
func (s *Server) sealOK(w http.ResponseWriter, recipient [auth.KeySize]byte, body any) {
	raw, err := json.Marshal(body)
	if err != nil {
		s.sealError(w, recipient, "server failed to encode response")
		return
	}
	s.writeSealed(w, recipient, rpcResponse{OK: true, Body: raw})
}

func (s *Server) sealError(w http.ResponseWriter, recipient [auth.KeySize]byte, msg string) {
	s.writeSealed(w, recipient, rpcResponse{OK: false, Error: msg})
}

func (s *Server) writeSealed(w http.ResponseWriter, recipient [auth.KeySize]byte, resp rpcResponse) {
	raw, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, "encode error", http.StatusInternalServerError)
		return
	}
	env, err := auth.Seal(raw, s.identity, recipient)
	if err != nil {
		http.Error(w, "seal error", http.StatusInternalServerError)
		return
	}
	out, _ := env.MarshalBinary()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(out)
}

// --- pairing (plaintext bootstrap) ---

// handlePair serves the plaintext pairing endpoint. It runs a two-message
// SPAKE2 exchange keyed by the code shown on the server: "start" exchanges
// SPAKE2 elements and returns the server's confirmation; "confirm" verifies the
// client's confirmation and authorizes its key.
func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	// Pairing is unauthenticated and the brute-force surface, so throttle it.
	if s.guard.throttle(w, clientIP(r)) {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	var req PairRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		http.Error(w, "malformed request", http.StatusBadRequest)
		return
	}
	switch req.Phase {
	case "request":
		s.handlePairRequest(w, r, req)
	case "start":
		s.handlePairStart(w, r, req)
	case "confirm":
		s.handlePairConfirm(w, r, req)
	default:
		http.Error(w, "unknown pairing phase", http.StatusBadRequest)
	}
}

// handlePairRequest is the first step a client makes: it asks the server to
// start pairing. The server mints a code (or reuses an active one) and shows it
// on its own terminal — never in the reply — so only someone watching the
// server can read it. The client's user then enters it to finish.
func (s *Server) handlePairRequest(w http.ResponseWriter, r *http.Request, req PairRequest) {
	code, expires, err := s.pairing.request()
	if err != nil {
		http.Error(w, "could not start pairing", http.StatusInternalServerError)
		return
	}
	requester := clientIP(r)
	if pub, perr := auth.ParsePublicKey(req.ClientPub); perr == nil {
		requester = fmt.Sprintf("%s from %s", auth.Fingerprint(pub), clientIP(r))
	}
	if s.onPairingCode != nil {
		s.onPairingCode(code, expires, requester)
	}
	s.logf("pairing requested by %s", requester)
	writeJSON(w, PairResponse{OK: true})
}

func (s *Server) handlePairStart(w http.ResponseWriter, r *http.Request, req PairRequest) {
	clientPub, err := auth.ParsePublicKey(req.ClientPub)
	if err != nil {
		s.guard.strike(clientIP(r))
		http.Error(w, "invalid client key", http.StatusBadRequest)
		return
	}
	msgA, err := base64.StdEncoding.DecodeString(req.MsgA)
	if err != nil {
		s.guard.strike(clientIP(r))
		http.Error(w, "invalid pairing message", http.StatusBadRequest)
		return
	}
	sid, msgB, serverConfirm, err := s.pairing.start(s.identity.Public(), clientPub, msgA)
	if err != nil {
		s.guard.strike(clientIP(r))
		s.logf("pairing start from %s rejected: %v", auth.Fingerprint(clientPub), err)
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	writeJSON(w, PairResponse{
		Session:       sid,
		ServerPub:     s.identity.PublicKeyString(),
		MsgB:          base64.StdEncoding.EncodeToString(msgB),
		ServerConfirm: serverConfirm,
	})
}

// handlePairConfirm verifies the client's SPAKE2 proof and, on success,
// authorizes its key live (no restart) and persists it.
func (s *Server) handlePairConfirm(w http.ResponseWriter, r *http.Request, req PairRequest) {
	clientPub, err := s.pairing.confirm(s.identity.Public(), req.Session, req.ClientConfirm)
	if err != nil {
		s.guard.strike(clientIP(r))
		s.logf("pairing confirm rejected: %v", err)
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if s.keys.Authorize(clientPub) {
		s.logf("paired new client %s", auth.Fingerprint(clientPub))
		if s.authzKey != nil {
			if err := s.authzKey(auth.EncodePublicKey(clientPub)); err != nil {
				s.logf("paired but could not persist authorized key: %v", err)
			}
		}
	}
	writeJSON(w, PairResponse{OK: true})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// --- operation handlers ---

func (s *Server) opPing(client *clientState) PingResponse {
	creds := client.currentCreds()
	resp := PingResponse{
		Version:  Version,
		HasToken: creds.Token != "",
		Jobs:     client.jobs.count(),
		Monitors: client.monitors.Count(),
	}
	// Best-effort: report who we're acting as, but never fail ping on a bad token.
	if creds.Token != "" {
		if _, res, err := s.buildEngine(creds); err == nil {
			resp.ActingAs = &DiscordIdentity{Username: res.user.Username, ID: res.user.Id}
			s.linkDiscordID(client, res.user.Id)
		}
	}
	return resp
}

func (s *Server) opCredentials(client *clientState, body json.RawMessage) (any, error) {
	var creds Credentials
	if err := json.Unmarshal(body, &creds); err != nil {
		return nil, fmt.Errorf("bad credentials payload")
	}
	if creds.Token == "" {
		return nil, fmt.Errorf("token is required")
	}
	_, res, err := s.buildEngine(creds)
	if err != nil {
		return nil, err
	}
	client.updateCreds(creds)
	s.linkDiscordID(client, res.user.Id)
	return CredentialsResponse{ActingAs: DiscordIdentity{Username: res.user.Username, ID: res.user.Id}}, nil
}

func (s *Server) opPreview(client *clientState, body json.RawMessage) (any, error) {
	var req PreviewRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("bad preview payload")
	}
	creds := client.effective(req.Credentials)
	eng, res, err := s.buildEngine(creds)
	if err != nil {
		return nil, err
	}
	s.linkDiscordID(client, res.user.Id)
	job, label, err := res.buildDeleteJob(req.Kind, req.Spec)
	if err != nil {
		return nil, err
	}
	total, err := eng.Preview(context.Background(), job)
	if err != nil {
		return nil, err
	}
	return PreviewResponse{
		ActingAs: DiscordIdentity{Username: res.user.Username, ID: res.user.Id},
		Target:   label,
		Total:    total,
	}, nil
}

// opPreviewStart begins an async count and returns a handle to poll. Resolving
// the target (and validating the token) happens synchronously inside start, so
// bad targets still fail fast here; only the counting runs in the background.
func (s *Server) opPreviewStart(client *clientState, body json.RawMessage) (any, error) {
	var req PreviewRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("bad preview payload")
	}
	creds := client.effective(req.Credentials)
	resp, err := client.previews.start(creds, req.Kind, req.Spec)
	if err != nil {
		return nil, err
	}
	s.linkDiscordID(client, resp.ActingAs.ID)
	return resp, nil
}

// opPreviewStatus reports one async preview's progress.
func (s *Server) opPreviewStatus(client *clientState, body json.RawMessage) (any, error) {
	var req IDRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("bad request")
	}
	st, ok := client.previews.status(req.ID)
	if !ok {
		return nil, fmt.Errorf("preview %q not found", req.ID)
	}
	return st, nil
}

func (s *Server) opSubmitJob(client *clientState, body json.RawMessage) (any, error) {
	var req JobRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("bad job payload")
	}
	creds := client.effective(req.Credentials)
	return client.jobs.submit(creds, req.Kind, req.Spec)
}

func (s *Server) opGetJob(client *clientState, body json.RawMessage) (any, error) {
	var req IDRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("bad request")
	}
	st, ok := client.jobs.get(req.ID)
	if !ok {
		return nil, fmt.Errorf("job %q not found", req.ID)
	}
	return st, nil
}

func (s *Server) opCancelJob(client *clientState, body json.RawMessage) (any, error) {
	var req IDRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("bad request")
	}
	st, ok := client.jobs.cancel(req.ID)
	if !ok {
		return nil, fmt.Errorf("job %q not found", req.ID)
	}
	return st, nil
}

func (s *Server) opRemoveJob(client *clientState, body json.RawMessage) (any, error) {
	var req IDRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("bad request")
	}
	if !client.jobs.remove(req.ID) {
		return nil, fmt.Errorf("job %q not found", req.ID)
	}
	return map[string]string{"removed": req.ID}, nil
}

// opRetryJob resubmits a failed or canceled job's original spec as a new job.
func (s *Server) opRetryJob(client *clientState, body json.RawMessage) (any, error) {
	var req IDRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("bad request")
	}
	return client.jobs.retry(client.effective(nil), req.ID)
}

func (s *Server) opExportJob(client *clientState, body json.RawMessage) (any, error) {
	var req IDRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("bad request")
	}
	return client.jobs.export(req.ID)
}

// opExportChunk streams one slice of a job's export log, letting the client pull
// a large log down in sequence without either side buffering the whole thing.
func (s *Server) opExportChunk(client *clientState, body json.RawMessage) (any, error) {
	var req ExportChunkRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("bad request")
	}
	return client.jobs.exportChunk(req.ID, req.Offset)
}

// opLogStats aggregates this account's own deletion logs into the same
// insights the desktop shows for local runs. Logs are segregated per account,
// so the summary never reflects another account's deletions.
func (s *Server) opLogStats(client *clientState) (any, error) {
	st, err := logstats.Parse(client.logDir, nil)
	if err != nil {
		return nil, err
	}
	st.Source = "server"
	return st, nil
}

func (s *Server) opSetMonitor(client *clientState, body json.RawMessage) (any, error) {
	var req MonitorRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("bad monitor payload")
	}
	client.effective(req.Credentials) // refresh this client's token if supplied
	return client.monitors.Upsert(req.Policy)
}

func (s *Server) opDelMonitor(client *clientState, body json.RawMessage) (any, error) {
	var req IDRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("bad request")
	}
	if !client.monitors.Delete(req.ID) {
		return nil, fmt.Errorf("monitor %q not found", req.ID)
	}
	return map[string]string{"deleted": req.ID}, nil
}

func (s *Server) opPreviewMonitor(client *clientState, body json.RawMessage) (any, error) {
	var req MonitorRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("bad monitor payload")
	}
	client.effective(req.Credentials)
	total, err := client.monitors.Preview(req.Policy)
	if err != nil {
		return nil, err
	}
	return PreviewResponse{Target: req.Policy.Name, Total: total}, nil
}
