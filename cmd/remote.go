package cmd

import (
	"bufio"
	"fmt"
	"github.com/inherentescapade/viaduct/auth"
	"github.com/inherentescapade/viaduct/cfg"
	"github.com/inherentescapade/viaduct/server"
	"github.com/inherentescapade/viaduct/token"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

var (
	remoteServer string
	remoteName   string
	remoteToken  string
	remoteCode   string
	remoteYes    bool

	rmChannels []string
	rmExclude  []string
	rmBefore   string
	rmAfter    string
	rmVerify   bool
	rmNoCount  bool

	monScope    string
	monMode     string
	monChannels []string
	monAge      int
	monUnit     string
	monInterval int
	monEnable   bool
)

var remoteCmd = &cobra.Command{
	Use:   "remote",
	Short: "Dispatch jobs to a self-hosted viaduct server",
	Long: `Talk to a viaduct server you run elsewhere (see ` + "`viaduct serve`" + `).

First run — pair with the server using the code it shows in its terminal:
  viaduct remote pair --name vps --server HOST:21776

After that, just use the saved name:
  viaduct remote delete "My Server" --server vps
  viaduct remote delete @me --exclude alice,bob --server vps
  viaduct remote dm @someone --server vps
  viaduct remote monitor add --server vps --name tidy --scope @me --age 7`,
}

// resolveClient builds a server client for a paired server named by --server.
// The server's key comes from the saved pairing, so there is no key to pass by
// hand: pair first with `viaduct remote pair`.
func resolveClient() (*server.Client, *cfg.Config, error) {
	config := loadOrCreateConfig()
	id, err := auth.LoadIdentity(cfg.IdentityPath())
	if err != nil {
		return nil, nil, fmt.Errorf("no client identity yet — pair with a server first using `viaduct remote pair`")
	}
	if remoteServer == "" {
		return nil, nil, fmt.Errorf("specify --server <name>")
	}
	r := config.FindRemote(remoteServer)
	if r == nil || r.PublicKey == "" {
		return nil, nil, fmt.Errorf("%q is not a paired server — pair it first with `viaduct remote pair --server %s`", remoteServer, remoteServer)
	}
	pub, err := auth.ParsePublicKey(r.PublicKey)
	if err != nil {
		return nil, nil, fmt.Errorf("the saved server key is corrupt — re-pair with `viaduct remote pair`")
	}
	return server.NewClient(r.Address, id, pub), config, nil
}

var remotePingCmd = &cobra.Command{
	Use:   "ping",
	Short: "Check a server is reachable and see its state",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := resolveClient()
		if err != nil {
			return err
		}
		resp, err := client.Ping()
		if err != nil {
			return err
		}
		fmt.Printf("Connected to %s\n", resp.Version)
		if resp.ActingAs != nil {
			fmt.Printf("  Acting as: %s (%s)\n", resp.ActingAs.Username, resp.ActingAs.ID)
		} else if resp.HasToken {
			fmt.Println("  Acting as: (token present but not validated)")
		} else {
			fmt.Println("  Acting as: nobody yet — push a token with `viaduct remote connect`")
		}
		fmt.Printf("  Jobs: %d   Monitors: %d\n", resp.Jobs, resp.Monitors)
		return nil
	},
}

var remoteConnectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Refresh the Discord token a paired server acts with",
	Long: `Push (or update) your Discord token on a server you've already paired with:
detect the token locally and send it encrypted end-to-end. Use this when your
token has changed; the first-time setup is ` + "`viaduct remote pair`" + `.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, config, err := resolveClient()
		if err != nil {
			return err
		}

		candidates, err := detectTokens(config)
		if err != nil {
			return err
		}

		var acted *server.CredentialsResponse
		var lastErr error
		for _, tk := range candidates {
			resp, err := client.PushCredentials(server.Credentials{Token: tk, BotMode: config.BotMode})
			if err == nil {
				acted = resp
				break
			}
			lastErr = err
		}
		if acted == nil {
			return fmt.Errorf("the server could not validate any detected token: %w", lastErr)
		}
		fmt.Printf("Updated. The server is now acting as %s (%s).\n", acted.ActingAs.Username, acted.ActingAs.ID)
		return nil
	},
}

var remotePairCmd = &cobra.Command{
	Use:   "pair",
	Short: "Pair with a server using the code shown in its terminal (no key copying)",
	Long: `Pair with a server in one step. Point --server at its host:port, then enter
