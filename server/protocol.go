// Package server implements viaduct's self-hosting architecture: a long-lived
// "job owner" you run on a cheap VPS (`viaduct serve`) and a client that
// dispatches jobs to it (`viaduct remote ...`).
//
// Trust model (see the auth package for the crypto):
//   - Both sides have one small X25519 keypair. Every message is an ECIES
//     envelope: ephemeral X25519 ECDH + XChaCha20-Poly1305. That gives
//     confidentiality, integrity, and sender authentication at once — no TLS,
//     no certificates, no fingerprints to manage.
//   - The server only acts on envelopes from a public key in its authorized
//     list, so exposing the port is not exposing control. A timestamp+nonce in
//     every request blocks replays.
//   - The Discord token is detected on the client and pushed to the server
//     inside the encrypted envelope, then persisted (0600) so monitor jobs keep
//     running while the client is offline.
package server

import (
	"encoding/json"
	"fmt"
	"github.com/inherentescapade/viaduct/logstats"
	"strings"
	"time"
)

// rpcPath is the single HTTP endpoint that carries every encrypted envelope.
// The real "method" is the Op field inside the decrypted request.
const rpcPath = "/rpc"

// pairPath is the plaintext endpoint used once, before a sealed channel exists,
// to bootstrap trust: GET returns the server's public key; POST confirms a
// pairing code (see pairing.go).
const pairPath = "/pair"

// Op identifies which server operation an RPC request invokes.
type Op string

const (
	OpPing           Op = "ping"
	OpCredentials    Op = "credentials"
	OpPreview        Op = "preview"
	OpPreviewStart   Op = "preview_start"
	OpPreviewStatus  Op = "preview_status"
	OpSubmitJob      Op = "submit_job"
	OpListJobs       Op = "list_jobs"
	OpGetJob         Op = "get_job"
	OpCancelJob      Op = "cancel_job"
	OpRemoveJob      Op = "remove_job"
	OpRetryJob       Op = "retry_job"
	OpExportJob      Op = "export_job"
	OpExportChunk    Op = "export_chunk"
	OpSetMonitor     Op = "set_monitor"
	OpListMonitor    Op = "list_monitors"
	OpDelMonitor     Op = "del_monitor"
	OpPreviewMon     Op = "preview_monitor"
	OpLogStats       Op = "log_stats"
	OpListRuns       Op = "list_runs"
	OpSearchLogs     Op = "search_logs"
	OpExportSearch   Op = "export_search"
	OpPurgeLogs      Op = "purge_logs"
	OpExportRunChunk Op = "export_run_chunk"
	OpDeleteRun      Op = "delete_run"
)

// rpcRequest is the plaintext inside a client->server envelope. Timestamp and
// Nonce make each request single-use (replay protection); Op selects the
// operation and Body is its operation-specific JSON payload.
type rpcRequest struct {
	Op        Op              `json:"op"`
	Timestamp int64           `json:"ts"`
	Nonce     string          `json:"nonce"`
	Body      json.RawMessage `json:"body,omitempty"`
}

// rpcResponse is the plaintext inside a server->client envelope.
type rpcResponse struct {
	OK    bool            `json:"ok"`
	Error string          `json:"error,omitempty"`
	Body  json.RawMessage `json:"body,omitempty"`
}

// IDRequest carries a single ID (for get/cancel/delete operations).
type IDRequest struct {
	ID string `json:"id"`
}

// Credentials carries the Discord auth the server should act with. It travels
// client -> server inside the encrypted envelope and is never written to logs.
type Credentials struct {
	Token   string `json:"token"`
	BotMode bool   `json:"bot_mode"`
}

// DiscordIdentity is the Discord account the server is currently acting as.
type DiscordIdentity struct {
	Username string `json:"username"`
	ID       string `json:"id"`
}

// PingResponse reports the server's state to a connecting client.
type PingResponse struct {
	Version  string           `json:"version"`
	HasToken bool             `json:"has_token"`
	ActingAs *DiscordIdentity `json:"acting_as,omitempty"`
	Jobs     int              `json:"jobs"`
	Monitors int              `json:"monitors"`
}

// CredentialsResponse confirms which Discord account the server now holds.
type CredentialsResponse struct {
	ActingAs DiscordIdentity `json:"acting_as"`
}

