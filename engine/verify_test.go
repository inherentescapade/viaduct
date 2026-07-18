package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/inherentescapade/viaduct/discord"
)

// roundTripFunc lets us stub the Discord HTTP client without a real server.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResponse(v any) *http.Response {
	b, _ := json.Marshal(v)
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(string(b))),
		Header:     make(http.Header),
	}
}

func countLogs(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "delete_") && strings.HasSuffix(e.Name(), ".ndjson") {
			n++
		}
	}
	return n
}

// TestReuseLogKeepsSingleFile verifies the single-log guarantee at the
// mechanism level: with reuse enabled, closeLog(false) is a no-op and a second
// openLog returns the same file, so only one log is ever created.
func TestReuseLogKeepsSingleFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	logDir := filepath.Join(tmp, "viaduct", "logs")

	e := New("token", false)
	e.ReuseLog(true)

	if err := e.openLog(); err != nil {
		t.Fatalf("openLog: %v", err)
	}
	first := e.logFile.Name()
	e.logMessage(discord.Message{Id: "1"})

	e.closeLog(false) // reuse mode: must NOT close
	if e.logFile == nil {
		t.Fatal("closeLog(false) closed the log in reuse mode")
	}

	if err := e.openLog(); err != nil { // reuse: same file
		t.Fatalf("openLog#2: %v", err)
	}
	if e.logFile.Name() != first {
		t.Errorf("reuse opened a new file: %s != %s", e.logFile.Name(), first)
	}

	e.CloseLog()
	if e.logFile != nil {
		t.Error("CloseLog did not close the log")
	}
	if n := countLogs(t, logDir); n != 1 {
		t.Errorf("expected exactly 1 log file, found %d", n)
	}
}

// TestExecuteVerifiedSingleLog runs a full verified deletion against a stub
// transport and asserts it deletes everything, reports zero remaining, calls
// the verify hook, and writes exactly one log file across all passes.
func TestExecuteVerifiedSingleLog(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	logDir := filepath.Join(tmp, "viaduct", "logs")

	const uid = "u1"
	present := map[string]bool{"m1": true, "m2": true}
	deletes := 0

	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		path := r.URL.Path
		switch {
		case strings.Contains(path, "/messages/search"):
			var clusters [][]discord.Message
			for id := range present {
				m := discord.Message{Id: id, ChannelId: "c1"}
				m.Author.Id = uid
				clusters = append(clusters, []discord.Message{m})
			}
			return jsonResponse(discord.SearchResponse{
				TotalResults: len(present),
				Messages:     clusters,
			}), nil
		case r.Method == "DELETE" && strings.Contains(path, "/messages/"):
			id := path[strings.LastIndex(path, "/")+1:]
			delete(present, id)
			deletes++
			return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
		default:
			return jsonResponse(discord.SearchResponse{}), nil
		}
	})

	e := New("token", false)
	e.Client = discord.NewClient("token", false, &http.Client{Transport: rt})
	e.indexWait = time.Millisecond // keep index-lag waits near-instant

	verified := false
	job := DeleteJob{GuildID: "g1", GuildName: "Test", UserID: uid}
	remaining, err := e.ExecuteVerified(context.Background(), job, 3, func() { verified = true })
	if err != nil {
		t.Fatalf("ExecuteVerified: %v", err)
	}
	if remaining != 0 {
		t.Errorf("remaining = %d, want 0", remaining)
	}
	if len(present) != 0 {
		t.Errorf("messages left undeleted: %v", present)
	}
	if deletes != 2 {
		t.Errorf("deletes = %d, want 2", deletes)
	}
	if !verified {
		t.Error("onVerify was not called")
	}
	if n := countLogs(t, logDir); n != 1 {
		t.Errorf("expected exactly 1 log file across passes, found %d", n)
	}
}

