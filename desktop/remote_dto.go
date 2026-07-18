package main

import (
	"fmt"
	"time"

	"github.com/inherentescapade/viaduct/server"
)

// This file defines the JSON-friendly DTOs for the self-hosting ("Server")
// section of the UI, plus converters from the server package's protocol types.
// Like dto.go, the frontend never sees raw server types: typed strings
// (JobKind/JobState/MonitorMode) become plain strings and time.Time becomes
// RFC3339 (empty for the zero time).

// ---- Requests (JS -> Go) ----

// RemoteJobRequest drives RemotePreview and RemoteSubmit. Kind is "delete_guild"
// (a server or @me) or "delete_dm" (your messages in DMs with a user).
type RemoteJobRequest struct {
	Kind     string   `json:"kind"`
	Guild    string   `json:"guild"`
	Channels []string `json:"channels"`
	Exclude  []string `json:"exclude"`
	User     string   `json:"user"`
	Before   string   `json:"before"`
	After    string   `json:"after"`
	MaxID    string   `json:"maxId"`
	MinID    string   `json:"minId"`
	Verify   bool     `json:"verify"`
}

func (r RemoteJobRequest) toSpec() server.DeleteSpec {
	return server.DeleteSpec{
		Guild:    r.Guild,
		Channels: r.Channels,
		Exclude:  r.Exclude,
		User:     r.User,
		Before:   r.Before,
		After:    r.After,
		MaxID:    r.MaxID,
		MinID:    r.MinID,
		Verify:   r.Verify,
	}
}

// kind validates and returns the server JobKind for this request.
func (r RemoteJobRequest) kind() (server.JobKind, error) {
	switch r.Kind {
	case string(server.KindDeleteGuild):
		return server.KindDeleteGuild, nil
	case string(server.KindDeleteDM):
		return server.KindDeleteDM, nil
	default:
		return "", fmt.Errorf("unknown task kind %q", r.Kind)
	}
}

// MonitorReq drives RemoteSetMonitor and RemotePreviewMonitor. It mirrors a
// monitor policy from the UI side.
type MonitorReq struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Enabled      bool     `json:"enabled"`
	Scope        string   `json:"scope"`
	Mode         string   `json:"mode"`
	Channels     []string `json:"channels"`
	MaxAgeAmount int      `json:"maxAgeAmount"`
	MaxAgeUnit   string   `json:"maxAgeUnit"`
	IntervalHrs  int      `json:"intervalHrs"`
}

func (r MonitorReq) toPolicy() server.MonitorPolicy {
	mode := server.MonitorMode(r.Mode)
	if mode != server.ModeInclude && mode != server.ModeExclude {
		mode = server.ModeExclude
	}
	return server.MonitorPolicy{
		ID:           r.ID,
		Name:         r.Name,
		Enabled:      r.Enabled,
		Scope:        r.Scope,
		Mode:         mode,
		Channels:     r.Channels,
		MaxAgeAmount: r.MaxAgeAmount,
		MaxAgeUnit:   server.MonitorAgeUnit(r.MaxAgeUnit),
		IntervalHrs:  r.IntervalHrs,
	}
}

// ---- Responses (Go -> JS) ----

type IdentityDTO struct {
	PublicKey   string `json:"publicKey"`
	Fingerprint string `json:"fingerprint"`
	Created     bool   `json:"created"`
}

type RemoteDTO struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	HasKey  bool   `json:"hasKey"`
}

type ActingAsDTO struct {
	Username string `json:"username"`
	ID       string `json:"id"`
}

type PingDTO struct {
	Version  string       `json:"version"`
	HasToken bool         `json:"hasToken"`
	ActingAs *ActingAsDTO `json:"actingAs"`
	Jobs     int          `json:"jobs"`
	Monitors int          `json:"monitors"`
}

type PreviewDTO struct {
	Target   string       `json:"target"`
	Total    int          `json:"total"`
	ActingAs *ActingAsDTO `json:"actingAs"`
}

type FeedMessageDTO struct {
	Content         string `json:"content"`
	Channel         string `json:"channel"`
	Timestamp       string `json:"timestamp"`
	AuthorName      string `json:"authorName"`
	AuthorAvatarURL string `json:"authorAvatarUrl"`
}

