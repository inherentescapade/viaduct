package cmd

import (
	"context"
	"fmt"
	"github.com/inherentescapade/viaduct/dates"
	"github.com/inherentescapade/viaduct/engine"
	"github.com/inherentescapade/viaduct/export"
	"os"
	"os/signal"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

var (
	importExclude   []string
	importInclude   []string
	importForgotten bool
	importNoDMs     bool
	importBefore    string
	importAfter     string
	importDryRun    bool
	importYes       bool
	importList      bool
)

var importCmd = &cobra.Command{
	Use:   "import <export-path>",
	Short: "Delete messages listed in a Discord data export",
	Long: `Delete your messages using a Discord "Data Package" export.

Point this at the unzipped data package (or its Messages folder). Every channel
you've sent messages in is included by default; use --exclude/--include to
narrow it down, or --list to see what's there first.

The export covers channels the search API can't reach — including servers you've
left — so --forgotten targets exactly those "lost access" server channels.

Channel selectors (--exclude/--include) match a channel ID, a case-insensitive
substring of its name, or its type (DM, GROUP_DM, GUILD_TEXT, ...).

Examples:
  viaduct import ~/Downloads/discord/package --list
  viaduct import ~/Downloads/discord/package --forgotten
  viaduct import ~/Downloads/discord/package --no-dms --exclude "My Server,123456789"
  viaduct import ~/Downloads/discord/package/Messages --before 2024-01-01 --dry-run`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ex, err := export.Load(args[0])
		if err != nil {
			return err
		}

		channels := selectChannels(ex.Channels)

		if importList {
			printChannelList(channels)
			return nil
		}

		if len(channels) == 0 {
			return fmt.Errorf("no channels selected — check your --include/--exclude/--forgotten filters")
		}

		job := engine.ImportJob{Channels: channels}
		if importBefore != "" {
			t, err := dates.Parse(importBefore)
			if err != nil {
				return fmt.Errorf("invalid --before value: %w", err)
			}
			job.Before = t
		}
		if importAfter != "" {
			t, err := dates.Parse(importAfter)
			if err != nil {
				return fmt.Errorf("invalid --after value: %w", err)
			}
			job.After = t
		}

		config := loadOrCreateConfig()
		applyFlagOverrides(config)
		eng := engine.New(config.Token, config.BotMode)

		total := eng.CountImport(job)
		fmt.Printf("Source: %s\n", ex.Root)
		fmt.Printf("Selected %d channel(s), %d message(s) to delete.\n\n", len(channels), total)

		if total == 0 {
			fmt.Println("Nothing to delete.")
			return nil
		}

		if importDryRun {
			printChannelList(channels)
			fmt.Println("\nDry run — no messages were deleted.")
			return nil
		}

		if config.Token == "" {
			return fmt.Errorf("no token configured — run `viaduct` to set up, or use --token")
		}

		user, err := eng.Client.ValidateToken()
		if err != nil {
			return err
		}
		fmt.Printf("Logged in as %s (%s)\n", user.Username, user.Id)

		if !importYes {
			fmt.Printf("\nDelete %d messages from %d channels? Press Enter to start, or Ctrl+C to cancel...", total, len(channels))
			fmt.Scanln()
			fmt.Println()
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()

		eng.OnProgress = importProgressPrinter()

		if err := eng.ExecuteImport(ctx, job); err != nil {
			if ctx.Err() != nil {
				fmt.Println("\nCancelled by user.")
				printFailureSummary(eng)
				return nil
			}
			return err
		}

		printFailureSummary(eng)
		fmt.Printf("\nDone. Log: %s\n", eng.LogPath())
		return nil
	},
}

// selectChannels applies the channel-level filters in order: forgotten, no-dms,
// include, then exclude.
func selectChannels(all []export.Channel) []export.Channel {
	var out []export.Channel
	for _, ch := range all {
		if len(ch.Messages) == 0 {
			continue
		}
		if importForgotten && !ch.IsForgotten() {
			continue
		}
		if importNoDMs && ch.IsDM() {
			continue
		}
		if len(importInclude) > 0 && !matchesAny(ch, importInclude) {
			continue
		}
		if matchesAny(ch, importExclude) {
			continue
		}
		out = append(out, ch)
	}
	return out
}

// matchesAny reports whether the channel matches any selector token: an exact
// channel ID, a case-insensitive substring of the name, or its type.
func matchesAny(ch export.Channel, tokens []string) bool {
	for _, raw := range tokens {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		if t == ch.ID {
			return true
		}
		if strings.EqualFold(t, ch.Type) {
			return true
		}
		lower := strings.ToLower(t)
		if strings.Contains(strings.ToLower(ch.Name), lower) {
			return true
		}
		if ch.IndexName != "" && strings.Contains(strings.ToLower(ch.IndexName), lower) {
			return true
		}
	}
	return false
}

func printChannelList(channels []export.Channel) {
	if len(channels) == 0 {
		fmt.Println("No channels match the current filters.")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "NAME\tTYPE\tID\tMESSAGES\tACCESS\n")
	fmt.Fprintf(w, "----\t----\t--\t--------\t------\n")
	for _, ch := range channels {
		access := "ok"
		if ch.IsForgotten() {
			access = "forgotten"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", ch.Name, ch.Type, ch.ID, len(ch.Messages), access)
	}
	w.Flush()
}

// printFailureSummary shows what Discord actually returned for failed deletes,
// grouped by reason and ordered by frequency.
func printFailureSummary(eng *engine.Engine) {
	reasons := eng.FailureSummary()
	if len(reasons) == 0 {
		return
	}
	fmt.Println("\nFailures by reason:")
	for _, r := range reasons {
		fmt.Printf("  %6d  %s\n", r.Count, r.Reason)
	}
}

func importProgressPrinter() func(engine.Progress) {
	return func(p engine.Progress) {
		if p.Done {
			if p.Error != nil {
				fmt.Printf("\r\033[K  Error: %v\n", p.Error)
				return
			}
			fmt.Printf("\r\033[K  Done — %d deleted, %d skipped, %d failed\n", p.Deleted, p.Skipped, p.Failed)
			return
		}

		processed := p.Deleted + p.Skipped + p.Failed
		pct := float64(0)
		if p.Total > 0 {
			pct = float64(processed) / float64(p.Total) * 100
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

		channel := p.Channel
		if len(channel) > 24 {
			channel = channel[:21] + "..."
		}

		fmt.Printf("\r\033[K  [%s %.0f%%] %d/%d (%.1f/s) skip:%d fail:%d  %s",
			bar, pct, processed, p.Total, rate, p.Skipped, p.Failed, channel)
	}
}

func init() {
	importCmd.Flags().StringSliceVar(&importExclude, "exclude", nil, "Channels to skip (ID, name substring, or type; comma-separated or repeated)")
	importCmd.Flags().StringSliceVar(&importInclude, "include", nil, "Only these channels (ID, name substring, or type)")
	importCmd.Flags().BoolVar(&importForgotten, "forgotten", false, "Only server channels you no longer have access to (never DMs)")
	importCmd.Flags().BoolVar(&importNoDMs, "no-dms", false, "Skip direct and group DM channels")
	importCmd.Flags().StringVar(&importBefore, "before", "", "Only messages before this date (YYYY-MM-DD, 30d, ...)")
	importCmd.Flags().StringVar(&importAfter, "after", "", "Only messages after this date")
	importCmd.Flags().BoolVar(&importDryRun, "dry-run", false, "Show what would be deleted without deleting")
	importCmd.Flags().BoolVar(&importYes, "yes", false, "Skip the confirmation prompt")
	importCmd.Flags().BoolVar(&importList, "list", false, "List matching channels and exit")
	rootCmd.AddCommand(importCmd)
}
