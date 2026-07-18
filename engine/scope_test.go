package engine

import (
	"context"
	"net/http"
	"sort"
	"testing"

	"github.com/inherentescapade/viaduct/discord"
)

// TestScopes covers the search-scope selection that drives both Preview and the
// delete loop: guild-wide when no channels are selected, per-channel for DMs,
// and per-channel for channel-filtered guild deletions.
func TestScopes(t *testing.T) {
	cases := []struct {
		name string
		job  DeleteJob
		want []string
	}{
		{
			name: "guild-wide",
			job:  DeleteJob{GuildID: "g1"},
			want: []string{""},
		},
		{
			name: "guild channel-filtered",
			job:  DeleteJob{GuildID: "g1", Channels: []discord.Channel{{Id: "c1"}, {Id: "c2"}}},
			want: []string{"c1", "c2"},
		},
		{
			name: "dm",
			job:  DeleteJob{GuildID: "@me", Channels: []discord.Channel{{Id: "d1"}}},
			want: []string{"d1"},
		},
		{
			// A DM job's channels ARE its search scope: with none selected there
			// is nothing to search. It must never fall into the guild-wide ""
			// scope, which would search far beyond the user's DMs.
			name: "dm without channels searches nothing",
			job:  DeleteJob{GuildID: "@me"},
			want: []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.job.scopes()
			if len(got) != len(tc.want) {
				t.Fatalf("scopes() = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("scopes()[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestPreviewChannelFilter verifies that previewing a channel-filtered guild
// deletion searches each selected channel (passing channel_id) and sums the
// per-channel totals, rather than running a single guild-wide search.
func TestPreviewChannelFilter(t *testing.T) {
	const uid = "u1"
	// Per-channel result counts the stub returns for each channel_id.
	counts := map[string]int{"c1": 3, "c2": 5}
	var seen []string

	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		ch := r.URL.Query().Get("channel_id")
		seen = append(seen, ch)
		return jsonResponse(discord.SearchResponse{TotalResults: counts[ch]}), nil
	})

	e := New("token", false)
	e.Client = discord.NewClient("token", false, &http.Client{Transport: rt})

	job := DeleteJob{
		GuildID:  "g1",
		UserID:   uid,
		Channels: []discord.Channel{{Id: "c1"}, {Id: "c2"}},
	}
	total, err := e.Preview(context.Background(), job)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if total != 8 {
		t.Errorf("total = %d, want 8 (3+5)", total)
	}

	sort.Strings(seen)
	if len(seen) != 2 || seen[0] != "c1" || seen[1] != "c2" {
		t.Errorf("searched channel_ids = %v, want [c1 c2]", seen)
	}
}
