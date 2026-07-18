package logstats

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// This file adds a queryable, full-text view over the same append-only deletion
// logs that Parse aggregates. Where Parse answers "how much, where, when" in
// summary, Search answers "show me the actual messages" — letting the Insights
// UI page through every individual delete (and every failure) ever logged,
// filtered by text, channel, kind, attachments, and date.
//
// Scanning is lenient in exactly the way Parse is: malformed lines are skipped,
// unknown fields ignored, a missing directory yields an empty result. Logs are
// read newest-run-first so the most recent deletions surface at the top.

// SearchKind narrows a search to one record shape.
type SearchKind string

const (
	// KindAny matches both deleted messages and failures.
	KindAny SearchKind = ""
	// KindDeleted matches only successfully deleted messages.
	KindDeleted SearchKind = "deleted"
	// KindFailed matches only delete failures.
	KindFailed SearchKind = "failed"
)

// SearchQuery describes which logged records to return. The zero value matches
// every record (subject to the limit). All filters combine with AND. Date
// bounds apply to a message's posted timestamp; failures carry no timestamp, so
// any date bound excludes them.
type SearchQuery struct {
	Text            string     `json:"text"`            // case-insensitive substring of content / failure reason / channel id
	ChannelID       string     `json:"channelId"`       // exact channel-id match
	Kind            SearchKind `json:"kind"`            // "", "deleted", or "failed"
	WithAttachments bool       `json:"withAttachments"` // only deleted messages that carried an attachment
	After           time.Time  `json:"after"`           // posted at or after (zero = no lower bound)
	Before          time.Time  `json:"before"`          // posted at or before (zero = no upper bound)
	Limit           int        `json:"limit"`           // page size (clamped to maxSearchLimit; <=0 uses the default)
	Offset          int        `json:"offset"`          // matches to skip, for paging
}

// SearchHit is one logged record matching a query: either a deleted message or a
// failure (distinguished by Kind). ChannelLabel is a best-effort friendly name;
// the desktop enriches it further with its live channel cache.
type SearchHit struct {
	File         string `json:"file"`         // log filename the record came from
	RunAt        string `json:"runAt"`        // RFC3339 run start, parsed from the filename
	Kind         string `json:"kind"`         // "deleted" | "failed"
	ID           string `json:"id"`           // message id
	ChannelID    string `json:"channelId"`    //
	ChannelLabel string `json:"channelLabel"` // resolved name, or a shortened id fallback
	Content      string `json:"content"`      // message text (deleted records)
	Timestamp    string `json:"timestamp"`    // RFC3339 posted time, "" when unknown
	Attachments  int    `json:"attachments"`  // attachment count (deleted records)
	Error        string `json:"error"`        // failure reason (failed records)

	// Author of the message. The desktop fills AuthorAvatarURL from the id/avatar
	// hash (and falls back to the signed-in user for records that predate author
	// logging, or for failures, which carry no author).
	AuthorName      string `json:"authorName"`
	AuthorAvatarURL string `json:"authorAvatarUrl"`
	AuthorID        string `json:"-"` // avatar-URL inputs, not shipped to JS
	AuthorAvatar    string `json:"-"`
}

// SearchResult is one page of hits plus the totals needed to drive pagination.
type SearchResult struct {
	Hits      []SearchHit `json:"hits"`
	Total     int         `json:"total"`     // matches across every log (not just this page)
	Offset    int         `json:"offset"`    // echoed back, the offset this page starts at
	Limit     int         `json:"limit"`     // effective page size used
	Scanned   int         `json:"scanned"`   // records examined
	Truncated bool        `json:"truncated"` // scan cap hit; Total is a lower bound
}

const (
	defaultSearchLimit = 100
	maxSearchLimit     = 500
	// maxSearchScan bounds how many records a single query will examine, so a
	// pathologically large history can't make the UI hang. When hit, Total is a
	// lower bound and Truncated is set.
	maxSearchScan = 2_000_000
)

// Search scans every deletion log in dir and returns the page of records
// matching q. labelFor, if non-nil, resolves a channel ID to a friendly label
// (the desktop passes its channel cache); names harvested from failure records
// are used as a fallback so even an unresolved channel reads better than a bare
// ID. A missing directory yields an empty result, not an error.
func Search(dir string, q SearchQuery, labelFor func(channelID string) string) (SearchResult, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	res := SearchResult{Hits: []SearchHit{}, Offset: offset, Limit: limit}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil
		}
		return res, err
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasPrefix(n, logFilePrefix) && strings.HasSuffix(n, logFileSuffix) {
			names = append(names, n)
		}
	}
	// Newest run first: filenames embed the run timestamp, so a reverse sort is
	// reverse-chronological.
	sort.Sort(sort.Reverse(sort.StringSlice(names)))

	needle := strings.ToLower(strings.TrimSpace(q.Text))
	// channelNames harvested from failure records, reused to label deleted
	// records in the same channel when no live label is available.
	channelNames := map[string]string{}

	label := func(id, harvested string) string {
		if labelFor != nil {
			if l := strings.TrimSpace(labelFor(id)); l != "" {
				return l
			}
		}
		if harvested == "" {
			harvested = channelNames[id]
		}
		if harvested != "" {
			return "#" + harvested
		}
		return shortID(id)
	}

