package server

import "github.com/inherentescapade/viaduct/discord"

// mkChannels builds simple text channels with the given names for filter tests.
func mkChannels(names ...string) []discord.Channel {
	out := make([]discord.Channel, 0, len(names))
	for i, n := range names {
		out = append(out, discord.Channel{Id: string(rune('a' + i)), Name: n})
	}
	return out
}
