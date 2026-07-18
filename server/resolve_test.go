package server

import (
	"bytes"
	"encoding/json"
	"github.com/inherentescapade/viaduct/discord"
	"github.com/inherentescapade/viaduct/engine"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// stubDiscord serves canned responses for the Discord endpoints monitor
// resolution touches, and records every path requested so tests can assert
// which parts of the API a policy actually reached.
type stubDiscord struct {
	mu      sync.Mutex
	paths   []string
	me      discord.User
	dmChans []discord.Channel
}

func (s *stubDiscord) RoundTrip(r *http.Request) (*http.Response, error) {
	s.mu.Lock()
	s.paths = append(s.paths, r.URL.Path)
	s.mu.Unlock()

	var body any
	switch r.URL.Path {
	case "/api/users/@me":
		body = s.me
	case "/api/users/@me/channels":
		body = s.dmChans
	default:
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader(`{"message":"unexpected endpoint"}`)),
			Header:     make(http.Header),
		}, nil
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(data)),
		Header:     make(http.Header),
	}, nil
}

func (s *stubDiscord) requested() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...)
}

// newStubResolver builds a resolver over a Discord client whose HTTP layer is
// the given stub, so buildMonitorJob runs its real code path without a network.
func newStubResolver(t *testing.T, stub *stubDiscord) *resolver {
	t.Helper()
	client := discord.NewClient("test-token", false, &http.Client{Transport: stub})
	res, err := newResolver(engine.NewWithClient(client))
	if err != nil {
		t.Fatalf("newResolver: %v", err)
	}
	return res
}

func dmStub() *stubDiscord {
	return &stubDiscord{
		me: discord.User{Id: "me-1", Username: "me"},
		dmChans: []discord.Channel{
			{Id: "d1", Type: 1, Recipients: []discord.User{{Id: "u-alice", Username: "alice"}}},
			{Id: "d2", Type: 1, Recipients: []discord.User{{Id: "u-bob", Username: "bob"}}},
			{Id: "d3", Type: 3, Name: "group chat", Recipients: []discord.User{{Id: "u-carol", Username: "carol"}}},
		},
	}
}

// TestDMMonitorExclusionStaysInDMs is the core safety property for DM monitors:
// a policy scoped to "@me" with an exclusion list must produce a job that
// targets exactly the non-excluded DM channels — never a guild, and never the
// guild-wide (empty channel list) form that would widen the deletion.
func TestDMMonitorExclusionStaysInDMs(t *testing.T) {
	stub := dmStub()
	res := newStubResolver(t, stub)

	job, err := res.buildMonitorJob(MonitorPolicy{
		Scope:        "@me",
		Mode:         ModeExclude,
		Channels:     []string{"alice"},
		MaxAgeAmount: 7,
	})
	if err != nil {
		t.Fatalf("buildMonitorJob: %v", err)
	}

	if !job.IsDM() {
		t.Fatalf("DM monitor produced a non-DM job: GuildID=%q", job.GuildID)
	}
	if len(job.Channels) != 2 {
		t.Fatalf("job.Channels = %+v, want exactly the 2 non-excluded DMs", job.Channels)
	}
	got := map[string]bool{}
	for _, ch := range job.Channels {
		got[ch.Id] = true
	}
	if got["d1"] {
		t.Fatal("excluded DM d1 (alice) must not be targeted")
	}
	if !got["d2"] || !got["d3"] {
		t.Fatalf("job.Channels = %+v, want d2 and d3", job.Channels)
	}

	// Resolution must never have gone near a guild endpoint: no guild listing,
	// no guild channel fetch.
	for _, p := range stub.requested() {
		if strings.Contains(p, "guild") {
			t.Fatalf("DM monitor resolution requested a guild endpoint: %s", p)
		}
	}
}

