package server

import (
	"github.com/inherentescapade/viaduct/discord"
	"testing"
)

func TestAppendFeedCaps(t *testing.T) {
	names := map[string]string{"c1": "#general"}
	var feed []FeedMessage
	for i := 0; i < feedCap+25; i++ {
		msg := discord.Message{ChannelId: "c1", Content: "x"}
		feed = appendFeed(feed, msg, names)
	}
	if len(feed) != feedCap {
		t.Fatalf("feed should cap at %d, got %d", feedCap, len(feed))
	}
	if feed[0].Channel != "#general" {
		t.Fatalf("channel label not applied: %q", feed[0].Channel)
	}
}

func TestLabelForChannel(t *testing.T) {
	guild := discord.Channel{Id: "1", Name: "general"}
	if got := labelForChannel(guild); got != "#general" {
		t.Fatalf("guild channel label = %q", got)
	}
	// A 1:1 DM reads as a location ("DM · <name>"), not as an author.
	dm := discord.Channel{Id: "2"}
	dm.Recipients = []discord.User{{Username: "bob", GlobalName: "Bob B"}}
	if got := labelForChannel(dm); got != "DM · Bob B" {
		t.Fatalf("dm label = %q, want %q", got, "DM · Bob B")
	}
	// A group DM lists its members under "group · ".
	grp := discord.Channel{Id: "3"}
	grp.Recipients = []discord.User{{Username: "a"}, {Username: "b"}}
	if got := labelForChannel(grp); got != "group · a, b" {
		t.Fatalf("group label = %q", got)
	}
}
