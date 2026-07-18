package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/inherentescapade/viaduct/cfg"
	"github.com/inherentescapade/viaduct/discord"
	"net/http"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var channelsJSON bool

var channelsCmd = &cobra.Command{
	Use:   "channels <guild-name-or-id>",
	Short: "List channels in a server",
	Long:  "Lists all text channels in the specified Discord server. Accepts server name or ID.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		config := loadOrCreateConfig()
		applyFlagOverrides(config)

		if config.Token == "" {
			return fmt.Errorf("no token configured — run `viaduct` to set up, or use --token")
		}

		client := discord.NewClient(config.Token, config.BotMode, http.DefaultClient)

		guild, err := resolveGuildArg(client, args[0])
		if err != nil {
			return err
		}

		channels, err := client.GetChannels(guild.Id)
		if err != nil {
			return fmt.Errorf("failed to fetch channels: %w", err)
		}

		// Filter to text channels only (type 0 = text, type 5 = announcement)
		var textChannels []discord.Channel
		for _, ch := range channels {
			if ch.Type == 0 || ch.Type == 5 {
				textChannels = append(textChannels, ch)
			}
		}

		if channelsJSON {
			data, _ := json.MarshalIndent(textChannels, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "NAME\tID\tNSFW\n")
		fmt.Fprintf(w, "----\t--\t----\n")
		for _, ch := range textChannels {
			nsfw := ""
			if ch.Nsfw {
				nsfw = "yes"
			}
			fmt.Fprintf(w, "#%s\t%s\t%s\n", ch.Name, ch.Id, nsfw)
		}
		w.Flush()

		return nil
	},
}

func resolveGuildArg(client *discord.Client, nameOrID string) (*discord.Guild, error) {
	// Try cached guilds first
	guilds, err := cfg.LoadGuildCache()
	if err != nil || len(guilds) == 0 {
		guilds, err = client.GetGuilds()
		if err != nil {
			return nil, fmt.Errorf("failed to fetch guilds: %w", err)
		}
		_ = cfg.SaveGuildCache(guilds)
	}

	// Handle @me
	if nameOrID == "@me" {
		return &discord.Guild{Id: "@me", Name: "Direct Messages"}, nil
	}

	g := client.ResolveGuild(nameOrID, guilds)
	if g != nil {
		return g, nil
	}

	// Refresh and retry
	guilds, err = client.GetGuilds()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch guilds: %w", err)
	}
	_ = cfg.SaveGuildCache(guilds)

	g = client.ResolveGuild(nameOrID, guilds)
	if g != nil {
		return g, nil
	}

	fmt.Println("Server not found. Available servers:")
	for _, guild := range guilds {
		fmt.Printf("  %s (%s)\n", guild.Name, guild.Id)
	}
	return nil, fmt.Errorf("server %q not found", nameOrID)
}

func init() {
	channelsCmd.Flags().BoolVar(&channelsJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(channelsCmd)
}
