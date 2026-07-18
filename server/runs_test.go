package server

import (
	"github.com/inherentescapade/viaduct/auth"
	"github.com/inherentescapade/viaduct/logstats"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRunLog drops one deletion log file into dir, named after the run
// timestamp, so tests can seed runs without going through a real deletion.
func writeRunLog(t *testing.T, dir, stamp string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "delete_"+stamp+".ndjson")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

// TestRunsAndSearchSegregated verifies the run-centric ops (list, search,
// export, purge, chunked download, delete) only ever see the calling
// account's own logs — the same isolation opLogStats already guarantees.
func TestRunsAndSearchSegregated(t *testing.T) {
	alice, _ := auth.GenerateIdentity()
	bob, _ := auth.GenerateIdentity()
	serverID, _ := auth.GenerateIdentity()
	logBase := t.TempDir()

	srv, err := New(Options{
		Identity:       serverID,
		AuthorizedKeys: []string{alice.PublicKeyString(), bob.PublicKeyString()},
		MonitorsPath:   t.TempDir() + "/monitors.bin",
		LogDir:         logBase,
		Logf:           func(string, ...any) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")

	aliceDir := ClientLogDir(logBase, alice.PublicKeyString())
	writeRunLog(t, aliceDir, "2024-01-01_000000",
		`{"id":"1","content":"hello world","channel_id":"c1","timestamp":"2024-01-01T00:00:00Z"}`)

	ac := NewClient(addr, alice, serverID.Public())
	bc := NewClient(addr, bob, serverID.Public())

	// Alice sees her run; Bob sees none.
	aliceRuns, err := ac.ListRuns()
	if err != nil {
		t.Fatalf("alice list runs: %v", err)
	}
	if len(aliceRuns) != 1 || aliceRuns[0].Deleted != 1 {
		t.Fatalf("alice should see her 1 run, got %+v", aliceRuns)
	}
	bobRuns, err := bc.ListRuns()
	if err != nil {
		t.Fatalf("bob list runs: %v", err)
	}
	if len(bobRuns) != 0 {
		t.Fatalf("bob must not see alice's runs, got %+v", bobRuns)
	}

	// Search: alice finds her message; bob finds nothing for the same query.
	aliceHits, err := ac.SearchLogs(logstats.SearchQuery{Text: "hello"})
	if err != nil {
		t.Fatalf("alice search: %v", err)
	}
	if len(aliceHits.Hits) != 1 {
		t.Fatalf("alice should find her 1 hit, got %+v", aliceHits.Hits)
	}
	bobHits, err := bc.SearchLogs(logstats.SearchQuery{Text: "hello"})
	if err != nil {
		t.Fatalf("bob search: %v", err)
	}
	if len(bobHits.Hits) != 0 {
		t.Fatalf("bob must not find alice's message, got %+v", bobHits.Hits)
	}

	// Chunked download: alice can pull her run's bytes; bob's request for the
	// same filename 404s within his own (empty) log directory.
	chunk, err := ac.ExportRunChunk(aliceRuns[0].File, 0)
	if err != nil {
		t.Fatalf("alice export run chunk: %v", err)
	}
	if !chunk.EOF || chunk.Total == 0 {
		t.Fatalf("alice's chunk should be the whole (small) file, got %+v", chunk)
	}
	if _, err := bc.ExportRunChunk(aliceRuns[0].File, 0); err == nil {
		t.Fatal("bob must not be able to download alice's run")
	}

	// Bob deleting alice's filename is a no-op in his own (empty) directory —
	// it must not touch alice's file.
	if err := bc.DeleteRun(aliceRuns[0].File); err != nil {
		t.Fatalf("bob delete (no-op) should not error: %v", err)
	}
	if aliceRuns2, err := ac.ListRuns(); err != nil || len(aliceRuns2) != 1 {
		t.Fatalf("alice's run must survive bob's no-op delete, got %+v, %v", aliceRuns2, err)
	}

	// Alice deleting her own run actually removes it.
	if err := ac.DeleteRun(aliceRuns[0].File); err != nil {
		t.Fatalf("alice delete run: %v", err)
	}
	if aliceRuns3, err := ac.ListRuns(); err != nil || len(aliceRuns3) != 0 {
		t.Fatalf("alice's run should be gone after deleting it, got %+v, %v", aliceRuns3, err)
	}
}

// TestPurgeLogsRefusesEmptyQuery ensures a purge with no filter is rejected,
// same as the local guard, so a request can't wipe a whole account's history
// by accident.
func TestPurgeLogsRefusesEmptyQuery(t *testing.T) {
	alice, _ := auth.GenerateIdentity()
	serverID, _ := auth.GenerateIdentity()
	logBase := t.TempDir()

	srv, err := New(Options{
		Identity:       serverID,
		AuthorizedKeys: []string{alice.PublicKeyString()},
		MonitorsPath:   t.TempDir() + "/monitors.bin",
		LogDir:         logBase,
		Logf:           func(string, ...any) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")

	writeRunLog(t, ClientLogDir(logBase, alice.PublicKeyString()), "2024-01-01_000000",
		`{"id":"1","content":"hello","channel_id":"c1","timestamp":"2024-01-01T00:00:00Z"}`)

	ac := NewClient(addr, alice, serverID.Public())
	if _, err := ac.PurgeLogs(logstats.SearchQuery{}); err == nil {
		t.Fatal("an unfiltered purge must be refused")
	}
	if n, err := ac.PurgeLogs(logstats.SearchQuery{Text: "hello"}); err != nil || n != 1 {
		t.Fatalf("a filtered purge should remove the 1 match, got n=%d err=%v", n, err)
	}
}

// TestRunFileNameRejectsPathTraversal ensures a crafted filename can't escape
// the account's log directory via the export/delete run ops.
func TestRunFileNameRejectsPathTraversal(t *testing.T) {
	cases := []string{
		"../../etc/passwd",
		"delete_../../secret.ndjson",
		"not-a-log.txt",
		"delete_2024-01-01_000000.ndjson/../../etc",
	}
	for _, c := range cases {
		if _, err := runFileName(c); err == nil {
			t.Fatalf("runFileName(%q) should have been rejected", c)
		}
	}
	if got, err := runFileName("delete_2024-01-01_000000.ndjson"); err != nil || got != "delete_2024-01-01_000000.ndjson" {
		t.Fatalf("a valid filename should pass through unchanged, got %q, %v", got, err)
	}
}
