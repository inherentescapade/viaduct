package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

// eciesInfo domain-separates this scheme's key derivation from any other use of
// the same keys.
const eciesInfo = "viaduct-ecies-v1"

// Envelope is a sealed message: an ephemeral public key, the sender's static
// public key, and the XChaCha20-Poly1305 ciphertext. It carries no plaintext
// metadata — even the sender identity is authenticated (it's mixed into the AEAD
// additional data) but only meaningful to a recipient who can decrypt.
type Envelope struct {
	Sender     [KeySize]byte // sender's static public key
	Ephemeral  [KeySize]byte // per-message ephemeral public key
	Nonce      [chacha20poly1305.NonceSizeX]byte
	Ciphertext []byte
}

// Seal encrypts plaintext from sender to the holder of recipientPub.
//
// The AEAD key is derived from two ECDH shared secrets:
//   - es = X25519(ephemeral, recipient)  — fresh per message (forward secrecy)
//   - ss = X25519(sender,    recipient)  — proves the sender's static identity
//
// Mixing ss in means only a holder of sender's private key can produce a message
// the recipient will successfully decrypt: that is the authentication.
func Seal(plaintext []byte, sender *Identity, recipientPub [KeySize]byte) (*Envelope, error) {
	var ephPriv [KeySize]byte
	if _, err := rand.Read(ephPriv[:]); err != nil {
		return nil, err
	}
	ephPubSlice, err := curve25519.X25519(ephPriv[:], curve25519.Basepoint)
	if err != nil {
		return nil, err
	}
	var ephPub [KeySize]byte
	copy(ephPub[:], ephPubSlice)

	es, err := curve25519.X25519(ephPriv[:], recipientPub[:])
	if err != nil {
		return nil, err
	}
	ss, err := curve25519.X25519(sender.priv[:], recipientPub[:])
	if err != nil {
		return nil, err
	}

	key := deriveKey(es, ss, sender.pub, ephPub, recipientPub)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}

	env := &Envelope{Sender: sender.pub, Ephemeral: ephPub}
	if _, err := io.ReadFull(rand.Reader, env.Nonce[:]); err != nil {
		return nil, err
	}
	aad := aadFor(sender.pub, ephPub, recipientPub)
	env.Ciphertext = aead.Seal(nil, env.Nonce[:], plaintext, aad)
	return env, nil
}

// Open decrypts an envelope addressed to recipient, returning the plaintext and
// the authenticated sender public key. A failure here means either the message
// wasn't for us or it was forged/tampered — callers should treat any error as
// "reject".
func Open(env *Envelope, recipient *Identity) (plaintext []byte, sender [KeySize]byte, err error) {
	es, err := curve25519.X25519(recipient.priv[:], env.Ephemeral[:])
	if err != nil {
		return nil, sender, err
	}
	ss, err := curve25519.X25519(recipient.priv[:], env.Sender[:])
	if err != nil {
		return nil, sender, err
	}

	key := deriveKey(es, ss, env.Sender, env.Ephemeral, recipient.pub)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, sender, err
	}
	aad := aadFor(env.Sender, env.Ephemeral, recipient.pub)
	pt, err := aead.Open(nil, env.Nonce[:], env.Ciphertext, aad)
	if err != nil {
		return nil, sender, fmt.Errorf("decryption failed (wrong recipient, tampered, or forged)")
	}
	return pt, env.Sender, nil
}

// deriveKey turns the two shared secrets into a 32-byte AEAD key via HKDF-SHA256,
// binding the derivation to all three public keys so a key can't be reused
// across different sender/recipient/ephemeral combinations.
func deriveKey(es, ss []byte, senderPub, ephPub, recipientPub [KeySize]byte) []byte {
	ikm := make([]byte, 0, len(es)+len(ss))
	ikm = append(ikm, es...)
	ikm = append(ikm, ss...)
	info := aadFor(senderPub, ephPub, recipientPub)
	r := hkdf.New(sha256.New, ikm, nil, info)
	key := make([]byte, chacha20poly1305.KeySize)
	_, _ = io.ReadFull(r, key)
	return key
}

func aadFor(senderPub, ephPub, recipientPub [KeySize]byte) []byte {
	aad := make([]byte, 0, len(eciesInfo)+3*KeySize)
	aad = append(aad, eciesInfo...)
	aad = append(aad, senderPub[:]...)
	aad = append(aad, ephPub[:]...)
	aad = append(aad, recipientPub[:]...)
	return aad
}
