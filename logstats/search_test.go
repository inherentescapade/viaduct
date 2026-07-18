package logstats

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeSearchLogs lays down two runs covering deletes (with/without
// attachments, across two channels) and a failure, for the search tests.
func writeSearchLogs(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run1 := "" +
		`{"id":"1","channel_id":"chanA","content":"hello world","timestamp":"2024-01-15T10:00:00Z","attachments":[{"id":"a"}],"author":{"id":"u1","username":"alice","global_name":"Alice","avatar":"abc"}}` + "\n" +
		`{"id":"2","channel_id":"chanB","content":"goodbye","timestamp":"2024-02-20T12:00:00Z"}` + "\n" +
		`{"event":"delete_failed","channel_id":"chanA","channel":"general","message_id":"9","error":"rate limited"}` + "\n" +
		`not json, skipped` + "\n"
	run2 := `{"id":"3","channel_id":"chanA","content":"hello again","timestamp":"2024-03-01T00:00:00Z"}` + "\n"

	if err := os.WriteFile(filepath.Join(dir, "delete_2024-01-15_100000.ndjson"), []byte(run1), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "delete_2024-03-01_000000.ndjson"), []byte(run2), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSearchAll(t *testing.T) {
	dir := writeSearchLogs(t)
	res, err := Search(dir, SearchQuery{}, nil)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// 3 deletes + 1 failure.
	if res.Total != 4 {
		t.Errorf("Total = %d, want 4", res.Total)
	}
	if len(res.Hits) != 4 {
		t.Fatalf("Hits = %d, want 4", len(res.Hits))
	}
	// Newest run first: run2's delete (id 3) leads.
	if res.Hits[0].ID != "3" || res.Hits[0].File != "delete_2024-03-01_000000.ndjson" {
		t.Errorf("Hits[0] = %+v, want id 3 from the newest run", res.Hits[0])
	}
	if res.Hits[0].RunAt == "" {
		t.Errorf("RunAt not parsed from filename")
	}
}

func TestSearchText(t *testing.T) {
	dir := writeSearchLogs(t)
	res, err := Search(dir, SearchQuery{Text: "hello"}, nil)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 2 {
		t.Errorf("Total = %d, want 2 (two 'hello' messages)", res.Total)
	}
	for _, h := range res.Hits {
		if h.Kind != "deleted" {
			t.Errorf("unexpected kind %q", h.Kind)
		}
	}
}

func TestSearchKindAndChannel(t *testing.T) {
	dir := writeSearchLogs(t)

	failed, _ := Search(dir, SearchQuery{Kind: KindFailed}, nil)
	if failed.Total != 1 || len(failed.Hits) != 1 {
		t.Fatalf("failed search Total/len = %d/%d, want 1/1", failed.Total, len(failed.Hits))
	}
	h := failed.Hits[0]
	if h.Kind != "failed" || h.Error != "rate limited" || h.ID != "9" {
		t.Errorf("failure hit = %+v, want id 9 / rate limited", h)
	}
	// channel name harvested from the failure record labels the channel.
	if h.ChannelLabel != "#general" {
		t.Errorf("ChannelLabel = %q, want #general", h.ChannelLabel)
	}

	chanA, _ := Search(dir, SearchQuery{ChannelID: "chanA", Kind: KindDeleted}, nil)
	if chanA.Total != 2 {
		t.Errorf("chanA deleted Total = %d, want 2", chanA.Total)
	}
}

func TestSearchAttachmentsAndDates(t *testing.T) {
	dir := writeSearchLogs(t)

	att, _ := Search(dir, SearchQuery{WithAttachments: true}, nil)
	if att.Total != 1 || att.Hits[0].ID != "1" {
		t.Errorf("attachment search = %+v, want only id 1", att.Hits)
	}
	// Author is parsed (global name preferred), with id/avatar exposed for the
	// desktop to build an avatar URL from.
	h := att.Hits[0]
	if h.AuthorName != "Alice" || h.AuthorID != "u1" || h.AuthorAvatar != "abc" {
		t.Errorf("author = %q/%q/%q, want Alice/u1/abc", h.AuthorName, h.AuthorID, h.AuthorAvatar)
	}

	// Only the March message is after 2024-02-25.
	after, _ := Search(dir, SearchQuery{After: time.Date(2024, 2, 25, 0, 0, 0, 0, time.UTC)}, nil)
	if after.Total != 1 || after.Hits[0].ID != "3" {
		t.Errorf("after-date search = %+v, want only id 3", after.Hits)
	}
}

func TestSearchPaging(t *testing.T) {
	dir := writeSearchLogs(t)

	page1, _ := Search(dir, SearchQuery{Limit: 2}, nil)
	if len(page1.Hits) != 2 || page1.Total != 4 {
		t.Fatalf("page1 len/Total = %d/%d, want 2/4", len(page1.Hits), page1.Total)
	}
	page2, _ := Search(dir, SearchQuery{Limit: 2, Offset: 2}, nil)
	if len(page2.Hits) != 2 || page2.Total != 4 {
		t.Fatalf("page2 len/Total = %d/%d, want 2/4", len(page2.Hits), page2.Total)
	}
	// The two pages must not overlap.
	seen := map[string]bool{}
	for _, h := range append(append([]SearchHit{}, page1.Hits...), page2.Hits...) {
		key := h.File + "/" + h.Kind + "/" + h.ID
		if seen[key] {
			t.Errorf("hit %q appeared on both pages", key)
		}
		seen[key] = true
	}
}

func TestSearchLabelFor(t *testing.T) {
	dir := writeSearchLogs(t)
	res, _ := Search(dir, SearchQuery{ChannelID: "chanB"}, func(id string) string {
		if id == "chanB" {
			return "random"
		}
		return ""
	})
	if len(res.Hits) != 1 || res.Hits[0].ChannelLabel != "random" {
		t.Errorf("labelFor not applied: %+v", res.Hits)
	}
}

func TestSearchMissingDir(t *testing.T) {
	res, err := Search(filepath.Join(t.TempDir(), "nope"), SearchQuery{}, nil)
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if res.Total != 0 || res.Hits == nil {
		t.Errorf("missing dir should yield empty result with non-nil hits, got %+v", res)
	}
}

func TestExportFiltered(t *testing.T) {
	dir := writeSearchLogs(t)
	var buf bytes.Buffer
	// Export the two chanA deletes (exclude the failure via kind).
	n, err := Export(dir, SearchQuery{ChannelID: "chanA", Kind: KindDeleted}, &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if n != 2 {
		t.Errorf("written = %d, want 2", n)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	// Newest run first: id 3 leads, and each line is valid, re-importable JSON.
	var rec record
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("exported line is not valid JSON: %v", err)
	}
	if rec.ID != "3" {
		t.Errorf("first exported id = %q, want 3 (newest first)", rec.ID)
	}
}

func TestPurgeFiltered(t *testing.T) {
	dir := writeSearchLogs(t)
	// Remove everything in chanA (2 deletes + 1 failure), leaving only chanB.
	removed, err := Purge(dir, SearchQuery{ChannelID: "chanA"})
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if removed != 3 {
		t.Errorf("removed = %d, want 3", removed)
	}
	after, _ := Search(dir, SearchQuery{}, nil)
	if after.Total != 1 || after.Hits[0].ID != "2" {
		t.Errorf("after purge = %+v, want only id 2 (chanB) left", after.Hits)
	}
	// run2 held only chanA deletes, so its file should be gone entirely.
	if _, err := os.Stat(filepath.Join(dir, "delete_2024-03-01_000000.ndjson")); !os.IsNotExist(err) {
		t.Errorf("emptied run file should have been removed, stat err = %v", err)
	}
	// The malformed, unparseable line in run1 must be preserved, not dropped.
	data, _ := os.ReadFile(filepath.Join(dir, "delete_2024-01-15_100000.ndjson"))
	if !strings.Contains(string(data), "not json, skipped") {
		t.Errorf("purge dropped an unparseable line it should have kept: %q", string(data))
	}
}

func TestPurgeIsEmptyGuard(t *testing.T) {
	if !(SearchQuery{}).IsEmpty() {
		t.Error("zero query should be empty")
	}
	if (SearchQuery{ChannelID: "x"}).IsEmpty() {
		t.Error("query with a channel filter should not be empty")
	}
	if (SearchQuery{WithAttachments: true}).IsEmpty() {
		t.Error("query with attachments filter should not be empty")
	}
}

func TestRunsList(t *testing.T) {
	dir := writeSearchLogs(t)
	runs, err := Runs(dir)
	if err != nil {
		t.Fatalf("Runs: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("got %d runs, want 2", len(runs))
	}
	// Newest run first.
	if runs[0].File != "delete_2024-03-01_000000.ndjson" {
		t.Errorf("runs[0].File = %q, want newest first", runs[0].File)
	}
	if runs[0].Deleted != 1 || runs[0].Failed != 0 || runs[0].TopChannel != "chanA" {
		t.Errorf("runs[0] = %+v, want 1 deleted / 0 failed / chanA", runs[0])
	}
	// run1: two deletes (chanA, chanB) and one failure; chanA/chanB tie at 1 each,
	// so topKey breaks the tie by key -> chanA.
	if runs[1].Deleted != 2 || runs[1].Failed != 1 || runs[1].TopChannel != "chanA" {
		t.Errorf("runs[1] = %+v, want 2 deleted / 1 failed / chanA", runs[1])
	}
}

func TestRunsMissingDir(t *testing.T) {
	runs, err := Runs(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("missing dir should yield no runs, got %d", len(runs))
	}
}
