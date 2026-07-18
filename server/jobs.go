package server

import (
	"context"
	"fmt"
	"github.com/inherentescapade/viaduct/discord"
	"github.com/inherentescapade/viaduct/engine"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// feedCap bounds how many recent deletions a job or monitor keeps for its live
// activity feed.
const feedCap = 60

// maxExportBytes caps a single-shot downloaded job log so it fits inside the RPC
// envelope. Larger logs come back truncated (flagged in the response); the
// chunked export path (exportChunk) avoids the cap entirely.
const maxExportBytes = 1 << 30

// exportChunkBytes is how many raw log bytes one streamed export chunk carries.
// Small enough to keep both sides' memory flat and progress smooth on a large
// log; the client requests chunks in sequence until EOF.
const exportChunkBytes = 1 << 20 // 1 MiB

// channelLabels maps channel IDs to a friendly label for the feed (a #name, or a
// DM recipient's name).
func channelLabels(channels []discord.Channel) map[string]string {
	names := make(map[string]string, len(channels))
	for _, ch := range channels {
		names[ch.Id] = labelForChannel(ch)
	}
	return names
}

// labelForChannel renders a channel as a LOCATION for the deletion feed, never
// as an author. A named channel/group is "#name"; a 1:1 DM is "DM · <name>"; a
// group DM is "group · a, b" — so the partner's name clearly marks the
// conversation, not who wrote the (always-yours) deleted message.
func labelForChannel(ch discord.Channel) string {
	if ch.Name != "" {
		return "#" + ch.Name
	}
	var names []string
	for _, r := range ch.Recipients {
		n := r.GlobalName
		if n == "" {
			n = r.Username
		}
		if n != "" {
			names = append(names, n)
		}
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return "DM · " + names[0]
	default:
		return "group · " + strings.Join(names, ", ")
	}
}

// appendFeed adds a deleted message to a capped, newest-last feed buffer.
func appendFeed(feed []FeedMessage, msg discord.Message, names map[string]string) []FeedMessage {
	label := names[msg.ChannelId]
	author := msg.Author.GlobalName
	if author == "" {
		author = msg.Author.Username
	}
	feed = append(feed, FeedMessage{
		Content:      msg.Content,
		Channel:      label,
		Timestamp:    msg.Timestamp,
		Author:       author,
		AuthorID:     msg.Author.Id,
		AuthorAvatar: msg.Author.Avatar,
	})
	if len(feed) > feedCap {
		feed = feed[len(feed)-feedCap:]
	}
	return feed
}

// jobManager tracks one-shot deletion jobs running in the background. Jobs
// survive for the life of the process so a client can poll their progress after
// dispatching and disconnecting.
type jobManager struct {
	mu    sync.Mutex
	jobs  map[string]*trackedJob
	seq   int
	build func(Credentials) (*engine.Engine, *resolver, error)
	logf  func(string, ...any)
}

type trackedJob struct {
	status   JobStatus
	cancel   context.CancelFunc
	deleted  int    // real deletions counted via OnMessage (survives verify passes)
	rawTotal int    // largest raw scope total seen (before excluding system messages)
	logPath  string // NDJSON deletion log this job wrote, for export download

	// kind + spec are the original request that created this job, kept so a
	// failed or canceled job can be resubmitted as-is via retry.
	kind JobKind
	spec DeleteSpec

	// loggedReasons tracks which distinct Discord failure reasons have already
	// been logged live for this job, so each one is announced the moment it
	// first appears but never repeated per-message (a job failing a million
	// times prints one line, not a million).
	loggedReasons map[string]bool
}

func newJobManager(build func(Credentials) (*engine.Engine, *resolver, error), logf func(string, ...any)) *jobManager {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &jobManager{jobs: make(map[string]*trackedJob), build: build, logf: logf}
}

// submit resolves the spec, registers a job, and starts deletion in the
// background. Resolution happens synchronously so the caller learns about bad
// targets (server not found, no DMs with that user) immediately; the deletion
// itself runs async.
func (m *jobManager) submit(creds Credentials, kind JobKind, spec DeleteSpec) (JobStatus, error) {
	eng, res, err := m.build(creds)
	if err != nil {
		return JobStatus{}, err
	}
	job, label, err := res.buildDeleteJob(kind, spec)
	if err != nil {
		return JobStatus{}, err
	}

	m.mu.Lock()
	m.seq++
	id := fmt.Sprintf("job-%d", m.seq)
	ctx, cancel := context.WithCancel(context.Background())
	tj := &trackedJob{
		status: JobStatus{
			ID:          id,
			Kind:        kind,
			Description: label,
			State:       StatePending,
			Created:     time.Now(),
			Updated:     time.Now(),
		},
		cancel:        cancel,
		kind:          kind,
		spec:          spec,
		loggedReasons: map[string]bool{},
	}
	m.jobs[id] = tj
	m.mu.Unlock()

	m.logf("job %s submitted: %s [%s], %d channel(s)%s", id, label, kind, len(job.Channels), verifyNote(spec.Verify))

	names := channelLabels(job.Channels)

	// Announce each distinct failure reason the moment it first occurs, so a
	// job going wrong is visible on the server log immediately — not only when
	// it finishes. Deduped per reason (see firstFailure) so it can't flood.
	eng.OnFailure = func(channelID, reason string) {
		if !m.firstFailure(tj, reason) {
			return
		}
		if loc := names[channelID]; loc != "" {
			m.logf("job %s first failure in %s: %s", id, loc, reason)
		} else {
			m.logf("job %s first failure: %s", id, reason)
		}
	}

	// Count deletions from OnMessage (fires once per real delete) rather than
	// p.Deleted, which the engine resets to 0 on each verification mop-up pass —
	// counting from p.Deleted would make a finished job report "0/X".
	eng.OnMessage = func(msg discord.Message) {
		m.mu.Lock()
		tj.deleted++
		tj.status.Deleted = tj.deleted
		tj.status.Recent = appendFeed(tj.status.Recent, msg, names)
		tj.status.Updated = time.Now()
		m.mu.Unlock()
	}

	eng.OnProgress = func(p engine.Progress) {
		m.recordProgress(tj, p)
	}

	go m.run(ctx, tj, eng, job, spec.Verify)

	m.mu.Lock()
	defer m.mu.Unlock()
	return tj.status, nil
}

// recordProgress folds one engine Progress snapshot into the job's status. The
// engine already discounts undeletable system messages from p.Total, so
// p.Total+p.Ignored is the raw scope total. It tracks the largest raw total
// across verify mop-up passes (each re-reports a shrinking total), then
// subtracts this pass's ignored count — so the reported target never counts
// system messages that can't be deleted.
func (m *jobManager) recordProgress(tj *trackedJob, p engine.Progress) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tj.status.State = StateRunning
	if raw := p.Total + p.Ignored; raw > tj.rawTotal {
		tj.rawTotal = raw
	}
	tj.status.Total = tj.rawTotal - p.Ignored
	tj.status.Ignored = p.Ignored
	tj.status.Failed = p.Failed
	tj.status.Skipped = p.Skipped
	tj.status.Updated = time.Now()
}

