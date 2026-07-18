package server

import (
	"github.com/inherentescapade/viaduct/auth"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newPairableServer spins up a server that authorizes NO clients up front. It
// returns the server, its raw host:port, the server identity, a pointer that
// captures the last persisted key, and a pointer that captures the code the
// server "shows" when a client requests pairing.
func newPairableServer(t *testing.T) (*Server, string, *auth.Identity, *string, *string) {
	t.Helper()
	serverID, _ := auth.GenerateIdentity()
	var persisted, shownCode string
	srv, err := New(Options{
		Identity:      serverID,
		MonitorsPath:  t.TempDir() + "/monitors.bin",
		AuthorizeKey:  func(k string) error { persisted = k; return nil },
		OnPairingCode: func(code string, _ time.Time, _ string) { shownCode = code },
		Logf:          func(string, ...any) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, strings.TrimPrefix(ts.URL, "http://"), serverID, &persisted, &shownCode
}

func TestPairingAuthorizesClientLive(t *testing.T) {
	_, addr, serverID, persisted, shownCode := newPairableServer(t)
	clientID, _ := auth.GenerateIdentity()

	// Before pairing, the client is a stranger and can't even ping.
	if _, err := NewClient(addr, clientID, serverID.Public()).Ping(); err == nil {
		t.Fatal("unpaired client should be rejected")
	}

	// Requesting pairing makes the server show a code (captured here); the user
	// then enters it to complete.
	if err := PairBegin(addr, clientID); err != nil {
		t.Fatalf("pair begin: %v", err)
	}
	if *shownCode == "" {
		t.Fatal("requesting pairing should have shown a code on the server")
	}
	serverPub, err := PairComplete(addr, clientID, *shownCode)
	if err != nil {
		t.Fatalf("pair complete: %v", err)
	}
	if serverPub != serverID.Public() {
		t.Fatal("pairing returned the wrong server key")
	}
	if *persisted != clientID.PublicKeyString() {
		t.Fatalf("paired key not persisted: %q", *persisted)
	}

	// The authorization must take effect immediately (no restart): a sealed ping
	// from the just-paired client now succeeds.
	if _, err := NewClient(addr, clientID, serverPub).Ping(); err != nil {
		t.Fatalf("paired client should be authorized live: %v", err)
	}
}

// TestPairingSurvivesRestart proves pairing is one-time: once a client pairs,
// it keeps working across a server restart with no code and no re-pairing,
// because the authorized key and token are persisted and reloaded.
func TestPairingSurvivesRestart(t *testing.T) {
	clientID, _ := auth.GenerateIdentity()
	serverID, _ := auth.GenerateIdentity()
	monitors := t.TempDir() + "/monitors.bin"

	// Stand-in for the server's persisted config.
	var authorized []string
	tokens := map[string]Credentials{}
	newServer := func() *Server {
		srv, err := New(Options{
			Identity:        serverID,
			AuthorizedKeys:  authorized,
			InitialAccounts: tokens,
			MonitorsPath:    monitors,
			AuthorizeKey:    func(k string) error { authorized = append(authorized, k); return nil },
			SaveAccount:     func(k string, c Credentials) error { tokens[k] = c; return nil },
			Logf:            func(string, ...any) {},
		})
		if err != nil {
			t.Fatal(err)
		}
		return srv
	}

	// First boot: pair the client.
	srv1 := newServer()
	ts1 := httptest.NewServer(srv1.Handler())
	addr1 := strings.TrimPrefix(ts1.URL, "http://")
	code, _, _ := srv1.ArmPairing()
	if _, err := PairComplete(addr1, clientID, code); err != nil {
		t.Fatalf("pair: %v", err)
	}
	ts1.Close()

	// Restart: a brand-new server from the persisted state, with NO pairing code
	// armed. The already-paired client must still be accepted.
	srv2 := newServer()
	ts2 := httptest.NewServer(srv2.Handler())
	defer ts2.Close()
	addr2 := strings.TrimPrefix(ts2.URL, "http://")

	if srv2.pairing.active() {
		t.Fatal("a restarted server should not have a pairing code armed")
	}
	if _, err := NewClient(addr2, clientID, serverID.Public()).Ping(); err != nil {
		t.Fatalf("a paired client must work after restart with no re-pairing: %v", err)
	}
}

func TestPairingWrongCodeRejected(t *testing.T) {
	_, addr, _, persisted, shownCode := newPairableServer(t)
	clientID, _ := auth.GenerateIdentity()

	if err := PairBegin(addr, clientID); err != nil {
		t.Fatal(err)
	}
	if _, err := PairComplete(addr, clientID, bumpDigits(*shownCode)); err == nil {
		t.Fatal("a wrong code must fail pairing")
	}
	if *persisted != "" {
		t.Fatal("a failed pairing must not authorize any key")
	}
}

func TestPairingRequiresRequestFirst(t *testing.T) {
	_, addr, _, _, _ := newPairableServer(t)
	clientID, _ := auth.GenerateIdentity()

	// No request was made, so no code is armed: completing must fail.
	if _, err := PairComplete(addr, clientID, "123456"); err == nil {
		t.Fatal("pairing must fail when no code has been requested")
	}
}

// runClientHalf performs the client side of the SPAKE2 exchange against the
// manager directly (no HTTP), returning whether the manager accepted it.
func runClientHalf(t *testing.T, p *pairingManager, serverPub, clientPub [auth.KeySize]byte, code string) error {
	t.Helper()
	sp, err := auth.NewSpake2(auth.SpakeClient, []byte(code))
	if err != nil {
		t.Fatal(err)
	}
	sid, msgB, _, err := p.start(serverPub, clientPub, sp.Message())
	if err != nil {
		return err
	}
	ke, err := sp.Finish(msgB)
	if err != nil {
		t.Fatal(err)
	}
	cc := pairConfirm(ke, pairClientConfirmLabel, serverPub, clientPub)
	_, err = p.confirm(serverPub, sid, cc)
	return err
}

func TestPairingCodeIsSingleUse(t *testing.T) {
	p := newPairingManager()
	server, _ := auth.GenerateIdentity()
	client, _ := auth.GenerateIdentity()
	code, _, _ := p.arm()

	if err := runClientHalf(t, p, server.Public(), client.Public(), code); err != nil {
		t.Fatalf("first pairing should succeed: %v", err)
	}
	if p.active() {
		t.Fatal("the code should be consumed after a successful pairing")
	}
	if err := runClientHalf(t, p, server.Public(), client.Public(), code); err == nil {
		t.Fatal("a consumed code must not pair a second time")
	}
}

func TestPairingCodeExpires(t *testing.T) {
	p := newPairingManager()
	server, _ := auth.GenerateIdentity()
	client, _ := auth.GenerateIdentity()
	code, _, _ := p.arm()

	// Jump past the TTL.
	p.now = func() time.Time { return time.Now().Add(pairingTTL + time.Minute) }
	if p.active() {
		t.Fatal("an expired code should not be active")
	}
	if err := runClientHalf(t, p, server.Public(), client.Public(), code); err == nil {
		t.Fatal("an expired code must not pair")
	}
}

func TestPairingBurnsAfterTooManyAttempts(t *testing.T) {
	p := newPairingManager()
	server, _ := auth.GenerateIdentity()
	client, _ := auth.GenerateIdentity()
	code, _, _ := p.arm()

	sp, _ := auth.NewSpake2(auth.SpakeClient, []byte(code))
	// Starts and wrong confirms draw on one shared budget; once it's spent the
	// code burns, so start eventually refuses to open a new session.
	for i := 0; i < maxPairAttempts; i++ {
		sid, _, _, err := p.start(server.Public(), client.Public(), sp.Message())
		if err != nil {
			break // budget already spent by a prior start
		}
		if _, err := p.confirm(server.Public(), sid, "wrong"); err == nil {
			t.Fatal("a wrong confirm should error")
		}
	}
	if p.active() {
		t.Fatal("the code should be burned after too many wrong attempts")
	}
}

// TestPairingBurnsAfterTooManyStarts is the regression test for the start-phase
// guess oracle: the serverConfirm returned by start lets the caller check the
// password guess baked into msgA, so a run of starts — with no confirm ever
// sent — must still exhaust the budget and burn the code.
func TestPairingBurnsAfterTooManyStarts(t *testing.T) {
	p := newPairingManager()
	server, _ := auth.GenerateIdentity()
	client, _ := auth.GenerateIdentity()
	code, _, _ := p.arm()
	sp, _ := auth.NewSpake2(auth.SpakeClient, []byte(code))

	burned := false
	for i := 0; i < maxPairAttempts; i++ {
		if _, _, _, err := p.start(server.Public(), client.Public(), sp.Message()); err != nil {
			burned = true
			break
		}
	}
	if !burned {
		t.Fatal("repeated starts must burn the code without any confirm")
	}
	if p.active() {
		t.Fatal("the code should be burned after too many start attempts")
	}
}

// bumpDigits returns a different code of the same length by incrementing the
// first digit modulo 10 — handy for producing a guaranteed-wrong code.
func bumpDigits(code string) string {
	b := []byte(code)
	b[0] = '0' + (b[0]-'0'+1)%10
	return string(b)
}
