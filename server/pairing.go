package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/inherentescapade/viaduct/auth"
	"sync"
	"time"
)

// Pairing establishes trust with a new client from a short code shown on the
// server's terminal — no keys copied by hand, no restart. The two sides run
// SPAKE2 (see auth/spake2.go) keyed by that code:
// a Password-Authenticated Key Exchange that yields a strong shared key only if
// both entered the same code, and from which neither captured traffic nor an
// active man in the middle can recover the code offline. Confirmation MACs over
// the shared key — binding both static public keys — let each side prove it knew
// the code and authenticate the key it received, so the client learns the
// server's real key with no copying.
//
// The code is single-use, short-lived, and attempt-limited. After a successful
// pairing the client's key is authorized live (no restart) and persisted.

const (
	// pairingCodeDigits is the length of the human-entered pairing code.
	pairingCodeDigits = 6
	// pairingTTL bounds how long a displayed code stays valid.
	pairingTTL = 10 * time.Minute
	// pairSessionTTL bounds how long a half-finished exchange may sit waiting for
	// its confirm message.
	pairSessionTTL = 2 * time.Minute
	// maxPairAttempts burns a code after this many failed confirms, forcing the
	// operator to show a fresh one.
	maxPairAttempts = 5
	// maxPairSessions caps concurrent in-flight exchanges to bound memory.
	maxPairSessions = 32
)

// Confirmation-MAC labels, domain-separated so a client proof can never be
// replayed as a server proof.
const (
	pairClientConfirmLabel = "viaduct-pair-client-v2"
	pairServerConfirmLabel = "viaduct-pair-server-v2"
)

// PairRequest is the client's POST /pair body. Phase selects the step:
//   - "start":   ClientPub + MsgA (the client's SPAKE2 element)
//   - "confirm": Session + ClientConfirm
type PairRequest struct {
	Phase         string `json:"phase"`
	ClientPub     string `json:"client_pub,omitempty"`
	MsgA          string `json:"msg_a,omitempty"` // base64 SPAKE2 element
	Session       string `json:"session,omitempty"`
	ClientConfirm string `json:"client_confirm,omitempty"`
}

// PairResponse is the server's reply. Start populates Session/ServerPub/MsgB/
// ServerConfirm; Confirm populates OK.
type PairResponse struct {
	Session       string `json:"session,omitempty"`
	ServerPub     string `json:"server_pub,omitempty"`
	MsgB          string `json:"msg_b,omitempty"` // base64 SPAKE2 element
	ServerConfirm string `json:"server_confirm,omitempty"`
	OK            bool   `json:"ok,omitempty"`
}

// pairSession is one half-finished exchange awaiting its confirm message.
type pairSession struct {
	ke        []byte
	clientPub [auth.KeySize]byte
	expires   time.Time
}

// pairingManager holds the one code currently armed and any in-flight exchanges.
type pairingManager struct {
	mu       sync.Mutex
	code     string
	expires  time.Time
	attempts int
	sessions map[string]*pairSession
	now      func() time.Time // injectable for tests
}

func newPairingManager() *pairingManager {
	return &pairingManager{sessions: make(map[string]*pairSession), now: time.Now}
}

// arm mints a fresh code, replacing any current one, and returns it with its
// expiry for display. Any in-flight exchanges keyed to the old code are dropped
// so re-arming is a clean reset.
func (p *pairingManager) arm() (string, time.Time, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.armLocked()
}

func (p *pairingManager) armLocked() (string, time.Time, error) {
	code, err := randomCode(pairingCodeDigits)
	if err != nil {
		return "", time.Time{}, err
	}
	p.code = code
	p.expires = p.now().Add(pairingTTL)
	p.attempts = 0
	p.sessions = make(map[string]*pairSession)
	return code, p.expires, nil
}

// request is called when a client asks to pair: it returns the code to show the
// operator, minting a fresh one only when none is currently active. Reusing an
// active code means repeated requests show a stable code instead of churning it
// (which would disrupt a pairing the operator is mid-way through entering).
func (p *pairingManager) request() (string, time.Time, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.codeLive() {
		return p.code, p.expires, nil
	}
	return p.armLocked()
}

// active reports whether a code is currently valid.
func (p *pairingManager) active() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.codeLive()
}

func (p *pairingManager) codeLive() bool {
	return p.code != "" && !p.now().After(p.expires)
}

