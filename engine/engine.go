package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/inherentescapade/viaduct/cfg"
	"github.com/inherentescapade/viaduct/discord"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// maxConcurrentChannels bounds how many channels delete at once. DELETE buckets
// are per-channel, so channels run in parallel; this cap keeps a guild or data
// package with many channels from opening too many request streams at once.
const maxConcurrentChannels = 16

type DeleteJob struct {
	GuildID   string
	GuildName string
	Channels  []discord.Channel // if nil/empty, search guild-wide
	UserID    string
	MaxID     string
	MinID     string
	Before    time.Time
	After     time.Time
	// PreScan enumerates every matching message up front (exact total/ETA) before
	// deleting, instead of streaming deletes as it scans.
	PreScan bool
	// IncludePinned deletes pinned messages too. It defaults to false so pinned
	// messages are kept — they're usually the ones worth preserving, and a bulk
	// delete shouldn't wipe them out unless the user explicitly opts in.
	IncludePinned bool
}

// IsDM reports whether this job targets direct messages rather than a guild.
// DMs have no guild to search across, so each DM channel must be searched
// individually via the channel search endpoint.
func (j DeleteJob) IsDM() bool {
	return j.GuildID == "@me"
}

type Progress struct {
	GuildName   string
	Channel     string // current channel being processed (import mode)
	Total       int
	Deleted     int
	Failed      int
	Skipped     int // messages already gone (e.g. 404 Unknown Message)
	Ignored     int // system messages that can't be deleted, excluded from the target
	RateLimited int
	Done        bool
	Error       error
	StartTime   time.Time
}

type Engine struct {
	Client     *discord.Client
	OnProgress func(Progress)
	OnMessage  func(discord.Message)          // called for each message deleted (or, in dry-run, enumerated)
	OnNotice   func(string)                   // called with human-readable status/decision messages
	OnFailure  func(channelID, reason string) // called for each failed delete, with Discord's reason (code + message)
	logFile    *os.File
	logPath    string // path of the current/most-recent log, survives close
	logDir     string // where logs are written; empty means cfg.LogDir()

	// noticeMu + lastNotice throttle per-channel pacing notices so a busy run
	// doesn't flood the user with rate-adjustment lines. lastIdxNotice throttles
	// the (potentially many, concurrent) search-index wait messages globally.
	// lastSkipNotice + skipSeen do the same for the "skipping system messages"
	// notice, so a DM full of call notices reports live progress rather than
	// looking frozen while it scans past them.
	noticeMu       sync.Mutex
	lastNotice     map[string]time.Time
	lastIdxNotice  time.Time
	lastSkipNotice time.Time
	skipSeen       int
	lastKeepNotice time.Time
	keepSeen       int
	channelNames   map[string]string // channel ID -> display name, for notices

	// indexWait is how long to wait between search retries while Discord's
	// historical index catches up. Configurable so tests can shrink it; New
	// sets the production default.
	indexWait time.Duration

	// reuseLog keeps a single log file open across multiple delete passes (used
	// by ExecuteVerified so its mop-up rounds all append to one file). When
	// false, each Execute opens and closes its own log, matching the CLI/TUI
	// default of one log file per run.
	reuseLog bool

	// failures aggregates delete failures by reason (code + message) so the
	// caller can show a breakdown of what Discord actually returned. Populated
	// by both the live-delete path and the data-package import path.
	failures map[string]int
}

// ReuseLog controls whether the engine keeps one log file open across delete
// passes. Callers that enable it are responsible for calling CloseLog when done.
func (e *Engine) ReuseLog(v bool) { e.reuseLog = v }

// SetLogDir overrides where deletion logs are written. The hosted server uses it
// to give each client its own log directory, so one client's deletion history is
// never readable by another. Empty (the default) means cfg.LogDir().
func (e *Engine) SetLogDir(dir string) { e.logDir = dir }

// CloseLog force-closes the log file regardless of reuse mode.
func (e *Engine) CloseLog() { e.closeLog(true) }

