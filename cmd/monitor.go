package cmd

import (
	"context"
	"fmt"
	"github.com/inherentescapade/viaduct/cfg"
	"github.com/inherentescapade/viaduct/engine"
	"github.com/inherentescapade/viaduct/server"
	"log"
	"os"
	"os/signal"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// Local monitor flags.
var (
	lmonName     string
	lmonScope    string
	lmonMode     string
	lmonChannels []string
	lmonAge      int
	lmonUnit     string
	lmonInterval int
	lmonEnable   bool
	lmonYes      bool
)

var monitorCmd = &cobra.Command{
	Use:   "monitor",
	Short: "Run automatic retention cleanups on this machine",
	Long: `Keep messages trimmed to a maximum age automatically.

Monitors are stored on this machine and executed by the foreground daemon:

  viaduct monitor add --name tidy --scope @me --age 7 --enable
  viaduct monitor run        # leave this running on an always-on box

This is the self-contained way to "host" cleanups — no server or keys needed.
To run a monitor on a separate viaduct server instead, use ` + "`viaduct remote monitor`" + `.`,
}

// localMonitors builds a MonitorManager backed by the local config/token. The
// engine is constructed from the supplied credentials; resolution and execution
// happen in-process.
func localMonitors(config *cfg.Config) *server.MonitorManager {
	build := func(c server.Credentials) (*engine.Engine, error) {
		if c.Token == "" {
			return nil, fmt.Errorf("no Discord token — run `viaduct` to set one up, or use --token")
		}
		return engine.New(c.Token, c.BotMode), nil
	}
	creds := func() server.Credentials {
		return server.Credentials{Token: config.Token, BotMode: config.BotMode}
	}
	logger := log.New(os.Stdout, "", log.LstdFlags)
	return server.NewMonitorManager(cfg.LocalMonitorsPath(), build, creds, func(f string, a ...any) {
		logger.Printf(f, a...)
	})
}

func monitorPolicyFromFlags() server.MonitorPolicy {
	mode := server.MonitorMode(lmonMode)
	if mode != server.ModeInclude && mode != server.ModeExclude {
		mode = server.ModeExclude
	}
	unit, _ := server.ParseMonitorAgeUnit(lmonUnit)
	return server.MonitorPolicy{
		Name:         lmonName,
		Enabled:      lmonEnable,
		Scope:        lmonScope,
		Mode:         mode,
		Channels:     lmonChannels,
		MaxAgeAmount: lmonAge,
		MaxAgeUnit:   unit,
		IntervalHrs:  lmonInterval,
	}
}

var monitorAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Create a local monitor",
	RunE: func(cmd *cobra.Command, args []string) error {
		if lmonName == "" {
			return fmt.Errorf("--name is required")
		}
		if lmonAge <= 0 {
			return fmt.Errorf("--age must be positive")
		}
		unit, err := server.ParseMonitorAgeUnit(lmonUnit)
		if err != nil {
			return err
		}
		config := loadOrCreateConfig()
		applyFlagOverrides(config)
		mgr := localMonitors(config)
		policy := monitorPolicyFromFlags()

		// Show an impact preview before committing, when a token is available.
		if config.Token != "" {
			if n, err := mgr.Preview(policy); err != nil {
				fmt.Printf("Note: couldn't preview impact: %v\n", err)
			} else {
				fmt.Printf("Right now this would delete %d message(s) older than %d %s in %s.\n",
					n, lmonAge, unit, scopeLabel(lmonScope))
			}
		}
		if lmonEnable && !lmonYes && !confirm("Enable this monitor now?") {
			fmt.Println("Cancelled.")
			return nil
		}

		saved, err := mgr.Upsert(policy)
		if err != nil {
			return err
		}
		state := "saved (disabled)"
		if saved.Enabled {
			state = "enabled"
		}
		fmt.Printf("Monitor %s %s. Run `viaduct monitor run` to start the daemon.\n", saved.ID, state)
		return nil
	},
}