type JobDTO struct {
	ID          string           `json:"id"`
	Kind        string           `json:"kind"`
	Description string           `json:"description"`
	State       string           `json:"state"`
	Total       int              `json:"total"`
	Deleted     int              `json:"deleted"`
	Failed      int              `json:"failed"`
	Skipped     int              `json:"skipped"`
	Ignored     int              `json:"ignored"` // undeletable system messages scanned past
	Residual    int              `json:"residual"`
	Error       string           `json:"error"`
	HasExport   bool             `json:"hasExport"`
	Created     string           `json:"created"`
	Recent      []FeedMessageDTO `json:"recent"`
	RatePerSec  float64          `json:"ratePerSec"`
	EtaMs       int64            `json:"etaMs"`
}

// ExportProgressDTO streams the byte progress of a remote export download to
// the frontend (via EvExportProgress) so it can render a progress bar. Received
// and Total are byte counts; Total is the full log size, known from the first
// chunk.
type ExportProgressDTO struct {
	JobID    string `json:"jobId"`
	Received int64  `json:"received"`
	Total    int64  `json:"total"`
}

type MonitorDTO struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Enabled      bool             `json:"enabled"`
	Scope        string           `json:"scope"`
	Mode         string           `json:"mode"`
	Channels     []string         `json:"channels"`
	MaxAgeAmount int              `json:"maxAgeAmount"`
	MaxAgeUnit   string           `json:"maxAgeUnit"`
	IntervalHrs  int              `json:"intervalHrs"`
	LastRun      string           `json:"lastRun"`
	NextRun      string           `json:"nextRun"`
	LastDeleted  int              `json:"lastDeleted"`
	Total        int              `json:"total"`
	Running      bool             `json:"running"`
	Recent       []FeedMessageDTO `json:"recent"`
}

// ---- Converters ----

func toActingAsDTO(d *server.DiscordIdentity) *ActingAsDTO {
	if d == nil {
		return nil
	}
	return &ActingAsDTO{Username: d.Username, ID: d.ID}
}

func toPingDTO(p *server.PingResponse) PingDTO {
	return PingDTO{
		Version:  p.Version,
		HasToken: p.HasToken,
		ActingAs: toActingAsDTO(p.ActingAs),
		Jobs:     p.Jobs,
		Monitors: p.Monitors,
	}
}

func toPreviewDTO(p *server.PreviewResponse) PreviewDTO {
	var acting *ActingAsDTO
	if p.ActingAs.Username != "" || p.ActingAs.ID != "" {
		acting = &ActingAsDTO{Username: p.ActingAs.Username, ID: p.ActingAs.ID}
	}
	return PreviewDTO{Target: p.Target, Total: p.Total, ActingAs: acting}
}

func toFeed(in []server.FeedMessage) []FeedMessageDTO {
	out := make([]FeedMessageDTO, 0, len(in))
	for _, f := range in {
		ts := ""
		if !f.Timestamp.IsZero() {
			ts = f.Timestamp.Format(time.RFC3339)
		}
		out = append(out, FeedMessageDTO{
			Content:         f.Content,
			Channel:         f.Channel,
			Timestamp:       ts,
			AuthorName:      f.Author,
			AuthorAvatarURL: userAvatarURL(f.AuthorID, f.AuthorAvatar),
		})
	}
	return out
}

func toJobDTO(j server.JobStatus) JobDTO {
	return JobDTO{
		ID:          j.ID,
		Kind:        string(j.Kind),
		Description: j.Description,
		State:       string(j.State),
		Total:       j.Total,
		Deleted:     j.Deleted,
		Failed:      j.Failed,
		Skipped:     j.Skipped,
		Ignored:     j.Ignored,
		Residual:    j.Residual,
		Error:       j.Error,
		HasExport:   j.HasExport,
		Created:     fmtTime(j.Created),
		Recent:      toFeed(j.Recent),
		RatePerSec:  j.RatePerSec,
		EtaMs:       j.EtaMs,
	}
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func toMonitorDTO(m server.MonitorPolicy) MonitorDTO {
	return MonitorDTO{
		ID:           m.ID,
		Name:         m.Name,
		Enabled:      m.Enabled,
		Scope:        m.Scope,
		Mode:         string(m.Mode),
		Channels:     m.Channels,
		MaxAgeAmount: m.MaxAgeAmount,
		MaxAgeUnit:   string(m.MaxAgeUnit),
		IntervalHrs:  m.IntervalHrs,
		LastRun:      fmtTime(m.LastRun),
		NextRun:      fmtTime(m.NextRun),
		LastDeleted:  m.LastDeleted,
		Total:        m.Total,
		Running:      m.Running,
		Recent:       toFeed(m.Recent),
	}
}
