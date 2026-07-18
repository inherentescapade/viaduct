package server

import (
	"context"
	"fmt"
	"github.com/inherentescapade/viaduct/engine"
	"sync"
	"time"
)

// previewMaxRetained bounds how many previews an account keeps before the
// oldest finished one is evicted. Counting is fire-and-poll, so a client that
// starts previews without ever polling them to completion must not be able to
// grow the map without bound.
const previewMaxRetained = 64

// previewState is the lifecycle of a background count.
type previewState string

const (
	previewRunning previewState = "running"
	previewDone    previewState = "done"
	previewFailed  previewState = "failed"
)

// trackedPreview is one background message count. The count itself is a live
// Discord search that can take far longer than a single request (large
// accounts, many DM channels, rate-limit backoff), so it runs in a goroutine
// and the client polls this state until it settles.
type trackedPreview struct {
	state   previewState
	total   int
	err     string
	created time.Time
	cancel  context.CancelFunc
}

// previewManager tracks an account's in-flight and recently-finished preview
// counts so a client can start one, disconnect, and poll it back — the same
// shape as jobManager, but for counting rather than deleting.
type previewManager struct {
	mu    sync.Mutex
	m     map[string]*trackedPreview
	seq   int
	build func(Credentials) (*engine.Engine, *resolver, error)
}

func newPreviewManager(build func(Credentials) (*engine.Engine, *resolver, error)) *previewManager {
	return &previewManager{m: make(map[string]*trackedPreview), build: build}
}

// start resolves the target synchronously — so a bad target (server not found,
// no DMs with that user, rejected token) errors immediately, before any
// handle is issued — then counts the affected messages in the background,
// returning a handle the client polls via status. This is what keeps the
// preview from dying when counting outlasts the client's request timeout: each
// call here returns promptly, and the slow work happens off the request path.
func (pm *previewManager) start(creds Credentials, kind JobKind, spec DeleteSpec) (PreviewStartResponse, error) {
	eng, res, err := pm.build(creds)
	if err != nil {
		return PreviewStartResponse{}, err
	}
	job, label, err := res.buildDeleteJob(kind, spec)
	if err != nil {
		return PreviewStartResponse{}, err
	}

	pm.mu.Lock()
	pm.seq++
	id := fmt.Sprintf("preview-%d", pm.seq)
	ctx, cancel := context.WithCancel(context.Background())
	tp := &trackedPreview{state: previewRunning, created: time.Now(), cancel: cancel}
	pm.m[id] = tp
	pm.evictLocked()
	pm.mu.Unlock()

	go func() {
		total, err := eng.Preview(ctx, job)
		pm.mu.Lock()
		defer pm.mu.Unlock()
		if err != nil {
			tp.state = previewFailed
			tp.err = err.Error()
			return
		}
		tp.total = total
		tp.state = previewDone
	}()

	return PreviewStartResponse{
		ID:       id,
		ActingAs: DiscordIdentity{Username: res.user.Username, ID: res.user.Id},
		Target:   label,
	}, nil
}

// status returns a snapshot of one preview, or ok=false if the id is unknown
// (never started, or evicted after completion).
func (pm *previewManager) status(id string) (PreviewStatusResponse, bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	tp, ok := pm.m[id]
	if !ok {
		return PreviewStatusResponse{}, false
	}
	return PreviewStatusResponse{
		ID:    id,
		Done:  tp.state != previewRunning,
		Total: tp.total,
		Error: tp.err,
	}, true
}

// evictLocked drops the oldest finished preview once the map exceeds its cap.
// Running previews are never evicted — their goroutines still write back — so a
// flood of unpolled running counts is bounded instead by the account's own
// rate limiter, not this map. Caller holds pm.mu.
func (pm *previewManager) evictLocked() {
	if len(pm.m) <= previewMaxRetained {
		return
	}
	var oldestID string
	var oldest time.Time
	for id, tp := range pm.m {
		if tp.state == previewRunning {
			continue
		}
		if oldestID == "" || tp.created.Before(oldest) {
			oldestID, oldest = id, tp.created
		}
	}
	if oldestID != "" {
		delete(pm.m, oldestID)
	}
}