the short code shown on the server's terminal. viaduct fetches the server's key,
proves to the server that you saw the same code, and verifies the server proved
it too — so no keys are copied by hand and the server needs no restart.

  viaduct remote pair --name vps --server HOST:21776

Your client identity is created automatically if you don't have one yet. If a
Discord token is detected locally, it's pushed to the server in the same step.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if remoteServer == "" {
			return fmt.Errorf("specify --server <host:port>")
		}
		config := loadOrCreateConfig()

		// Resolve the address (a saved name is allowed, but pairing usually uses a
		// raw host:port the first time).
		addr := remoteServer
		if r := config.FindRemote(remoteServer); r != nil {
			addr = r.Address
		}

		// Create the client identity on the fly so pairing is genuinely one step.
		id, created, err := auth.LoadOrCreateIdentity(cfg.IdentityPath())
		if err != nil {
			return fmt.Errorf("could not load or create your client identity: %w", err)
		}
		if created {
			fmt.Println("Generated a new client identity for this machine.")
		}

		// Ask the server to start pairing; it shows a code on its own terminal.
		if err := server.PairBegin(addr, id); err != nil {
			return err
		}
		fmt.Println("A pairing code is now shown on the server's terminal.")

		code := strings.TrimSpace(remoteCode)
		if code == "" {
			code = promptLine("Enter the code shown on the server: ")
		}
		if code == "" {
			return fmt.Errorf("no code entered")
		}

		serverPub, err := server.PairComplete(addr, id, code)
		if err != nil {
			return err
		}
		fmt.Println("Paired — the server now trusts this client.")

		// Save the server so future commands only need --server <name>.
		name := remoteName
		if name == "" {
			name = remoteServer
		}
		remote := cfg.RemoteServer{
			Name:      name,
			Address:   addr,
			PublicKey: auth.EncodePublicKey(serverPub),
		}
		// Merge onto the on-disk config rather than saving our startup snapshot:
		// on a single-machine setup the server writes its own fields (authorized
		// keys, accounts) to this same file during pairing, and a blind Save
		// here would erase them.
		if err := cfg.Update(func(fresh *cfg.Config) {
			fresh.UpsertRemote(remote)
		}); err != nil {
			return err
		}
		fmt.Printf("Saved this server as %q — next time just use --server %s.\n", name, name)

		// Push the Discord token now, over the freshly trusted channel, so the
		// server can act for you straight away.
		client := server.NewClient(addr, id, serverPub)
		candidates, err := detectTokens(config)
		if err != nil {
			fmt.Println("No Discord token found yet — push one later with `viaduct remote connect`.")
			return nil
		}
		var acted *server.CredentialsResponse
		var lastErr error
		for _, tk := range candidates {
			resp, err := client.PushCredentials(server.Credentials{Token: tk, BotMode: config.BotMode})
			if err == nil {
				acted = resp
				break
			}
			lastErr = err
		}
		if acted == nil {
			fmt.Printf("Paired, but the server could not validate a token: %v\n", lastErr)
			fmt.Println("Push a working token later with `viaduct remote connect`.")
			return nil
		}
		fmt.Printf("The server is now acting as %s (%s).\n", acted.ActingAs.Username, acted.ActingAs.ID)
		return nil
	},
}