// JobKind identifies what a one-shot job does.
type JobKind string

const (
	KindDeleteGuild JobKind = "delete_guild" // delete your messages in a server (or @me)
	KindDeleteDM    JobKind = "delete_dm"    // delete your messages in DMs with a user
)

// DeleteSpec describes a one-shot deletion target.
type DeleteSpec struct {
	// Guild is a server name or ID, or "@me" for direct messages. Required for
	// delete_guild; ignored for delete_dm (always @me).
	Guild string `json:"guild,omitempty"`
	// Channels narrows a delete_guild job to specific channels (names or IDs):
	// an allow-list — delete ONLY in these.
	Channels []string `json:"channels,omitempty"`
	// Exclude is the deny-list counterpart to Channels: delete everywhere in
	// scope EXCEPT these channels/DMs ("delete all other direct messages/
	// groups"). Applied after Channels, so the two can be combined.
	Exclude []string `json:"exclude,omitempty"`
	// User identifies the DM partner for delete_dm (recipient ID or username).
	User string `json:"user,omitempty"`
	// Before / After are date expressions ("2024-01-01", "30d", ...).
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
	// MaxID / MinID bound the message snowflake range (advanced).
	MaxID string `json:"max_id,omitempty"`
	MinID string `json:"min_id,omitempty"`
	// Verify runs follow-up passes until the remaining count reaches zero.
	Verify bool `json:"verify,omitempty"`
	// IncludePinned deletes pinned messages too; false (the default) keeps them.
	IncludePinned bool `json:"include_pinned,omitempty"`
}

// PreviewRequest asks how many messages a spec would affect, without deleting.
type PreviewRequest struct {
	Credentials *Credentials `json:"credentials,omitempty"`
	Kind        JobKind      `json:"kind"`
	Spec        DeleteSpec   `json:"spec"`
}

// PreviewResponse is the result of a preview.
type PreviewResponse struct {
	ActingAs DiscordIdentity `json:"acting_as"`
	Target   string          `json:"target"` // human label of what's targeted
	Total    int             `json:"total"`
}

// PreviewStartResponse acknowledges an async preview: the target has resolved
// (so a bad target has already failed by the time this returns), and counting
// continues in the background. The client polls PreviewStatus with ID until the
// count settles — each poll is a short request, so a count that takes minutes
// never trips the client's per-request timeout the way the synchronous preview
// does.
type PreviewStartResponse struct {
	ID       string          `json:"id"`
	ActingAs DiscordIdentity `json:"acting_as"`
	Target   string          `json:"target"`
}

// PreviewStatusResponse is one poll of a running async preview. Done is true
// once the count has settled; Error (non-empty) means it settled by failing.
type PreviewStatusResponse struct {
	ID    string `json:"id"`
	Done  bool   `json:"done"`
	Total int    `json:"total"`
	Error string `json:"error,omitempty"`
}

// JobRequest submits a one-shot deletion job.
type JobRequest struct {
	Credentials *Credentials `json:"credentials,omitempty"`
	Kind        JobKind      `json:"kind"`
	Spec        DeleteSpec   `json:"spec"`
}

// JobState is the lifecycle state of a job.
type JobState string

const (
	StatePending   JobState = "pending"
	StateRunning   JobState = "running"
	StateCanceling JobState = "canceling"
	StateDone      JobState = "done"
	StateFailed    JobState = "failed"
	StateCanceled  JobState = "canceled"
)

// FeedMessage is one deleted message, surfaced live so the client can show what
// a job or monitor is actually removing — the same feed you see when deleting
// locally. Author / AuthorID / AuthorAvatar let the client render the message
// with its avatar and name, matching the local message views.
type FeedMessage struct {
	Content      string    `json:"content"`
	Channel      string    `json:"channel"`
	Timestamp    time.Time `json:"timestamp"`
	Author       string    `json:"author,omitempty"`        // display name (global name or username)
	AuthorID     string    `json:"author_id,omitempty"`     // for building the avatar URL
	AuthorAvatar string    `json:"author_avatar,omitempty"` // avatar hash
}