// closeLog closes the log file. In reuse mode it only closes when forced, so a
// run's mop-up passes can keep appending to the same file.
func (e *Engine) closeLog(force bool) {
	if e.logFile == nil {
		return
	}
	if force || !e.reuseLog {
		e.logFile.Close()
		e.logFile = nil
	}
}

func New(token string, botMode bool) *Engine {
	return NewWithClient(discord.NewClient(token, botMode, http.DefaultClient))
}

// NewWithClient builds an Engine over an existing Discord client. Because the
// rate limiter lives in the client, sharing one client across an account's
// concurrent jobs and monitors makes them coordinate on a single limiter — they
// pace the account-wide delete budget together instead of each running an
// independent limiter that just 429s the others. The client is safe for such
// concurrent use; the per-run state (logs, progress) stays on the Engine.
func NewWithClient(client *discord.Client) *Engine {
	return &Engine{
		Client:    client,
		indexWait: 3 * time.Second,
	}
}

// notice emits a human-readable status/decision line if a handler is set.
func (e *Engine) notice(msg string) {
	if e.OnNotice != nil {
		e.OnNotice(msg)
	}
}

// channelParallelismNote describes how N channels are processed, accurately for
// the token type: bot tokens are limited per channel so parallelism multiplies
// throughput; user tokens share one account-wide delete limit, so channels run
// concurrently but the rate is coordinated, not multiplied.
func (e *Engine) channelParallelismNote(n int) string {
	if e.Client.PerChannelLimits() {
		return fmt.Sprintf("%d channels in parallel (up to %d at once), each auto-tuning its own rate", n, maxConcurrentChannels)
	}
	return fmt.Sprintf("%d channels concurrently (up to %d at once) under one auto-tuned account-wide rate", n, maxConcurrentChannels)
}

// chanLabel returns the display name for a channel ID, or "" if unknown.
func (e *Engine) chanLabel(channelID string) string {
	if channelID == "" || e.channelNames == nil {
		return ""
	}
	return e.channelNames[channelID]
}

// indexWaitNotice tells the user we're pausing for Discord's search index to
// catch up — otherwise the run looks frozen. Throttled globally so concurrent
// channels don't flood the feed; carries an elapsed countdown so it's clearly
// alive rather than stuck.
func (e *Engine) indexWaitNotice(channelID string, elapsed time.Duration) {
	if e.OnNotice == nil {
		return
	}
	e.noticeMu.Lock()
	if time.Since(e.lastIdxNotice) < 3*time.Second {
		e.noticeMu.Unlock()
		return
	}
	e.lastIdxNotice = time.Now()
	e.noticeMu.Unlock()

	secs := int(elapsed.Seconds())
	if label := e.chanLabel(channelID); label != "" {
		e.notice(fmt.Sprintf("Waiting for Discord's search index to catch up on #%s… (%ds)", label, secs))
	} else {
		e.notice(fmt.Sprintf("Waiting for Discord's search index to catch up after that batch… (%ds)", secs))
	}
}

// skipNotice tells the user we're scanning past undeletable system messages
// (call notices, joins, pins, ...) rather than sitting idle — otherwise a DM
// with lots of them looks frozen at "waiting for deletions" while the engine
// pages through search results that produce no deletes. It keeps a running
// count and is throttled globally so a long scan reports steady progress
// without flooding the feed.
func (e *Engine) skipNotice(channelID string) {
	if e.OnNotice == nil {
		return
	}
	e.noticeMu.Lock()
	e.skipSeen++
	n := e.skipSeen
	if time.Since(e.lastSkipNotice) < 2*time.Second {
		e.noticeMu.Unlock()
		return
	}
	e.lastSkipNotice = time.Now()
	e.noticeMu.Unlock()

	if label := e.chanLabel(channelID); label != "" {
		e.notice(fmt.Sprintf("Scanning #%s — skipped %d system message(s) that can't be deleted (call notices, joins, pins)…", label, n))
	} else {
		e.notice(fmt.Sprintf("Scanning — skipped %d system message(s) that can't be deleted (call notices, joins, pins)…", n))
	}
}

