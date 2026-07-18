package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/inherentescapade/viaduct/auth"
	"github.com/inherentescapade/viaduct/cfg"
	"github.com/inherentescapade/viaduct/logstats"
	"github.com/inherentescapade/viaduct/server"
	"github.com/inherentescapade/viaduct/token"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// This file binds viaduct's self-hosting layer (server/ + auth/) to the desktop
// UI. The flow: generate a client identity, point it at a server you run
// elsewhere, push your Discord token (encrypted end-to-end), then dispatch
// "tasks" (one-off deletions) and monitor policies. Jobs/monitors are polled by
// the frontend: they live on a remote machine, so they don't flow through the
// local engine's event bus.

// remoteSlot is the fixed name under which the desktop stores its single server.
const remoteSlot = "default"

// remoteClient builds a server.Client from the saved remote and this machine's
// client identity. It is constructed fresh per call (cheap, no shared state) so
// concurrent polling from the UI is safe.
func (a *App) remoteClient() (*server.Client, *cfg.RemoteServer, error) {
	a.mu.Lock()
	r := a.config.FindRemote(remoteSlot)
	a.mu.Unlock()
	if r == nil {
		return nil, nil, fmt.Errorf("no server configured yet; set one up first")
	}
	id, err := auth.LoadIdentity(cfg.IdentityPath())
	if err != nil {
		return nil, nil, fmt.Errorf("no client identity yet; finish the setup steps first")
	}
	pub, err := auth.ParsePublicKey(r.PublicKey)
	if err != nil {
		return nil, nil, fmt.Errorf("the saved server key is invalid: %w", err)
	}
	// Copy the remote so callers can't mutate config state through the pointer.
	rc := *r
	return server.NewClient(r.Address, id, pub), &rc, nil
}

// EnsureIdentity loads the client identity, creating and saving one (0600) on
// first use. Returns the shareable public key for the setup wizard.
func (a *App) EnsureIdentity() (*IdentityDTO, error) {
	id, created, err := auth.LoadOrCreateIdentity(cfg.IdentityPath())
	if err != nil {
		return nil, fmt.Errorf("could not create your client key: %w", err)
	}
	return &IdentityDTO{
		PublicKey:   id.PublicKeyString(),
		Fingerprint: id.Fingerprint(),
		Created:     created,
	}, nil
}

// GetRemote returns the saved server, or null when none is configured.
func (a *App) GetRemote() (*RemoteDTO, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.config.FindRemote(remoteSlot)
	if r == nil {
		return nil, nil
	}
	return &RemoteDTO{Name: r.Name, Address: r.Address, HasKey: r.PublicKey != ""}, nil
}

// normalizeRemoteAddr trims an address and appends the default port if absent.
func normalizeRemoteAddr(address string) (string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", fmt.Errorf("enter the server address (host:port)")
	}
	if !strings.Contains(address, ":") {
		address = fmt.Sprintf("%s:%d", address, cfg.DefaultPort)
	}
	return address, nil
}

// RemotePairRequest asks the server to start pairing. The server shows a code on
// its own terminal; the user reads it there and passes it to RemotePairComplete.
// It creates the client identity on first use.
func (a *App) RemotePairRequest(address string) error {
	address, err := normalizeRemoteAddr(address)
	if err != nil {
		return err
	}
	id, _, err := auth.LoadOrCreateIdentity(cfg.IdentityPath())
	if err != nil {
		return fmt.Errorf("could not create your client key: %w", err)
	}
	return server.PairBegin(address, id)
}

// RemotePairComplete finishes pairing with the code the server showed: it runs
// the SPAKE2 exchange (learning and authenticating the server's key with no
// copying), saves the server, and pushes the local Discord token. The returned
// account is nil when no token was available — the caller can send one later
// with RemoteConnect.
func (a *App) RemotePairComplete(address, code string) (*ActingAsDTO, error) {
	address, err := normalizeRemoteAddr(address)
	if err != nil {
		return nil, err
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, fmt.Errorf("enter the code shown on the server")
	}

	id, _, err := auth.LoadOrCreateIdentity(cfg.IdentityPath())
	if err != nil {
		return nil, fmt.Errorf("could not create your client key: %w", err)
	}

	serverPub, err := server.PairComplete(address, id, code)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	a.config.UpsertRemote(cfg.RemoteServer{Name: remoteSlot, Address: address, PublicKey: auth.EncodePublicKey(serverPub)})
	err = a.config.Save()
	a.mu.Unlock()
	if err != nil {
		return nil, err
	}

	// Push the Discord token over the freshly trusted channel, if we have one.
	tok, bot := a.tokenForPush()
	if tok == "" {
		return nil, nil
	}
	resp, err := server.NewClient(address, id, serverPub).PushCredentials(server.Credentials{Token: tok, BotMode: bot})
	if err != nil {
		return nil, nil // paired fine; the token just couldn't be validated yet
	}
	return &ActingAsDTO{Username: resp.ActingAs.Username, ID: resp.ActingAs.ID}, nil
}

