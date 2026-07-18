package server

import (
	"context"
	"encoding/gob"
	"fmt"
	"github.com/inherentescapade/viaduct/discord"
	"github.com/inherentescapade/viaduct/engine"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const defaultMonitorIntervalHrs = 6

// EngineBuilder returns a Discord engine for the given credentials. The monitor
// manager uses it to build a per-run engine; returning an error (e.g. no token)
// surfaces cleanly to the caller.
type EngineBuilder func(Credentials) (*engine.Engine, error)

// MonitorManager owns standing monitor policies: it persists them to disk so they
// survive restarts and runs each on its schedule, deleting messages older than
// the policy's retention window.
//
// It is used both by the hosted server and — with a locally-built engine — by the
// CLI's `viaduct monitor run` daemon and the desktop app's in-process scheduler,
// so a monitor can run on any always-on machine without the networking layer.
type MonitorManager struct {
	mu       sync.Mutex
	policies map[string]*MonitorPolicy
	seq      int
	path     string // where policies are persisted
	build    EngineBuilder
	creds    func() Credentials
	logf     func(string, ...any)
}

// NewMonitorManager builds a manager that persists to path (empty = in-memory),
// builds engines via build, reads the acting credentials via creds, and logs via
// logf (nil-safe).
func NewMonitorManager(path string, build EngineBuilder, creds func() Credentials, logf func(string, ...any)) *MonitorManager {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	m := &MonitorManager{
		policies: make(map[string]*MonitorPolicy),
		path:     path,
		build:    build,
		creds:    creds,
		logf:     logf,
	}
	m.load()
	return m
}

// Upsert validates and stores a policy, returning the stored copy (with an ID
// assigned for new policies).
func (m *MonitorManager) Upsert(p MonitorPolicy) (MonitorPolicy, error) {
	p.normalizeAge()
	if p.MaxAgeAmount <= 0 {
		return MonitorPolicy{}, fmt.Errorf("max age must be positive")
	}
	if _, ok := p.MaxAgeUnit.unitDuration(); !ok {
		return MonitorPolicy{}, fmt.Errorf("unknown age unit %q", p.MaxAgeUnit)
	}
	if p.Mode != ModeInclude && p.Mode != ModeExclude {
		p.Mode = ModeExclude
	}
	if p.IntervalHrs <= 0 {
		p.IntervalHrs = defaultMonitorIntervalHrs
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	isNew := p.ID == ""
	if isNew {
		m.seq++
		p.ID = fmt.Sprintf("mon-%d", m.seq)
	} else if old, ok := m.policies[p.ID]; ok {
		// Updates carry only the policy fields; keep the runtime state (run
		// history, live progress) so editing or toggling a monitor doesn't erase
		// what it has done.
		p.LastRun = old.LastRun
		p.LastDeleted = old.LastDeleted
		p.Total = old.Total
		p.Running = old.Running
		p.Recent = old.Recent
	}
	// An enabled policy should run on the next scheduler tick (within ~a minute),
	// not a full interval from now — otherwise a freshly-created monitor looks
	// dead for hours. A disabled policy has no scheduled run.
	if p.Enabled {
		p.NextRun = time.Now()
	} else {
		p.NextRun = time.Time{}
	}
	stored := p
	m.policies[p.ID] = &stored
	m.persistLocked()
	if isNew {
		m.logf("monitor %s (%q) created: scope=%s mode=%s age=%d%s interval=%dh enabled=%v",
			p.ID, p.Name, p.Scope, p.Mode, p.MaxAgeAmount, p.MaxAgeUnit.Short(), p.IntervalHrs, p.Enabled)
	} else {
		m.logf("monitor %s (%q) updated: enabled=%v", p.ID, p.Name, p.Enabled)
	}
	return stored, nil
}

func (m *MonitorManager) Delete(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.policies[id]; !ok {
		return false
	}
	delete(m.policies, id)
	m.persistLocked()
	return true
}

func (m *MonitorManager) List() []MonitorPolicy {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MonitorPolicy, 0, len(m.policies))
	for _, p := range m.policies {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *MonitorManager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.policies)
}

// run is the scheduler loop. It wakes periodically, runs any enabled policy
// whose NextRun has passed, and reschedules it. It exits when ctx is cancelled.
func (m *MonitorManager) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.tick(ctx)
		}
	}
}