// keepNotice tells the user we're keeping pinned messages rather than deleting
// them (the default), so a channel full of pins doesn't look like a stalled run
// producing no deletes. Like skipNotice it keeps a running count and is throttled
// globally so a long scan reports steady progress without flooding the feed.
func (e *Engine) keepNotice(channelID string) {
	if e.OnNotice == nil {
		return
	}
	e.noticeMu.Lock()
	e.keepSeen++
	n := e.keepSeen
	if time.Since(e.lastKeepNotice) < 2*time.Second {
		e.noticeMu.Unlock()
		return
	}
	e.lastKeepNotice = time.Now()
	e.noticeMu.Unlock()

	if label := e.chanLabel(channelID); label != "" {
		e.notice(fmt.Sprintf("Scanning #%s — kept %d pinned message(s) (use --include-pinned to delete them)…", label, n))
	} else {
		e.notice(fmt.Sprintf("Scanning — kept %d pinned message(s) (use --include-pinned to delete them)…", n))
	}
}

// wireRateNotices makes the client report its per-channel pacing decisions as
// throttled, human-readable notices (at most one per channel every few seconds).
func (e *Engine) wireRateNotices(job DeleteJob) {
	if e.OnNotice == nil {
		e.Client.OnRateEvent = nil
		return
	}
	names := make(map[string]string, len(job.Channels))
	for _, ch := range job.Channels {
		names[ch.Id] = ch.Name
	}
	e.channelNames = names
	e.lastNotice = make(map[string]time.Time)
	e.lastSkipNotice = time.Time{}
	e.skipSeen = 0
	e.lastKeepNotice = time.Time{}
	e.keepSeen = 0

	e.Client.OnRateEvent = func(ev discord.RateEvent) {
		// Throttle: don't report the same channel more than once every 4s.
		e.noticeMu.Lock()
		if t, ok := e.lastNotice[ev.ChannelID]; ok && time.Since(t) < 4*time.Second {
			e.noticeMu.Unlock()
			return
		}
		e.lastNotice[ev.ChannelID] = time.Now()
		e.noticeMu.Unlock()

		label := names[ev.ChannelID]
		if label == "" {
			label = ev.ChannelID
		}
		gapMs := ev.Gap.Milliseconds()
		if ev.SpedUp {
			e.notice(fmt.Sprintf("#%s steady — speeding up to %dms/msg", label, gapMs))
		} else {
			e.notice(fmt.Sprintf("#%s rate-limited — backing off to %dms/msg (retry %.1fs)", label, gapMs, ev.RetryAfter))
		}
	}
}

func (e *Engine) openLog() error {
	// In reuse mode a log opened by an earlier pass stays open.
	if e.reuseLog && e.logFile != nil {
		return nil
	}
	dir := e.logDir
	if dir == "" {
		dir = cfg.LogDir()
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, fmt.Sprintf("delete_%s.ndjson", time.Now().Format("2006-01-02_150405")))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	e.logFile = f
	e.logPath = path
	return nil
}

