package auth

import (
	"testing"
	"time"
)

func TestPublicKeyRoundTrip(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	enc := id.PublicKeyString()
	pub, err := ParsePublicKey(enc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if Fingerprint(pub) != id.Fingerprint() {
		t.Fatalf("fingerprint mismatch after round trip")
	}
	if _, err := ParsePublicKey(enc[len("viaduct1:"):]); err != nil {
		t.Fatalf("bare base64 should parse: %v", err)
	}
}

func TestParsePublicKeyRejectsGarbage(t *testing.T) {
	if _, err := ParsePublicKey("not-a-key"); err == nil {
		t.Fatal("expected error for garbage key")
	}
	if _, err := ParsePublicKey("viaduct1:AAAA"); err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestSaveLoadIdentity(t *testing.T) {
	id, _ := GenerateIdentity()
	path := t.TempDir() + "/identity.key"
	if err := SaveIdentity(id, path, false); err != nil {
		t.Fatal(err)
	}
	if err := SaveIdentity(id, path, false); err == nil {
		t.Fatal("expected refusal to overwrite without force")
	}
	if err := SaveIdentity(id, path, true); err != nil {
		t.Fatalf("force overwrite should succeed: %v", err)
	}
	loaded, err := LoadIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PublicKeyString() != id.PublicKeyString() {
		t.Fatal("loaded identity differs from saved")
	}
}

func TestLoadOrCreateIdentity(t *testing.T) {
	path := t.TempDir() + "/id.key"
	id1, created, err := LoadOrCreateIdentity(path)
	if err != nil || !created {
		t.Fatalf("first call should create: created=%v err=%v", created, err)
	}
	id2, created, err := LoadOrCreateIdentity(path)
	if err != nil || created {
		t.Fatalf("second call should load: created=%v err=%v", created, err)
	}
	if id1.PublicKeyString() != id2.PublicKeyString() {
		t.Fatal("identity changed between load-or-create calls")
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	client, _ := GenerateIdentity()
	server, _ := GenerateIdentity()
	msg := []byte("delete my embarrassing messages")

	env, err := Seal(msg, client, server.Public())
	if err != nil {
		t.Fatal(err)
	}
	got, sender, err := Open(env, server)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(got) != string(msg) {
		t.Fatalf("plaintext mismatch: %q", got)
	}
	if sender != client.Public() {
		t.Fatal("authenticated sender does not match client")
	}
}

func TestOpenRejectsWrongRecipient(t *testing.T) {
	client, _ := GenerateIdentity()
	server, _ := GenerateIdentity()
	eve, _ := GenerateIdentity()

	env, _ := Seal([]byte("secret"), client, server.Public())
	if _, _, err := Open(env, eve); err == nil {
		t.Fatal("a different recipient must not be able to open the envelope")
	}
}

func TestOpenRejectsTamperedCiphertext(t *testing.T) {
	client, _ := GenerateIdentity()
	server, _ := GenerateIdentity()
	env, _ := Seal([]byte("secret"), client, server.Public())
	env.Ciphertext[0] ^= 0xff
	if _, _, err := Open(env, server); err == nil {
		t.Fatal("tampered ciphertext must fail to open")
	}
}

func TestKeySetAuthorization(t *testing.T) {
	a, _ := GenerateIdentity()
	b, _ := GenerateIdentity()
	ks, err := NewKeySet([]string{a.PublicKeyString()})
	if err != nil {
		t.Fatal(err)
	}
	if !ks.Authorized(a.Public()) {
		t.Fatal("a should be authorized")
	}
	if ks.Authorized(b.Public()) {
		t.Fatal("b should not be authorized")
	}
}

func TestNewKeySetRejectsBadKey(t *testing.T) {
	if _, err := NewKeySet([]string{"garbage"}); err == nil {
		t.Fatal("expected error for malformed authorized key")
	}
}

func TestReplayGuard(t *testing.T) {
	g := NewReplayGuard()
	now := time.Now().Unix()
	if err := g.Check("nonce-1", now); err != nil {
		t.Fatalf("first use should pass: %v", err)
	}
	if err := g.Check("nonce-1", now); err == nil {
		t.Fatal("replayed nonce should be rejected")
	}
	if err := g.Check("nonce-2", now); err != nil {
		t.Fatalf("fresh nonce should pass: %v", err)
	}
}

func TestReplayGuardRejectsStale(t *testing.T) {
	g := NewReplayGuard()
	g.now = func() time.Time { return time.Now().Add(10 * time.Minute) }
	if err := g.Check("nonce", time.Now().Unix()); err == nil {
		t.Fatal("stale timestamp should be rejected")
	}
}
