package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/inherentescapade/viaduct/auth"
	"github.com/inherentescapade/viaduct/logstats"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client dispatches operations to a viaduct server. It seals every request to
// the server's public key and verifies that replies are sealed by that same
// server, so a man in the middle can neither read nor forge traffic.
type Client struct {
	addr      string
	httpc     *http.Client
	identity  *auth.Identity
	serverPub [auth.KeySize]byte
}

// NewClient builds a client for a server at addr (host:port) whose public key is
// serverPub. identity is this client's own X25519 key.
func NewClient(addr string, identity *auth.Identity, serverPub [auth.KeySize]byte) *Client {
	return &Client{
		addr:      addr,
		httpc:     &http.Client{Timeout: 60 * time.Second},
		identity:  identity,
		serverPub: serverPub,
	}
}

// call performs one sealed round trip. reqBody is JSON-marshalled into the
// request; on success the response body is unmarshalled into out (which may be
// nil to ignore it).
func (c *Client) call(op Op, reqBody, out any) error {
	var rawBody json.RawMessage
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		rawBody = b
	}

	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return err
	}
	req := rpcRequest{
		Op:        op,
		Timestamp: time.Now().Unix(),
		Nonce:     hex.EncodeToString(nonceBytes),
		Body:      rawBody,
	}
	plaintext, err := json.Marshal(req)
	if err != nil {
		return err
	}

	env, err := auth.Seal(plaintext, c.identity, c.serverPub)
	if err != nil {
		return err
	}
	wire, _ := env.MarshalBinary()

	url := fmt.Sprintf("http://%s%s", c.addr, rpcPath)
	httpResp, err := c.httpc.Post(url, "application/octet-stream", bytes.NewReader(wire))
	if err != nil {
		return fmt.Errorf("could not reach server at %s: %w", c.addr, err)
	}
	defer httpResp.Body.Close()
	respBytes, err := io.ReadAll(io.LimitReader(httpResp.Body, maxResponseBytes))
	if err != nil {
		return err
	}
	if httpResp.StatusCode != http.StatusOK {
		// Auth-layer failures come back as plaintext HTTP errors.
		return fmt.Errorf("server rejected the request (%s): %s", httpResp.Status, bytes.TrimSpace(respBytes))
	}

	var respEnv auth.Envelope
	if err := respEnv.UnmarshalBinary(respBytes); err != nil {
		return fmt.Errorf("server sent a malformed reply")
	}
	plain, sender, err := auth.Open(&respEnv, c.identity)
	if err != nil {
		return fmt.Errorf("could not decrypt the server's reply (wrong server key?)")
	}
	if sender != c.serverPub {
		return fmt.Errorf("reply was not sealed by the expected server key — possible impostor")
	}

	var resp rpcResponse
	if err := json.Unmarshal(plain, &resp); err != nil {
		return fmt.Errorf("malformed response body")
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	if out != nil && len(resp.Body) > 0 {
		return json.Unmarshal(resp.Body, out)
	}
	return nil
}

// PairBegin asks the server to start pairing. The server responds by showing a
// short code on its own terminal (it is never sent back here), which the user
// then reads off and passes to PairComplete. Calling it does not authorize
// anything on its own.
func PairBegin(addr string, identity *auth.Identity) error {
	httpc := &http.Client{Timeout: 30 * time.Second}
	url := fmt.Sprintf("http://%s%s", addr, pairPath)
	var resp PairResponse
	return pairPost(httpc, url, PairRequest{
		Phase:     "request",
		ClientPub: auth.EncodePublicKey(identity.Public()),
	}, &resp)
}

