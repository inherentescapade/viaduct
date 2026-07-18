package server

import (
	"github.com/inherentescapade/viaduct/auth"
	"os"
	"path/filepath"
	"testing"
)

// TestLinkDiscordIDMergesDifferentTokenSameUser verifies the core identity
// invariant: a token's bytes aren't stable across devices or re-logins, so
// two accounts that authenticate as the SAME verified Discord user — even
// under two different token strings — must merge into one, carrying over
// monitors and deletion logs and repointing any client key that was routed
// to the duplicate.
func TestLinkDiscordIDMergesDifferentTokenSameUser(t *testing.T) {
	serverID, _ := auth.GenerateIdentity()
	logBase := t.TempDir()
	srv, err := New(Options{
		Identity:     serverID,
		MonitorsPath: t.TempDir() + "/monitors.bin",
		LogDir:       logBase,
		Logf:         func(string, ...any) {},
	})
	if err != nil {
		t.Fatal(err)
	}

	acctA := AccountKey("token-string-one")
	acctB := AccountKey("a-completely-different-token-string")
	csA := srv.newClientState(acctA, Credentials{Token: "token-string-one"})
	csB := srv.newClientState(acctB, Credentials{Token: "a-completely-different-token-string"})
	srv.clients[acctA] = csA
	srv.clients[acctB] = csB

	// Register B under an unrelated placeholder identity first, so linking A
	// below can't trigger the (network-dependent) first-sighting orphan scan
	// against B — this test targets the "already seen, different canonical"
	// merge path specifically.
	srv.byDiscordID["unrelated-placeholder-user"] = acctB
	srv.keyToAccount["desktop-pubkey"] = acctB

	if _, err := csA.monitors.Upsert(MonitorPolicy{Name: "from-a", Scope: "@me", Mode: ModeExclude, MaxAgeAmount: 7}); err != nil {
		t.Fatal(err)
	}
	if _, err := csB.monitors.Upsert(MonitorPolicy{Name: "from-b", Scope: "@me", Mode: ModeExclude, MaxAgeAmount: 3}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(csB.logDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(csB.logDir, "delete_2024-01-01_000000.ndjson"), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}

	const discordUserID = "12345678901234567"
	srv.linkDiscordID(csA, discordUserID) // A becomes canonical for this user
	srv.linkDiscordID(csB, discordUserID) // B turns out to be the same user — merges into A

	if _, ok := srv.clients[acctB]; ok {
		t.Fatal("B's account should be removed from routing after merging into A")
	}
	if got := srv.keyToAccount["desktop-pubkey"]; got != acctA {
		t.Fatalf("desktop-pubkey should now route to A, got %q", got)
	}

	mons := csA.monitors.List()
	names := map[string]bool{}
	for _, m := range mons {
		names[m.Name] = true
	}
	if len(mons) != 2 || !names["from-a"] || !names["from-b"] {
		t.Fatalf("A should hold both monitors after the merge, got %+v", mons)
	}
	if bMons := csB.monitors.List(); len(bMons) != 0 {
		t.Fatalf("B's monitor should be removed after moving, so it can't double-run, got %+v", bMons)
	}

	entries, err := os.ReadDir(csA.logDir)
	if err != nil {
		t.Fatalf("reading A's log dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("A's log dir should now hold B's moved log file, got %d entries", len(entries))
	}
}

// TestLinkDiscordIDNoopForSameAccount ensures re-confirming the same
// account's own Discord ID is a harmless no-op, not a self-merge. There are
// no other accounts on the server, so the first-sighting orphan scan has
// nothing to check and this runs with no network dependency.
func TestLinkDiscordIDNoopForSameAccount(t *testing.T) {
	serverID, _ := auth.GenerateIdentity()
	srv, err := New(Options{
		Identity:     serverID,
		MonitorsPath: t.TempDir() + "/monitors.bin",
		Logf:         func(string, ...any) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	acct := AccountKey("tok")
	cs := srv.newClientState(acct, Credentials{Token: "tok"})
	srv.clients[acct] = cs

	srv.linkDiscordID(cs, "user-1")
	srv.linkDiscordID(cs, "user-1")

	if _, ok := srv.clients[acct]; !ok {
		t.Fatal("the account should still be present after a repeated same-account link")
	}
}

// TestLinkDiscordIDSkipsOrphansWithNoToken ensures the first-sighting orphan
// scan doesn't attempt to validate (or crash on) an account that has no
// stored token at all — the exact case for a client key that's paired but
// never pushed credentials.
func TestLinkDiscordIDSkipsOrphansWithNoToken(t *testing.T) {
	serverID, _ := auth.GenerateIdentity()
	srv, err := New(Options{
		Identity:     serverID,
		MonitorsPath: t.TempDir() + "/monitors.bin",
		Logf:         func(string, ...any) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	acctSeen := AccountKey("tok-seen")
	csSeen := srv.newClientState(acctSeen, Credentials{Token: "tok-seen"})
	srv.clients[acctSeen] = csSeen

	blankKey := "never-authenticated-client-pubkey"
	csBlank := srv.newClientState(blankKey, Credentials{}) // no token at all
	srv.clients[blankKey] = csBlank

	srv.linkDiscordID(csSeen, "user-1") // scans orphans; csBlank has no token to validate

	if _, ok := srv.clients[blankKey]; !ok {
		t.Fatal("an orphan with no stored token must be left alone, not merged or removed")
	}
}
