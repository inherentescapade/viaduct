package main

import (
	"testing"
	"time"

	"github.com/inherentescapade/viaduct/server"
)

func TestRemoteJobRequestKind(t *testing.T) {
	if k, err := (RemoteJobRequest{Kind: "delete_guild"}).kind(); err != nil || k != server.KindDeleteGuild {
		t.Fatalf("delete_guild: got %q err %v", k, err)
	}
	if k, err := (RemoteJobRequest{Kind: "delete_dm"}).kind(); err != nil || k != server.KindDeleteDM {
		t.Fatalf("delete_dm: got %q err %v", k, err)
	}
	if _, err := (RemoteJobRequest{Kind: "nonsense"}).kind(); err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestRemoteJobRequestToSpec(t *testing.T) {
	req := RemoteJobRequest{Guild: "My Server", Channels: []string{"general"}, User: "bob", Before: "30d", Verify: true}
	spec := req.toSpec()
	if spec.Guild != "My Server" || spec.User != "bob" || spec.Before != "30d" || !spec.Verify {
		t.Fatalf("spec not mapped: %+v", spec)
	}
	if len(spec.Channels) != 1 || spec.Channels[0] != "general" {
		t.Fatalf("channels not mapped: %+v", spec.Channels)
	}
}

func TestMonitorReqToPolicyDefaultsMode(t *testing.T) {
	p := MonitorReq{Name: "x", Mode: "bogus", MaxAgeAmount: 7}.toPolicy()
	if p.Mode != server.ModeExclude {
		t.Fatalf("invalid mode should default to exclude, got %q", p.Mode)
	}
	inc := MonitorReq{Mode: "include"}.toPolicy()
	if inc.Mode != server.ModeInclude {
		t.Fatalf("include mode not preserved, got %q", inc.Mode)
	}
}

func TestToJobDTO(t *testing.T) {
	d := toJobDTO(server.JobStatus{ID: "job-1", Kind: server.KindDeleteDM, State: server.StateRunning, Deleted: 5, Total: 10})
	if d.ID != "job-1" || d.Kind != "delete_dm" || d.State != "running" || d.Deleted != 5 || d.Total != 10 {
		t.Fatalf("job dto mismatch: %+v", d)
	}
}

func TestToMonitorDTOTime(t *testing.T) {
	zero := toMonitorDTO(server.MonitorPolicy{ID: "mon-1", Mode: server.ModeExclude})
	if zero.LastRun != "" {
		t.Fatalf("zero LastRun should be empty, got %q", zero.LastRun)
	}
	ts := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	run := toMonitorDTO(server.MonitorPolicy{ID: "mon-2", LastRun: ts})
	if run.LastRun != ts.Format(time.RFC3339) {
		t.Fatalf("LastRun not RFC3339: %q", run.LastRun)
	}
}

func TestToPingDTO(t *testing.T) {
	p := toPingDTO(&server.PingResponse{Version: "v", HasToken: true, ActingAs: &server.DiscordIdentity{Username: "u", ID: "1"}, Jobs: 2, Monitors: 3})
	if p.Version != "v" || !p.HasToken || p.ActingAs == nil || p.ActingAs.Username != "u" || p.Jobs != 2 || p.Monitors != 3 {
		t.Fatalf("ping dto mismatch: %+v", p)
	}
	noTok := toPingDTO(&server.PingResponse{Version: "v"})
	if noTok.ActingAs != nil {
		t.Fatal("nil ActingAs should map to nil")
	}
}
