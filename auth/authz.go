package auth

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// ReplayWindow bounds how stale a request timestamp may be and how long a nonce
// must be remembered to reject replays.
const ReplayWindow = 2 * time.Minute

// KeySet is the set of client public keys a server will accept messages from.
type KeySet struct {
	mu   sync.RWMutex
	keys map[string][KeySize]byte // fingerprint -> key
}

// NewKeySet builds a KeySet from encoded public keys. A malformed key is an
// error so a typo in the server config is caught at startup.
func NewKeySet(encoded []string) (*KeySet, error) {
	ks := &KeySet{keys: make(map[string][KeySize]byte)}
	for _, k := range encoded {
		if strings.TrimSpace(k) == "" {
			continue
		}
		pub, err := ParsePublicKey(k)
		if err != nil {
			return nil, fmt.Errorf("authorized key %q: %w", k, err)
		}
		ks.keys[Fingerprint(pub)] = pub
	}
	return ks, nil
}

// Authorized reports whether pub is in the set.
func (ks *KeySet) Authorized(pub [KeySize]byte) bool {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	_, ok := ks.keys[Fingerprint(pub)]
	return ok
}

// Authorize adds pub to the set at runtime, so a server can start trusting a
// newly paired client without a restart. It returns false if the key was
// already authorized.
func (ks *KeySet) Authorize(pub [KeySize]byte) bool {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	fp := Fingerprint(pub)
	if _, ok := ks.keys[fp]; ok {
		return false
	}
	ks.keys[fp] = pub
	return true
}

// ReplayGuard rejects requests whose timestamp is outside the window or whose
// nonce has already been used. It is safe for concurrent use.
type ReplayGuard struct {
	mu   sync.Mutex
	seen map[string]time.Time
	now  func() time.Time // injectable for tests
}

// NewReplayGuard creates an empty guard.
func NewReplayGuard() *ReplayGuard {
	return &ReplayGuard{seen: make(map[string]time.Time), now: time.Now}
}

// Check validates a request's freshness and single-use nonce. It returns an
// error (and records nothing) when the timestamp is stale or the nonce repeats.
func (g *ReplayGuard) Check(nonce string, unixTime int64) error {
	now := g.now()
	reqTime := time.Unix(unixTime, 0)
	if d := now.Sub(reqTime); d > ReplayWindow || d < -ReplayWindow {
		return fmt.Errorf("request timestamp outside the allowed window (clock skew?)")
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	for n, t := range g.seen {
		if now.Sub(t) > ReplayWindow {
			delete(g.seen, n)
		}
	}
	if _, ok := g.seen[nonce]; ok {
		return fmt.Errorf("nonce already used (replay rejected)")
	}
	g.seen[nonce] = now
	return nil
}
