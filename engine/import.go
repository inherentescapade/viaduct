package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/inherentescapade/viaduct/discord"
	"github.com/inherentescapade/viaduct/export"
)

// ImportJob deletes messages enumerated in a Discord data export rather than
// discovered through the search API. Channels should already be filtered to
// the set the user wants to delete from; Before/After further restrict by
// message time.
type ImportJob struct {
	Channels []export.Channel
	Before   time.Time
	After    time.Time
}

// CountImport returns how many messages the job would attempt to delete after
// applying the time filters.
func (e *Engine) CountImport(job ImportJob) int {
	n := 0
	for i := range job.Channels {
		for _, msg := range job.Channels[i].Messages {
			if e.messagePasses(msg, job.Before, job.After) {
				n++
			}
		}
	}
	return n
}

func (e *Engine) messagePasses(msg export.Message, before, after time.Time) bool {
	if msg.Timestamp.IsZero() {
		return true // can't filter what we can't date; don't silently drop it
	}
	if !before.IsZero() && !msg.Timestamp.Before(before) {
		return false
	}
	if !after.IsZero() && !msg.Timestamp.After(after) {
		return false
	}
	return true
}

// ExecuteImport deletes every message in the job, channel by channel, reusing
// the client's rate-limit backoff. Messages that are already gone (Discord
// returns 404 / code 10008) are counted as skipped rather than failed.
func (e *Engine) ExecuteImport(ctx context.Context, job ImportJob) error {
	if err := e.openLog(); err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	if e.logFile != nil {
		defer e.logFile.Close()
	}

	e.failures = map[string]int{}

	progress := Progress{
		Total:     e.CountImport(job),
		StartTime: time.Now(),
	}

	if progress.Total == 0 {
		progress.Done = true
		e.emit(progress)
		return nil
	}
	e.emit(progress)

	// The data package already lists every message ID, so there's no searching or
	// pagination. Channels run concurrently (bounded); whether that multiplies
	// throughput depends on the token type (see channelParallelismNote).
	if len(job.Channels) > 1 {
		e.notice(fmt.Sprintf("Deleting %d messages straight from your data package (no searching), across %s.",
			progress.Total, e.channelParallelismNote(len(job.Channels))))
	} else {
		e.notice(fmt.Sprintf("Deleting %d messages from your data package — no searching.", progress.Total))
	}

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		firstErr error
	)
	sem := make(chan struct{}, maxConcurrentChannels)
	for ci := range job.Channels {
		ch := job.Channels[ci]
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := e.deleteImportChannel(ctx, job, ch, &progress, &mu); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	progress.Done = true
	e.emit(progress)
	return firstErr
}

// deleteImportChannel deletes one export channel's messages by ID. progress and
// the log are shared across channels, so all access goes through mu.
func (e *Engine) deleteImportChannel(ctx context.Context, job ImportJob, ch export.Channel, progress *Progress, mu *sync.Mutex) error {
	for _, msg := range ch.Messages {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if !e.messagePasses(msg, job.Before, job.After) {
			continue
		}

		res, err := e.Client.DeleteMessages(ch.ID, msg.ID)

		mu.Lock()
		progress.Channel = ch.Name
		if err != nil {
			switch {
			case res != nil && res.Code == 10008:
				// Unknown Message — already deleted elsewhere.
				progress.Skipped++
			case isUndeletable(res):
				// System message Discord won't delete — not a real target.
				ignore(progress)
			default:
				progress.Failed++
				e.logFailure(ch, msg, res, err)
			}
			e.emit(*progress)
			mu.Unlock()
			continue
		}

		e.logImported(ch, msg)
		progress.Deleted++
		e.emit(*progress)
		mu.Unlock()
	}
	return nil
}

// logImported writes a record of a deleted export message to the NDJSON log.
func (e *Engine) logImported(ch export.Channel, msg export.Message) {
	e.logMessage(discord.Message{
		Id:        msg.ID,
		ChannelId: ch.ID,
		Content:   msg.Content,
		Timestamp: msg.Timestamp,
	})
}

// failureReason renders the actual Discord response (code + message) for a
// failed delete, falling back to the raw error when there's no parsed body.
func failureReason(res *discord.DeleteResponse, err error) string {
	if res != nil && res.Message != "" {
		return fmt.Sprintf("%d %s", res.Code, res.Message)
	}
	if err != nil {
		return err.Error()
	}
	return "unknown error"
}

// failureRecord is one line in the NDJSON log describing a delete that failed.
type failureRecord struct {
	Event     string `json:"event"`
	ChannelID string `json:"channel_id"`
	Channel   string `json:"channel"`
	MessageID string `json:"message_id"`
	Code      int    `json:"code,omitempty"`
	Error     string `json:"error"`
}

// logFailure records why an imported delete failed. It delegates to
// recordFailure, shared with the live-delete path.
func (e *Engine) logFailure(ch export.Channel, msg export.Message, res *discord.DeleteResponse, err error) {
	e.recordFailure(ch.ID, ch.Name, msg.ID, res, err)
}

// recordFailure aggregates a failed delete's reason for the end-of-run summary
// and appends a full delete_failed record to the NDJSON log, so logstats /
// Insights and anyone tailing the log can see exactly what Discord returned.
// channelName may be empty (e.g. a guild-wide search hit an unlabelled channel);
// the channel ID is always recorded so the failure is still attributable.
func (e *Engine) recordFailure(channelID, channelName, messageID string, res *discord.DeleteResponse, err error) {
	reason := failureReason(res, err)
	if e.failures != nil {
		e.failures[reason]++
	}
	// Fire the live hook first, so a caller watching failures learns of each one
	// as it happens rather than only in the end-of-run summary.
	if e.OnFailure != nil {
		e.OnFailure(channelID, reason)
	}

	if e.logFile == nil {
		return
	}
	rec := failureRecord{
		Event:     "delete_failed",
		ChannelID: channelID,
		Channel:   channelName,
		MessageID: messageID,
		Error:     reason,
	}
	if res != nil {
		rec.Code = res.Code
	}
	data, mErr := json.Marshal(rec)
	if mErr != nil {
		return
	}
	e.logFile.Write(data)
	e.logFile.Write([]byte("\n"))
}

// FailReason is an aggregated failure cause and how many messages hit it.
type FailReason struct {
	Reason string
	Count  int
}

// FailureSummary returns the delete failures from the last run (live delete or
// data-package import), ordered from most to least common.
func (e *Engine) FailureSummary() []FailReason {
	out := make([]FailReason, 0, len(e.failures))
	for reason, count := range e.failures {
		out = append(out, FailReason{Reason: reason, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}