func (e *Engine) logMessage(msg discord.Message) {
	if e.logFile == nil {
		return
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	e.logFile.Write(data)
	e.logFile.Write([]byte("\n"))
}

// Preview returns the total message count for the job by summing the count for
// each search scope: a single guild-wide search when no channels are selected,
// or one search per channel for DMs and channel-filtered guild deletions.
func (e *Engine) Preview(ctx context.Context, job DeleteJob) (int, error) {
	maxid, minid := e.resolveIDFilters(job)

	total := 0
	for _, scope := range job.scopes() {
		resp, err := e.searchScope(ctx, job, scope, maxid, minid, 0)
		if err != nil {
			return 0, err
		}
		total += resp.TotalResults
	}
	return total, nil
}

// scopes returns the search scopes for the job. A guild deletion with no
// channels selected uses a single guild-wide scope (channelID ""). DMs, and
// guild deletions filtered to specific channels, use one scope per channel.
func (job DeleteJob) scopes() []string {
	if !job.IsDM() && len(job.Channels) == 0 {
		return []string{""}
	}
	out := make([]string, 0, len(job.Channels))
	for i := range job.Channels {
		out = append(out, job.Channels[i].Id)
	}
	return out
}

// searchScope runs the search appropriate for the job: a per-channel DM search
// when targeting direct messages, or a guild search otherwise. For guilds a
// non-empty channelID narrows the search to that channel; an empty channelID
// searches the whole guild.
func (e *Engine) searchScope(ctx context.Context, job DeleteJob, channelID string, maxid, minid *string, offset int) (*discord.SearchResponse, error) {
	if job.IsDM() {
		return e.Client.GetDMMessages(ctx, channelID, &job.UserID, maxid, minid, offset)
	}
	var channelFilter *string
	if channelID != "" {
		channelFilter = &channelID
	}
	return e.Client.GetMessages(ctx, job.GuildID, channelFilter, &job.UserID, maxid, minid, offset, true)
}

func (e *Engine) Execute(ctx context.Context, job DeleteJob) error {
	if err := e.openLog(); err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer e.closeLog(false)
	e.failures = map[string]int{}
	_, err := e.runDelete(ctx, job)
	return err
}

// ExecuteVerified runs a deletion and then verifies completion: it re-queries
// the remaining count and re-runs deletion for any stragglers (index lag,
// transient failures) until the count reaches zero or maxPasses is exhausted.
// All passes share a single log file. It returns the residual count (0 means
// fully verified) and any fatal error from the initial delete.
//
// onVerify, if non-nil, is called once when the verification phase begins.
func (e *Engine) ExecuteVerified(ctx context.Context, job DeleteJob, maxPasses int, onVerify func()) (int, error) {
	e.ReuseLog(true)
	if err := e.openLog(); err != nil {
		return 0, fmt.Errorf("failed to open log file: %w", err)
	}
	defer e.closeLog(true)
	e.failures = map[string]int{}

	ignored, err := e.runDelete(ctx, job)
	if err != nil {
		return 0, err
	}

	if onVerify != nil {
		onVerify()
	}

	remaining := 0
	prev := -1
	for pass := 0; pass < maxPasses; pass++ {
		if ctx.Err() != nil {
			break
		}
		// Give Discord's search index a moment to reflect the deletions.
		if !sleepCtx(ctx, e.indexWait) {
			break
		}
		n, err := e.Preview(ctx, job)
		if err != nil {
			break
		}
		// Preview counts undeletable system messages too (they always match the
		// search), so discount the ones the last pass ignored — otherwise a run
		// with, say, one call notice would never verify as complete.
		remaining = n - ignored
		if remaining < 0 {
			remaining = 0
		}
		if remaining == 0 {
			break
		}
		// No fewer than last pass — the remaining messages are stuck (e.g.
		// permission-blocked). Stop rather than re-attempt them indefinitely.
		if prev >= 0 && n >= prev {
			break
		}
		prev = n
		ignored, err = e.runDelete(ctx, job)
		if err != nil {
			break
		}
	}
	return remaining, nil
}

// sleepCtx sleeps for d, returning false if the context is cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// runDelete performs one full delete pass over the job. The log file is assumed
// to already be open. It emits progress throughout and a final Done, and returns
// how many system messages it ignored (excluded from the target) this pass.
func (e *Engine) runDelete(ctx context.Context, job DeleteJob) (int, error) {
	if job.PreScan {
		return e.runDeletePrescan(ctx, job)
	}

	maxid, minid := e.resolveIDFilters(job)

	progress := Progress{
		GuildName: job.GuildName,
		StartTime: time.Now(),
	}

	scopes := job.scopes()
	e.wireRateNotices(job)

	// No up-front counting pass. Previously we searched every scope first, purely
	// to sum a grand total before deleting anything. With many channels that's a
	// long SERIAL stall — search is heavily rate-limited and shared per account,
	// so each successive count takes longer and the job looks frozen for minutes
	// before the first delete. It was also redundant: deleteScope searches each
	// scope itself anyway. Now each scope's own first search contributes its
	// total (see deleteScope), so deletion starts on the first channel right away
	// and Progress.Total fills in live as the remaining scopes begin.
	if len(scopes) == 0 {
		progress.Done = true
		e.emit(progress)
		return 0, nil
	}

	e.emit(progress)

	if len(scopes) == 1 {
		e.notice("Deleting your messages; auto-tuning the rate from Discord's responses.")
		if err := e.deleteScope(ctx, job, scopes[0], maxid, minid, &progress, nil); err != nil {
			progress.Done = true
			e.emit(progress)
			return progress.Ignored, err
		}
	} else {
		e.notice(fmt.Sprintf("Deleting your messages across %s — tallying each channel as it starts, so deletion begins right away instead of counting all %d first.", e.channelParallelismNote(len(scopes)), len(scopes)))
		var (
			mu       sync.Mutex
			firstErr error
			wg       sync.WaitGroup
		)
		// Per-channel limiters let channels delete in parallel; maxConcurrentChannels
		// bounds how many run at once so a guild with dozens of channels doesn't
		// open dozens of concurrent request streams at the same instant.
		sem := make(chan struct{}, maxConcurrentChannels)
		for _, scope := range scopes {
			scope := scope
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				if err := e.deleteScope(ctx, job, scope, maxid, minid, &progress, &mu); err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		if firstErr != nil {
			progress.Done = true
			e.emit(progress)
			return progress.Ignored, firstErr
		}
	}

	progress.Done = true
	e.emit(progress)
	return progress.Ignored, nil
}

// deleteScope deletes every matching message in a single search scope, looping
// at offset 0 because each deletion shrinks the result set. For guilds the
// scope is guild-wide (channelID is ""); for DMs it is a single channel.
// mu, if non-nil, guards concurrent access to progress when multiple scopes run
// in parallel.
func (e *Engine) deleteScope(ctx context.Context, job DeleteJob, channelID string, maxid, minid *string, progress *Progress, mu *sync.Mutex) error {
	// Walk newest -> oldest by advancing a max_id cursor below the oldest message
	// handled so far. Each page is strictly older than the last, so we never
	// re-query a range we've already deleted from — re-querying would stall
	// waiting for Discord's search index to catch up with the deletions.
	// Anything genuinely missed (a transient failure) is swept up by the
	// verification passes in ExecuteVerified.
	cursor := ""
	if maxid != nil {
		cursor = *maxid
	}
	deepWaits := 0
	counted := false

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var cursorPtr *string
		if cursor != "" {
			cursorPtr = &cursor
		}
		messages, err := e.searchScope(ctx, job, channelID, cursorPtr, minid, 0)
		if err != nil {
			return err
		}

		if messages.DoingDeepHistoricalIndex {
			deepWaits++
			if deepWaits > 20 {
				return nil
			}
			e.indexWaitNotice(channelID, 0)
			if !sleepCtx(ctx, e.indexWait) {
				return ctx.Err()
			}
			continue
		}
		deepWaits = 0

		// The first real page for this scope reports the scope's total; fold it
		// into shared Progress once so Total accumulates as scopes start, without
		// a separate up-front counting pass. Later pages report a shrinking total
		// (we're deleting as we walk), so only the first one is authoritative.
		if !counted {
			counted = true
			e.addTotal(messages.TotalResults, progress, mu)
		}

		// Empty page means we've walked past the oldest matching message. Since we
		// only ever move OLDER, this is a true end-of-history, not index lag —
		// older messages aren't affected by the deletions we just made.
		if len(messages.Messages) == 0 {
			return nil
		}

		var oldest string
		for _, cluster := range messages.Messages {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			msg, ok := clusterHit(cluster, job.UserID)
			if !ok {
				continue
			}
			oldest = msg.Id

			// Pinned messages are kept unless the user opts in: they're usually the
			// ones worth preserving, so treat them as non-targets rather than
			// deleting them in a bulk sweep.
			if msg.Pinned && !job.IncludePinned {
				e.applyIgnored(progress, mu)
				e.keepNotice(channelID)
				continue
			}

			// System messages (call notices, group add/remove, pins, ...) can't be
			// deleted; don't waste a DELETE request on them, and don't let them
			// count toward the total or the failures.
			if isSystemMessage(msg) {
				e.applyIgnored(progress, mu)
				e.skipNotice(channelID)
				continue
			}

			res, err := e.Client.DeleteMessages(msg.ChannelId, msg.Id)
			e.applyDelete(msg, res, err, progress, mu)
		}

		// No matching messages in this page — nothing older to anchor on.
		if oldest == "" {
			return nil
		}
		// Advance strictly below the oldest message seen so the next page is older
		// and excludes everything already processed.
		cursor = decrementSnowflake(oldest)
	}
}

// systemMessageTypes are Discord message types that are system-generated and
// cannot be deleted through the message-delete endpoint — Discord rejects them
// with 50021 "Cannot execute action on a system message". These are the
// long-stable classic types (call notices, group recipient add/remove, channel
// name/icon changes, pins, joins, boosts, thread-created). It is a DENY-list on
// purpose: any type NOT listed is still attempted normally, so a user-deletable
// type is never silently left behind, and the 50021 handler in applyDelete
// catches any undeletable type this list happens to miss.
var systemMessageTypes = map[int]bool{
	1:  true, // RECIPIENT_ADD
	2:  true, // RECIPIENT_REMOVE
	3:  true, // CALL
	4:  true, // CHANNEL_NAME_CHANGE
	5:  true, // CHANNEL_ICON_CHANGE
	6:  true, // CHANNEL_PINNED_MESSAGE
	7:  true, // USER_JOIN
	8:  true, // GUILD_BOOST
	9:  true, // GUILD_BOOST_TIER_1
	10: true, // GUILD_BOOST_TIER_2
	11: true, // GUILD_BOOST_TIER_3
	12: true, // CHANNEL_FOLLOW_ADD
	18: true, // THREAD_CREATED
}

// isSystemMessage reports whether a message is a non-deletable system message
// (a call notice, group recipient add/remove, pin, boost, ...). The search API
// surfaces these because they list the acting user as their author, but Discord
// refuses to delete them; skipping them up front avoids spending a DELETE
// request — and a rate-limit slot — on a message that could only ever 50021.
func isSystemMessage(msg discord.Message) bool {
	return systemMessageTypes[msg.Type]
}

// isUndeletable reports whether a failed delete's code means the message is not
// a real, deletable target and should be ignored rather than counted as a
// failure: 50021 (system message Discord won't delete). It's the safety net for
// any system type not caught by isSystemMessage up front.
func isUndeletable(res *discord.DeleteResponse) bool {
	return res != nil && res.Code == 50021
}

// ignore adjusts the counters for a message that isn't a real deletion target
// (a system message): it drops one off the target total and tallies it as
// ignored, so system messages never inflate the total or show up as failures.
func ignore(progress *Progress) {
	progress.Ignored++
	if progress.Total > 0 {
		progress.Total--
	}
}

// applyIgnored records that a message was skipped as a non-target (system
// message) without attempting a delete, taking mu when parallel scopes share
// one progress.
func (e *Engine) applyIgnored(progress *Progress, mu *sync.Mutex) {
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	ignore(progress)
	e.emit(*progress)
}

// clusterHit picks the message within a search-result cluster that actually
// matched the query. Discord flags it with Hit == true; it isn't necessarily
// at index 0, since a cluster can include surrounding context messages from
// other authors (common in group DMs). Falls back to the first message
// authored by userID if no Hit flag is set, and reports ok=false if neither
// is found.
func clusterHit(cluster []discord.Message, userID string) (msg discord.Message, ok bool) {
	for _, m := range cluster {
		if m.Hit {
			if m.Author.Id != userID {
				return discord.Message{}, false
			}
			return m, true
		}
	}
	for _, m := range cluster {
		if m.Author.Id == userID {
			return m, true
		}
	}
	return discord.Message{}, false
}

// decrementSnowflake returns the snowflake one less than id, for use as an
// exclusive max_id upper bound on the next page. Returns id unchanged if it
// can't be parsed.
func decrementSnowflake(id string) string {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return id
	}
	return strconv.FormatInt(n-1, 10)
}

