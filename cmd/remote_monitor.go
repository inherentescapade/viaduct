package cmd

import (
	"fmt"
	"github.com/inherentescapade/viaduct/server"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var remoteMonitorCmd = &cobra.Command{
	Use:   "monitor",
	Short: "Manage standing monitor policies on the server",
	Long: `A monitor is a standing rule the server applies on a schedule: keep messages
no older than a set age in the channels you choose. Use exclude mode to delete
everywhere EXCEPT a few protected DMs/channels, or include mode to delete ONLY
in specific ones.

Example — trim everything in your DMs older than 7 days, but never touch your
DMs with "mom" or the "work" group:
  viaduct remote monitor add --server vps --name tidy-dms \
    --scope @me --mode exclude --channels mom,work --age 7`,
}

var remoteMonitorAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Create (and optionally enable) a monitor policy",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := resolveClient()
		if err != nil {
			return err
		}
		if remoteName == "" {
			return fmt.Errorf("--name is required")
		}
		if monAge <= 0 {
			return fmt.Errorf("--age must be positive")
		}
		unit, err := server.ParseMonitorAgeUnit(monUnit)
		if err != nil {
			return err
		}
		mode := server.MonitorMode(monMode)
		if mode != server.ModeInclude && mode != server.ModeExclude {
			return fmt.Errorf("--mode must be 'include' or 'exclude'")
		}

		policy := server.MonitorPolicy{
			Name:         remoteName,
			Enabled:      monEnable,
			Scope:        monScope,
			Mode:         mode,
			Channels:     monChannels,
			MaxAgeAmount: monAge,
			MaxAgeUnit:   unit,
			IntervalHrs:  monInterval,
		}

		// Show how many messages this would affect right now before committing —
		// especially important before enabling.
		prev, err := client.PreviewMonitor(server.MonitorRequest{Policy: policy})
		if err != nil {
			return err
		}
		fmt.Printf("Right now this policy would delete %d message(s) older than %d %s in %s.\n",
			prev.Total, monAge, unit, scopeLabel(monScope))
		if monEnable {
			fmt.Printf("It will then re-run every %d hours.\n", intervalOr(monInterval))
			if !remoteYes && !confirm(fmt.Sprintf("Enable monitor %q now?", remoteName)) {
				fmt.Println("Cancelled — not saved.")
				return nil
			}
		}

		saved, err := client.SetMonitor(server.MonitorRequest{Policy: policy})
		if err != nil {
			return err
		}
		state := "saved (disabled)"
		if saved.Enabled {
			state = "enabled"
		}
		fmt.Printf("Monitor %s %s, runs every %d hours.\n", saved.ID, state, saved.IntervalHrs)
		return nil
	},
}

var remoteMonitorListCmd = &cobra.Command{
	Use:   "list",
	Short: "List monitor policies on the server",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := resolveClient()
		if err != nil {
			return err
		}
		mons, err := client.ListMonitors()
		if err != nil {
			return err
		}
		if len(mons) == 0 {
			fmt.Println("No monitors.")
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

var remoteMonitorRmCmd = &cobra.Command{
	Use:   "rm <id>",
	Short: "Delete a monitor policy",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := resolveClient()
		if err != nil {
			return err
		}
		if err := client.DeleteMonitor(args[0]); err != nil {
			return err
		}
		fmt.Printf("Deleted monitor %s.\n", args[0])
		return nil
	},
}

// addMonitorCommands wires the monitor subtree onto the remote command. Called
// from remote.go's init.
func addMonitorCommands() {
	remoteMonitorAddCmd.Flags().StringVar(&monScope, "scope", "@me", "Server name/ID, or @me for DMs")
	remoteMonitorAddCmd.Flags().StringVar(&monMode, "mode", "exclude", "exclude (everywhere but --channels) or include (only --channels)")
	remoteMonitorAddCmd.Flags().StringSliceVar(&monChannels, "channels", nil, "Channels for include/exclude (names or IDs)")
	remoteMonitorAddCmd.Flags().IntVar(&monAge, "age", 0, "Delete messages older than this many --unit (required)")
	remoteMonitorAddCmd.Flags().StringVar(&monUnit, "unit", "days", "Unit for --age: minutes, hours, days, or weeks")
	remoteMonitorAddCmd.Flags().IntVar(&monInterval, "interval", 0, "How often to run, in hours (default 6)")
	remoteMonitorAddCmd.Flags().BoolVar(&monEnable, "enable", false, "Enable the monitor immediately")
	remoteMonitorAddCmd.Flags().StringVar(&remoteName, "name", "", "Name for this monitor (required)")
	remoteMonitorAddCmd.Flags().BoolVar(&remoteYes, "yes", false, "Skip the confirmation prompt")

	remoteMonitorCmd.AddCommand(remoteMonitorAddCmd, remoteMonitorListCmd, remoteMonitorRmCmd)
	remoteCmd.AddCommand(remoteMonitorCmd)
}

func intervalOr(h int) int {
	if h <= 0 {
		return 6
	}
	return h
}
