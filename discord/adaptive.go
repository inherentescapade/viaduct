package discord

import (
	"sync"
	"time"
)

// RateEvent describes an automatic pacing decision for one channel, so callers
// can tell the user what the tool is doing and why.
type RateEvent struct {
	ChannelID  string
	SpedUp     bool          // true = sped up after a success streak; false = backed off after a 429
	Gap        time.Duration // the new inter-request gap
	RetryAfter float64       // retry_after seconds when backing off (0 otherwise)
}

// AdaptiveLimiter paces requests to a single channel and tunes the gap toward
// that channel's real sustainable rate. It learns where the edge is: once a 429
// reveals a too-fast gap, it records a threshold just above it and, near that
// threshold, switches from fast ramp-up to slow creeping — so the gap settles in
// a tight band around the limit instead of sawtoothing from the floor to the
// ceiling and back. There is no separate measurement step; it keeps adapting as
// server conditions drift.
type AdaptiveLimiter struct {
	mu        sync.Mutex
	gap       time.Duration // current target inter-request gap
	minGap    time.Duration // floor — never pace faster than this
	maxGap    time.Duration // ceiling — never back off slower than this
	threshold time.Duration // learned safe gap; 0 until the first 429
	last      time.Time     // start time of the most recent request
	notBefore time.Time     // hard floor imposed by a 429's retry_after
	okStreak  int
}

const (
	adaptiveSpeedupStreak = 4                     // clean deletes before adjusting the gap
	adaptiveCreep         = 40 * time.Millisecond // slow step used near the learned edge
	adaptiveBackoffMargin = 1.15                  // how far above a failing gap the safe estimate sits
)

func NewAdaptiveLimiter(start, min, max time.Duration) *AdaptiveLimiter {
	if start < min {
		start = min
	}
	return &AdaptiveLimiter{gap: start, minGap: min, maxGap: max}
}

// Wait blocks until the next request to this channel is allowed.
func (a *AdaptiveLimiter) Wait() {
	a.mu.Lock()
	now := time.Now()
	earliest := a.last.Add(a.gap)
	if a.notBefore.After(earliest) {
		earliest = a.notBefore
	}
	var wait time.Duration
	if earliest.After(now) {
		wait = earliest.Sub(now)
	}
	a.last = now.Add(wait)
	a.mu.Unlock()
	if wait > 0 {
		time.Sleep(wait)
	}
}

// OnSuccess records a clean delete and, after a streak, eases the gap down.
// Far above the learned edge it ramps fast (multiplicative); near or below the
// edge it creeps by a small fixed step so it settles in a tight band instead of
// diving back to the floor and getting snapped back. Returns true (with the new
// gap) only when the gap actually changed.
func (a *AdaptiveLimiter) OnSuccess() (changed bool, gap time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.okStreak++
	if a.okStreak < adaptiveSpeedupStreak || a.gap <= a.minGap {
		return false, a.gap
	}
	a.okStreak = 0

	var next time.Duration
	if a.threshold == 0 || a.gap > a.threshold {
		// Above the known-safe edge (or no edge learned yet): ramp up fast.
		next = time.Duration(float64(a.gap) * 0.9)
	} else {
		// At/below the edge: probe downward cautiously so we don't trigger a 429
		// every few requests.
		next = a.gap - adaptiveCreep
	}
	if next < a.minGap {
		next = a.minGap
	}
	if next >= a.gap {
		return false, a.gap
	}
	a.gap = next
	return true, a.gap
}

// OnRateLimited records a 429: honour retry_after, and learn the edge. The gap
// that just failed was too fast, so the safe gap sits just above it — remember
// that as the threshold and settle the gap there, rather than diving back to the
// floor on the next success streak. Repeated 429s ratchet the threshold up until
// they stop, so it converges on the real limit.
func (a *AdaptiveLimiter) OnRateLimited(retryAfter float64) (gap time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.okStreak = 0
	ra := time.Duration(retryAfter * float64(time.Second))
	if retryAfter > 0 {
		a.notBefore = time.Now().Add(ra)
	}
	a.threshold = time.Duration(float64(a.gap) * adaptiveBackoffMargin)
	a.gap = a.threshold
	if a.gap > a.maxGap {
		a.gap = a.maxGap
	}
	if a.gap < a.minGap {
		a.gap = a.minGap
	}
	return a.gap
}

// Gap returns the current inter-request gap.
func (a *AdaptiveLimiter) Gap() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.gap
}
