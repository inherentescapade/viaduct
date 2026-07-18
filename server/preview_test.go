package server

import (
	"errors"
	"fmt"
	"github.com/inherentescapade/viaduct/auth"
	"github.com/inherentescapade/viaduct/engine"
	"testing"
	"time"
)

// failingBuild is a preview/job engine builder that always fails, used to
// exercise the fast-fail path without a live Discord token.
func failingBuild(err error) func(Credentials) (*engine.Engine, *resolver, error) {
	return func(Credentials) (*engine.Engine, *resolver, error) { return nil, nil, err }
}

// A target that won't resolve (bad token, unknown server) must fail inside
// start, before any handle is issued — otherwise the client would poll a
// preview that never runs.
func TestPreviewStartResolveErrorIssuesNoHandle(t *testing.T) {
	pm := newPreviewManager(failingBuild(errors.New("bad token")))
	if _, err := pm.start(Credentials{}, KindDeleteGuild, DeleteSpec{Guild: "x"}); err == nil {
		t.Fatal("expected a resolve error")
	}
	if len(pm.m) != 0 {
		t.Fatalf("a failed start must register no preview, got %d", len(pm.m))
	}
}

func TestPreviewStatusUnknownID(t *testing.T) {
	pm := newPreviewManager(failingBuild(errors.New("x")))
	if _, ok := pm.status("preview-999"); ok {
		t.Fatal("an unknown preview id must report ok=false")
	}
}

// Eviction trims the oldest FINISHED preview once the cap is exceeded, and
// never a still-running one (its goroutine still writes back to that entry).
func TestPreviewEvictionKeepsRunningDropsOldestDone(t *testing.T) {
	pm := newPreviewManager(failingBuild(errors.New("x")))
	base := time.Now()
	for i := 0; i < previewMaxRetained; i++ {
		pm.m[fmt.Sprintf("done-%d", i)] = &trackedPreview{
			state:   previewDone,
			created: base.Add(time.Duration(i) * time.Second),
		}
	}
	// Older by clock than any done preview, but running — must be spared.
	pm.m["running"] = &trackedPreview{state: previewRunning, created: base.Add(-time.Hour)}

	pm.evictLocked() // len is now cap+1, so exactly one entry should go

	if _, ok := pm.m["running"]; !ok {
		t.Fatal("eviction must never drop a running preview")
	}
	if _, ok := pm.m["done-0"]; ok {
		t.Fatal("eviction should drop the oldest finished preview first")
	}
}

// The ops are reachable over the sealed RPC path: a status poll for an
// unknown id and a start with no pushed token both surface as errors rather
// than hanging or panicking.
func TestPreviewOpsOverRPC(t *testing.T) {
	clientID, _ := auth.GenerateIdentity()
	c, _ := newTestServer(t, clientID)

	if _, err := c.PreviewStatus("nope"); err == nil {
		t.Fatal("PreviewStatus on an unknown id should error")
	}
	if _, err := c.PreviewStart(PreviewRequest{Kind: KindDeleteGuild, Spec: DeleteSpec{Guild: "x"}}); err == nil {
		t.Fatal("PreviewStart without a pushed token should error")
	}
}