func (m *jobManager) run(ctx context.Context, tj *trackedJob, eng *engine.Engine, job engine.DeleteJob, verify bool) {
	var residual int
	var err error
	if verify {
		residual, err = eng.ExecuteVerified(ctx, job, 5, nil)
	} else {
		err = eng.Execute(ctx, job)
	}

	m.mu.Lock()
	tj.status.Updated = time.Now()
	tj.status.Residual = residual
	if lp := eng.LogPath(); lp != "" {
		tj.logPath = lp
		tj.status.HasExport = true
	}
	switch {
	case ctx.Err() != nil:
		tj.status.State = StateCanceled
	case err != nil:
		tj.status.State = StateFailed
		tj.status.Error = err.Error()
	default:
		tj.status.State = StateDone
	}
	st := tj.status
	m.mu.Unlock()

	// Log the outcome — and, if any deletes failed, exactly what Discord
	// returned — so an operator watching the server can see why a job (a group
	// DM, say) came up empty instead of having to poll status from a client.
	m.logOutcome(st, err, eng.FailureSummary())
}

// firstFailure records reason against the job and reports whether this is the
// first time it's been seen, so the caller logs each distinct Discord failure
// reason live exactly once instead of once per failed message.
func (m *jobManager) firstFailure(tj *trackedJob, reason string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if tj.loggedReasons == nil {
		tj.loggedReasons = map[string]bool{}
	}
	if tj.loggedReasons[reason] {
		return false
	}
	tj.loggedReasons[reason] = true
	return true
}

