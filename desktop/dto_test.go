package main

import (
	"strings"
	"testing"

	"github.com/inherentescapade/viaduct/discord"
)

func TestCDNHelpers(t *testing.T) {
	if got := guildIconURL("123", "abc"); got != "https://cdn.discordapp.com/icons/123/abc.png?size=64" {
		t.Errorf("guildIconURL = %q", got)
	}
	if got := userAvatarURL("9", "h"); !strings.Contains(got, "/avatars/9/h.png") {
		t.Errorf("userAvatarURL = %q", got)
	}
	// Empty hash → empty string (frontend falls back to initials).
	if got := guildIconURL("123", ""); got != "" {
		t.Errorf("empty icon should yield empty URL, got %q", got)
	}
	if got := userAvatarURL("9", ""); got != "" {
		t.Errorf("empty avatar should yield empty URL, got %q", got)
	}
}

func TestIsImageAttachment(t *testing.T) {
	cases := []struct {
		a    discord.Attachment
		want bool
	}{
		{discord.Attachment{ContentType: "image/png"}, true},
		{discord.Attachment{Filename: "pic.JPG"}, true},
		{discord.Attachment{Filename: "doc.pdf", ContentType: "application/pdf"}, false},
		{discord.Attachment{}, false},
	}
	for _, c := range cases {
		if got := isImageAttachment(c.a); got != c.want {
			t.Errorf("isImageAttachment(%+v) = %v, want %v", c.a, got, c.want)
		}
	}
}

func TestChannelLabel(t *testing.T) {
	// 1:1 DM uses the recipient's name.
	dm := discord.Channel{Type: 1, Recipients: []discord.User{{Username: "alice", GlobalName: "Alice A"}}}
	if got := channelLabel(dm); got != "Alice A" {
		t.Errorf("DM label = %q, want Alice A", got)
	}
	// Group DM keeps its title.
	group := discord.Channel{Type: 3, Name: "Squad"}
	if got := channelLabel(group); got != "Squad" {
		t.Errorf("group label = %q, want Squad", got)
	}
	// Guild text channel uses its name.
	text := discord.Channel{Type: 0, Name: "general"}
	if got := channelLabel(text); got != "general" {
		t.Errorf("text label = %q, want general", got)
	}
}