// addTotal folds a scope's message total into the shared Progress and emits the
// update, taking mu when non-nil so parallel scopes don't race on Progress.Total.
func (e *Engine) addTotal(n int, progress *Progress, mu *sync.Mutex) {
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	progress.Total += n
	e.emit(*progress)
}

// applyDelete records the outcome of one delete attempt into progress, taking mu
// when non-nil (deletes from parallel scopes share one progress).
func (e *Engine) applyDelete(msg discord.Message, res *discord.DeleteResponse, err error, progress *Progress, mu *sync.Mutex) {
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	if err != nil {
		switch {
		case res != nil && res.Code == 10008:
			// Unknown Message — already gone. Count as skipped, not failed.
			progress.Skipped++
		case isUndeletable(res):
			// System message (50021) that slipped past the up-front type filter.
			// It's not a real target, so ignore it for the counts entirely rather
			// than logging a failure that can never be fixed.
			ignore(progress)
		default:
			progress.Failed++
			// Record WHY it failed (Discord's code + message) into the log and the
			// aggregated summary, so a bare "N failed" becomes explainable — e.g. a
			// group DM the account can't act in — instead of a silent count.
			e.recordFailure(msg.ChannelId, e.chanLabel(msg.ChannelId), msg.Id, res, err)
		}
		e.emit(*progress)
		return
	}
	if res != nil {
		progress.RateLimited++
		if res.Code == 50013 {
			// Missing permissions — this will never succeed.
			progress.Failed++
		}
		e.emit(*progress)
		return
	}
	e.logMessage(msg)
	e.emitMessage(msg)
	progress.Deleted++
	e.emit(*progress)
}

