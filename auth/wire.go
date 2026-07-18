package auth

import (
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

// nonceSize is the XChaCha20-Poly1305 nonce length.
const nonceSize = chacha20poly1305.NonceSizeX

// headerSize is the fixed prefix before the ciphertext: sender key, ephemeral
// key, and nonce.
const headerSize = KeySize + KeySize + nonceSize

// MarshalBinary encodes the envelope as a compact byte slice:
//
//	[ sender(32) | ephemeral(32) | nonce(24) | ciphertext... ]
func (e *Envelope) MarshalBinary() ([]byte, error) {
	out := make([]byte, 0, headerSize+len(e.Ciphertext))
	out = append(out, e.Sender[:]...)
	out = append(out, e.Ephemeral[:]...)
	out = append(out, e.Nonce[:]...)
	out = append(out, e.Ciphertext...)
	return out, nil
}

// UnmarshalBinary parses an envelope produced by MarshalBinary.
func (e *Envelope) UnmarshalBinary(data []byte) error {
	if len(data) < headerSize {
		return fmt.Errorf("envelope too short: %d bytes", len(data))
	}
	off := 0
	copy(e.Sender[:], data[off:off+KeySize])
	off += KeySize
	copy(e.Ephemeral[:], data[off:off+KeySize])
	off += KeySize
	copy(e.Nonce[:], data[off:off+nonceSize])
	off += nonceSize
	e.Ciphertext = append([]byte(nil), data[off:]...)
	return nil
}
