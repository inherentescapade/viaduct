package server

import (
	"github.com/inherentescapade/viaduct/auth"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClientStateSegregated verifies that two authorized clients on the same
// server see entirely separate worlds: one client's monitors and pushed token
// are invisible to the other.
func TestClientStateSegregated(t *testing.T) {
	alice, _ := auth.GenerateIdentity()
	bob, _ := auth.GenerateIdentity()
	serverID, _ := auth.GenerateIdentity()

	saved := map[string]Credentials{}
	srv, err := New(Options{
		Identity:       serverID,
		AuthorizedKeys: []string{alice.PublicKeyString(), bob.PublicKeyString()},
		MonitorsPath:   t.TempDir() + "/monitors.bin",
		SaveAccount:    func(key string, c Credentials) error { saved[key] = c; return nil },
		Logf:           func(string, ...any) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")

	ac := NewClient(addr, alice, serverID.Public())
	bc := NewClient(addr, bob, serverID.Public())

	// Alice creates a monitor.
	if _, err := ac.SetMonitor(MonitorRequest{Policy: MonitorPolicy{
		Name: "alice-tidy", Scope: "@me", Mode: ModeExclude, MaxAgeAmount: 7,
	}}); err != nil {
		t.Fatalf("alice set monitor: %v", err)
	}

	// Alice sees her monitor; Bob sees none.
	if mons, _ := ac.ListMonitors(); len(mons) != 1 {
		t.Fatalf("alice should see her 1 monitor, got %d", len(mons))
	}
	if mons, _ := bc.ListMonitors(); len(mons) != 0 {
		t.Fatalf("bob must not see alice's monitors, got %d", len(mons))
	}

	// Each client's ping reports only its own monitor count.
	if p, _ := ac.Ping(); p.Monitors != 1 {
		t.Fatalf("alice ping monitors = %d, want 1", p.Monitors)
	}
	if p, _ := bc.Ping(); p.Monitors != 0 {
		t.Fatalf("bob ping monitors = %d, want 0", p.Monitors)
	}
}

// TestNoSharedTokenFallback ensures a client cannot ride on a token another
// client pushed: with no token of its own, an operation that needs Discord must
// fail rather than silently using someone else's credentials.
func TestNoSharedTokenFallback(t *testing.T) {
	alice, _ := auth.GenerateIdentity()
	bob, _ := auth.GenerateIdentity()
	serverID, _ := auth.GenerateIdentity()

	srv, err := New(Options{
		Identity:       serverID,
		AuthorizedKeys: []string{alice.PublicKeyString(), bob.PublicKeyString()},
		// Alice already has a persisted token; Bob has none.
		InitialAccounts: map[string]Credentials{
			AccountKey("alice-token"): {Token: "alice-token"},
		},
		InitialClientKeys: map[string]string{
			alice.PublicKeyString(): AccountKey("alice-token"),
		},
		MonitorsPath: t.TempDir() + "/monitors.bin",
		Logf:         func(string, ...any) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")

	// Alice's ping reports a token; Bob's does not — Bob never inherits hers.
	if p, _ := NewClient(addr, alice, serverID.Public()).Ping(); !p.HasToken {
		t.Fatal("alice should have her persisted token")
	}
	if p, _ := NewClient(addr, bob, serverID.Public()).Ping(); p.HasToken {
		t.Fatal("bob must not see alice's token")
	}
}

// TestSameTokenSharesAccount verifies the inverse of segregation: two
// different client keys (e.g. two machines) that present the identical
// Discord token are guaranteed to be the same person, so they land on the
// very same account — same jobs, same monitors, same rate limiter — rather
// than each getting a blank world of their own. A client that never presents
// the token stays segregated, and a linked client keeps routing to its
// account on later requests that omit credentials (ping, list_jobs, ...).
func TestSameTokenSharesAccount(t *testing.T) {
	serverID, _ := auth.GenerateIdentity()
	laptop, _ := auth.GenerateIdentity()
	desktop, _ := auth.GenerateIdentity()
	stranger, _ := auth.GenerateIdentity()

	srv, err := New(Options{
		Identity:     serverID,
		MonitorsPath: t.TempDir() + "/monitors.bin",
		Logf:         func(string, ...any) {},
	})
	if err != nil {
		t.Fatal(err)
	}

	const sharedToken = "same-person-token"

	laptopState := srv.clientFor(laptop.Public(), &Credentials{Token: sharedToken})
	desktopState := srv.clientFor(desktop.Public(), &Credentials{Token: sharedToken})
	if laptopState != desktopState {
		t.Fatal("two client keys presenting the same token must share one account")
	}

	// Desktop keeps routing to the shared account even on a request that omits
	// credentials, because the routing was remembered.
	if again := srv.clientFor(desktop.Public(), nil); again != desktopState {
		t.Fatal("a linked client key should keep routing to its account without resending credentials")
	}

	// A stranger who never presents the token gets its own, separate bucket.
	strangerState := srv.clientFor(stranger.Public(), nil)
	if strangerState == laptopState {
		t.Fatal("a client that never presented the token must not share the account")
	}
}

// TestConcurrentRunsShareOneLimiter checks that all of an account's deletion
// work (jobs and monitors) runs over a single Discord client, so they coordinate
// on one rate limiter instead of 429-ing each other. A credential change rebuilds
// it.
func TestConcurrentRunsShareOneLimiter(t *testing.T) {
	serverID, _ := auth.GenerateIdentity()
	srv, err := New(Options{
		Identity:     serverID,
		MonitorsPath: t.TempDir() + "/monitors.bin",
		Logf:         func(string, ...any) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	cs := srv.newClientState("viaduct1:test", Credentials{})

	a := cs.sharedAPIClient(Credentials{Token: "tok"})
	b := cs.sharedAPIClient(Credentials{Token: "tok"})
	if a != b {
		t.Fatal("same credentials must reuse one client (one shared limiter)")
	}
	if c := cs.sharedAPIClient(Credentials{Token: "rotated"}); c == a {
		t.Fatal("a changed token must rebuild the client")
	}
}

// TestLogStatsSegregated checks that one client's deletion logs never surface in
// another client's log-stats summary.
func TestLogStatsSegregated(t *testing.T) {
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

	// Drop a deletion log into alice's private log directory only.
	aliceDir := ClientLogDir(logBase, alice.PublicKeyString())
	if err := os.MkdirAll(aliceDir, 0700); err != nil {
		t.Fatal(err)
	}
	line := []byte(`{"id":"1","content":"hello","channel_id":"c1"}` + "\n")
	if err := os.WriteFile(filepath.Join(aliceDir, "delete_2024-01-01_000000.ndjson"), line, 0600); err != nil {
		t.Fatal(err)
	}

	aliceStats, err := NewClient(addr, alice, serverID.Public()).LogStats()
	if err != nil {
		t.Fatalf("alice log stats: %v", err)
	}
	if aliceStats.TotalDeleted != 1 {
		t.Fatalf("alice should see her 1 deletion, got %d", aliceStats.TotalDeleted)
	}

	bobStats, err := NewClient(addr, bob, serverID.Public()).LogStats()
	if err != nil {
		t.Fatalf("bob log stats: %v", err)
	}
	if bobStats.TotalDeleted != 0 {
		t.Fatalf("bob must not see alice's deletions, got %d", bobStats.TotalDeleted)
	}
}
