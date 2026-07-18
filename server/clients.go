package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/inherentescapade/viaduct/auth"
	"github.com/inherentescapade/viaduct/discord"
	"github.com/inherentescapade/viaduct/engine"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
)

// errNoToken is returned when an operation needs Discord credentials the client
// hasn't pushed yet.
var errNoToken = fmt.Errorf("the server has no Discord token yet — run `viaduct remote connect` from your client first")

// AccountKey identifies the Discord account a token belongs to, for routing
// server-side state. Two client keys (e.g. two machines) that present the
// identical token are guaranteed to be the same person, so both resolve to
// this one account key and therefore share one clientState — the same jobs,
// monitors, rate limiter, and log directory — rather than each getting its
// own blank world. The token itself isn't used as the map key directly so it
// doesn't linger in a place built for iteration/debugging.
func AccountKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "tok:" + hex.EncodeToString(sum[:])
}

// clientState is one Discord account's private world on the server: its
// token, its jobs, and its monitors. It's reached by every client key that
// has ever pushed this account's token (see AccountKey) — a client key that
// has never pushed a token gets its own private bucket instead, keyed by that
// key, segregated like any other account.
type clientState struct {
	key string // this state's account key — a token hash, or a client pubkey for a not-yet-authenticated bucket

	mu      sync.Mutex
	token   string
	botMode bool

	jobs     *jobManager
	previews *previewManager
	monitors *MonitorManager
	save     func(Credentials) error // persists this client's token
	logDir   string                  // this client's private deletion-log directory

	// One Discord client shared by all of this account's concurrent jobs and
	// monitors, so they coordinate on a single rate limiter rather than each
	// 429-ing the others. Rebuilt when the token or mode changes.
	apiMu     sync.Mutex
	apiClient *discord.Client
	apiToken  string
	apiBot    bool
}

// sharedAPIClient returns the Discord client shared across this account's
// concurrent deletion work, building (or rebuilding) it when the credentials
// change. The returned client is safe for concurrent use.
func (cs *clientState) sharedAPIClient(c Credentials) *discord.Client {
	cs.apiMu.Lock()
	defer cs.apiMu.Unlock()
	if cs.apiClient == nil || cs.apiToken != c.Token || cs.apiBot != c.BotMode {
		cs.apiClient = discord.NewClient(c.Token, c.BotMode, http.DefaultClient)
		cs.apiToken = c.Token
		cs.apiBot = c.BotMode
	}
	return cs.apiClient
}

// currentCreds returns the client's stored Discord credentials.
func (cs *clientState) currentCreds() Credentials {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return Credentials{Token: cs.token, BotMode: cs.botMode}
}

// updateCreds stores and persists the client's pushed credentials.
func (cs *clientState) updateCreds(c Credentials) {
	cs.mu.Lock()
	cs.token = c.Token
	cs.botMode = c.BotMode
	save := cs.save
	cs.mu.Unlock()
	if save != nil {
		_ = save(c)
	}
}

// effective returns the credentials to act with: the request's, if it carried a
// token (which also refreshes the stored one so this client's monitors keep
// working), otherwise the stored ones.
func (cs *clientState) effective(reqCreds *Credentials) Credentials {
	if reqCreds != nil && reqCreds.Token != "" {
		cs.updateCreds(*reqCreds)
	}
	return cs.currentCreds()
}

// newClientState builds an account's isolated job + monitor managers, each
// wired to write deletion logs into the account's own directory. initial is
// the persisted token to resume with (zero value for a brand-new account).
func (s *Server) newClientState(key string, initial Credentials) *clientState {
	logDir := ClientLogDir(s.logBase, key)
	cs := &clientState{
		key:     key,
		token:   initial.Token,
		botMode: initial.BotMode,
		logDir:  logDir,
	}
	// Every job, preview, and monitor for this account builds a fresh Engine over
	// the ONE shared Discord client, so concurrent runs coordinate on a single
	// rate limiter and log into this account's own directory.
	buildEngine := func(c Credentials) (*engine.Engine, *resolver, error) {
		if c.Token == "" {
			return nil, nil, errNoToken
		}
		eng := engine.NewWithClient(cs.sharedAPIClient(c))
		eng.SetLogDir(logDir)
		res, err := newResolver(eng)
		if err != nil {
			return nil, nil, err
		}
		return eng, res, nil
	}
	cs.jobs = newJobManager(buildEngine, s.logf)
	cs.previews = newPreviewManager(buildEngine)
	cs.monitors = NewMonitorManager(ClientMonitorPath(s.monitorsBase, key), func(c Credentials) (*engine.Engine, error) {
		if c.Token == "" {
			return nil, errNoToken
		}
		eng := engine.NewWithClient(cs.sharedAPIClient(c))
		eng.SetLogDir(logDir)
		return eng, nil
	}, cs.currentCreds, s.logf)
	cs.save = func(c Credentials) error { return s.persistAccount(key, c) }
	return cs
}

// clientFor returns the calling client's account state. When the request
// carries a token, it routes (and keeps routing, even for later requests that
// omit credentials) to that token's shared account — so a second machine
// presenting the same token lands on the very same jobs, monitors, and log
// directory as the first, because the same token is guaranteed to be the same
// person. A client key that has never presented a token gets its own private
// bucket instead, segregated like any other account.
func (s *Server) clientFor(sender [auth.KeySize]byte, creds *Credentials) *clientState {
	senderKey := auth.EncodePublicKey(sender)
	var acct string
	if creds != nil && creds.Token != "" {
		acct = AccountKey(creds.Token)
	}

	s.clientsMu.Lock()
	if acct == "" {
		if existing, ok := s.keyToAccount[senderKey]; ok {
			acct = existing
		} else {
			acct = senderKey
		}
	}
	cs, ok := s.clients[acct]
	if !ok {
		cs = s.newClientState(acct, Credentials{})
		s.clients[acct] = cs
		if s.schedCtx != nil {
			go cs.monitors.Run(s.schedCtx)
		}
	}
	changed := s.keyToAccount[senderKey] != acct
	if changed {
		s.keyToAccount[senderKey] = acct
	}
	s.clientsMu.Unlock()

	if changed {
		s.persistClientKey(senderKey, acct)
	}
	return cs
}

// persistAccount saves one account's credentials via the configured hook.
func (s *Server) persistAccount(acctKey string, c Credentials) error {
	if s.saveAccount == nil {
		return nil
	}
	if err := s.saveAccount(acctKey, c); err != nil {
		s.logf("could not persist account credentials: %v", err)
		return err
	}
	return nil
}

// persistClientKey saves a client key's routing to its account via the
// configured hook, so a restart doesn't strand that key on a blank bucket
// until it happens to push credentials again.
func (s *Server) persistClientKey(clientKey, acctKey string) {
	if s.linkClientKey == nil {
		return
	}
	if err := s.linkClientKey(clientKey, acctKey); err != nil {
		s.logf("could not persist client routing: %v", err)
	}
}

// ClientMonitorPath derives a per-account monitor store path from a base path
// by inserting a short hash of the account key before the extension, so each
// account's monitors persist to a separate file. An empty base means in-memory.
func ClientMonitorPath(base, key string) string {
	if base == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext) + "-" + hex.EncodeToString(sum[:6]) + ext
}

// clientLogDir is the per-account deletion-log directory under base, named by
// a short hash of the account key so one account's logs are never read for
// another's log-stats or exports.
func ClientLogDir(base, key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(base, "clients", hex.EncodeToString(sum[:6]))
}