// TestDMMonitorNeverGetsGuildWideChannels guards the guild-wide optimisation in
// buildMonitorJob: an exclude-mode policy with no exclusions clears the channel
// list for guilds (the engine then searches guild-wide), but for a DM scope the
// channel list must be kept — a DM job's channels are its only targets, and the
// engine treats them as the complete search scope.
func TestDMMonitorNeverGetsGuildWideChannels(t *testing.T) {
	res := newStubResolver(t, dmStub())

	job, err := res.buildMonitorJob(MonitorPolicy{
		Scope:        "@me",
		Mode:         ModeExclude, // no Channels: "delete everywhere in scope"
		MaxAgeAmount: 7,
	})
	if err != nil {
		t.Fatalf("buildMonitorJob: %v", err)
	}
	if !job.IsDM() {
		t.Fatalf("DM monitor produced a non-DM job: GuildID=%q", job.GuildID)
	}
	if len(job.Channels) != 3 {
		t.Fatalf("job.Channels = %+v, want all 3 DM channels kept (never nil for DMs)", job.Channels)
	}
}

// TestDeleteExclusionStaysInDMs is the one-shot ("regular deletion")
// counterpart to TestDMMonitorExclusionStaysInDMs: a delete_guild job scoped to
// "@me" with an exclusion must delete every DM/group EXCEPT the excluded ones,
// as an explicit channel list — never the guild-wide form that would widen it.
func TestDeleteExclusionStaysInDMs(t *testing.T) {
	stub := dmStub()
	res := newStubResolver(t, stub)

	job, label, err := res.buildDeleteJob(KindDeleteGuild, DeleteSpec{
		Guild:   "@me",
		Exclude: []string{"alice"},
	})
	if err != nil {
		t.Fatalf("buildDeleteJob: %v", err)
	}
	if !job.IsDM() {
		t.Fatalf("DM deletion produced a non-DM job: GuildID=%q", job.GuildID)
	}
	if len(job.Channels) != 2 {
		t.Fatalf("job.Channels = %+v, want exactly the 2 non-excluded DMs", job.Channels)
	}
	got := map[string]bool{}
	for _, ch := range job.Channels {
		got[ch.Id] = true
	}
	if got["d1"] {
		t.Fatal("excluded DM d1 (alice) must not be targeted")
	}
	if !got["d2"] || !got["d3"] {
		t.Fatalf("job.Channels = %+v, want d2 and d3 kept", job.Channels)
	}
	// The label must not claim "All direct messages" once an exclusion applies.
	if label == "All direct messages" {
		t.Fatalf("label = %q, want the surviving conversations named, not 'All'", label)
	}
	for _, p := range stub.requested() {
		if strings.Contains(p, "guild") {
			t.Fatalf("DM deletion resolution requested a guild endpoint: %s", p)
		}
	}
}

// TestDeleteExcludingEverythingErrs: excluding every DM must refuse to build a
// job rather than silently produce an empty or broader deletion.
func TestDeleteExcludingEverythingErrs(t *testing.T) {
	res := newStubResolver(t, dmStub())

	_, _, err := res.buildDeleteJob(KindDeleteGuild, DeleteSpec{
		Guild:   "@me",
		Exclude: []string{"alice", "bob", "group chat"},
	})
	if err == nil {
		t.Fatal("a deletion whose exclusions match every conversation must error, not delete nothing/everything")
	}
}

// TestDeleteExcludeNoMatchKeepsAll: an exclusion that matches nothing is a
// no-op — every DM is still targeted (delete all, none kept back).
func TestDeleteExcludeNoMatchKeepsAll(t *testing.T) {
	res := newStubResolver(t, dmStub())

	job, _, err := res.buildDeleteJob(KindDeleteGuild, DeleteSpec{
		Guild:   "@me",
		Exclude: []string{"nobody-by-this-name"},
	})
	if err != nil {
		t.Fatalf("buildDeleteJob: %v", err)
	}
	if len(job.Channels) != 3 {
		t.Fatalf("job.Channels = %+v, want all 3 DMs kept when the exclusion matches none", job.Channels)
	}
}

// TestDMMonitorExcludingEverythingErrs: excluding every DM must refuse to build
// a job rather than fall back to any broader scope.
func TestDMMonitorExcludingEverythingErrs(t *testing.T) {
	res := newStubResolver(t, dmStub())

	_, err := res.buildMonitorJob(MonitorPolicy{
		Scope:        "@me",
		Mode:         ModeExclude,
		Channels:     []string{"alice", "bob", "group chat"},
		MaxAgeAmount: 7,
	})
	if err == nil {
		t.Fatal("a DM monitor whose exclusions match every conversation must error, not widen its scope")
	}
}