func (m *MonitorManager) tick(ctx context.Context) {
	now := time.Now()
	m.mu.Lock()
	var due []*MonitorPolicy
	for _, p := range m.policies {
		if p.Enabled && !p.NextRun.After(now) {
			due = append(due, p)
		}
	}
	m.mu.Unlock()

	for _, p := range due {
		select {
		case <-ctx.Done():
			return
		default:
		}
		m.mu.Lock()
		if cur, ok := m.policies[p.ID]; ok {
			cur.Running = true
			cur.Recent = nil
			cur.Total = 0
			cur.LastDeleted = 0
		}
		m.mu.Unlock()

		deleted, feed, err := m.execute(ctx, *p)

		m.mu.Lock()
		if cur, ok := m.policies[p.ID]; ok {
			cur.Running = false
			cur.LastRun = time.Now()
			cur.LastDeleted = deleted
			cur.Recent = feed
			cur.NextRun = time.Now().Add(time.Duration(cur.IntervalHrs) * time.Hour)
		}
		m.persistLocked()
		m.mu.Unlock()
		if err != nil {
			m.logf("monitor %s (%s) failed: %v", p.ID, p.Name, err)
		} else {
			m.logf("monitor %s (%s) ran: %d message(s) deleted", p.ID, p.Name, deleted)
		}
	}
}

// execute runs a single monitor policy once, returning how many messages were
// deleted and a feed of the most recent deletions.
func (m *MonitorManager) execute(ctx context.Context, p MonitorPolicy) (int, []FeedMessage, error) {
	eng, err := m.build(m.creds())
	if err != nil {
		return 0, nil, err
	}
	res, err := newResolver(eng)
	if err != nil {
		return 0, nil, err
	}
	job, err := res.buildMonitorJob(p)
	if err != nil {
		return 0, nil, err
	}
	names := channelLabels(job.Channels)
	var (
		fmu     sync.Mutex
		deleted int
		feed    []FeedMessage
	)
	// Push the matched total onto the live policy so the UI can show a bar.
	eng.OnProgress = func(pr engine.Progress) {
		m.mu.Lock()
		if cur, ok := m.policies[p.ID]; ok && pr.Total > cur.Total {
			cur.Total = pr.Total
		}
		m.mu.Unlock()
	}
	// Each real deletion bumps the live deleted count + feed.
	eng.OnMessage = func(msg discord.Message) {
		fmu.Lock()
		deleted++
		feed = appendFeed(feed, msg, names)
		d := deleted
		snapshot := append([]FeedMessage(nil), feed...)
		fmu.Unlock()
		m.mu.Lock()
		if cur, ok := m.policies[p.ID]; ok {
			cur.LastDeleted = d
			cur.Recent = snapshot
		}
		m.mu.Unlock()
	}
	if err := eng.Execute(ctx, job); err != nil {
		return deleted, feed, err
	}
	return deleted, feed, nil
}

// previewPolicy reports how many messages a policy would delete right now,
// without deleting anything — used to guide the user when enabling a monitor.
func (m *MonitorManager) Preview(p MonitorPolicy) (int, error) {
	eng, err := m.build(m.creds())
	if err != nil {
		return 0, err
	}
	res, err := newResolver(eng)
	if err != nil {
		return 0, err
	}
	job, err := res.buildMonitorJob(p)
	if err != nil {
		return 0, err
	}
	return eng.Preview(context.Background(), job)
}

// --- persistence ---

// persistLocked writes the policies to disk with gob, atomically: it writes a
// ".swp" file then renames it over the real file, so a crash mid-write can never
// leave a half-written, unparseable store.
func (m *MonitorManager) persistLocked() {
	if m.path == "" {
		return
	}
	list := make([]MonitorPolicy, 0, len(m.policies))
	for _, p := range m.policies {
		list = append(list, *p)
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		m.logf("monitor store: mkdir failed: %v", err)
		return
	}
	tmp := m.path + ".swp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		m.logf("monitor store: open temp failed: %v", err)
		return
	}
	if err := gob.NewEncoder(f).Encode(list); err != nil {
		f.Close()
		m.logf("monitor store: encode failed: %v", err)
		return
	}
	if err := f.Close(); err != nil {
		m.logf("monitor store: close temp failed: %v", err)
		return
	}
	if err := os.Rename(tmp, m.path); err != nil {
		m.logf("monitor store: rename failed: %v", err)
	}
}

func (m *MonitorManager) load() {
	if m.path == "" {
		return
	}
	f, err := os.Open(m.path)
	if err != nil {
		return
	}
	defer f.Close()
	var list []MonitorPolicy
	if err := gob.NewDecoder(f).Decode(&list); err != nil {
		m.logf("monitor store: could not read %s: %v", m.path, err)
		return
	}
	for i := range list {
		p := list[i]
		p.normalizeAge()
		m.policies[p.ID] = &p
		if n := seqFromID(p.ID); n > m.seq {
			m.seq = n
		}
	}
}

// seqFromID extracts the numeric suffix of an ID like "mon-3".
func seqFromID(id string) int {
	var n int
	if _, err := fmt.Sscanf(id, "mon-%d", &n); err != nil {
		return 0
	}
	return n
}