// collectScope walks a scope newest -> oldest (descending max_id cursor) and
// returns every matching message without deleting anything. Used by the pre-scan
// path to get an exact total before deletion begins. System messages are left
// out of the collected list (they can't be deleted); the count of those skipped
// is returned so the caller can keep them out of the total too.
func (e *Engine) collectScope(ctx context.Context, job DeleteJob, channelID string, maxid, minid *string) ([]discord.Message, int, error) {
	var out []discord.Message
	ignored := 0
	cursor := ""
	if maxid != nil {
		cursor = *maxid
	}
	deepWaits := 0

	for {
		select {
		case <-ctx.Done():
			return out, ignored, ctx.Err()
		default:
		}

		var cursorPtr *string
		if cursor != "" {
			cursorPtr = &cursor
		}
		messages, err := e.searchScope(ctx, job, channelID, cursorPtr, minid, 0)
		if err != nil {
			return out, ignored, err
		}
		if messages.DoingDeepHistoricalIndex {
			deepWaits++
			if deepWaits > 20 {
				return out, ignored, nil
			}
			e.indexWaitNotice(channelID, 0)
			if !sleepCtx(ctx, e.indexWait) {
				return out, ignored, ctx.Err()
			}
			continue
		}
		deepWaits = 0

		if len(messages.Messages) == 0 {
			return out, ignored, nil
		}

		var oldest string
		for _, cluster := range messages.Messages {
			msg, ok := clusterHit(cluster, job.UserID)
			if !ok {
				continue
			}
			oldest = msg.Id
			if msg.Pinned && !job.IncludePinned {
				ignored++
				e.keepNotice(channelID)
				continue
			}
			if isSystemMessage(msg) {
				ignored++
				e.skipNotice(channelID)
				continue
			}
			out = append(out, msg)
		}
		if oldest == "" {
			return out, ignored, nil
		}
		cursor = decrementSnowflake(oldest)
	}
}