// verifyNote annotates a job's log line when verification passes are enabled.
func verifyNote(verify bool) string {
	if verify {
		return ", verify on"
	}
	return ""
}

// logOutcome writes a job's terminal state and a per-reason failure breakdown
// (most common first) to the server log.
func (m *jobManager) logOutcome(st JobStatus, err error, fails []engine.FailReason) {
	switch st.State {
	case StateCanceled:
		m.logf("job %s canceled: %d deleted, %d failed, %d skipped, %d ignored", st.ID, st.Deleted, st.Failed, st.Skipped, st.Ignored)
	case StateFailed:
		m.logf("job %s failed: %v — %d deleted, %d failed, %d skipped, %d ignored", st.ID, err, st.Deleted, st.Failed, st.Skipped, st.Ignored)
	default:
		m.logf("job %s done: %d deleted, %d failed, %d skipped, %d ignored, %d residual", st.ID, st.Deleted, st.Failed, st.Skipped, st.Ignored, st.Residual)
	}
	for _, fr := range fails {
		m.logf("job %s failure reason: %s (x%d)", st.ID, fr.Reason, fr.Count)
	}
}

func (m *jobManager) list() []JobStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]JobStatus, 0, len(m.jobs))
	for _, tj := range m.jobs {
		out = append(out, withETA(tj.status))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}

func (m *jobManager) get(id string) (JobStatus, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tj, ok := m.jobs[id]
	if !ok {
		return JobStatus{}, false
	}
	return withETA(tj.status), true
}

// withETA fills in RatePerSec and EtaMs for a running job, computed from
// elapsed time since Created and messages deleted so far — the same formula
// the desktop app uses for local deletions (see toProgressDTO), applied
// here so remote jobs get an ETA despite the client only seeing polled
// snapshots rather than a live stream.
func withETA(status JobStatus) JobStatus {
	if status.State != StateRunning || status.Deleted <= 0 {
		return status
	}
	elapsed := time.Since(status.Created).Seconds()
	if elapsed <= 0 {
		return status
	}
	rate := float64(status.Deleted) / elapsed
	remaining := status.Total - status.Deleted - status.Failed - status.Skipped
	if rate <= 0 || remaining <= 0 {
		return status
	}
	status.RatePerSec = rate
	status.EtaMs = int64(float64(remaining) / rate * 1000)
	return status
}

// cancel signals a running job to stop and immediately flips it to the
// transient "canceling" state, so the API response and the very next client
// poll both reflect that the stop took effect — even though the background
// goroutine only reaches its cancellation checkpoint (and writes the terminal
// "canceled" line) once the in-flight delete unwinds. Cancelling is cooperative
// and rate-limited, so that gap can be a second or more; surfacing "canceling"
// closes the feedback gap without pretending the job is already done.
func (m *jobManager) cancel(id string) (JobStatus, bool) {
	m.mu.Lock()
	tj, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return JobStatus{}, false
	}
	// Only a still-active job transitions to canceling; a job that already
	// reached a terminal state (it finished the instant the user clicked) keeps
	// that state rather than being dragged back into canceling.
	if s := tj.status.State; s == StateRunning || s == StatePending {
		tj.status.State = StateCanceling
		tj.status.Updated = time.Now()
	}
	st := tj.status
	m.mu.Unlock()

	tj.cancel()
	m.logf("job %s cancel requested", id)
	return st, true
}

