package server

import (
	"github.com/inherentescapade/viaduct/auth"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestServer spins up a Server (no Discord token) authorizing the given
// client, returns a Client wired to it, and a cleanup func.
func newTestServer(t *testing.T, clientID *auth.Identity) (*Client, *Server) {
	t.Helper()
	serverID, _ := auth.GenerateIdentity()
	srv, err := New(Options{
		Identity:       serverID,
		AuthorizedKeys: []string{clientID.PublicKeyString()},
		MonitorsPath:   t.TempDir() + "/monitors.bin",
		Logf:           func(string, ...any) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	addr := strings.TrimPrefix(ts.URL, "http://")
	return NewClient(addr, clientID, serverID.Public()), srv
}

func TestPingRoundTrip(t *testing.T) {
	clientID, _ := auth.GenerateIdentity()
	c, _ := newTestServer(t, clientID)

	resp, err := c.Ping()
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	if resp.Version != Version {
		t.Fatalf("unexpected version %q", resp.Version)
	}
	if resp.HasToken {
		t.Fatal("fresh server should not report a token")
	}
}

func TestUnauthorizedClientRejected(t *testing.T) {
	authorized, _ := auth.GenerateIdentity()
	c, srv := newTestServer(t, authorized)

	// Re-point the client to use a DIFFERENT (unauthorized) identity but the
	// same server key.
	stranger, _ := auth.GenerateIdentity()
	c.identity = stranger

	if _, err := c.Ping(); err == nil {
		t.Fatal("server must reject an unauthorized client key")
	}
	_ = srv
}

func TestWrongServerKeyRejected(t *testing.T) {
	clientID, _ := auth.GenerateIdentity()
	c, _ := newTestServer(t, clientID)

	// Client expects the wrong server key: requests are sealed to a key the
	// server can't open, so it can't decrypt them.
	bogus, _ := auth.GenerateIdentity()
	c.serverPub = bogus.Public()

	if _, err := c.Ping(); err == nil {
		t.Fatal("a client sealing to the wrong server key must fail")
	}
}

func TestMonitorLifecycle(t *testing.T) {
	clientID, _ := auth.GenerateIdentity()
	c, _ := newTestServer(t, clientID)

	// Creating a monitor doesn't need Discord; it just stores the policy.
	pol, err := c.SetMonitor(MonitorRequest{Policy: MonitorPolicy{
		Name:         "keep dms tidy",
		Enabled:      true,
		Scope:        "@me",
		Mode:         ModeExclude,
		MaxAgeAmount: 7,
	}})
	if err != nil {
		t.Fatalf("set monitor: %v", err)
	}
	if pol.ID == "" {
		t.Fatal("server should assign a monitor ID")
	}
	if pol.IntervalHrs != defaultMonitorIntervalHrs {
		t.Fatalf("interval default not applied: %d", pol.IntervalHrs)
	}

	list, err := c.ListMonitors()
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 monitor, got %d (err %v)", len(list), err)
	}

	if err := c.DeleteMonitor(pol.ID); err != nil {
		t.Fatalf("delete monitor: %v", err)
	}
	list, _ = c.ListMonitors()
	if len(list) != 0 {
		t.Fatalf("expected 0 monitors after delete, got %d", len(list))
	}
}

func TestMonitorRejectsBadAge(t *testing.T) {
	clientID, _ := auth.GenerateIdentity()
	c, _ := newTestServer(t, clientID)
	if _, err := c.SetMonitor(MonitorRequest{Policy: MonitorPolicy{Name: "x", MaxAgeAmount: 0}}); err == nil {
		t.Fatal("monitor with non-positive age should be rejected")
	}
}

func TestMonitorPersistence(t *testing.T) {
	path := t.TempDir() + "/monitors.bin"

	mk := func() *MonitorManager {
		return NewMonitorManager(path, nil, func() Credentials { return Credentials{} }, func(string, ...any) {})
	}
	m1 := mk()
	if _, err := m1.Upsert(MonitorPolicy{Name: "a", Scope: "@me", Mode: ModeExclude, MaxAgeAmount: 30}); err != nil {
		t.Fatal(err)
	}

	// A fresh manager reading the same file must see the persisted policy.
	m2 := mk()
	if m2.Count() != 1 {
		t.Fatalf("persisted policy not reloaded: count=%d", m2.Count())
	}
}

func TestMonitorRunsPromptlyWhenEnabled(t *testing.T) {
	m := NewMonitorManager("", nil, func() Credentials { return Credentials{} }, func(string, ...any) {})

	on, err := m.Upsert(MonitorPolicy{Name: "on", Scope: "@me", Mode: ModeExclude, MaxAgeAmount: 7, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	// An enabled monitor must be due now (not a full interval from now), so the
	// scheduler runs it on the next tick rather than hours later.
	if on.NextRun.After(time.Now()) {
		t.Fatalf("enabled monitor should be due immediately, NextRun=%v", on.NextRun)
	}

	off, err := m.Upsert(MonitorPolicy{Name: "off", Scope: "@me", Mode: ModeExclude, MaxAgeAmount: 7, Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if !off.NextRun.IsZero() {
		t.Fatalf("disabled monitor should have no scheduled run, NextRun=%v", off.NextRun)
	}

	// Toggling on schedules it; toggling off clears it.
	off.Enabled = true
	reEnabled, _ := m.Upsert(off)
	if reEnabled.NextRun.IsZero() {
		t.Fatal("re-enabling should schedule a run")
	}
}

func TestMonitorUpdateKeepsRunHistory(t *testing.T) {
	m := NewMonitorManager("", nil, func() Credentials { return Credentials{} }, func(string, ...any) {})

	p, err := m.Upsert(MonitorPolicy{Name: "keep", Scope: "@me", Mode: ModeExclude, MaxAgeAmount: 7, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a completed run.
	ran := time.Now().Add(-time.Hour)
	m.mu.Lock()
	m.policies[p.ID].LastRun = ran
	m.policies[p.ID].LastDeleted = 42
	m.policies[p.ID].Recent = []FeedMessage{{Content: "bye"}}
	m.mu.Unlock()

	// An edit sends only policy fields (as the UI does) — run history must survive.
	upd, err := m.Upsert(MonitorPolicy{ID: p.ID, Name: "keep-renamed", Scope: "@me", Mode: ModeExclude, MaxAgeAmount: 3, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if !upd.LastRun.Equal(ran) || upd.LastDeleted != 42 || len(upd.Recent) != 1 {
		t.Fatalf("update dropped run history: lastRun=%v deleted=%d recent=%d", upd.LastRun, upd.LastDeleted, len(upd.Recent))
	}
	if upd.Name != "keep-renamed" || upd.MaxAgeAmount != 3 {
		t.Fatalf("update did not apply policy fields: %+v", upd)
	}
}

func TestJobNotFound(t *testing.T) {
	clientID, _ := auth.GenerateIdentity()
	c, _ := newTestServer(t, clientID)
	if _, err := c.GetJob("job-999"); err == nil {
		t.Fatal("expected not-found error for unknown job")
	}
}

func TestApplyMonitorMode(t *testing.T) {
	chans := mkChannels("general", "secret-dm", "memes")
	inc := applyMonitorMode(chans, MonitorPolicy{Mode: ModeInclude, Channels: []string{"secret"}})
	if len(inc) != 1 || inc[0].Name != "secret-dm" {
		t.Fatalf("include mode wrong: %+v", inc)
	}
	exc := applyMonitorMode(chans, MonitorPolicy{Mode: ModeExclude, Channels: []string{"secret"}})
	if len(exc) != 2 {
		t.Fatalf("exclude mode wrong: %+v", exc)
	}
}