// JobStatus is the observable state of a submitted job. RatePerSec and EtaMs
// are derived at read time (see withETA) from Created and the current
// counts, rather than stored, so they stay accurate between polls without
// needing historical snapshots.
type JobStatus struct {
	ID          string        `json:"id"`
	Kind        JobKind       `json:"kind"`
	Description string        `json:"description"`
	State       JobState      `json:"state"`
	Total       int           `json:"total"`
	Deleted     int           `json:"deleted"`
	Failed      int           `json:"failed"`
	Skipped     int           `json:"skipped"`
	Ignored     int           `json:"ignored,omitempty"`  // system messages excluded from the target (can't be deleted)
	Residual    int           `json:"residual,omitempty"` // messages left after a verified run
	Error       string        `json:"error,omitempty"`
	Recent      []FeedMessage `json:"recent,omitempty"`     // most-recent deletions (live feed)
	HasExport   bool          `json:"has_export,omitempty"` // a downloadable NDJSON log exists
	Created     time.Time     `json:"created"`
	Updated     time.Time     `json:"updated"`
	RatePerSec  float64       `json:"ratePerSec,omitempty"`
	EtaMs       int64         `json:"etaMs,omitempty"`
}

// ExportResponse carries a job's deletion log (NDJSON) back to the client so it
// can be saved locally — the same record you'd get from a local deletion.
// Content is capped to keep it inside the RPC envelope; Truncated flags when the
// full log was larger than the cap. This is the single-shot path; the desktop
// uses the chunked OpExportChunk path so large logs stream without truncation.
type ExportResponse struct {
	JobID     string `json:"job_id"`
	Filename  string `json:"filename"`
	Content   string `json:"content"`
	Bytes     int    `json:"bytes"`
	Truncated bool   `json:"truncated"`
}

// ExportChunkRequest asks for one slice of a job's export log, starting at
// Offset bytes. The client requests slices in sequence until it sees EOF.
type ExportChunkRequest struct {
	ID     string `json:"id"`
	Offset int64  `json:"offset"`
}

// ExportChunkResponse carries one slice of a job's NDJSON deletion log. The
// client requests slices in order and writes each straight to disk, so a
// multi-gigabyte log streams down without ever living wholly in memory on
// either side. Content holds the raw bytes (base64 in JSON) so a chunk boundary
// landing mid-rune or mid-line can't corrupt the file the way a JSON string of
// re-escaped text could.
type ExportChunkResponse struct {
	JobID    string `json:"job_id"`
	Filename string `json:"filename"`
	Total    int64  `json:"total"`   // full log size in bytes
	Offset   int64  `json:"offset"`  // byte offset this chunk starts at
	Content  []byte `json:"content"` // this slice's raw bytes
	EOF      bool   `json:"eof"`     // true when no bytes remain after this chunk
}

// JobListResponse wraps a list of jobs.
type JobListResponse struct {
	Jobs []JobStatus `json:"jobs"`
}

// RunListResponse wraps every deletion run on the account (see
// logstats.Runs) — the run-centric list the Insights UI browses and exports
// from, as distinct from the one-shot jobs above.
type RunListResponse struct {
	Runs []logstats.RunStat `json:"runs"`
}

// RunChunkRequest asks for one slice of a specific run's deletion log,
// identified by its filename (see logstats.RunStat.File).
type RunChunkRequest struct {
	File   string `json:"file"`
	Offset int64  `json:"offset"`
}

// RunChunkResponse carries one slice of a run's NDJSON deletion log — the
// run-centric analogue of ExportChunkResponse, keyed by filename rather than
// job ID since a run isn't necessarily the output of a tracked job (it may
// equally be a monitor's scheduled pass).
type RunChunkResponse struct {
	File    string `json:"file"`
	Total   int64  `json:"total"`   // full log size in bytes
	Offset  int64  `json:"offset"`  // byte offset this chunk starts at
	Content []byte `json:"content"` // this slice's raw bytes
	EOF     bool   `json:"eof"`     // true when no bytes remain after this chunk
}

// PurgeLogsResponse reports how many records a purge removed.
type PurgeLogsResponse struct {
	Removed int `json:"removed"`
}

// MonitorMode selects whether Channels is an allow-list or a deny-list.
type MonitorMode string

const (
	// ModeExclude deletes everywhere in scope EXCEPT the listed channels
	// ("never auto-delete these DMs/groups").
	ModeExclude MonitorMode = "exclude"
	// ModeInclude deletes ONLY in the listed channels.
	ModeInclude MonitorMode = "include"
)