var monitorListCmd = &cobra.Command{
	Use:   "list",
	Short: "List local monitors",
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := localMonitors(loadOrCreateConfig())
		mons := mgr.List()
		if len(mons) == 0 {
			fmt.Println("No monitors. Add one with `viaduct monitor add`.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "ID\tNAME\tENABLED\tSCOPE\tMODE\tAGE\tEVERY(h)\tLAST\n")
		for _, m := range mons {
			last := "never"
			if !m.LastRun.IsZero() {
				last = fmt.Sprintf("%s (%d del)", m.LastRun.Format("2006-01-02 15:04"), m.LastDeleted)
			}
			fmt.Fprintf(w, "%s\t%s\t%v\t%s\t%s\t%d%s\t%d\t%s\n",
				m.ID, m.Name, m.Enabled, m.Scope, m.Mode, m.MaxAgeAmount, m.MaxAgeUnit.Short(), m.IntervalHrs, last)
		}
		w.Flush()
		return nil
	},
}

var monitorRmCmd = &cobra.Command{
	Use:   "rm <id>",
	Short: "Delete a local monitor",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := localMonitors(loadOrCreateConfig())
		if !mgr.Delete(args[0]) {
			return fmt.Errorf("monitor %q not found", args[0])
		}
		fmt.Printf("Deleted %s.\n", args[0])
		return nil
	},
}

// setEnabled flips a stored monitor's enabled flag by re-upserting it.
func setEnabled(id string, enabled bool) error {
	mgr := localMonitors(loadOrCreateConfig())
	for _, m := range mgr.List() {
		if m.ID == id {
			m.Enabled = enabled
			if _, err := mgr.Upsert(m); err != nil {
				return err
			}
			state := "off"
			if enabled {
				state = "on"
			}
			fmt.Printf("Monitor %s is now %s.\n", id, state)
			return nil
		}
	}
	return fmt.Errorf("monitor %q not found", id)
}

var monitorOnCmd = &cobra.Command{
	Use:   "on <id>",
	Short: "Enable a local monitor",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return setEnabled(args[0], true) },
}

var monitorOffCmd = &cobra.Command{
	Use:   "off <id>",
	Short: "Disable a local monitor",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return setEnabled(args[0], false) },
}

var monitorRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the monitor daemon in the foreground (Ctrl+C to stop)",
	Long: `Run enabled monitors on their schedule until interrupted.

Leave this running on an always-on machine (e.g. under systemd, tmux, or nohup)
to keep your messages trimmed automatically.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		config := loadOrCreateConfig()
		applyFlagOverrides(config)
		if config.Token == "" {
			return fmt.Errorf("no Discord token — run `viaduct` to set one up, or use --token")
		}
		mgr := localMonitors(config)
		mons := mgr.List()
		enabled := 0
		for _, m := range mons {
			if m.Enabled {
				enabled++
			}
		}
		fmt.Printf("Monitor daemon started: %d monitor(s), %d enabled.\n", len(mons), enabled)
		if enabled == 0 {
			fmt.Println("Nothing enabled yet — add one with `viaduct monitor add ... --enable`.")
		}
		fmt.Println("Running. Press Ctrl+C to stop.")

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()
		mgr.Run(ctx)
		fmt.Println("\nMonitor daemon stopped.")
		return nil
	},
}

func scopeLabel(scope string) string {
	if scope == "@me" || scope == "" {
		return "your DMs"
	}
	return scope
}

func init() {
	monitorAddCmd.Flags().StringVar(&lmonName, "name", "", "Name for this monitor (required)")
	monitorAddCmd.Flags().StringVar(&lmonScope, "scope", "@me", "Server name/ID, or @me for DMs")
	monitorAddCmd.Flags().StringVar(&lmonMode, "mode", "exclude", "exclude (everywhere but --channels) or include (only --channels)")
	monitorAddCmd.Flags().StringSliceVar(&lmonChannels, "channels", nil, "Channels for include/exclude (names or IDs)")
	monitorAddCmd.Flags().IntVar(&lmonAge, "age", 0, "Delete messages older than this many --unit (required)")
	monitorAddCmd.Flags().StringVar(&lmonUnit, "unit", "days", "Unit for --age: minutes, hours, days, or weeks")
	monitorAddCmd.Flags().IntVar(&lmonInterval, "interval", 0, "How often to run, in hours (default 6)")
	monitorAddCmd.Flags().BoolVar(&lmonEnable, "enable", false, "Enable immediately")
	monitorAddCmd.Flags().BoolVar(&lmonYes, "yes", false, "Skip the confirmation prompt")

	monitorCmd.AddCommand(monitorAddCmd, monitorListCmd, monitorRmCmd, monitorOnCmd, monitorOffCmd, monitorRunCmd)
	rootCmd.AddCommand(monitorCmd)
}