// deleteCollected deletes a pre-enumerated slice of messages by ID, paced by the
// client's per-channel limiters. progress is shared, so all access goes via mu.
func (e *Engine) deleteCollected(ctx context.Context, msgs []discord.Message, progress *Progress, mu *sync.Mutex) error {
	for _, msg := range msgs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		res, err := e.Client.DeleteMessages(msg.ChannelId, msg.Id)
		e.applyDelete(msg, res, err, progress, mu)
	}
	return nil
}

// runDeletePrescan enumerates every matching message first (exact total/ETA),
// then deletes the collected list with channels running in parallel. It returns
// how many system messages were excluded from the target.
func (e *Engine) runDeletePrescan(ctx context.Context, job DeleteJob) (int, error) {
	maxid, minid := e.resolveIDFilters(job)
	progress := Progress{GuildName: job.GuildName, StartTime: time.Now()}
	scopes := job.scopes()
	e.wireRateNotices(job)

	e.notice("Scanning your full message list before deleting anything…")
	collected := make([][]discord.Message, len(scopes))
	for i, scope := range scopes {
		msgs, ignored, err := e.collectScope(ctx, job, scope, maxid, minid)
		if err != nil {
			return progress.Ignored, err
		}
		collected[i] = msgs
		progress.Total += len(msgs)
		progress.Ignored += ignored
	}

	if progress.Total == 0 {
		progress.Done = true
		e.emit(progress)
		return progress.Ignored, nil
	}
	e.notice(fmt.Sprintf("Found %d messages — now deleting across %s.", progress.Total, e.channelParallelismNote(len(scopes))))
	e.emit(progress)

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		firstErr error
	)
	sem := make(chan struct{}, maxConcurrentChannels)
	for i := range collected {
		msgs := collected[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := e.deleteCollected(ctx, msgs, &progress, &mu); err != nil {
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
	return progress.Ignored, firstErr
}

// Enumerate walks every message the job would delete and reports each one via
// the OnMessage callback, without deleting anything. It paginates with an
// offset so it works as a non-destructive dry-run listing. Returns the number
// of messages enumerated.
func (e *Engine) Enumerate(ctx context.Context, job DeleteJob) (int, error) {
	maxid, minid := e.resolveIDFilters(job)

	count := 0
	for _, scope := range job.scopes() {
		offset := 0
		retries := 0
		for {
			select {
			case <-ctx.Done():
				return count, ctx.Err()
			default:
			}

			resp, err := e.searchScope(ctx, job, scope, maxid, minid, offset)
			if err != nil {
				return count, err
			}
			if resp.DoingDeepHistoricalIndex {
				retries++
				if retries > 12 {
					return count, fmt.Errorf("search index timed out")
				}
				time.Sleep(e.indexWait)
				continue
			}
			if len(resp.Messages) == 0 {
				break
			}

			for _, cluster := range resp.Messages {
				msg, ok := clusterHit(cluster, job.UserID)
				if !ok {
					continue
				}
				// A dry run should list what would actually be deleted, so leave out
				// system messages the real run would skip, and pinned messages it
				// keeps by default.
				if msg.Pinned && !job.IncludePinned {
					continue
				}
				if isSystemMessage(msg) {
					continue
				}
				e.emitMessage(msg)
				count++
			}

			offset += len(resp.Messages)
			if offset >= resp.TotalResults {
				break
			}
		}
	}
	return count, nil
}

func (e *Engine) emitMessage(msg discord.Message) {
	if e.OnMessage != nil {
		e.OnMessage(msg)
	}
}

func (e *Engine) resolveIDFilters(job DeleteJob) (maxid, minid *string) {
	if job.MaxID != "" {
		maxid = &job.MaxID
	} else if !job.Before.IsZero() {
		s := discord.TimeToSnowflake(job.Before)
		maxid = &s
	}

	if job.MinID != "" {
		minid = &job.MinID
	} else if !job.After.IsZero() {
		s := discord.TimeToSnowflake(job.After)
		minid = &s
	}

	return
}

func (e *Engine) emit(p Progress) {
	if e.OnProgress != nil {
		e.OnProgress(p)
	}
}

func (e *Engine) LogPath() string {
	return e.logPath
}
