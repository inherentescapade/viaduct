// Package auth provides viaduct's self-hosting crypto: a tiny ECIES scheme
// (X25519 key agreement + XChaCha20-Poly1305) that gives every message
// confidentiality, integrity, and sender authentication at once.
//
// The user only ever sees keys. Each side — the client and the server — has one
// small X25519 keypair. You add the other side's public key (a single
// "viaduct1:..." line) to your config, and that's the entire setup. There are no
// TLS certificates, no fingerprints, and no signing ceremony: a message can only
// be decrypted by its intended recipient, and only a holder of an *authorized*
// private key can produce one the recipient will accept.
package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/curve25519"
)

// keyPrefix tags an encoded public key so it's recognisable when copied into a
// config file. The bytes after it are the raw 32-byte X25519 public key,
// standard-base64 encoded.
const keyPrefix = "viaduct1:"

// KeySize is the length of an X25519 public or private key.
const KeySize = 32

// Identity is one side's static X25519 keypair. The private half never leaves
// the machine that generated it.
type Identity struct {
	priv [KeySize]byte
	pub  [KeySize]byte
}

// GenerateIdentity creates a fresh random X25519 identity.
func GenerateIdentity() (*Identity, error) {
	var priv [KeySize]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	return identityFromPrivate(priv)
}

// identityFromPrivate derives the public key for a private scalar.
func identityFromPrivate(priv [KeySize]byte) (*Identity, error) {
	pubSlice, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("derive public key: %w", err)
	}
	id := &Identity{priv: priv}
	copy(id.pub[:], pubSlice)
	return id, nil
}

// Public returns the identity's 32-byte public key.
func (id *Identity) Public() [KeySize]byte { return id.pub }

// PublicKeyString returns the shareable "viaduct1:<base64>" encoding of the
// public key — the line a user adds to the other side's config.
func (id *Identity) PublicKeyString() string { return EncodePublicKey(id.pub) }

// Fingerprint returns a short, human-comparable fingerprint of the public key.
func (id *Identity) Fingerprint() string { return Fingerprint(id.pub) }

// EncodePublicKey renders a public key as "viaduct1:<base64>".
func EncodePublicKey(pub [KeySize]byte) string {
	return keyPrefix + base64.StdEncoding.EncodeToString(pub[:])
}

// ParsePublicKey decodes a public key from either the prefixed form or bare
// base64. Whitespace is tolerated so keys can be pasted from config files.
func ParsePublicKey(s string) ([KeySize]byte, error) {
	var out [KeySize]byte
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, keyPrefix)
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return out, fmt.Errorf("not valid base64: %w", err)
	}
	if len(raw) != KeySize {
		return out, fmt.Errorf("wrong key length: got %d bytes, want %d", len(raw), KeySize)
	}
	copy(out[:], raw)
	return out, nil
}

// Fingerprint returns the first 16 base64 chars of a public key, for quick
// visual comparison in prompts and logs.
func Fingerprint(pub [KeySize]byte) string {
	enc := base64.StdEncoding.EncodeToString(pub[:])
	if len(enc) > 16 {
		enc = enc[:16]
	}
	return enc
}

// SaveIdentity writes the identity's private key (base64) to path with 0600
// permissions. It refuses to overwrite an existing file unless force is set.
func SaveIdentity(id *Identity, path string, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("identity already exists at %s (use --force to overwrite)", path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	enc := base64.StdEncoding.EncodeToString(id.priv[:])
	return os.WriteFile(path, []byte(enc+"\n"), 0600)
}

// LoadIdentity reads a private key previously written by SaveIdentity.
func LoadIdentity(path string) (*Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("identity file is corrupt: %w", err)
	}
	if len(raw) != KeySize {
		return nil, fmt.Errorf("identity file is corrupt: wrong key length %d", len(raw))
	}
	var priv [KeySize]byte
	copy(priv[:], raw)
	return identityFromPrivate(priv)
}

// LoadOrCreateIdentity loads the identity at path, generating and saving a new
// one if the file does not exist. The bool reports whether a new key was made.
func LoadOrCreateIdentity(path string) (*Identity, bool, error) {
	if _, err := os.Stat(path); err == nil {
		id, err := LoadIdentity(path)
		return id, false, err
	}
	id, err := GenerateIdentity()
	if err != nil {
		return nil, false, err
	}
	if err := SaveIdentity(id, path, false); err != nil {
		return nil, false, err
	}
	return id, true, nil
}
