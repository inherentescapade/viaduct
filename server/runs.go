package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/inherentescapade/viaduct/logstats"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// This file serves the run-centric view over an account's deletion logs — the
// remote counterpart to desktop/insights.go's local-only ListRuns/SearchLogs/
// ExportSearch/PurgeLogs/DownloadLog/DeleteLog. Every op here reads or writes
// within client.logDir only, so one account's logs are never reachable from
// another's request.

// runFileName validates that file is a bare deletion-log filename (no path
// component), so a run operation can't be pointed outside the account's log
// directory via a crafted path — the same guard the desktop applies locally.
func runFileName(file string) (string, error) {
	name := strings.TrimSpace(file)
	if name != filepath.Base(name) || !strings.HasPrefix(name, "delete_") || !strings.HasSuffix(name, ".ndjson") {
		return "", fmt.Errorf("not a deletion log: %q", file)
	}
	return name, nil
}

// readFileChunk reads up to exportChunkBytes of path starting at offset — the
// shared logic behind a job's and a run's chunked download.
func readFileChunk(path string, offset int64) (content []byte, total int64, eof bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, false, fmt.Errorf("could not read the log: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, 0, false, fmt.Errorf("could not read the log: %w", err)
	}
	total = info.Size()
	if offset < 0 || offset > total {
		return nil, 0, false, fmt.Errorf("export offset %d is out of range (log is %d bytes)", offset, total)
	}
	buf := make([]byte, exportChunkBytes)
	n, rerr := f.ReadAt(buf, offset)
	if rerr != nil && rerr != io.EOF {
		return nil, 0, false, fmt.Errorf("could not read the log: %w", rerr)
	}
	return buf[:n], total, offset+int64(n) >= total, nil
}

// opListRuns lists every deletion run on this account, newest first — the
// run-centric companion to opLogStats's aggregated summary.
func (s *Server) opListRuns(client *clientState) (any, error) {
	runs, err := logstats.Runs(client.logDir)
	if err != nil {
		return nil, err
	}
	return RunListResponse{Runs: runs}, nil
}

// opSearchLogs runs a full-text query over this account's deletion logs.
// Channel labels aren't resolved server-side (the server has no channel
// cache); the desktop enriches them from its own, same as it already does for
// local search results.
func (s *Server) opSearchLogs(client *clientState, body json.RawMessage) (any, error) {
	var q logstats.SearchQuery
	if err := json.Unmarshal(body, &q); err != nil {
		return nil, fmt.Errorf("bad search payload")
	}
	return logstats.Search(client.logDir, q, nil)
}

// opExportSearch writes every record matching the query to a single response,
// capped like a job export so it stays within the RPC envelope.
func (s *Server) opExportSearch(client *clientState, body json.RawMessage) (any, error) {
	var q logstats.SearchQuery
	if err := json.Unmarshal(body, &q); err != nil {
		return nil, fmt.Errorf("bad search payload")
	}
	var buf bytes.Buffer
	if _, err := logstats.Export(client.logDir, q, &buf); err != nil {
		return nil, err
	}
	data := buf.Bytes()
	resp := ExportResponse{Filename: "viaduct-deletions.ndjson", Bytes: len(data)}
	if len(data) > maxExportBytes {
		data = data[:maxExportBytes]
		resp.Truncated = true
	}
	resp.Content = string(data)
	return resp, nil
}

// opPurgeLogs removes every record matching the query from this account's
// logs. Mirrors the local PurgeLogs guard: an unfiltered query is refused so
// it can't wipe the whole history by accident.
func (s *Server) opPurgeLogs(client *clientState, body json.RawMessage) (any, error) {
	var q logstats.SearchQuery
	if err := json.Unmarshal(body, &q); err != nil {
		return nil, fmt.Errorf("bad search payload")
	}
	if q.IsEmpty() {
		return nil, fmt.Errorf("add at least one filter before deleting matches")
	}
	removed, err := logstats.Purge(client.logDir, q)
	if err != nil {
		return nil, err
	}
	return PurgeLogsResponse{Removed: removed}, nil
}

// opExportRunChunk streams one slice of a specific run's deletion log, the
// run-centric analogue of opExportChunk for jobs — a run may equally be the
// output of a monitor's scheduled pass, which has no job ID to key off.
func (s *Server) opExportRunChunk(client *clientState, body json.RawMessage) (any, error) {
	var req RunChunkRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("bad request")
	}
	name, err := runFileName(req.File)
	if err != nil {
		return nil, err
	}
	content, total, eof, err := readFileChunk(filepath.Join(client.logDir, name), req.Offset)
	if err != nil {
		return nil, err
	}
	return RunChunkResponse{File: name, Total: total, Offset: req.Offset, Content: content, EOF: eof}, nil
}

// opDeleteRun permanently removes one run's deletion log from this account.
func (s *Server) opDeleteRun(client *clientState, body json.RawMessage) (any, error) {
	var req RunChunkRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("bad request")
	}
	name, err := runFileName(req.File)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(filepath.Join(client.logDir, name)); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("could not delete the log: %w", err)
	}
	return map[string]string{"deleted": name}, nil
}
