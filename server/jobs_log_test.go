package server

import (
	"context"
	"fmt"
	"github.com/inherentescapade/viaduct/engine"
	"strings"
	"sync"
	"testing"
)

// captureLogf returns a logf that appends each formatted line to lines, guarded
// for the concurrent use a real server logger sees.
func captureLogf(lines *[]string) (func(string, ...any), *sync.Mutex) {
	var mu sync.Mutex
	return func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		*lines = append(*lines, fmt.Sprintf(format, args...))
	}, &mu
}

// TestJobLogOutcomeSurfacesFailureReasons is the crux of the observability fix:
// a job that finishes failed must log its terminal state, its counts, and — per
// distinct Discord reason — WHY deletes failed, so an operator watching the
// server can see (e.g.) that a group DM couldn't be acted on instead of staring
// at a silent process.
func TestJobLogOutcomeSurfacesFailureReasons(t *testing.T) {
	var lines []string
	logf, _ := captureLogf(&lines)
	m := newJobManager(nil, logf)

	st := JobStatus{
		ID:          "job-7",
		Description: "group · alice, bob",
		State:       StateFailed,
		Deleted:     0,
		Failed:      3,
		Skipped:     1,
	}
	fails := []engine.FailReason{
		{Reason: "50003 Cannot execute action on this channel type", Count: 3},
	}
	m.logOutcome(st, fmt.Errorf("discord rejected the delete"), fails)

	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "job-7 failed") {
		t.Errorf("outcome log missing failed state:\n%s", joined)
	}
	if !strings.Contains(joined, "3 failed") {
		t.Errorf("outcome log missing failure count:\n%s", joined)
	}
	if !strings.Contains(joined, "Cannot execute action on this channel type") || !strings.Contains(joined, "x3") {
		t.Errorf("outcome log missing the per-reason breakdown:\n%s", joined)
	}
}

