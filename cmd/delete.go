package cmd

import (
	"context"
	"fmt"
	"github.com/inherentescapade/viaduct/dates"
	"github.com/inherentescapade/viaduct/discord"
	"github.com/inherentescapade/viaduct/engine"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	deleteChannels      string
	deleteExclude       string
	deleteBefore        string
	deleteAfter         string
	deleteDryRun        bool
	deleteYes           bool
	deleteMaxID         string
	deleteMinID         string
	deletePreScan       bool
	deleteIncludePinned bool
)

var deleteCmd = &cobra.Command{
	Use:   "delete <guild-name-or-id>",
	Short: "Delete your messages from a server",
	Long: `Delete your messages from a Discord server or DMs.

Examples:
  viaduct delete "My Server"
  viaduct delete "My Server" --before 2024-01-01
  viaduct delete "My Server" --after 30d --channels general,memes
  viaduct delete @me --exclude alice,bob
  viaduct delete @me --dry-run
  viaduct delete 123456789 --yes`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		config := loadOrCreateConfig()
		applyFlagOverrides(config)

		if config.Token == "" {
			return fmt.Errorf("no token configured — run `viaduct` to set up, or use --token")
		}

		client := discord.NewClient(config.Token, config.BotMode, http.DefaultClient)

		user, err := client.ValidateToken()
		if err != nil {
			return err
		}
		fmt.Printf("Logged in as %s (%s)\n\n", user.Username, user.Id)

		guild, err := resolveGuildArg(client, args[0])
		if err != nil {
			return err
		}

		channels, err := getDeleteChannels(client, guild)
		if err != nil {
			return err
		}

		if len(channels) == 0 {
			return fmt.Errorf("no channels to delete from")
		}

		job := engine.DeleteJob{
			GuildID:       guild.Id,
			GuildName:     guild.Name,
			Channels:      channels,
			UserID:        user.Id,
			MaxID:         deleteMaxID,
			MinID:         deleteMinID,
			PreScan:       deletePreScan || config.Prefs.PreScan,
			IncludePinned: deleteIncludePinned,
		}

		if deleteBefore != "" {
			t, err := dates.Parse(deleteBefore)
			if err != nil {
				return fmt.Errorf("invalid --before value: %w", err)
			}
			job.Before = t
		}
		if deleteAfter != "" {
			t, err := dates.Parse(deleteAfter)
			if err != nil {
				return fmt.Errorf("invalid --after value: %w", err)
			}
			job.After = t
		}

		eng := engine.New(config.Token, config.BotMode)

		// Print each message to the screen as it's enumerated/deleted.
		eng.OnMessage = printMessage

		// Surface the engine's pacing decisions above the live progress bar.
		eng.OnNotice = func(s string) {
			fmt.Printf("\r\033[K  • %s\n", s)
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()

		fmt.Printf("Scanning messages in %q...\n\n", guild.Name)

		if deleteDryRun {
			count, err := eng.Enumerate(ctx, job)
			if err != nil {
				return err
			}
			fmt.Printf("\n  Found %d messages\n\n", count)
			if count == 0 {
				fmt.Println("Nothing to delete.")
				return nil
			}
			fmt.Println("Dry run — no messages were deleted.")
			return nil
		}

		total, err := eng.Preview(ctx, job)
		if err != nil {
			return err
		}

		fmt.Printf("  Found %d messages\n\n", total)

		if total == 0 {
			fmt.Println("Nothing to delete.")
			return nil
		}

		if !deleteYes {
			fmt.Print("Press Enter to start deleting, or Ctrl+C to cancel...")
			fmt.Scanln()
			fmt.Println()
		}

		eng.OnProgress = func(p engine.Progress) {
			if p.Done {
				if p.Error != nil {
					fmt.Printf("\r\033[K  Error: %v\n", p.Error)
				} else if p.Ignored > 0 {
					fmt.Printf("\r\033[K  Done — %d deleted, %d failed, %d ignored (kept pinned or undeletable system messages)\n", p.Deleted, p.Failed, p.Ignored)
				} else {
					fmt.Printf("\r\033[K  Done — %d deleted, %d failed\n", p.Deleted, p.Failed)
				}
				return
			}

			pct := float64(0)
			if p.Total > 0 {
				pct = float64(p.Deleted) / float64(p.Total) * 100
			}
			elapsed := time.Since(p.StartTime)
			rate := float64(0)
			if elapsed.Seconds() > 0 {
				rate = float64(p.Deleted) / elapsed.Seconds()
			}

			width := 20
			filled := int(float64(width) * pct / 100)
			if filled > width {
				filled = width
			}
			bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)

			ign := ""
			if p.Ignored > 0 {
				ign = fmt.Sprintf(" ign:%d", p.Ignored)
			}
			fmt.Printf("\r\033[K  [%s %.0f%%] %d/%d (%.1f/s) rl:%d failed:%d%s",
				bar, pct, p.Deleted, p.Total, rate, p.RateLimited, p.Failed, ign)
		}

		if err := eng.Execute(ctx, job); err != nil {
			if ctx.Err() != nil {
				fmt.Println("\nCancelled by user.")
				return nil
			}
			return err
		}

		fmt.Printf("\nDone. Log: %s\n", eng.LogPath())
		return nil
	},
}

