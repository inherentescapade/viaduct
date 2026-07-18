// Package logstats turns viaduct's append-only deletion logs into a compact,
// aggregated summary the UI can render as "insights": how much you've deleted,
// where, when, and what failed.
//
// Every run appends one NDJSON file (delete_<timestamp>.ndjson) under the log
// directory. Each line is either a deleted message (a raw discord.Message, with
// an "id" and "timestamp") or a failure record ({"event":"delete_failed", ...}).
// Parsing is deliberately lenient: unknown fields are ignored and a malformed
// line is skipped rather than failing the whole report, so a half-written line
// from a crash never costs you the rest of your history.
package logstats

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// logFilePrefix / logFileSuffix bound which files in the directory are deletion
// logs, and the layout the run timestamp is encoded with in the filename.
const (
	logFilePrefix = "delete_"
	logFileSuffix = ".ndjson"
	// fileTimeLayout matches the timestamp baked into a log filename, e.g.
	// delete_2024-01-02_150405.ndjson.
	fileTimeLayout = "2006-01-02_150405"
)

// ChannelStat is one deletion target (a channel/DM) and how many of your
// messages were removed there.
type ChannelStat struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

// Bucket is a labelled tally used for the month/hour histograms.
type Bucket struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// RunStat summarises a single run (one log file).
type RunStat struct {
	File            string `json:"file"`
	StartedAt       string `json:"startedAt"` // RFC3339, parsed from the filename
	Deleted         int    `json:"deleted"`
	Failed          int    `json:"failed"`
	TopChannel      string `json:"topChannel"`      // busiest channel ID
	TopChannelLabel string `json:"topChannelLabel"` // resolved name, filled by the caller (empty in Parse)
}

// FailReason is an aggregated failure cause and how many messages hit it.
type FailReason struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// Stats is the full aggregated picture across every log file.
type Stats struct {
	Source          string        `json:"source"` // "local" | "server", set by the caller
	Runs            int           `json:"runs"`
	TotalDeleted    int           `json:"totalDeleted"`
	TotalFailed     int           `json:"totalFailed"`
	WithAttachments int           `json:"withAttachments"`
	Attachments     int           `json:"attachments"`
	TotalChars      int           `json:"totalChars"`
	Channels        int           `json:"channels"` // distinct targets touched
	FirstPostedAt   string        `json:"firstPostedAt"`
	LastPostedAt    string        `json:"lastPostedAt"`
	FirstRunAt      string        `json:"firstRunAt"`
	LastRunAt       string        `json:"lastRunAt"`
	TopChannels     []ChannelStat `json:"topChannels"`
	ByMonth         []Bucket      `json:"byMonth"`
	ByHour          []int         `json:"byHour"` // 24 buckets, by message send hour (UTC)
	Recent          []RunStat     `json:"recent"`
	Failures        []FailReason  `json:"failures"`
}

// record is the lenient union of the two line shapes we log. A deleted message
// carries id/timestamp/content/attachments; a failure carries event/error and a
// resolved channel name (which we also harvest to label channel IDs).
type record struct {
	Event       string            `json:"event"`
	ID          string            `json:"id"`
	MessageID   string            `json:"message_id"` // set on failure records instead of id
	ChannelID   string            `json:"channel_id"`
	Channel     string            `json:"channel"`
	Content     string            `json:"content"`
	Timestamp   time.Time         `json:"timestamp"`
	Attachments []json.RawMessage `json:"attachments"`
	Error       string            `json:"error"`
	Author      struct {
		ID         string `json:"id"`
		Username   string `json:"username"`
		GlobalName string `json:"global_name"`
		Avatar     string `json:"avatar"`
	} `json:"author"`
}

const (
	topChannelsLimit = 15
	recentRunsLimit  = 50
	maxLineBytes     = 16 << 20 // some messages + attachment metadata get long
)

