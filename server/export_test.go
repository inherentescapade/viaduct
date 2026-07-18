package server

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// writeTempLog creates a job log file with n bytes of varied content and returns
// a jobManager that knows about it under id "job-1".
func writeTempLog(t *testing.T, n int) (*jobManager, []byte) {
	t.Helper()
	data := make([]byte, n)
	for i := range data {
		data[i] = byte(i % 251) // non-trivial, includes high bytes and newlines
	}
	path := filepath.Join(t.TempDir(), "deletions.ndjson")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write temp log: %v", err)
	}
	m := newJobManager(nil, nil)
	m.jobs["job-1"] = &trackedJob{logPath: path}
	return m, data
}

// drain streams a job's whole log via exportChunk and returns the reassembled
// bytes, asserting the chunking invariants along the way.
func drain(t *testing.T, m *jobManager, id string) []byte {
	t.Helper()
	var got []byte
	var offset int64
	for {
		chunk, err := m.exportChunk(id, offset)
		if err != nil {
			t.Fatalf("exportChunk at offset %d: %v", offset, err)
		}
		if len(chunk.Content) > exportChunkBytes {
			t.Fatalf("chunk exceeded exportChunkBytes: %d", len(chunk.Content))
		}
		if chunk.Offset != offset {
			t.Fatalf("chunk offset = %d, want %d", chunk.Offset, offset)
		}
		got = append(got, chunk.Content...)
		offset += int64(len(chunk.Content))
		if chunk.EOF {
			if offset != chunk.Total {
				t.Fatalf("EOF at %d but total is %d", offset, chunk.Total)
			}
			break
		}
		if len(chunk.Content) == 0 {
			t.Fatal("non-EOF chunk returned no bytes — would loop forever")
		}
	}
	return got
}

func TestExportChunkReassembles(t *testing.T) {
	// A log spanning several chunks (plus a partial tail) must round-trip exactly.
	m, data := writeTempLog(t, exportChunkBytes*2+1234)
	got := drain(t, m, "job-1")
	if !bytes.Equal(got, data) {
		t.Fatalf("reassembled %d bytes, want %d (content mismatch)", len(got), len(data))
	}
}

func TestExportChunkSmallAndEmpty(t *testing.T) {
	// A sub-chunk log returns in one EOF chunk; an empty log returns one empty
	// EOF chunk rather than erroring or looping.
	for _, n := range []int{0, 10, exportChunkBytes} {
		m, data := writeTempLog(t, n)
		got := drain(t, m, "job-1")
		if !bytes.Equal(got, data) {
			t.Fatalf("n=%d: reassembled %d bytes, want %d", n, len(got), len(data))
		}
	}
}

func TestExportChunkOffsetOutOfRange(t *testing.T) {
	m, data := writeTempLog(t, 100)
	if _, err := m.exportChunk("job-1", int64(len(data)+1)); err == nil {
		t.Fatal("expected error for offset past end of log")
	}
	if _, err := m.exportChunk("job-1", -1); err == nil {
		t.Fatal("expected error for negative offset")
	}
	// Offset exactly at end is valid: an empty EOF chunk.
	chunk, err := m.exportChunk("job-1", int64(len(data)))
	if err != nil {
		t.Fatalf("offset at EOF: %v", err)
	}
	if !chunk.EOF || len(chunk.Content) != 0 {
		t.Fatalf("offset at EOF: got eof=%v len=%d, want eof=true len=0", chunk.EOF, len(chunk.Content))
	}
}

func TestExportChunkErrors(t *testing.T) {
	m := newJobManager(nil, nil)
	if _, err := m.exportChunk("missing", 0); err == nil {
		t.Fatal("expected error for unknown job")
	}
	m.jobs["job-1"] = &trackedJob{} // no logPath yet
	if _, err := m.exportChunk("job-1", 0); err == nil {
		t.Fatal("expected error for job with no export written")
	}
}