// PairComplete finishes pairing with the code the server showed, returning the
// server's now-authenticated public key. Client and server run SPAKE2 keyed by
// the code, deriving a shared key only if both used the same code. Each then
// proves knowledge of that key with a MAC that binds both static public keys,
// so the client learns the server's real key with no copying, and a man in the
// middle who swapped a key satisfies neither proof. Because the key comes from a
// PAKE, captured traffic gives an attacker no offline guesses at the code. On
// success the server has authorized identity's key (live, no restart); callers
// should persist the returned key as the server's identity.
func PairComplete(addr string, identity *auth.Identity, code string) ([auth.KeySize]byte, error) {
	var zero [auth.KeySize]byte
	httpc := &http.Client{Timeout: 30 * time.Second}
	url := fmt.Sprintf("http://%s%s", addr, pairPath)
	clientPub := identity.Public()

	// 1. Run the client half of SPAKE2 and send our element.
	sp, err := auth.NewSpake2(auth.SpakeClient, []byte(code))
	if err != nil {
		return zero, fmt.Errorf("could not start pairing")
	}
	var start PairResponse
	if err := pairPost(httpc, url, PairRequest{
		Phase:     "start",
		ClientPub: auth.EncodePublicKey(clientPub),
		MsgA:      base64.StdEncoding.EncodeToString(sp.Message()),
	}, &start); err != nil {
		return zero, err
	}

	serverPub, err := auth.ParsePublicKey(start.ServerPub)
	if err != nil {
		return zero, fmt.Errorf("server advertised an invalid key: %w", err)
	}
	msgB, err := base64.StdEncoding.DecodeString(start.MsgB)
	if err != nil {
		return zero, fmt.Errorf("server sent a malformed pairing message")
	}

	// 2. Derive the shared key and verify the server proved knowledge of the code.
	// This is what authenticates the server key we just received.
	ke, err := sp.Finish(msgB)
	if err != nil {
		return zero, fmt.Errorf("pairing failed: %v", err)
	}
	expect := pairConfirm(ke, pairServerConfirmLabel, serverPub, clientPub)
	if !hmac.Equal([]byte(expect), []byte(start.ServerConfirm)) {
		return zero, fmt.Errorf("the server could not prove it knows the code — wrong code or an impostor; pairing aborted")
	}

	// 3. Prove we know the code too; the server authorizes us on success.
	var conf PairResponse
	if err := pairPost(httpc, url, PairRequest{
		Phase:         "confirm",
		Session:       start.Session,
		ClientConfirm: pairConfirm(ke, pairClientConfirmLabel, serverPub, clientPub),
	}, &conf); err != nil {
		return zero, err
	}
	return serverPub, nil
}

// pairPost sends one plaintext pairing message and decodes the JSON reply,
// surfacing a non-2xx body as the error.
func pairPost(httpc *http.Client, url string, req PairRequest, out *PairResponse) error {
	body, _ := json.Marshal(req)
	resp, err := httpc.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("could not reach the server: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", strings.TrimSpace(string(raw)))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("server sent a malformed pairing reply")
	}
	return nil
}