// ForgetRemote removes the saved server.
func (a *App) ForgetRemote() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	kept := a.config.Remotes[:0]
	for _, r := range a.config.Remotes {
		if r.Name != remoteSlot {
			kept = append(kept, r)
		}
	}
	a.config.Remotes = kept
	return a.config.Save()
}

// RemotePing checks the server is reachable and reports its state.
func (a *App) RemotePing() (*PingDTO, error) {
	client, _, err := a.remoteClient()
	if err != nil {
		return nil, err
	}
	resp, err := client.Ping()
	if err != nil {
		return nil, err
	}
	d := toPingDTO(resp)
	return &d, nil
}

// RemoteConnect pushes the local Discord token to the server and returns the
// account it will act as. The token is the active session token, or the first
// auto-detected one.
func (a *App) RemoteConnect() (*ActingAsDTO, error) {
	client, _, err := a.remoteClient()
	if err != nil {
		return nil, err
	}
	tok, bot := a.tokenForPush()
	if tok == "" {
		return nil, fmt.Errorf("no Discord token to send; log in on the Live tab first, or it couldn't be auto-detected")
	}
	resp, err := client.PushCredentials(server.Credentials{Token: tok, BotMode: bot})
	if err != nil {
		return nil, err
	}
	return &ActingAsDTO{Username: resp.ActingAs.Username, ID: resp.ActingAs.ID}, nil
}

// tokenForPush returns the token to push to the server: the active session
// token, else the first locally-detected one.
func (a *App) tokenForPush() (string, bool) {
	a.mu.Lock()
	tok := a.token
	bot := a.botMode
	a.mu.Unlock()
	if tok != "" {
		return tok, bot
	}
	if toks, err := token.GetTokens(); err == nil && len(toks) > 0 {
		return toks[0], bot
	}
	return "", bot
}

// RemotePreview reports how many messages a task would delete, without deleting.
func (a *App) RemotePreview(req RemoteJobRequest) (*PreviewDTO, error) {
	client, _, err := a.remoteClient()
	if err != nil {
		return nil, err
	}
	kind, err := req.kind()
	if err != nil {
		return nil, err
	}
	resp, err := client.Preview(server.PreviewRequest{Kind: kind, Spec: req.toSpec()})
	if err != nil {
		return nil, err
	}
	d := toPreviewDTO(resp)
	return &d, nil
}

// RemoteSubmit dispatches a one-off deletion task to the server.
func (a *App) RemoteSubmit(req RemoteJobRequest) (*JobDTO, error) {
	client, _, err := a.remoteClient()
	if err != nil {
		return nil, err
	}
	kind, err := req.kind()
	if err != nil {
		return nil, err
	}
	resp, err := client.SubmitJob(server.JobRequest{Kind: kind, Spec: req.toSpec()})
	if err != nil {
		return nil, err
	}
	d := toJobDTO(*resp)
	return &d, nil
}

// RemoteJobs lists all tasks on the server.
func (a *App) RemoteJobs() ([]JobDTO, error) {
	client, _, err := a.remoteClient()
	if err != nil {
		return nil, err
	}
	jobs, err := client.ListJobs()
	if err != nil {
		return nil, err
	}
	out := make([]JobDTO, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, toJobDTO(j))
	}
	return out, nil
}

// RemoteJob returns one task's status.
func (a *App) RemoteJob(id string) (*JobDTO, error) {
	client, _, err := a.remoteClient()
	if err != nil {
		return nil, err
	}
	j, err := client.GetJob(id)
	if err != nil {
		return nil, err
	}
	d := toJobDTO(*j)
	return &d, nil
}

// RemoteCancel requests cancellation of a running task.
func (a *App) RemoteCancel(id string) (*JobDTO, error) {
	client, _, err := a.remoteClient()
	if err != nil {
		return nil, err
	}
	j, err := client.CancelJob(id)
	if err != nil {
		return nil, err
	}
	d := toJobDTO(*j)
	return &d, nil
}