// Parse reads every deletion log in dir and aggregates it. labelFor, if
// non-nil, resolves a channel ID to a friendly label (the desktop passes its
// channel cache); names found in failure records are used as a fallback so even
// runs with no live cache get some real names. A missing directory yields an
// empty (non-nil-fielded) Stats, not an error.
func Parse(dir string, labelFor func(channelID string) string) (Stats, error) {
	st := Stats{
		TopChannels: []ChannelStat{},
		ByMonth:     []Bucket{},
		ByHour:      make([]int, 24),
		Recent:      []RunStat{},
		Failures:    []FailReason{},
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return st, err
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasPrefix(n, logFilePrefix) && strings.HasSuffix(n, logFileSuffix) {
			names = append(names, n)
		}
	}
	sort.Strings(names) // chronological, since the timestamp is the filename

	channelCounts := map[string]int{}
	channelNames := map[string]string{} // id -> name harvested from failures
	monthCounts := map[string]int{}
	failCounts := map[string]int{}
	var firstPosted, lastPosted time.Time

	for _, name := range names {
		run := RunStat{File: name}
		if ts, perr := time.Parse(fileTimeLayout, strings.TrimSuffix(strings.TrimPrefix(name, logFilePrefix), logFileSuffix)); perr == nil {
			run.StartedAt = ts.Format(time.RFC3339)
		}
		runChannelCounts := map[string]int{}

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

			if rec.Event == "delete_failed" {
				st.TotalFailed++
				run.Failed++
				reason := rec.Error
				if reason == "" {
					reason = "unknown"
				}
				failCounts[reason]++
				if rec.ChannelID != "" && rec.Channel != "" {
					channelNames[rec.ChannelID] = rec.Channel
				}
				continue
			}
			if rec.ID == "" {
				continue // not a message we recognise
			}

			st.TotalDeleted++
			run.Deleted++
			st.TotalChars += utf8.RuneCountInString(rec.Content)
			if len(rec.Attachments) > 0 {
				st.WithAttachments++
				st.Attachments += len(rec.Attachments)
			}
			if rec.ChannelID != "" {
				channelCounts[rec.ChannelID]++
				runChannelCounts[rec.ChannelID]++
			}
			if !rec.Timestamp.IsZero() {
				monthCounts[rec.Timestamp.UTC().Format("2006-01")]++
				st.ByHour[rec.Timestamp.UTC().Hour()]++
				if firstPosted.IsZero() || rec.Timestamp.Before(firstPosted) {
					firstPosted = rec.Timestamp
				}
				if rec.Timestamp.After(lastPosted) {
					lastPosted = rec.Timestamp
				}
			}
		}
		f.Close()

		run.TopChannel = topKey(runChannelCounts)
		st.Runs++
		st.Recent = append(st.Recent, run)
	}

	// Resolve channel labels: live cache first, then a name harvested from a
	// failure record, then a shortened ID so the row is never blank.
	label := func(id string) string {
		if labelFor != nil {
			if l := strings.TrimSpace(labelFor(id)); l != "" {
				return l
			}
		}
		if n := channelNames[id]; n != "" {
			return "#" + n
		}
		return shortID(id)
	}

	st.Channels = len(channelCounts)
	for id, count := range channelCounts {
		st.TopChannels = append(st.TopChannels, ChannelStat{ID: id, Label: label(id), Count: count})
	}
	sort.Slice(st.TopChannels, func(i, j int) bool {
		if st.TopChannels[i].Count != st.TopChannels[j].Count {
			return st.TopChannels[i].Count > st.TopChannels[j].Count
		}
		return st.TopChannels[i].ID < st.TopChannels[j].ID
	})
	if len(st.TopChannels) > topChannelsLimit {
		st.TopChannels = st.TopChannels[:topChannelsLimit]
	}

	// Per-run TopChannel is left as a raw channel ID; the UI maps it to a label
	// using the (enriched) TopChannels list so names resolved after parsing
	// flow through here too.

	st.ByMonth = sortedMonths(monthCounts)

	st.Failures = make([]FailReason, 0, len(failCounts))
	for reason, count := range failCounts {
		st.Failures = append(st.Failures, FailReason{Reason: reason, Count: count})
	}
	sort.Slice(st.Failures, func(i, j int) bool { return st.Failures[i].Count > st.Failures[j].Count })

	if !firstPosted.IsZero() {
		st.FirstPostedAt = firstPosted.UTC().Format(time.RFC3339)
		st.LastPostedAt = lastPosted.UTC().Format(time.RFC3339)
	}
	if len(st.Recent) > 0 {
		st.FirstRunAt = st.Recent[0].StartedAt
		st.LastRunAt = st.Recent[len(st.Recent)-1].StartedAt
	}

	// Newest run first, capped — the list is for a recent-activity panel.
	sort.Slice(st.Recent, func(i, j int) bool { return st.Recent[i].File > st.Recent[j].File })
	if len(st.Recent) > recentRunsLimit {
		st.Recent = st.Recent[:recentRunsLimit]
	}

	return st, nil
}

// topKey returns the map key with the highest value (ties broken by key), or ""
// for an empty map.
func topKey(m map[string]int) string {
	best, bestN := "", -1
	for k, n := range m {
		if n > bestN || (n == bestN && k < best) {
			best, bestN = k, n
		}
	}
	return best
}

// sortedMonths turns the month->count map into a chronologically sorted slice,
// filling in any gap months with zero so a bar chart reads as a real timeline.
func sortedMonths(m map[string]int) []Bucket {
	if len(m) == 0 {
		return []Bucket{}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	start, err1 := time.Parse("2006-01", keys[0])
	end, err2 := time.Parse("2006-01", keys[len(keys)-1])
	if err1 != nil || err2 != nil {
		out := make([]Bucket, 0, len(keys))
		for _, k := range keys {
			out = append(out, Bucket{Label: k, Count: m[k]})
		}
		return out
	}

	out := []Bucket{}
	for t := start; !t.After(end); t = t.AddDate(0, 1, 0) {
		key := t.Format("2006-01")
		out = append(out, Bucket{Label: key, Count: m[key]})
		if len(out) > 600 { // safety bound against a corrupt far-future timestamp
			break
		}
	}
	return out
}

// shortID renders a channel ID compactly when no friendly name is available.
func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:4] + "…" + id[len(id)-4:]
}