var remoteDeleteCmd = &cobra.Command{
	Use:   "delete <server-name-or-id>",
	Short: "Dispatch a server/DM message deletion to the remote",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := resolveClient()
		if err != nil {
			return err
		}
		spec := server.DeleteSpec{
			Guild:    args[0],
			Channels: rmChannels,
			Exclude:  rmExclude,
			Before:   rmBefore,
			After:    rmAfter,
			Verify:   rmVerify,
		}
		return previewConfirmSubmit(client, server.KindDeleteGuild, spec)
	},
}

var remoteDMCmd = &cobra.Command{
	Use:   "dm <user-id-or-username>",
	Short: "Dispatch deletion of your messages in DMs with a user",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := resolveClient()
		if err != nil {
			return err
		}
		spec := server.DeleteSpec{
			User:   strings.TrimPrefix(args[0], "@"),
			Before: rmBefore,
			After:  rmAfter,
			Verify: rmVerify,
		}
		return previewConfirmSubmit(client, server.KindDeleteDM, spec)
	},
}

// previewConfirmSubmit previews the affected count, asks for confirmation, then
// submits the job.
//
// With --no-count the preview is skipped entirely: counting is a live Discord
// search that runs synchronously inside the preview RPC, and on a large account
// or under rate-limiting it can outlast the client's request timeout, killing
// the command before any job is even dispatched. Skipping it submits the job
// straight away — the server tallies the real total as it works, visible via
// `viaduct remote job <id>`.
func previewConfirmSubmit(client *server.Client, kind server.JobKind, spec server.DeleteSpec) error {
	if rmNoCount {
		if !remoteYes && !confirm("Dispatch this deletion without counting first?") {
			fmt.Println("Cancelled.")
			return nil
		}
		return submitJob(client, kind, spec)
	}
	// Count in the background and poll, so a long count (large account, heavy
	// rate-limiting) isn't bounded by a single request's timeout — a synchronous
	// preview would time out on big accounts.
	fmt.Print("Counting affected messages… ")
	prev, err := client.PreviewAwait(
		server.PreviewRequest{Kind: kind, Spec: spec},
		2*time.Second,
		func() { fmt.Print(".") },
	)
	fmt.Println()
	if err != nil {
		return err
	}
	fmt.Printf("Server is acting as %s.\n", prev.ActingAs.Username)
	fmt.Printf("This will delete %d message(s) in %s.\n", prev.Total, prev.Target)
	if prev.Total == 0 {
		fmt.Println("Nothing to delete.")
		return nil
	}
	if !remoteYes && !confirm("Dispatch this deletion?") {
		fmt.Println("Cancelled.")
		return nil
	}
	return submitJob(client, kind, spec)
}

// submitJob dispatches the deletion and prints how to follow its progress.
func submitJob(client *server.Client, kind server.JobKind, spec server.DeleteSpec) error {
	job, err := client.SubmitJob(server.JobRequest{Kind: kind, Spec: spec})
	if err != nil {
		return err
	}
	fmt.Printf("Dispatched job %s. The server will keep working even if you disconnect.\n", job.ID)
	fmt.Printf("Check progress with: viaduct remote job %s --server %s\n", job.ID, remoteServer)
	return nil
}