scan:
	for _, name := range names {
		runAt := ""
		if ts, perr := time.Parse(fileTimeLayout, strings.TrimSuffix(strings.TrimPrefix(name, logFilePrefix), logFileSuffix)); perr == nil {
			runAt = ts.Format(time.RFC3339)
		}

		f, oerr := os.Open(filepath.Join(dir, name))
		if oerr != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
		for sc.Scan() {
			line := sc.Bytes()
			if len(line) == 0 {
				continue
			}
			var rec record
			if json.Unmarshal(line, &rec) != nil {
				continue
			}
			failed := rec.Event == "delete_failed"
			if !failed && rec.ID == "" {
				continue // neither a recognised message nor a failure
			}
			if rec.ChannelID != "" && rec.Channel != "" {
				channelNames[rec.ChannelID] = rec.Channel
			}

			res.Scanned++
			if !q.matches(rec, failed, needle) {
				if res.Scanned >= maxSearchScan {
					res.Truncated = true
					f.Close()
					break scan
				}
				continue
			}

			// A match: count it toward the total and, if it falls in the
			// requested page window, materialise it.
			idx := res.Total
			res.Total++
			if idx >= offset && len(res.Hits) < limit {
				res.Hits = append(res.Hits, hitFrom(rec, failed, name, runAt, label))
			}

			if res.Scanned >= maxSearchScan {
				res.Truncated = true
				f.Close()
				break scan
			}
		}
		f.Close()
	}

	return res, nil
}

// IsEmpty reports whether q has no active filter (it would match every record).
// Callers that mutate logs use this to refuse an unfiltered purge, which would
// otherwise wipe the whole history.
func (q SearchQuery) IsEmpty() bool {
	return strings.TrimSpace(q.Text) == "" &&
		q.ChannelID == "" &&
		q.Kind == KindAny &&
		!q.WithAttachments &&
		q.After.IsZero() &&
		q.Before.IsZero()
}

// matches reports whether rec passes every active filter in q. failed is whether
// rec is a failure record; needle is the already-lowercased text filter.
func (q SearchQuery) matches(rec record, failed bool, needle string) bool {
	switch q.Kind {
	case KindDeleted:
		if failed {
			return false
		}
	case KindFailed:
		if !failed {
			return false
		}
	}
	if q.ChannelID != "" && rec.ChannelID != q.ChannelID {
		return false
	}
	if q.WithAttachments && (failed || len(rec.Attachments) == 0) {
		return false
	}
	if !q.After.IsZero() || !q.Before.IsZero() {
		// Records without a posted time (failures, or messages logged without a
		// timestamp) can't satisfy a date bound.
		if rec.Timestamp.IsZero() {
			return false
		}
		if !q.After.IsZero() && rec.Timestamp.Before(q.After) {
			return false
		}
		if !q.Before.IsZero() && rec.Timestamp.After(q.Before) {
			return false
		}
	}
	if needle != "" {
		hay := strings.ToLower(rec.Content)
		if failed {
			hay = strings.ToLower(rec.Error)
		}
		if !strings.Contains(hay, needle) &&
			!strings.Contains(strings.ToLower(rec.Channel), needle) &&
			!strings.Contains(strings.ToLower(rec.ChannelID), needle) {
			return false
		}
	}
	return true
}

// hitFrom converts a matched record into a SearchHit.
func hitFrom(rec record, failed bool, file, runAt string, label func(id, harvested string) string) SearchHit {
	h := SearchHit{
		File:         file,
		RunAt:        runAt,
		ChannelID:    rec.ChannelID,
		ChannelLabel: label(rec.ChannelID, rec.Channel),
	}
	if failed {
		h.Kind = "failed"
		h.ID = rec.MessageID
		h.Error = rec.Error
		if h.Error == "" {
			h.Error = "unknown"
		}
		return h
	}
	h.Kind = "deleted"
	h.ID = rec.ID
	h.Content = rec.Content
	h.Attachments = len(rec.Attachments)
	if !rec.Timestamp.IsZero() {
		h.Timestamp = rec.Timestamp.UTC().Format(time.RFC3339)
	}
	h.AuthorName = rec.Author.GlobalName
	if h.AuthorName == "" {
		h.AuthorName = rec.Author.Username
	}
	h.AuthorID = rec.Author.ID
	h.AuthorAvatar = rec.Author.Avatar
	return h
}

// logFileNames returns the deletion-log filenames among entries, in directory
// order (callers sort as needed).
func logFileNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasPrefix(n, logFilePrefix) && strings.HasSuffix(n, logFileSuffix) {
			names = append(names, n)
		}
	}
	return names
}

