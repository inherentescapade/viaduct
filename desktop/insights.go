package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/inherentescapade/viaduct/cfg"
	"github.com/inherentescapade/viaduct/dates"
	"github.com/inherentescapade/viaduct/logstats"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// This file exposes the deletion-log "insights" to the UI. Viaduct already
// records every removed message to an append-only NDJSON log; LogStats turns
// that history into the aggregated figures the Insights tab renders (totals,
// targets, activity over time, run history, failures).

// LogStats parses this machine's local deletion logs into an aggregated summary,
// then resolves deletion targets (recorded only by channel ID) to friendly
// names using the live Discord client.
func (a *App) LogStats() (logstats.Stats, error) {
	st, err := logstats.Parse(cfg.LogDir(), a.chanLabelFor)
	if err != nil {
		return st, err
	}
	st.Source = "local"
	a.enrichTargets(&st)
	return st, nil
}

// LogSearchRequest is the JS-facing query for the deletion-log search. Date
// fields are raw expressions parsed via dates.Parse (same as the delete flow).
type LogSearchRequest struct {
	Text            string `json:"text"`
	ChannelID       string `json:"channelId"`
	Kind            string `json:"kind"` // "" | "deleted" | "failed"
	WithAttachments bool   `json:"withAttachments"`
	Before          string `json:"before"`
	After           string `json:"after"`
	Limit           int    `json:"limit"`
	Offset          int    `json:"offset"`
}

// toQuery converts the request into a logstats.SearchQuery, parsing the date
// expressions.
func (r LogSearchRequest) toQuery() (logstats.SearchQuery, error) {
	q := logstats.SearchQuery{
		Text:            strings.TrimSpace(r.Text),
		ChannelID:       strings.TrimSpace(r.ChannelID),
		Kind:            logstats.SearchKind(strings.TrimSpace(r.Kind)),
		WithAttachments: r.WithAttachments,
		Limit:           r.Limit,
		Offset:          r.Offset,
	}
	if s := strings.TrimSpace(r.Before); s != "" {
		t, err := dates.Parse(s)
		if err != nil {
			return q, fmt.Errorf("invalid 'before' date: %w", err)
		}
		q.Before = t
	}
	if s := strings.TrimSpace(r.After); s != "" {
		t, err := dates.Parse(s)
		if err != nil {
			return q, fmt.Errorf("invalid 'after' date: %w", err)
		}
		q.After = t
	}
	return q, nil
}

// SearchLogs runs a full-text query over this machine's deletion logs and
// resolves each hit's channel to a friendly name, paging via the request's
// limit/offset.
func (a *App) SearchLogs(req LogSearchRequest) (logstats.SearchResult, error) {
	q, err := req.toQuery()
	if err != nil {
		return logstats.SearchResult{}, err
	}
	res, err := logstats.Search(cfg.LogDir(), q, a.chanLabelFor)
	if err != nil {
		return res, err
	}
	a.enrichHits(res.Hits)
	return res, nil
}

// ListRuns returns every deletion run on this machine (newest first), with its
// message/failure counts and busiest channel resolved to a friendly name — the
// run-centric list the Insights UI browses and exports from.
func (a *App) ListRuns() ([]logstats.RunStat, error) {
	runs, err := logstats.Runs(cfg.LogDir())
	if err != nil {
		return nil, err
	}
	a.enrichRuns(runs)
	return runs, nil
}

// enrichRuns fills each run's TopChannelLabel by resolving its busiest channel
// ID, fetching each distinct channel at most once. Unresolved IDs are left blank
// for the UI to shorten.
func (a *App) enrichRuns(runs []logstats.RunStat) {
	if len(runs) == 0 || a.activeClient() == nil {
		return
	}
	resolved := map[string]string{}
	for i := range runs {
		id := runs[i].TopChannel
		if id == "" {
			continue
		}
		name, ok := resolved[id]
		if !ok {
			name = a.resolveChannelName(id)
			resolved[id] = name
		}
		runs[i].TopChannelLabel = name
	}
}

// ExportSearch writes every local log record matching req to a file the user
// picks, as NDJSON (the same lossless, re-importable format the app appends). An
// empty query exports the whole history. Returns the saved path, or "" if the
// user cancels the dialog.
func (a *App) ExportSearch(req LogSearchRequest) (string, error) {
	q, err := req.toQuery()
	if err != nil {
		return "", err
	}
	dest, err := wruntime.SaveFileDialog(a.appCtx, wruntime.SaveDialogOptions{
		Title:           "Export deletion records",
		DefaultFilename: "viaduct-deletions.ndjson",
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
	if _, err := logstats.Export(cfg.LogDir(), q, f); err != nil {
		f.Close()
		return "", fmt.Errorf("could not write the export: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("could not save the export: %w", err)
	}
	return dest, nil
}

// PurgeLogs removes every local log record matching req, rewriting the affected
// logs (and deleting any left empty). It refuses to run without at least one
// active filter, so it can't wipe everything by accident — that's what "Clear
// all logs" in Settings is for. Returns the number of records removed.
func (a *App) PurgeLogs(req LogSearchRequest) (int, error) {
	q, err := req.toQuery()
	if err != nil {
		return 0, err
	}
	if q.IsEmpty() {
		return 0, fmt.Errorf("add at least one filter before deleting matches; use “Clear all logs” in Settings to remove everything")
	}
	return logstats.Purge(cfg.LogDir(), q)
}

// enrichHits resolves each hit's channel to a friendly name (fetching each
// distinct channel at most once) and fills in the author's avatar URL. Records
// that carry no author — failures, or messages logged before author capture —
// fall back to the signed-in user, since every logged delete is one of their
// own messages.
func (a *App) enrichHits(hits []logstats.SearchHit) {
	if len(hits) == 0 {
		return
	}
	a.mu.Lock()
	user := a.user
	client := a.client
	a.mu.Unlock()

	resolved := map[string]string{}
	for i := range hits {
		if id := hits[i].ChannelID; id != "" && client != nil {
			name, ok := resolved[id]
			if !ok {
				name = a.resolveChannelName(id)
				resolved[id] = name
			}
			if name != "" {
				hits[i].ChannelLabel = name
			}
		}
		if hits[i].AuthorName == "" && user != nil {
			hits[i].AuthorName = recipientName(*user)
			hits[i].AuthorID = user.Id
			hits[i].AuthorAvatar = user.Avatar
		}
		hits[i].AuthorAvatarURL = userAvatarURL(hits[i].AuthorID, hits[i].AuthorAvatar)
	}
}

// enrichTargets replaces bare-ID target labels with channel/DM names. It is
// best-effort: anything it can't resolve keeps the parser's fallback label.
func (a *App) enrichTargets(st *logstats.Stats) {
	if a.activeClient() == nil {
		return
	}
	for i := range st.TopChannels {
		if name := a.resolveChannelName(st.TopChannels[i].ID); name != "" {
			st.TopChannels[i].Label = name
		}
	}
}

// resolveChannelName returns a friendly label for a channel ID, consulting the
// in-memory channel cache first and falling back to a live fetch (whose result
// is cached for next time). Returns "" when it can't be resolved.
func (a *App) resolveChannelName(id string) string {
	if id == "" {
		return ""
	}
	a.mu.Lock()
	ch, ok := a.chanCache[id]
	client := a.client
	a.mu.Unlock()
	if ok {
		return channelLabel(ch)
	}
	if client == nil {
		return ""
	}
	full, err := client.GetChannel(id)
	if err != nil || full == nil {
		return ""
	}
	a.mu.Lock()
	a.chanCache[id] = *full
	a.mu.Unlock()
	return channelLabel(*full)
}
