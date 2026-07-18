package auth

import (
	"crypto/rand"
	"crypto/sha512"
	"fmt"

	"filippo.io/edwards25519"
)

// SPAKE2 (RFC 9382, edwards25519 group) is a balanced Password-Authenticated Key
// Exchange. Two parties who share only a low-entropy secret — viaduct's 6-digit
// pairing code — derive a strong shared key, with the property that an attacker
// (even an active man in the middle) gets at most ONE online password guess per
// exchange. Captured traffic never enables an offline dictionary attack, which
// is the weakness a plain HMAC-of-the-code scheme would have.
//
// How it works: each side blinds a Diffie-Hellman share with the password.
// The client sends T_A = x·B + w·M; the server sends T_B = y·B + w·N (B is the
// base point, w the password scalar, M/N fixed nothing-up-my-sleeve points).
// Each unblinds the peer's value with w and multiplies by its own scalar, so
// both reach K = (cofactor)·x·y·B only if they used the same w. The shared key
// is a hash of the full transcript; a confirmation MAC over it (see
// server/pairing.go)
// proves agreement and binds the static identity keys being exchanged.

// SpakeRole distinguishes the two ends; they must use different blinding points
// (M vs N) to avoid a reflection attack.
type SpakeRole int

const (
	SpakeClient SpakeRole = iota // blinds with M
	SpakeServer                  // blinds with N
)

// spakeTranscriptLabel domain-separates the shared-key derivation.
const spakeTranscriptLabel = "viaduct-spake2-v1"

// spakePasswordLabel domain-separates the password-to-scalar mapping.
const spakePasswordLabel = "viaduct-spake2-pw-v1"

// M and N are the two fixed SPAKE2 points for edwards25519 from RFC 9382 §4.
// They are "nothing up my sleeve": no one knows their discrete log, which the
// scheme's security depends on.
var spakeM, spakeN *edwards25519.Point

func init() {
	mBytes := mustHex("d048032c6ea0b6d697ddc2e86bda85a33adac920f1bf18e1b0c6d166a5cecdaf")
	nBytes := mustHex("d3bfb518f44f3430f29d0c92af503865a1ed3281dc69b35dd868ba85f886c4ab")
	var err error
	if spakeM, err = new(edwards25519.Point).SetBytes(mBytes); err != nil {
		panic("auth: invalid SPAKE2 M constant: " + err.Error())
	}
	if spakeN, err = new(edwards25519.Point).SetBytes(nBytes); err != nil {
		panic("auth: invalid SPAKE2 N constant: " + err.Error())
	}
}

// Spake2 holds one party's in-progress exchange.
type Spake2 struct {
	role   SpakeRole
	w      *edwards25519.Scalar // password scalar
	x      *edwards25519.Scalar // our random scalar
	msg    []byte               // our outgoing element (32 bytes)
	myMask *edwards25519.Point  // M (client) or N (server)
	peMask *edwards25519.Point  // the peer's mask (N or M)
}

// newSpake2 starts a SPAKE2 exchange for the given role using a shared password.
func NewSpake2(role SpakeRole, password []byte) (*Spake2, error) {
	w, err := passwordScalar(password)
	if err != nil {
		return nil, err
	}
	x, err := randomScalar()
	if err != nil {
		return nil, err
	}

	s := &Spake2{role: role, w: w, x: x}
	if role == SpakeClient {
		s.myMask, s.peMask = spakeM, spakeN
	} else {
		s.myMask, s.peMask = spakeN, spakeM
	}

	// T = x·B + w·mask
	xB := new(edwards25519.Point).ScalarBaseMult(x)
	wMask := new(edwards25519.Point).ScalarMult(w, s.myMask)
	T := new(edwards25519.Point).Add(xB, wMask)
	s.msg = T.Bytes()
	return s, nil
}

// message returns this party's element to send to the peer.
func (s *Spake2) Message() []byte { return s.msg }

// finish consumes the peer's element and returns the shared key. It errors if
// the peer element is not a valid group element or the exchange degenerates to
// the identity (a small-subgroup / invalid-point attempt).
func (s *Spake2) Finish(peerMsg []byte) ([]byte, error) {
	peer, err := new(edwards25519.Point).SetBytes(peerMsg)
	if err != nil {
		return nil, fmt.Errorf("invalid SPAKE2 element")
	}

	// K = cofactor · x · (peer − w·peerMask)
	wPeMask := new(edwards25519.Point).ScalarMult(s.w, s.peMask)
	unblinded := new(edwards25519.Point).Subtract(peer, wPeMask)
	K := new(edwards25519.Point).ScalarMult(s.x, unblinded)
	K.MultByCofactor(K)
	if K.Equal(edwards25519.NewIdentityPoint()) == 1 {
		return nil, fmt.Errorf("SPAKE2 produced a degenerate shared point")
	}

	// Order the two elements identically on both sides (client element first) so
	// the transcript — and thus the derived key — is symmetric.
	clientMsg, serverMsg := s.msg, peerMsg
	if s.role == SpakeServer {
		clientMsg, serverMsg = peerMsg, s.msg
	}

	h := sha512.New()
	writeChunk(h, []byte(spakeTranscriptLabel))
	writeChunk(h, spakeM.Bytes())
	writeChunk(h, spakeN.Bytes())
	writeChunk(h, clientMsg)
	writeChunk(h, serverMsg)
	writeChunk(h, K.Bytes())
	writeChunk(h, s.w.Bytes())
	return h.Sum(nil)[:32], nil
}

// passwordScalar maps a password to a scalar mod the group order, uniformly via
// a 64-byte hash so no bias leaks the password length or value.
func passwordScalar(password []byte) (*edwards25519.Scalar, error) {
	h := sha512.New()
	h.Write([]byte(spakePasswordLabel))
	h.Write(password)
	return edwards25519.NewScalar().SetUniformBytes(h.Sum(nil))
}

func randomScalar() (*edwards25519.Scalar, error) {
	var b [64]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	return edwards25519.NewScalar().SetUniformBytes(b[:])
}

// writeChunk feeds a length-prefixed chunk into the transcript hash so distinct
// field boundaries can't be confused.
func writeChunk(h interface{ Write([]byte) (int, error) }, b []byte) {
	var l [2]byte
	l[0] = byte(len(b) >> 8)
	l[1] = byte(len(b))
	h.Write(l[:])
	h.Write(b)
}

func mustHex(s string) []byte {
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		hi, lo := fromHex(s[2*i]), fromHex(s[2*i+1])
		if hi < 0 || lo < 0 {
			panic("auth: bad hex constant")
		}
		out[i] = byte(hi<<4 | lo)
	}
	return out
}

func fromHex(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}