// TestLiveDeleteRecordsFailureReason covers the diagnostic that turns a silent
// "N failed" into something explainable: when a live delete is rejected by
// Discord (here a group DM the account can't act in), the reason must be both
// aggregated into FailureSummary and written to the NDJSON log as a
// delete_failed record — so an operator (or the server log) can see WHY the
// deletion came up empty instead of guessing.
func TestLiveDeleteRecordsFailureReason(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	const uid = "u1"
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		path := r.URL.Path
		switch {
		case strings.Contains(path, "/messages/search"):
			// Only the first (unbounded) page returns the message; once the walk
			// advances its max_id cursor the search is empty, so the pass ends.
			if r.URL.Query().Get("max_id") != "" {
				return jsonResponse(discord.SearchResponse{}), nil
			}
			m := discord.Message{Id: "100", ChannelId: "g-dm"}
			m.Author.Id = uid
			return jsonResponse(discord.SearchResponse{
				TotalResults: 1,
				Messages:     [][]discord.Message{{m}},
			}), nil
		case r.Method == "DELETE":
			// 50003 "Cannot execute action on this channel type" — what Discord
			// returns when the account can't delete in this (group) channel.
			return jsonResponse(discord.DeleteResponse{Code: 50003, Message: "Cannot execute action on this channel type"}), nil
		default:
			return jsonResponse(discord.SearchResponse{}), nil
		}
	})

	e := New("token", false)
	e.Client = discord.NewClient("token", false, &http.Client{Transport: rt})
	e.indexWait = time.Millisecond

	// The live hook must fire as each delete fails, carrying the channel and the
	// reason — this is what lets the server log a failure the instant it happens.
	var liveMu sync.Mutex
	var live []string
	e.OnFailure = func(channelID, reason string) {
		liveMu.Lock()
		live = append(live, channelID+": "+reason)
		liveMu.Unlock()
	}

	// A group DM (type 3) under the @me scope, so this drives the DM/group path.
	grp := discord.Channel{Id: "g-dm", Type: 3, Recipients: []discord.User{{Id: "a", Username: "alice"}, {Id: "b", Username: "bob"}}}
	job := DeleteJob{GuildID: "@me", GuildName: "Direct Messages", UserID: uid, Channels: []discord.Channel{grp}}
	if err := e.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The live hook fired for the failed delete, with the channel + reason.
	if len(live) != 1 || !strings.Contains(live[0], "g-dm") || !strings.Contains(live[0], "Cannot execute action on this channel type") {
		t.Errorf("OnFailure fired %v, want one call carrying the group-DM reason", live)
	}

	// The reason Discord gave must be aggregated for the end-of-run summary.
	sum := e.FailureSummary()
	if len(sum) != 1 {
		t.Fatalf("FailureSummary = %+v, want exactly one reason", sum)
	}
	if !strings.Contains(sum[0].Reason, "Cannot execute action on this channel type") || sum[0].Count != 1 {
		t.Errorf("summary = %+v, want the group-DM reason with count 1", sum[0])
	}

	// ...and it must be persisted to the NDJSON log as a delete_failed record,
	// so logstats/Insights and a tailing operator both pick it up.
	data, err := os.ReadFile(e.LogPath())
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), `"event":"delete_failed"`) {
		t.Errorf("log missing delete_failed record:\n%s", data)
	}
	if !strings.Contains(string(data), `"channel_id":"g-dm"`) {
		t.Errorf("delete_failed record missing channel_id:\n%s", data)
	}
}

// TestSystemMessagesIgnoredNotDeleted verifies the proactive filter: a system
// message (a call notice, type 3) sharing a DM with a real message must never be
// sent to the DELETE endpoint, and must be excluded from the counts entirely —
// not deleted, not failed, and dropped from the total — while the real message
// is deleted normally.
func TestSystemMessagesIgnoredNotDeleted(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	const uid = "u1"
	var deletedIDs []string
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		path := r.URL.Path
		switch {
		case strings.Contains(path, "/messages/search"):
			if r.URL.Query().Get("max_id") != "" {
				return jsonResponse(discord.SearchResponse{}), nil
			}
			real := discord.Message{Id: "200", Type: 0, ChannelId: "dm"}
			real.Author.Id = uid
			call := discord.Message{Id: "100", Type: 3, ChannelId: "dm"} // CALL system message
			call.Author.Id = uid
			return jsonResponse(discord.SearchResponse{
				TotalResults: 2,
				Messages:     [][]discord.Message{{real}, {call}},
			}), nil
		case r.Method == "DELETE":
			deletedIDs = append(deletedIDs, path[strings.LastIndex(path, "/")+1:])
			return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
		default:
			return jsonResponse(discord.SearchResponse{}), nil
		}
	})

	e := New("token", false)
	e.Client = discord.NewClient("token", false, &http.Client{Transport: rt})
	e.indexWait = time.Millisecond

	var last Progress
	e.OnProgress = func(p Progress) { last = p }

	job := DeleteJob{GuildID: "@me", GuildName: "Direct Messages", UserID: uid,
		Channels: []discord.Channel{{Id: "dm", Type: 3}}}
	if err := e.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Only the real message was ever sent to DELETE.
	if len(deletedIDs) != 1 || deletedIDs[0] != "200" {
		t.Errorf("DELETE called for %v, want only the real message [200]", deletedIDs)
	}
	// Counts: one deleted, one ignored, none failed, and the system message is
	// gone from the total (2 found - 1 ignored = 1).
	if last.Deleted != 1 || last.Ignored != 1 || last.Failed != 0 {
		t.Errorf("counts = deleted %d, ignored %d, failed %d; want 1/1/0", last.Deleted, last.Ignored, last.Failed)
	}
	if last.Total != 1 {
		t.Errorf("Total = %d, want 1 (system message excluded)", last.Total)
	}
}