// MonitorAgeUnit is the time unit a monitor's retention window is expressed in.
type MonitorAgeUnit string

const (
	AgeMinutes MonitorAgeUnit = "minutes"
	AgeHours   MonitorAgeUnit = "hours"
	AgeDays    MonitorAgeUnit = "days"
	AgeWeeks   MonitorAgeUnit = "weeks"
)

// unitDuration returns the length of one unit and whether the unit is known.
func (u MonitorAgeUnit) unitDuration() (time.Duration, bool) {
	switch u {
	case AgeMinutes:
		return time.Minute, true
	case AgeHours:
		return time.Hour, true
	case AgeDays:
		return 24 * time.Hour, true
	case AgeWeeks:
		return 7 * 24 * time.Hour, true
	default:
		return 0, false
	}
}

// Short renders the unit as a single-letter suffix for compact tables (m/h/d/w).
func (u MonitorAgeUnit) Short() string {
	switch u {
	case AgeMinutes:
		return "m"
	case AgeHours:
		return "h"
	case AgeWeeks:
		return "w"
	default:
		return "d"
	}
}

// ParseMonitorAgeUnit accepts a unit name or single-letter alias, defaulting to
// days for the empty string.
func ParseMonitorAgeUnit(s string) (MonitorAgeUnit, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "d", "day", "days":
		return AgeDays, nil
	case "m", "min", "mins", "minute", "minutes":
		return AgeMinutes, nil
	case "h", "hr", "hrs", "hour", "hours":
		return AgeHours, nil
	case "w", "week", "weeks":
		return AgeWeeks, nil
	default:
		return "", fmt.Errorf("unknown age unit %q (use minutes, hours, days, or weeks)", s)
	}
}

// MonitorPolicy is a standing rule the server applies on a schedule: keep
// messages no older than the retention window (MaxAgeAmount MaxAgeUnit) in the
// selected channels.
type MonitorPolicy struct {
	ID       string      `json:"id,omitempty"`
	Name     string      `json:"name"`
	Enabled  bool        `json:"enabled"`
	Scope    string      `json:"scope"` // guild name/ID or "@me"
	Mode     MonitorMode `json:"mode"`
	Channels []string    `json:"channels,omitempty"` // selectors for include/exclude
	// Retention window: keep messages no older than MaxAgeAmount MaxAgeUnit.
	MaxAgeAmount int            `json:"max_age_amount"`
	MaxAgeUnit   MonitorAgeUnit `json:"max_age_unit"`
	IntervalHrs  int            `json:"interval_hours"` // how often to run (default 6)
	// IncludePinned deletes pinned messages too; false (the default) keeps them.
	IncludePinned bool      `json:"include_pinned,omitempty"`
	LastRun       time.Time `json:"last_run,omitempty"`
	LastDeleted   int       `json:"last_deleted,omitempty"`
	NextRun       time.Time `json:"next_run,omitempty"`
	// Running is true while a scheduled run is in progress. While running,
	// LastDeleted and Total update live (deleted-so-far / matched total) for a
	// progress indicator, and Recent holds the run's most-recent deletions.
	Running bool          `json:"running,omitempty"`
	Total   int           `json:"total,omitempty"`
	Recent  []FeedMessage `json:"recent,omitempty"`
}

// normalizeAge defaults an unset age unit to days.
func (p *MonitorPolicy) normalizeAge() {
	if p.MaxAgeUnit == "" {
		p.MaxAgeUnit = AgeDays
	}
}

// MaxAge returns the retention window as a duration.
func (p MonitorPolicy) MaxAge() time.Duration {
	d, ok := p.MaxAgeUnit.unitDuration()
	if !ok {
		d = 24 * time.Hour // treat an unset/unknown unit as days
	}
	return time.Duration(p.MaxAgeAmount) * d
}

// MonitorRequest creates or updates a monitor policy.
type MonitorRequest struct {
	Credentials *Credentials  `json:"credentials,omitempty"`
	Policy      MonitorPolicy `json:"policy"`
}

// MonitorListResponse wraps a list of monitor policies.
type MonitorListResponse struct {
	Monitors []MonitorPolicy `json:"monitors"`
}

// ErrorResponse is the body returned for any non-2xx response.
type ErrorResponse struct {
	Error string `json:"error"`
}
