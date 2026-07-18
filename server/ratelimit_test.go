package server

import (
	"testing"
	"time"
)

func tightGuard() *ipGuard {
	return newIPGuard(rateLimitConfig{
		burst:        3,
		refillPerSec: 1,
		banThreshold: 3,
		banWindow:    time.Minute,
		banDuration:  10 * time.Minute,
	}, func(string, ...any) {})
}

func TestRateLimitBurstThenThrottle(t *testing.T) {
	g := tightGuard()
	now := time.Now()
	g.now = func() time.Time { return now }

	// The burst (3) passes; the next request is throttled.
	for i := 0; i < 3; i++ {
		if ok, _ := g.allow("1.2.3.4"); !ok {
			t.Fatalf("request %d within burst should be allowed", i)
		}
	}
	if ok, retry := g.allow("1.2.3.4"); ok || retry <= 0 {
		t.Fatal("a request past the burst should be throttled with a retry delay")
	}

	// Hammering the rate limit must NOT ban: going too fast is throttled, not
	// treated as abuse.
	for i := 0; i < 20; i++ {
		g.allow("1.2.3.4")
	}
	if g.banned("1.2.3.4") {
		t.Fatal("exceeding the rate limit alone must never ban a client")
	}

	// After enough time to refill a token, a request passes again.
	now = now.Add(2 * time.Second)
	if ok, _ := g.allow("1.2.3.4"); !ok {
		t.Fatal("a token should have refilled after waiting")
	}
}

func TestRateLimitIsolatesIPs(t *testing.T) {
	g := tightGuard()
	now := time.Now()
	g.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		g.allow("1.1.1.1")
	}
	if ok, _ := g.allow("1.1.1.1"); ok {
		t.Fatal("first IP should be throttled")
	}
	// A different IP has its own full bucket.
	if ok, _ := g.allow("2.2.2.2"); !ok {
		t.Fatal("a different IP must not be affected by another's limit")
	}
}

func TestStrikesLeadToBanThatExpires(t *testing.T) {
	g := tightGuard()
	now := time.Now()
	g.now = func() time.Time { return now }

	for i := 0; i < 3; i++ { // banThreshold
		g.strike("9.9.9.9")
	}
	if !g.banned("9.9.9.9") {
		t.Fatal("reaching the strike threshold should ban the IP")
	}
	if ok, retry := g.allow("9.9.9.9"); ok || retry <= 0 {
		t.Fatal("a banned IP must be refused with a retry delay")
	}

	// The ban lifts once its duration passes.
	now = now.Add(11 * time.Minute)
	if g.banned("9.9.9.9") {
		t.Fatal("the ban should have expired")
	}
	if ok, _ := g.allow("9.9.9.9"); !ok {
		t.Fatal("after the ban expires the IP should be allowed again")
	}
}

func TestStrikesForgottenAfterWindow(t *testing.T) {
	g := tightGuard()
	now := time.Now()
	g.now = func() time.Time { return now }

	g.strike("5.5.5.5")
	g.strike("5.5.5.5")
	// A long gap resets the strike count, so the next strike doesn't ban.
	now = now.Add(2 * time.Minute)
	g.strike("5.5.5.5")
	if g.banned("5.5.5.5") {
		t.Fatal("strikes older than the window should not accumulate into a ban")
	}
}