// retry resubmits a failed or canceled job's original spec as a brand-new job,
// so a transient failure (e.g. exhausted rate-limit patience, a dropped
// connection) doesn't require the user to re-enter the target by hand. The
// old job entry is left alone; the new job gets its own ID and shows up
// alongside it in the list.
func (m *jobManager) retry(creds Credentials, id string) (JobStatus, error) {
	m.mu.Lock()
	tj, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return JobStatus{}, fmt.Errorf("job %q not found", id)
	}
	state := tj.status.State
	kind, spec := tj.kind, tj.spec
	m.mu.Unlock()

	if state != StateFailed && state != StateCanceled {
		return JobStatus{}, fmt.Errorf("job %q is %s; only a failed or canceled job can be retried", id, state)
	}

	return m.submit(creds, kind, spec)
}

func (m *jobManager) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.jobs)
}

// export reads a finished job's NDJSON deletion log so the client can save it.
// The content is capped to maxExportBytes to stay within the RPC envelope.
func (m *jobManager) export(id string) (ExportResponse, error) {
	m.mu.Lock()
	tj, ok := m.jobs[id]
	var path string
	if ok {
		path = tj.logPath
	}
	m.mu.Unlock()
	if !ok {
		return ExportResponse{}, fmt.Errorf("job %q not found", id)
	}
	if path == "" {
		return ExportResponse{}, fmt.Errorf("this job hasn't written an export yet")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ExportResponse{}, fmt.Errorf("could not read the job log: %w", err)
	}
	resp := ExportResponse{JobID: id, Filename: filepath.Base(path), Bytes: len(data)}
	if len(data) > maxExportBytes {
		data = data[:maxExportBytes]
		resp.Truncated = true
	}
	resp.Content = string(data)
	return resp, nil
}

// exportChunk reads up to exportChunkBytes of a finished job's NDJSON log
// starting at offset, so the client can stream a large export to disk without
// either side holding the whole log in memory. EOF marks the final chunk.
func (m *jobManager) exportChunk(id string, offset int64) (ExportChunkResponse, error) {
	m.mu.Lock()
	tj, ok := m.jobs[id]
	var path string
	if ok {
		path = tj.logPath
	}
	m.mu.Unlock()
	if !ok {
		return ExportChunkResponse{}, fmt.Errorf("job %q not found", id)
	}
	if path == "" {
		return ExportChunkResponse{}, fmt.Errorf("this job hasn't written an export yet")
	}
	f, err := os.Open(path)
	if err != nil {
		return ExportChunkResponse{}, fmt.Errorf("could not read the job log: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ExportChunkResponse{}, fmt.Errorf("could not read the job log: %w", err)
	}
	total := info.Size()
	if offset < 0 || offset > total {
		return ExportChunkResponse{}, fmt.Errorf("export offset %d is out of range (log is %d bytes)", offset, total)
	}
	buf := make([]byte, exportChunkBytes)
	n, err := f.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		return ExportChunkResponse{}, fmt.Errorf("could not read the job log: %w", err)
	}
	return ExportChunkResponse{
		JobID:    id,
		Filename: filepath.Base(path),
		Total:    total,
		Offset:   offset,
		Content:  buf[:n],
		EOF:      offset+int64(n) >= total,
	}, nil
}

// remove cancels (if still running) and forgets a job, so it leaves the list.
func (m *jobManager) remove(id string) bool {
	m.mu.Lock()
	tj, ok := m.jobs[id]
	if ok {
		delete(m.jobs, id)
	}
	m.mu.Unlock()
	if ok {
		tj.cancel()
	}
	return ok
}
