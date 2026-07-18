package discord

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fastMeta swaps in a near-instant metadata limiter so the rate-limit retry
// tests don't spend real time sleeping between attempts.
func fastMeta(c *Client) {
	c.metaLimiter = NewAdaptiveLimiter(time.Millisecond, time.Millisecond, 5*time.Millisecond)
}

// A 429 on a metadata read is transient: getJSON should honour retry_after and
// retry, transparently succeeding once the limit clears — never surfacing the
// 429 to the caller.
func TestGetJSON_RetriesThrough429(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"You are being rate limited.","retry_after":0.01,"global":false}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"42","username":"alice"}`))
	}))
	defer srv.Close()

	c := NewClient("tok", false, srv.Client())
	fastMeta(c)

	var u User
	if err := c.getJSON(srv.URL, &u); err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if u.Id != "42" {
		t.Errorf("expected decoded body, got id %q", u.Id)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("expected 2 requests (429 then 200), got %d", got)
	}
}

// If Discord keeps returning 429 past our retries, the caller gets a clean,
// actionable message — not the raw status code or Discord's body.
func TestGetJSON_ExhaustedGivesCleanMessage(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"You are being rate limited.","retry_after":0,"global":false}`))
	}))
	defer srv.Close()

	c := NewClient("tok", false, srv.Client())
	fastMeta(c)

	var u User
	err := c.getJSON(srv.URL, &u)
	if err == nil {
		t.Fatal("expected an error when persistently rate limited")
	}
	if strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "status") {
		t.Errorf("raw status leaked into user-facing message: %q", err)
	}
	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("expected a rate-limit message, got %q", err)
	}
	if got := atomic.LoadInt32(&calls); got != 5 { // maxRetries=4 → up to 5 attempts
		t.Errorf("expected 5 attempts, got %d", got)
	}
}

// Non-429 failures (e.g. an invalid token → 401) are real and must surface, so
// the token-entry flow can tell the user their token is bad rather than
// silently retrying.
func TestGetJSON_Non429StatusSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"401: Unauthorized","code":0}`))
	}))
	defer srv.Close()

	c := NewClient("tok", false, srv.Client())
	fastMeta(c)

	var u User
	err := c.getJSON(srv.URL, &u)
	if err == nil {
		t.Fatal("expected an error on 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected the 401 to surface for non-rate-limit failures, got %q", err)
	}
}