// RemoteRemoveJob removes a task from the server's list.
func (a *App) RemoteRemoveJob(id string) error {
	client, _, err := a.remoteClient()
	if err != nil {
		return err
	}
	return client.RemoveJob(id)
}

// RemoteRetryJob resubmits a failed or canceled task's original target as a
// new task, so a transient failure doesn't force the user to re-enter it.
func (a *App) RemoteRetryJob(id string) (*JobDTO, error) {
	client, _, err := a.remoteClient()
	if err != nil {
		return nil, err
	}
	j, err := client.RetryJob(id)
	if err != nil {
		return nil, err
	}
	d := toJobDTO(*j)
	return &d, nil
}

// RemoteExportJob downloads a finished server task's deletion log and saves it
// locally via a native file dialog. The log is streamed in chunks and written
// straight to disk, so even a multi-gigabyte log never lives wholly in memory;
// byte progress is emitted on EvExportProgress for a download bar. Returns the
// saved path, or "" if the user cancels the dialog.
func (a *App) RemoteExportJob(id string) (string, error) {
	client, _, err := a.remoteClient()
	if err != nil {
		return "", err
	}
	// Pull the first slice up front to learn the filename and total size before
	// prompting for a destination.
	chunk, err := client.ExportChunk(id, 0)
	if err != nil {
		return "", err
	}
	dest, err := wruntime.SaveFileDialog(a.appCtx, wruntime.SaveDialogOptions{
		Title:           "Save deletion export",
		DefaultFilename: chunk.Filename,
	})
	if err != nil {
		return "", err
	}
	if dest == "" {
		return "", nil // user cancelled
	}

	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return "", fmt.Errorf("could not save the export: %w", err)
	}
	defer f.Close()

	var received int64
	for {
		if _, err := f.Write(chunk.Content); err != nil {
			return "", fmt.Errorf("could not save the export: %w", err)
		}
		received += int64(len(chunk.Content))
		wruntime.EventsEmit(a.appCtx, EvExportProgress, ExportProgressDTO{
			JobID: id, Received: received, Total: chunk.Total,
		})
		if chunk.EOF {
			break
		}
		chunk, err = client.ExportChunk(id, received)
		if err != nil {
			return "", err
		}
	}
	// Surface any deferred flush error from closing the file.
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("could not save the export: %w", err)
	}
	return dest, nil
}

// RemoteLogStats aggregates the server's deletion logs into an insights summary.
func (a *App) RemoteLogStats() (logstats.Stats, error) {
	client, _, err := a.remoteClient()
	if err != nil {
		return logstats.Stats{}, err
	}
	st, err := client.LogStats()
	if err != nil {
		return logstats.Stats{}, err
	}
	st.Source = "server"
	// The server only logs channel IDs, but this machine holds the same Discord
	// session, so it can name any of those targets just as well as local ones.
	a.enrichTargets(st)
	return *st, nil
}

// RemoteMonitors lists the server's monitor policies.
func (a *App) RemoteMonitors() ([]MonitorDTO, error) {
	client, _, err := a.remoteClient()
	if err != nil {
		return nil, err
	}
	mons, err := client.ListMonitors()
	if err != nil {
		return nil, err
	}
	out := make([]MonitorDTO, 0, len(mons))
	for _, m := range mons {
		out = append(out, toMonitorDTO(m))
	}
	return out, nil
}

// RemoteSetMonitor creates or updates a monitor policy.
func (a *App) RemoteSetMonitor(req MonitorReq) (*MonitorDTO, error) {
	client, _, err := a.remoteClient()
	if err != nil {
		return nil, err
	}
	m, err := client.SetMonitor(server.MonitorRequest{Policy: req.toPolicy()})
	if err != nil {
		return nil, err
	}
	d := toMonitorDTO(*m)
	return &d, nil
}

// RemotePreviewMonitor reports how many messages a monitor policy would delete
// right now: the guidance shown before enabling it.
func (a *App) RemotePreviewMonitor(req MonitorReq) (*PreviewDTO, error) {
	client, _, err := a.remoteClient()
	if err != nil {
		return nil, err
	}
	resp, err := client.PreviewMonitor(server.MonitorRequest{Policy: req.toPolicy()})
	if err != nil {
		return nil, err
	}
	d := toPreviewDTO(resp)
	return &d, nil
}

// RemoteDeleteMonitor removes a monitor policy.
func (a *App) RemoteDeleteMonitor(id string) error {
	client, _, err := a.remoteClient()
	if err != nil {
		return err
	}
	return client.DeleteMonitor(id)
}