// Runs summarises every deletion log in dir as a RunStat — its message and
// failure counts and its busiest channel — newest run first. It is the
// run-centric companion to Parse: where Parse caps its Recent list for a
// dashboard, Runs returns every run so the UI can browse and export them.
// TopChannelLabel is left empty for the caller to resolve. A missing directory
// yields an empty slice, not an error.
func Runs(dir string) ([]RunStat, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []RunStat{}, nil
		}
		return nil, err
	}
	names := logFileNames(entries)
	sort.Sort(sort.Reverse(sort.StringSlice(names))) // newest run first

	out := make([]RunStat, 0, len(names))
	for _, name := range names {
		run := RunStat{File: name}
		if ts, perr := time.Parse(fileTimeLayout, strings.TrimSuffix(strings.TrimPrefix(name, logFilePrefix), logFileSuffix)); perr == nil {
			run.StartedAt = ts.Format(time.RFC3339)
		}
		channelCounts := map[string]int{}
		if f, oerr := os.Open(filepath.Join(dir, name)); oerr == nil {
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
			for sc.Scan() {
				line := sc.Bytes()
				if len(line) == 0 {
					continue
				}
				var rec record
				if json.Unmarshal(line, &rec) != nil {
					continue
				}
				if rec.Event == "delete_failed" {
					run.Failed++
					continue
				}
				if rec.ID == "" {
					continue
				}
				run.Deleted++
				if rec.ChannelID != "" {
					channelCounts[rec.ChannelID]++
				}
			}
			f.Close()
		}
		run.TopChannel = topKey(channelCounts)
		out = append(out, run)
	}
	return out, nil
}

// Export writes every log record in dir matching q to w as NDJSON, newest run
// first. Each match is written as its original raw log line, so the output is
// byte-for-byte re-importable — the same format the app appends. Limit/Offset on
// q are ignored (an export is the whole matching set); it returns the number of
// records written.
func Export(dir string, q SearchQuery, w io.Writer) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	names := logFileNames(entries)
	sort.Sort(sort.Reverse(sort.StringSlice(names)))

	needle := strings.ToLower(strings.TrimSpace(q.Text))
	bw := bufio.NewWriter(w)
	written := 0
	for _, name := range names {
		f, oerr := os.Open(filepath.Join(dir, name))
		if oerr != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
		for sc.Scan() {
			line := sc.Bytes()
			if len(line) == 0 {
				continue
			}
			var rec record
			if json.Unmarshal(line, &rec) != nil {
				continue
			}
			failed := rec.Event == "delete_failed"
			if !failed && rec.ID == "" {
				continue
			}
			if !q.matches(rec, failed, needle) {
				continue
			}
			// bufio copies immediately, so writing the scanner's slice is safe.
			bw.Write(line)
			bw.WriteByte('\n')
			written++
		}
		f.Close()
	}
	if err := bw.Flush(); err != nil {
		return written, err
	}
	return written, nil
}

// Purge removes every log record in dir matching q, rewriting each affected file
// without the matching lines (and deleting a file left empty). It returns the
// number of records removed. A query matching everything would empty the
// directory, so callers should reject an empty query (see IsEmpty). Rewrites are
// atomic: a temp file is written and renamed over the original, so an
// interrupted purge never leaves a half-written log.
func Purge(dir string, q SearchQuery) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	needle := strings.ToLower(strings.TrimSpace(q.Text))
	removed := 0
	for _, name := range logFileNames(entries) {
		n, err := purgeFile(filepath.Join(dir, name), q, needle)
		if err != nil {
			return removed, err
		}
		removed += n
	}
	return removed, nil
}

// purgeFile rewrites one log file, dropping records that match q. It streams the
// kept lines to a temp file so a huge log never lives in memory, then renames it
// over the original. When nothing matched, the file is left untouched; when
// everything matched, the file is removed.
func purgeFile(path string, q SearchQuery, needle string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, nil // unreadable: skip rather than fail the whole purge
	}
	tmp := path + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		f.Close()
		return 0, err
	}
	bw := bufio.NewWriter(out)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	removed, kept := 0, 0
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec record
		if json.Unmarshal(line, &rec) == nil {
			failed := rec.Event == "delete_failed"
			if (failed || rec.ID != "") && q.matches(rec, failed, needle) {
				removed++
				continue
			}
		}
		// Keep everything else, including lines we couldn't parse.
		bw.Write(line)
		bw.WriteByte('\n')
		kept++
	}
	flushErr := bw.Flush()
	closeErr := out.Close()
	f.Close()
	if flushErr != nil || closeErr != nil {
		os.Remove(tmp)
		if flushErr != nil {
			return 0, flushErr
		}
		return 0, closeErr
	}
	if removed == 0 {
		os.Remove(tmp) // no change; leave the original alone
		return 0, nil
	}
	if kept == 0 {
		os.Remove(tmp)
		if err := os.Remove(path); err != nil {
			return removed, err
		}
		return removed, nil
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return removed, err
	}
	return removed, nil
}