// start runs the server's half of the SPAKE2 exchange against the client's
// element, returning a session id, the server's element, and the server's
// confirmation MAC (which binds both static keys). It does not consume the code
// — that happens only once the client proves it derived the same key — but it
// does count against the attempt budget: the serverConfirm it returns reveals
// whether the password guess embedded in msgA was right, so each start is one
// online guess, exactly like a failed confirm. Without counting it, an attacker
// could brute-force the code through start alone, never tripping the limit. Once
// the budget is spent the code burns.
func (p *pairingManager) start(serverPub, clientPub [auth.KeySize]byte, msgA []byte) (id string, msgB []byte, serverConfirm string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.codeLive() {
		return "", nil, "", fmt.Errorf("no pairing code is active — generate one on the server (press Enter in its terminal)")
	}
	p.attempts++
	if p.attempts >= maxPairAttempts {
		p.code = "" // burn it; a fresh one must be shown
		p.sessions = make(map[string]*pairSession)
		return "", nil, "", fmt.Errorf("too many pairing attempts — generate a new code on the server")
	}
	p.pruneLocked()
	if len(p.sessions) >= maxPairSessions {
		return "", nil, "", fmt.Errorf("too many pairing attempts in flight, try again shortly")
	}

	sp, err := auth.NewSpake2(auth.SpakeServer, []byte(p.code))
	if err != nil {
		return "", nil, "", fmt.Errorf("could not start pairing")
	}
	ke, err := sp.Finish(msgA)
	if err != nil {
		return "", nil, "", fmt.Errorf("invalid pairing message")
	}

	sid, err := randomHex(16)
	if err != nil {
		return "", nil, "", err
	}
	p.sessions[sid] = &pairSession{ke: ke, clientPub: clientPub, expires: p.now().Add(pairSessionTTL)}
	return sid, sp.Message(), pairConfirm(ke, pairServerConfirmLabel, serverPub, clientPub), nil
}

// confirm verifies the client's proof for a session. On success it consumes the
// code (single use) and returns the client key to authorize.
func (p *pairingManager) confirm(serverPub [auth.KeySize]byte, sessionID, clientConfirm string) ([auth.KeySize]byte, error) {
	var zero [auth.KeySize]byte
	p.mu.Lock()
	defer p.mu.Unlock()

	p.pruneLocked()
	sess, ok := p.sessions[sessionID]
	if !ok {
		return zero, fmt.Errorf("pairing session expired — generate a new code on the server")
	}
	if !p.codeLive() {
		delete(p.sessions, sessionID)
		return zero, fmt.Errorf("the pairing code has expired — generate a new one on the server")
	}

	expected := pairConfirm(sess.ke, pairClientConfirmLabel, serverPub, sess.clientPub)
	if !hmac.Equal([]byte(expected), []byte(clientConfirm)) {
		delete(p.sessions, sessionID)
		p.attempts++
		if p.attempts >= maxPairAttempts {
			p.code = "" // burn it; a fresh one must be shown
			p.sessions = make(map[string]*pairSession)
			return zero, fmt.Errorf("incorrect code, and too many attempts — generate a new code on the server")
		}
		return zero, fmt.Errorf("incorrect code")
	}

	delete(p.sessions, sessionID)
	p.code = "" // single use
	return sess.clientPub, nil
}

// pruneLocked drops expired sessions. Caller holds p.mu.
func (p *pairingManager) pruneLocked() {
	now := p.now()
	for id, s := range p.sessions {
		if now.After(s.expires) {
			delete(p.sessions, id)
		}
	}
}

// pairConfirm is the HMAC over (label, serverPub, clientPub) keyed by the SPAKE2
// shared key. Because the key comes from the PAKE, only a party that knew the
// code can compute it, and it binds the exact static keys being exchanged.
func pairConfirm(ke []byte, label string, serverPub, clientPub [auth.KeySize]byte) string {
	mac := hmac.New(sha256.New, ke)
	mac.Write([]byte(label))
	mac.Write(serverPub[:])
	mac.Write(clientPub[:])
	return hex.EncodeToString(mac.Sum(nil))
}

// randomCode returns a cryptographically random decimal string of the given
// length, rejection-sampling each digit to avoid modulo bias.
func randomCode(digits int) (string, error) {
	out := make([]byte, digits)
	var b [1]byte
	for i := 0; i < digits; i++ {
		for {
			if _, err := rand.Read(b[:]); err != nil {
				return "", err
			}
			if b[0] < 250 { // 250 == 25*10, the largest multiple of 10 under 256
				out[i] = '0' + b[0]%10
				break
			}
		}
	}
	return string(out), nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