func getDeleteChannels(client *discord.Client, guild *discord.Guild) ([]discord.Channel, error) {
	allChannels, err := client.GetChannels(guild.Id)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch channels: %w", err)
	}

	// Filter to text channels
	var textChannels []discord.Channel
	for _, ch := range allChannels {
		if ch.Type == 0 || ch.Type == 5 || guild.Id == "@me" {
			textChannels = append(textChannels, ch)
		}
	}

	// Start from either the whole set or the explicitly requested channels.
	selected := textChannels
	if deleteChannels != "" {
		wanted := strings.Split(deleteChannels, ",")
		selected = nil
		for _, name := range wanted {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			ch := client.ResolveChannel(name, textChannels)
			if ch != nil {
				selected = append(selected, *ch)
			} else {
				fmt.Printf("Warning: channel %q not found, skipping\n", name)
			}
		}
	}

	// Drop any channels named with --exclude: "delete all other DMs/groups".
	if deleteExclude != "" {
		selected = excludeNamedChannels(selected, deleteExclude)
	}

	return selected, nil
}

// excludeNamedChannels removes from chans every channel matched by a
// comma-separated list of selectors — the deny-list counterpart to --channels,
// used to keep a few conversations while deleting the rest. A selector matches a
// channel's exact ID, a case-insensitive substring of its name, or (for DMs) a
// recipient's username/display name, so DMs can be excluded by who they're with.
func excludeNamedChannels(chans []discord.Channel, exclude string) []discord.Channel {
	var selectors []string
	for _, s := range strings.Split(exclude, ",") {
		if s = strings.TrimSpace(s); s != "" {
			selectors = append(selectors, s)
		}
	}
	if len(selectors) == 0 {
		return chans
	}
	var result []discord.Channel
	for _, ch := range chans {
		if !channelMatchesSelector(ch, selectors) {
			result = append(result, ch)
		}
	}
	return result
}

// channelMatchesSelector reports whether a channel matches any selector by exact
// ID, case-insensitive name substring, or DM recipient name.
func channelMatchesSelector(ch discord.Channel, selectors []string) bool {
	for _, raw := range selectors {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if s == ch.Id {
			return true
		}
		low := strings.ToLower(s)
		if ch.Name != "" && strings.Contains(strings.ToLower(ch.Name), low) {
			return true
		}
		for _, rcp := range ch.Recipients {
			if strings.Contains(strings.ToLower(rcp.Username), low) ||
				strings.Contains(strings.ToLower(rcp.GlobalName), low) {
				return true
			}
		}
	}
	return false
}

// printMessage writes a single message to stdout. It first clears the current
// terminal line (\r\033[K) so it prints cleanly above the live progress bar,
// which redraws itself on the next progress update.
func printMessage(msg discord.Message) {
	content := strings.TrimSpace(strings.ReplaceAll(msg.Content, "\n", " "))
	if content == "" {
		content = "[no text content]"
	}
	ts := msg.Timestamp.Local().Format("2006-01-02 15:04")
	fmt.Printf("\r\033[K  %s  %s\n", ts, content)
}

func init() {
	deleteCmd.Flags().StringVar(&deleteChannels, "channels", "", "Channels to delete from (comma-separated names or IDs)")
	deleteCmd.Flags().StringVar(&deleteExclude, "exclude", "", "Channels/DMs to keep — delete everything else (comma-separated names or IDs)")
	deleteCmd.Flags().StringVar(&deleteBefore, "before", "", "Only messages before this date (YYYY-MM-DD, 30d, ...)")
	deleteCmd.Flags().StringVar(&deleteAfter, "after", "", "Only messages after this date")
	deleteCmd.Flags().BoolVar(&deleteDryRun, "dry-run", false, "Show what would be deleted without deleting")
	deleteCmd.Flags().BoolVar(&deleteYes, "yes", false, "Skip the confirmation prompt")
	deleteCmd.Flags().StringVar(&deleteMaxID, "maxid", "", "Maximum message snowflake ID")
	deleteCmd.Flags().StringVar(&deleteMinID, "minid", "", "Minimum message snowflake ID")
	deleteCmd.Flags().BoolVar(&deletePreScan, "prescan", false, "Enumerate the full message list first for an exact total/ETA before deleting")
	deleteCmd.Flags().BoolVar(&deleteIncludePinned, "include-pinned", false, "Also delete pinned messages (kept by default)")
	rootCmd.AddCommand(deleteCmd)
}
