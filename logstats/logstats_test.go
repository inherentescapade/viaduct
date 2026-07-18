package logstats

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAggregates(t *testing.T) {
	dir := t.TempDir()

	// One run: two deletes (one with an attachment) in two channels, one failure.
	run1 := "" +
		`{"id":"1","channel_id":"chanA","content":"hello","timestamp":"2024-01-15T10:00:00Z","attachments":[{"id":"a"}]}` + "\n" +
		`{"id":"2","channel_id":"chanB","content":"hi","timestamp":"2024-02-20T12:00:00Z","attachments":[]}` + "\n" +
		`{"event":"delete_failed","channel_id":"chanA","channel":"general","message_id":"9","error":"rate limited"}` + "\n" +
		`not json, should be skipped` + "\n"
	// Second run: one more delete in chanA.
	run2 := `{"id":"3","channel_id":"chanA","content":"again","timestamp":"2024-03-01T00:00:00Z"}` + "\n"

	if err := os.WriteFile(filepath.Join(dir, "delete_2024-01-15_100000.ndjson"), []byte(run1), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "delete_2024-03-01_000000.ndjson"), []byte(run2), 0600); err != nil {
		t.Fatal(err)
	}

	st, err := Parse(dir, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if st.Runs != 2 {
		t.Errorf("Runs = %d, want 2", st.Runs)
	}
	if st.TotalDeleted != 3 {
		t.Errorf("TotalDeleted = %d, want 3", st.TotalDeleted)
	}
	if st.TotalFailed != 1 {
		t.Errorf("TotalFailed = %d, want 1", st.TotalFailed)
	}
	if st.WithAttachments != 1 || st.Attachments != 1 {
		t.Errorf("attachments = %d/%d, want 1/1", st.WithAttachments, st.Attachments)
	}
	if st.Channels != 2 {
		t.Errorf("Channels = %d, want 2", st.Channels)
	}
	if len(st.TopChannels) == 0 || st.TopChannels[0].ID != "chanA" || st.TopChannels[0].Count != 2 {
		t.Errorf("TopChannels[0] = %+v, want chanA x2", st.TopChannels)
	}
	// chanA got a name from the failure record.
	if st.TopChannels[0].Label != "#general" {
		t.Errorf("TopChannels[0].Label = %q, want #general", st.TopChannels[0].Label)
	}
	if len(st.Failures) != 1 || st.Failures[0].Reason != "rate limited" {
		t.Errorf("Failures = %+v, want one 'rate limited'", st.Failures)
	}
	// Jan, Feb, Mar = 3 contiguous months.
	if len(st.ByMonth) != 3 {
		t.Errorf("ByMonth = %+v, want 3 buckets", st.ByMonth)
	}
	if st.FirstPostedAt == "" || st.LastPostedAt == "" {
		t.Errorf("posted range empty: %q..%q", st.FirstPostedAt, st.LastPostedAt)
	}
	// Recent is newest-first.
	if len(st.Recent) != 2 || st.Recent[0].File != "delete_2024-03-01_000000.ndjson" {
		t.Errorf("Recent[0] = %+v, want newest run first", st.Recent)
	}
}

func TestParseMissingDir(t *testing.T) {
	st, err := Parse(filepath.Join(t.TempDir(), "nope"), nil)
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if st.Runs != 0 || st.TopChannels == nil || st.ByMonth == nil {
		t.Errorf("missing dir should yield empty stats with non-nil slices, got %+v", st)
	}
}