var remoteJobsCmd = &cobra.Command{
	Use:   "jobs",
	Short: "List jobs on the server",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := resolveClient()
		if err != nil {
			return err
		}
		jobs, err := client.ListJobs()
		if err != nil {
			return err
		}
		if len(jobs) == 0 {
			fmt.Println("No jobs.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "ID\tKIND\tSTATE\tDELETED\tTOTAL\tTARGET\n")
		for _, j := range jobs {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\n", j.ID, j.Kind, j.State, j.Deleted, j.Total, j.Description)
		}
		w.Flush()
		return nil
	},
}

var remoteJobCmd = &cobra.Command{
	Use:   "job <id>",
	Short: "Show one job's status",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := resolveClient()
		if err != nil {
			return err
		}
		j, err := client.GetJob(args[0])
		if err != nil {
			return err
		}
		fmt.Printf("Job %s (%s)\n", j.ID, j.Kind)
		fmt.Printf("  Target:  %s\n", j.Description)
		fmt.Printf("  State:   %s\n", j.State)
		fmt.Printf("  Deleted: %d / %d   Failed: %d   Skipped: %d\n", j.Deleted, j.Total, j.Failed, j.Skipped)
		if j.Ignored > 0 {
			fmt.Printf("  Ignored (undeletable system messages): %d\n", j.Ignored)
		}
		if j.Residual > 0 {
			fmt.Printf("  Residual after verify: %d\n", j.Residual)
		}
		if j.Error != "" {
			fmt.Printf("  Error:   %s\n", j.Error)
		}
		return nil
	},
}

var remoteCancelCmd = &cobra.Command{
	Use:   "cancel <id>",
	Short: "Cancel a running job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, _, err := resolveClient()
		if err != nil {
			return err
		}
		j, err := client.CancelJob(args[0])
		if err != nil {
			return err
		}
		fmt.Printf("Requested cancellation of %s (state: %s).\n", j.ID, j.State)
		return nil
	},
}

func init() {
	remoteCmd.PersistentFlags().StringVarP(&remoteServer, "server", "s", "", "Paired server name (or host:port when pairing)")

	remotePairCmd.Flags().StringVar(&remoteName, "name", "", "Save the server under this name for later use")
	remotePairCmd.Flags().StringVar(&remoteCode, "code", "", "Pairing code shown on the server (otherwise you're prompted)")
	remotePairCmd.Flags().StringVar(&remoteToken, "token", "", "Use this Discord token instead of auto-detecting")
	remoteConnectCmd.Flags().StringVar(&remoteToken, "token", "", "Use this Discord token instead of auto-detecting")

	for _, c := range []*cobra.Command{remoteDeleteCmd, remoteDMCmd} {
		c.Flags().StringVar(&rmBefore, "before", "", "Only messages before this date (YYYY-MM-DD, 30d, ...)")
		c.Flags().StringVar(&rmAfter, "after", "", "Only messages after this date")
		c.Flags().BoolVar(&rmVerify, "verify", false, "Re-check and re-delete stragglers until none remain")
		c.Flags().BoolVar(&rmNoCount, "no-count", false, "Skip the up-front count and dispatch immediately (avoids the preview timing out on large accounts)")
		c.Flags().BoolVar(&remoteYes, "yes", false, "Skip the confirmation prompt")
	}
	remoteDeleteCmd.Flags().StringSliceVar(&rmChannels, "channels", nil, "Limit to these channels (names or IDs)")
	remoteDeleteCmd.Flags().StringSliceVar(&rmExclude, "exclude", nil, "Delete everywhere in scope EXCEPT these channels/DMs (names or IDs)")

	remoteCmd.AddCommand(remotePingCmd, remotePairCmd, remoteConnectCmd, remoteDeleteCmd, remoteDMCmd, remoteJobsCmd, remoteJobCmd, remoteCancelCmd)
	addMonitorCommands()
	rootCmd.AddCommand(remoteCmd)
}

// --- helpers ---

// detectTokens returns the token candidates to try: an explicit --token, the
// configured token, or whatever the local token detector finds.
func detectTokens(config *cfg.Config) ([]string, error) {
	if remoteToken != "" {
		return []string{remoteToken}, nil
	}
	var out []string
	if config.Token != "" {
		out = append(out, config.Token)
	}
	if found, err := token.GetTokens(); err == nil {
		out = append(out, found...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no Discord token found — pass --token, or run `viaduct config set token <token>`")
	}
	return out, nil
}

// promptLine prints a prompt and reads a single trimmed line from stdin.
func promptLine(prompt string) string {
	fmt.Print(prompt)
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}

func confirm(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}
