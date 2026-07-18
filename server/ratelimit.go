package server

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// ipGuard defends the server's *unauthenticated* surface from a hostile IP —
// hammering /pair to brute-force the 6-digit code, or /rpc with garbage
// envelopes to burn CPU. It deliberately does NOT touch authenticated traffic:
// a client that has proven it holds an authorized key is trusted, so its
// requests (job polling, monitors, etc.) are never throttled or banned.
//
//   - Rate limit (token bucket): applied to pairing attempts, which are
//     interactive and low-volume. Excess gets a 429 to back off. It never bans.
//   - Ban (strikes): a strike is recorded only for a genuinely bad request — a
//     rejected RPC envelope (forged/unauthorized/replayed) or a failed pairing
//     attempt. A few strikes in a short window earn a temporary ban; a banned
//     IP is rejected up front, before any work. A legitimate client never
//     strikes, so the threshold is strict.
//
// The hard cap on code brute-forcing is at the pairing layer (a code is
// single-use, expires, and dies after a few wrong guesses); this is the
// network-level backstop.

type rateLimitConfig struct {
	burst        float64       // bucket capacity (max requests in a quick burst)
	refillPerSec float64       // sustained requests/sec once the burst is spent
	banThreshold int           // strikes within banWindow that trigger a ban
	banWindow    time.Duration // strikes older than this are forgotten
	banDuration  time.Duration // how long a ban lasts
}

func defaultRateLimit() rateLimitConfig {
	return rateLimitConfig{
		burst:        20,
		refillPerSec: 5,
		banThreshold: 5,
		banWindow:    2 * time.Minute,
		banDuration:  15 * time.Minute,
	}
}

type visitor struct {
	tokens    float64
	last      time.Time
	strikes   int
	strikeAt  time.Time
	bannedTil time.Time
}

type ipGuard struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	cfg      rateLimitConfig
	logf     func(string, ...any)
	now      func() time.Time // injectable for tests
}

func newIPGuard(cfg rateLimitConfig, logf func(string, ...any)) *ipGuard {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &ipGuard{
		visitors: make(map[string]*visitor),
		cfg:      cfg,
		logf:     logf,
		now:      time.Now,
	}
}

// rejectBanned writes a 429 and returns true if ip is currently banned. It's
// used to turn away a proven abuser cheaply, before any decryption work — but
// without throttling, so trusted authenticated clients (which never get banned)
// are unaffected.
func (g *ipGuard) rejectBanned(w http.ResponseWriter, ip string) bool {
	g.mu.Lock()
	v, ok := g.visitors[ip]
	banned := ok && g.now().Before(v.bannedTil)
	var retry time.Duration
	if banned {
		retry = v.bannedTil.Sub(g.now())
	}
	g.mu.Unlock()
	if !banned {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
	http.Error(w, "temporarily blocked after repeated failed attempts", http.StatusTooManyRequests)
	return true
}

// throttle writes a 429 and returns true if ip has exceeded its request rate or
// is banned. Used on the unauthenticated pairing path.
func (g *ipGuard) throttle(w http.ResponseWriter, ip string) bool {
	ok, retry := g.allow(ip)
	if ok {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
	if g.banned(ip) {
		http.Error(w, "temporarily blocked after repeated failed attempts", http.StatusTooManyRequests)
	} else {
		http.Error(w, "too many pairing attempts; slow down and retry", http.StatusTooManyRequests)
	}
	return true
}

// allow charges one request against ip's bucket. It returns false (with a
// suggested retry delay) when ip is banned or out of tokens; sustained flooding
// also counts as a strike toward a ban.
func (g *ipGuard) allow(ip string) (bool, time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	v := g.visitorLocked(ip, now)

	if now.Before(v.bannedTil) {
		return false, v.bannedTil.Sub(now)
	}

	v.tokens = min(g.cfg.burst, v.tokens+now.Sub(v.last).Seconds()*g.cfg.refillPerSec)
	v.last = now
	if v.tokens >= 1 {
		v.tokens--
		return true, 0
	}

	// Out of tokens: throttle only. Going too fast is not by itself abuse, so it
	// never bans — that keeps a bursty but legitimate client from being locked
	// out. Bans come solely from strike() on genuinely bad requests.
	return false, time.Duration(float64(time.Second) / g.cfg.refillPerSec)
}

// strike records one failed/suspicious request from ip (a rejected envelope or
// pairing attempt). Enough strikes in the window earn a ban.
func (g *ipGuard) strike(ip string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	g.strikeLocked(ip, g.visitorLocked(ip, now), now)
}

func (g *ipGuard) strikeLocked(ip string, v *visitor, now time.Time) {
	if now.Sub(v.strikeAt) > g.cfg.banWindow {
		v.strikes = 0
	}
	v.strikes++
	v.strikeAt = now
	if v.strikes >= g.cfg.banThreshold {
		v.bannedTil = now.Add(g.cfg.banDuration)
		v.strikes = 0
		g.logf("banned %s for %s after repeated abuse", ip, g.cfg.banDuration)
	}
}

// banned reports whether ip is currently banned (used by tests).
func (g *ipGuard) banned(ip string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	v, ok := g.visitors[ip]
	return ok && g.now().Before(v.bannedTil)
}

// visitorLocked returns ip's state, creating it on first sighting and pruning
// stale entries so the map can't grow without bound.
func (g *ipGuard) visitorLocked(ip string, now time.Time) *visitor {
	if v, ok := g.visitors[ip]; ok {
		return v
	}
	if len(g.visitors) >= 4096 {
		for k, v := range g.visitors {
			if now.After(v.bannedTil) && now.Sub(v.last) > g.cfg.banWindow {
				delete(g.visitors, k)
			}
		}
	}
	v := &visitor{tokens: g.cfg.burst, last: now}
	g.visitors[ip] = v
	return v
}

// clientIP extracts the peer IP from a request's remote address.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