// TestSystemMessageSkipEmitsNotice verifies the user gets live feedback while
// the engine scans past undeletable system messages: skipping one must emit a
// human-readable notice mentioning system messages, so a DM full of call
// notices reports progress instead of sitting silently at "waiting for
// deletions".
func TestSystemMessageSkipEmitsNotice(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	const uid = "u1"
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		path := r.URL.Path
		switch {
		case strings.Contains(path, "/messages/search"):
			if r.URL.Query().Get("max_id") != "" {
				return jsonResponse(discord.SearchResponse{}), nil
			}
			call := discord.Message{Id: "100", Type: 3, ChannelId: "dm"} // CALL system message
			call.Author.Id = uid
			return jsonResponse(discord.SearchResponse{
				TotalResults: 1,
				Messages:     [][]discord.Message{{call}},
			}), nil
		default:
			return jsonResponse(discord.SearchResponse{}), nil
		}
	})

	e := New("token", false)
	e.Client = discord.NewClient("token", false, &http.Client{Transport: rt})
	e.indexWait = time.Millisecond

	var mu sync.Mutex
	var notices []string
	e.OnNotice = func(s string) {
		mu.Lock()
		notices = append(notices, s)
		mu.Unlock()
	}

	job := DeleteJob{GuildID: "@me", GuildName: "Direct Messages", UserID: uid,
		Channels: []discord.Channel{{Id: "dm", Type: 3}}}
	if err := e.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, n := range notices {
		if strings.Contains(strings.ToLower(n), "system message") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a notice about skipping system messages, got %v", notices)
	}
}

// TestUndeletable50021IgnoredNotFailed covers the safety net: if a system
// message slips past the type filter and Discord rejects the DELETE with 50021,
// it's still counted as ignored (and dropped from the total), never as a
// failure.
func TestUndeletable50021IgnoredNotFailed(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	const uid = "u1"
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		path := r.URL.Path
		switch {
		case strings.Contains(path, "/messages/search"):
			if r.URL.Query().Get("max_id") != "" {
				return jsonResponse(discord.SearchResponse{}), nil
			}
			// Type 99 isn't in the deny-list, so it IS attempted — and Discord
			// answers 50021, exercising the response-code safety net.
			m := discord.Message{Id: "100", Type: 99, ChannelId: "dm"}
			m.Author.Id = uid
			return jsonResponse(discord.SearchResponse{TotalResults: 1, Messages: [][]discord.Message{{m}}}), nil
		case r.Method == "DELETE":
			return jsonResponse(discord.DeleteResponse{Code: 50021, Message: "Cannot execute action on a system message"}), nil
		default:
			return jsonResponse(discord.SearchResponse{}), nil
		}
	})

	e := New("token", false)
	e.Client = discord.NewClient("token", false, &http.Client{Transport: rt})
	e.indexWait = time.Millisecond

	var last Progress
	e.OnProgress = func(p Progress) { last = p }

	job := DeleteJob{GuildID: "@me", GuildName: "Direct Messages", UserID: uid,
		Channels: []discord.Channel{{Id: "dm", Type: 3}}}
	if err := e.Execute(context.Background(), job); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if last.Failed != 0 {
		t.Errorf("Failed = %d, want 0 (50021 is not a real failure)", last.Failed)
	}
	if last.Ignored != 1 || last.Total != 0 {
		t.Errorf("Ignored = %d, Total = %d; want 1 ignored and 0 total", last.Ignored, last.Total)
	}
	// A 50021 must not be recorded as a failure reason.
	if len(e.FailureSummary()) != 0 {
		t.Errorf("FailureSummary = %+v, want empty for a 50021", e.FailureSummary())
	}
}

// TestDeleteScopeStuckMessagesTerminate guards against the infinite re-attempt
// loop: a message that always fails with missing-permissions (50013) must be
// tried once and then left alone, not retried until the user force-quits.
func TestDeleteScopeStuckMessagesTerminate(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	const uid = "u1"
	attempts := 0
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		path := r.URL.Path
		switch {
		case strings.Contains(path, "/messages/search"):
			// The stuck message never deletes. Real Discord search honours
			// max_id, so once the cursor walk pages past it (max_id set) it is no
			// longer returned — which is exactly how the walk avoids retrying a
			// stuck message forever. Only the first (unbounded) page returns it.
			if r.URL.Query().Get("max_id") != "" {
				return jsonResponse(discord.SearchResponse{}), nil
			}
			m := discord.Message{Id: "100", ChannelId: "c1"}
			m.Author.Id = uid
			return jsonResponse(discord.SearchResponse{
				TotalResults: 1,
				Messages:     [][]discord.Message{{m}},
			}), nil
		case r.Method == "DELETE":
			attempts++
			// 50013 Missing Permissions, returned as a body with an error.
			return jsonResponse(discord.DeleteResponse{Code: 50013, Message: "Missing Permissions"}), nil
		default:
			return jsonResponse(discord.SearchResponse{}), nil
		}
	})

	e := New("token", false)
	e.Client = discord.NewClient("token", false, &http.Client{Transport: rt})
	e.indexWait = time.Millisecond

	done := make(chan error, 1)
	go func() {
		done <- e.Execute(context.Background(), DeleteJob{GuildID: "g1", UserID: uid})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("deleteScope did not terminate — stuck message retried %d times", attempts)
	}

	// The stuck message should be attempted a small, bounded number of times,
	// not hundreds. (Allow for the client's internal 429/permission retries.)
	if attempts > 5 {
		t.Errorf("stuck message attempted %d times, expected it to be left alone after the first failures", attempts)
	}
}