// Ping checks connectivity and returns the server's state.
func (c *Client) Ping() (*PingResponse, error) {
	var resp PingResponse
	if err := c.call(OpPing, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// PushCredentials sends the Discord token to the server and returns the account
// it will act as.
func (c *Client) PushCredentials(creds Credentials) (*CredentialsResponse, error) {
	var resp CredentialsResponse
	if err := c.call(OpCredentials, creds, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Preview asks how many messages a spec would affect.
func (c *Client) Preview(req PreviewRequest) (*PreviewResponse, error) {
	var resp PreviewResponse
	if err := c.call(OpPreview, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// PreviewStart begins an async count on the server and returns a handle to poll
// with PreviewStatus. Unlike Preview, this returns as soon as the target
// resolves; the count runs in the background, so even a slow count is never
// bounded by a single request's timeout.
func (c *Client) PreviewStart(req PreviewRequest) (*PreviewStartResponse, error) {
	var resp PreviewStartResponse
	if err := c.call(OpPreviewStart, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// PreviewStatus polls one async preview started with PreviewStart.
func (c *Client) PreviewStatus(id string) (*PreviewStatusResponse, error) {
	var resp PreviewStatusResponse
	if err := c.call(OpPreviewStatus, IDRequest{ID: id}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// PreviewAwait starts an async count and polls it to completion, calling
// onWait (if non-nil) before each wait so the caller can show progress. It
// returns a PreviewResponse shaped like the synchronous Preview so callers can
// treat the two the same. The count runs server-side regardless of this
// client's connection, so a dropped poll can simply be retried.
func (c *Client) PreviewAwait(req PreviewRequest, poll time.Duration, onWait func()) (*PreviewResponse, error) {
	start, err := c.PreviewStart(req)
	if err != nil {
		return nil, err
	}
	for {
		st, err := c.PreviewStatus(start.ID)
		if err != nil {
			return nil, err
		}
		if st.Done {
			if st.Error != "" {
				return nil, fmt.Errorf("%s", st.Error)
			}
			return &PreviewResponse{
				ActingAs: start.ActingAs,
				Target:   start.Target,
				Total:    st.Total,
			}, nil
		}
		if onWait != nil {
			onWait()
		}
		time.Sleep(poll)
	}
}

// SubmitJob dispatches a one-shot deletion job.
func (c *Client) SubmitJob(req JobRequest) (*JobStatus, error) {
	var resp JobStatus
	if err := c.call(OpSubmitJob, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListJobs returns all jobs known to the server.
func (c *Client) ListJobs() ([]JobStatus, error) {
	var resp JobListResponse
	if err := c.call(OpListJobs, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Jobs, nil
}

// GetJob returns one job's status.
func (c *Client) GetJob(id string) (*JobStatus, error) {
	var resp JobStatus
	if err := c.call(OpGetJob, IDRequest{ID: id}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CancelJob requests cancellation of a running job.
func (c *Client) CancelJob(id string) (*JobStatus, error) {
	var resp JobStatus
	if err := c.call(OpCancelJob, IDRequest{ID: id}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RemoveJob cancels (if running) and forgets a job so it leaves the list.
func (c *Client) RemoveJob(id string) error {
	return c.call(OpRemoveJob, IDRequest{ID: id}, nil)
}

// RetryJob resubmits a failed or canceled job's original spec as a new job.
func (c *Client) RetryJob(id string) (*JobStatus, error) {
	var resp JobStatus
	if err := c.call(OpRetryJob, IDRequest{ID: id}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ExportJob downloads a finished job's NDJSON deletion log.
func (c *Client) ExportJob(id string) (*ExportResponse, error) {
	var resp ExportResponse
	if err := c.call(OpExportJob, IDRequest{ID: id}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ExportChunk fetches one slice of a finished job's export log starting at
// offset. Callers stream a large log by requesting chunks in sequence until the
// response's EOF is set, writing each chunk to disk as it arrives.
func (c *Client) ExportChunk(id string, offset int64) (*ExportChunkResponse, error) {
	var resp ExportChunkResponse
	if err := c.call(OpExportChunk, ExportChunkRequest{ID: id, Offset: offset}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// LogStats aggregates the server's deletion logs into an insights summary.
func (c *Client) LogStats() (*logstats.Stats, error) {
	var resp logstats.Stats
	if err := c.call(OpLogStats, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListRuns returns every deletion run on the server, newest first.
func (c *Client) ListRuns() ([]logstats.RunStat, error) {
	var resp RunListResponse
	if err := c.call(OpListRuns, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Runs, nil
}

// SearchLogs runs a full-text query over the server's deletion logs.
func (c *Client) SearchLogs(q logstats.SearchQuery) (*logstats.SearchResult, error) {
	var resp logstats.SearchResult
	if err := c.call(OpSearchLogs, q, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ExportSearch downloads every server-side log record matching q as a single
// response, capped like a job export to stay within the RPC envelope.
func (c *Client) ExportSearch(q logstats.SearchQuery) (*ExportResponse, error) {
	var resp ExportResponse
	if err := c.call(OpExportSearch, q, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// PurgeLogs removes every server-side log record matching q, returning the
// number of records removed.
func (c *Client) PurgeLogs(q logstats.SearchQuery) (int, error) {
	var resp PurgeLogsResponse
	if err := c.call(OpPurgeLogs, q, &resp); err != nil {
		return 0, err
	}
	return resp.Removed, nil
}

// ExportRunChunk fetches one slice of a specific run's deletion log starting
// at offset. Callers stream a large run by requesting chunks in sequence
// until the response's EOF is set, writing each chunk to disk as it arrives.
func (c *Client) ExportRunChunk(file string, offset int64) (*RunChunkResponse, error) {
	var resp RunChunkResponse
	if err := c.call(OpExportRunChunk, RunChunkRequest{File: file, Offset: offset}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteRun permanently removes one run's deletion log from the server.
func (c *Client) DeleteRun(file string) error {
	return c.call(OpDeleteRun, RunChunkRequest{File: file}, nil)
}

// SetMonitor creates or updates a monitor policy.
func (c *Client) SetMonitor(req MonitorRequest) (*MonitorPolicy, error) {
	var resp MonitorPolicy
	if err := c.call(OpSetMonitor, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListMonitors returns the server's monitor policies.
func (c *Client) ListMonitors() ([]MonitorPolicy, error) {
	var resp MonitorListResponse
	if err := c.call(OpListMonitor, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Monitors, nil
}

// DeleteMonitor removes a monitor policy.
func (c *Client) DeleteMonitor(id string) error {
	return c.call(OpDelMonitor, IDRequest{ID: id}, nil)
}

// PreviewMonitor reports how many messages a monitor policy would delete now.
func (c *Client) PreviewMonitor(req MonitorRequest) (*PreviewResponse, error) {
	var resp PreviewResponse
	if err := c.call(OpPreviewMon, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