// TestJobLogOutcomeDoneNoFailures keeps the happy path quiet: a clean run logs a
// single "done" line and no failure-reason spam.
func TestJobLogOutcomeDoneNoFailures(t *testing.T) {
	var lines []string
	logf, _ := captureLogf(&lines)
	m := newJobManager(nil, logf)

	m.logOutcome(JobStatus{ID: "job-1", State: StateDone, Deleted: 42}, nil, nil)

	if len(lines) != 1 {
		t.Fatalf("a clean run should log exactly one line, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "job-1 done") || !strings.Contains(lines[0], "42 deleted") {
		t.Errorf("done log = %q, want it to report the deleted count", lines[0])
	}
}

// TestNewJobManagerNilLogf guards the nil-safety contract: a manager built
// without a logger must not panic when it logs an outcome.
func TestNewJobManagerNilLogf(t *testing.T) {
	m := newJobManager(nil, nil)
	m.logOutcome(JobStatus{ID: "job-1", State: StateDone}, nil, nil) // must not panic
}

// TestCancelReportsCancelingImmediately is the crux of the stop-feedback fix:
// clicking stop must flip the job to the transient "canceling" state and log the
// request the instant it's signalled — before the background goroutine reaches
// its (rate-limited, cooperative) cancellation checkpoint and writes the
// terminal "canceled" line — so the client sees the stop took effect right away
// instead of the row still saying "running" until the goroutine unwinds.
func TestCancelReportsCancelingImmediately(t *testing.T) {
	var lines []string
	logf, _ := captureLogf(&lines)
	m := newJobManager(nil, logf)

	_, cancel := context.WithCancel(context.Background())
	m.jobs["job-1"] = &trackedJob{
		status: JobStatus{ID: "job-1", State: StateRunning},
		cancel: cancel,
	}

	st, ok := m.cancel("job-1")
	if !ok {
		t.Fatal("cancel of a known running job should succeed")
	}
	if st.State != StateCanceling {
		t.Errorf("cancel returned state %q, want %q so the response reflects the stop", st.State, StateCanceling)
	}
	if got, _ := m.get("job-1"); got.State != StateCanceling {
		t.Errorf("stored state = %q, want %q so the next poll sees canceling", got.State, StateCanceling)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "job-1 cancel requested") {
		t.Errorf("cancel should log the request immediately, got: %v", lines)
	}
}

// TestCancelDoesNotResurrectFinishedJob guards the terminal-state check: a job
// that reached done/failed/canceled the instant the user clicked keeps that
// state rather than being dragged back into canceling.
func TestCancelDoesNotResurrectFinishedJob(t *testing.T) {
	m := newJobManager(nil, nil)
	_, cancel := context.WithCancel(context.Background())
	m.jobs["job-1"] = &trackedJob{
		status: JobStatus{ID: "job-1", State: StateDone},
		cancel: cancel,
	}

	st, ok := m.cancel("job-1")
	if !ok {
		t.Fatal("cancel of a known job should report found")
	}
	if st.State != StateDone {
		t.Errorf("cancel of an already-done job returned %q, want it left at %q", st.State, StateDone)
	}
}

// TestFirstFailureDedupes is the anti-flood guarantee behind live failure
// logging: a reason is announced the first time it's seen and never again, so a
// job failing a million messages with the same error logs one line, not a
// million. A genuinely new reason still gets its own first announcement.
func TestFirstFailureDedupes(t *testing.T) {
	m := newJobManager(nil, nil)
	tj := &trackedJob{loggedReasons: map[string]bool{}}

	const reasonA = "50003 Cannot execute action on this channel type"
	const reasonB = "50013 Missing Permissions"

	if !m.firstFailure(tj, reasonA) {
		t.Fatal("first sighting of reason A should log")
	}
	for i := 0; i < 1_000; i++ {
		if m.firstFailure(tj, reasonA) {
			t.Fatalf("reason A logged again on repeat %d — would flood the log", i)
		}
	}
	if !m.firstFailure(tj, reasonB) {
		t.Fatal("a new reason B should get its own first announcement")
	}
}

// TestRecordProgressExcludesSystemMessages verifies the reported total never
// counts undeletable system messages: the engine discounts them from p.Total and
// reports them as Ignored, and the job manager tracks the raw peak across verify
// passes while always subtracting the current ignored count.
func TestRecordProgressExcludesSystemMessages(t *testing.T) {
	m := newJobManager(nil, nil)
	tj := &trackedJob{}

	// Pass 1: 11 found (1 is a system message the engine has already discounted,
	// so it reports Total 10, Ignored 1). Reported target must be 10, not 11.
	m.recordProgress(tj, engine.Progress{Total: 10, Ignored: 1, Deleted: 10})
	if tj.status.Total != 10 || tj.status.Ignored != 1 {
		t.Fatalf("after pass 1: Total=%d Ignored=%d, want 10/1", tj.status.Total, tj.status.Ignored)
	}

	// A verify mop-up pass re-reports a much smaller shrinking total; the peak
	// (10 deletable) must stick, and the system message must stay excluded.
	m.recordProgress(tj, engine.Progress{Total: 0, Ignored: 1})
	if tj.status.Total != 10 {
		t.Errorf("after verify pass: Total=%d, want the peak 10 to stick", tj.status.Total)
	}
	if tj.status.Ignored != 1 {
		t.Errorf("Ignored=%d, want 1 to persist", tj.status.Ignored)
	}
}

// TestJobLogOutcomeReportsIgnored confirms the terminal log line surfaces the
// ignored (system-message) count alongside the rest.
func TestJobLogOutcomeReportsIgnored(t *testing.T) {
	var lines []string
	logf, _ := captureLogf(&lines)
	m := newJobManager(nil, logf)

	m.logOutcome(JobStatus{ID: "job-9", State: StateDone, Deleted: 340, Ignored: 5}, nil, nil)

	if len(lines) != 1 || !strings.Contains(lines[0], "5 ignored") {
		t.Errorf("done log = %v, want it to report '5 ignored'", lines)
	}
}

// TestFirstFailureNilMap ensures the dedup is safe even if loggedReasons was
// never initialised, so a code path that skips submit's setup can't panic.
func TestFirstFailureNilMap(t *testing.T) {
	m := newJobManager(nil, nil)
	tj := &trackedJob{} // loggedReasons nil
	if !m.firstFailure(tj, "some reason") {
		t.Fatal("first sighting should log even from a nil map")
	}
	if m.firstFailure(tj, "some reason") {
		t.Fatal("second sighting should be suppressed")
	}
}