// RemoteListRuns lists every deletion run on the server, newest first, with
// its busiest channel resolved to a friendly name — the server-side
// counterpart to ListRuns, merged by the frontend with the local list.
func (a *App) RemoteListRuns() ([]logstats.RunStat, error) {
	client, _, err := a.remoteClient()
	if err != nil {
		return nil, err
	}
	runs, err := client.ListRuns()
	if err != nil {
		return nil, err
	}
	a.enrichRuns(runs)
	return runs, nil
}

// RemoteSearchLogs runs a full-text query over the server's deletion logs and
// resolves each hit's channel to a friendly name, paging via the request's
// limit/offset — the server-side counterpart to SearchLogs.
func (a *App) RemoteSearchLogs(req LogSearchRequest) (logstats.SearchResult, error) {
	q, err := req.toQuery()
	if err != nil {
		return logstats.SearchResult{}, err
	}
	client, _, err := a.remoteClient()
	if err != nil {
		return logstats.SearchResult{}, err
	}
	res, err := client.SearchLogs(q)
	if err != nil {
		return logstats.SearchResult{}, err
	}
	a.enrichHits(res.Hits)
	return *res, nil
}

// RemoteExportSearch downloads every server-side log record matching req to a
// file the user picks, as NDJSON. Content is capped to stay within the RPC
// envelope (see maxExportBytes server-side); a very large matching set comes
// back truncated. Returns the saved path, or "" if the user cancels the
// dialog.
func (a *App) RemoteExportSearch(req LogSearchRequest) (string, error) {
	q, err := req.toQuery()
	if err != nil {
		return "", err
	}
	client, _, err := a.remoteClient()
	if err != nil {
		return "", err
	}
	resp, err := client.ExportSearch(q)
	if err != nil {
		return "", err
	}
	dest, err := wruntime.SaveFileDialog(a.appCtx, wruntime.SaveDialogOptions{
		Title: "Export server deletion records",
		// Distinct from the local export's default name — the combined export
		// flow prompts for this file right after the local one, and reusing the
		// same default would risk silently overwriting it if the user just hits
		// Save both times.
		DefaultFilename: "viaduct-deletions-server.ndjson",
	})
	if err != nil {
		return "", err
	}
	if dest == "" {
		return "", nil // user cancelled
	}
	if err := os.WriteFile(dest, []byte(resp.Content), 0600); err != nil {
		return "", fmt.Errorf("could not save the export: %w", err)
	}
	return dest, nil
}

// RemotePurgeLogs removes every server-side log record matching req. It
// refuses to run without at least one active filter, same as the local guard.
func (a *App) RemotePurgeLogs(req LogSearchRequest) (int, error) {
	q, err := req.toQuery()
	if err != nil {
		return 0, err
	}
	if q.IsEmpty() {
		return 0, fmt.Errorf("add at least one filter before deleting matches; use “Clear all logs” in Settings to remove everything")
	}
	client, _, err := a.remoteClient()
	if err != nil {
		return 0, err
	}
	return client.PurgeLogs(q)
}

// RemoteDownloadRun downloads one specific server-side run's deletion log to a
// file the user picks, streaming it in chunks so even a multi-gigabyte log
// never lives wholly in memory on either side. Returns the saved path, or ""
// if the user cancels the dialog.
func (a *App) RemoteDownloadRun(file string) (string, error) {
	client, _, err := a.remoteClient()
	if err != nil {
		return "", err
	}
	// Pull the first slice up front to learn the total size before prompting
	// for a destination.
	chunk, err := client.ExportRunChunk(file, 0)
	if err != nil {
		return "", err
	}
	dest, err := wruntime.SaveFileDialog(a.appCtx, wruntime.SaveDialogOptions{
		Title:           "Save deletion log",
		DefaultFilename: chunk.File,
	})
	if err != nil {
		return "", err
	}
	if dest == "" {
		return "", nil // user cancelled
	}

	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return "", fmt.Errorf("could not save the log: %w", err)
	}
	defer f.Close()

	var received int64
	for {
		if _, err := f.Write(chunk.Content); err != nil {
			return "", fmt.Errorf("could not save the log: %w", err)
		}
		received += int64(len(chunk.Content))
		if chunk.EOF {
			break
		}
		chunk, err = client.ExportRunChunk(file, received)
		if err != nil {
			return "", err
		}
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("could not save the log: %w", err)
	}
	return dest, nil
}

// RemoteDeleteRun permanently removes one run's deletion log from the server.
func (a *App) RemoteDeleteRun(file string) error {
	client, _, err := a.remoteClient()
	if err != nil {
		return err
	}
	return client.DeleteRun(file)
}
