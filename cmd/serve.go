package cmd

import (
	"context"
	"fmt"
	"github.com/inherentescapade/viaduct/auth"
	"github.com/inherentescapade/viaduct/cfg"
	"github.com/inherentescapade/viaduct/server"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

var (
	servePort   int
	serveListen string
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run viaduct as a self-hosted job owner (e.g. on a VPS)",
	Long: `Run viaduct as a long-lived server that owns deletion jobs.

You run this on a small always-on machine (a cheap VPS). An authorized client
connects, pushes its Discord token (encrypted end-to-end), and dispatches jobs
like "delete my DMs with X" or sets up monitors that keep messages trimmed to a
maximum age — all while your own machine stays offline. Jobs and monitors are
kept private to the Discord account they were made for: any client that pushes
the same token (e.g. you pairing a second machine) shares that one account's
jobs and monitors, since the same token is guaranteed to be the same person.

Every connection is end-to-end encrypted with X25519 + ChaCha20-Poly1305. To
authorize a client, run the server and pair it: the server shows a 6-digit code
that the client enters once (a SPAKE2 exchange), with no keys to copy.

  viaduct serve --port 21776`,
	RunE: func(cmd *cobra.Command, args []string) error {
		config := loadOrCreateConfig()

		// The server has its own identity key; clients encrypt to it.
		id, created, err := auth.LoadOrCreateIdentity(cfg.ServerIdentityPath())
		if err != nil {
			return fmt.Errorf("could not load server identity: %w", err)
		}

		if servePort != 0 {
			config.Server.Port = servePort
		}
		if serveListen != "" {
			config.Server.Listen = serveListen
		}
		addr := config.Server.Addr()

		// Resume each account's persisted token so its monitors run again, and
		// restore each paired client key's routing to its account. Any client key
		// that pushes the same token shares that one account's jobs and monitors.
		initialAccounts := make(map[string]server.Credentials, len(config.Server.Accounts))
		for acctKey, cred := range config.Server.Accounts {
			initialAccounts[acctKey] = server.Credentials{Token: cred.Token, BotMode: cred.BotMode}
		}
		initialClientKeys := make(map[string]string, len(config.Server.ClientKeys))
		for clientKey, acctKey := range config.Server.ClientKeys {
			initialClientKeys[clientKey] = acctKey
		}

		// Config writes from concurrent RPCs (token pushes, pairings) share one
		// file, so serialize them.
		var cfgMu sync.Mutex
		srv, err := server.New(server.Options{
			Identity:          id,
			AuthorizedKeys:    config.Server.AuthorizedKeys,
			InitialAccounts:   initialAccounts,
			InitialClientKeys: initialClientKeys,
			MonitorsPath:      cfg.MonitorsPath(),
			LogDir:            cfg.LogDir(),
			SaveAccount: func(acctKey string, c server.Credentials) error {
				// Persist one account's pushed token, keyed by its account key.
				// Merge onto the on-disk config so a client sharing this file
				// (same-machine setup) doesn't lose its saved `remotes`.
				cfgMu.Lock()
				defer cfgMu.Unlock()
				return cfg.Update(func(fresh *cfg.Config) {
					fresh.SetAccountToken(acctKey, cfg.ClientCredential{Token: c.Token, BotMode: c.BotMode})
				})
			},
			LinkClientKey: func(clientKey, acctKey string) error {
				// Persist a client key's routing to its account.
				cfgMu.Lock()
				defer cfgMu.Unlock()
				return cfg.Update(func(fresh *cfg.Config) {
					fresh.SetClientAccount(clientKey, acctKey)
				})
			},
			AuthorizeKey: func(pubKey string) error {
				// Persist a freshly paired client key so it survives restarts.
				cfgMu.Lock()
				defer cfgMu.Unlock()
				return cfg.Update(func(fresh *cfg.Config) {
					fresh.AddAuthorizedKey(pubKey)
				})
			},
			OnPairingCode: func(code string, expires time.Time, requester string) {
				// A client asked to pair: show the code here so the operator can
				// read it off and enter it on the client.
				mins := int(time.Until(expires).Round(time.Minute).Minutes())
				fmt.Printf("\nPairing requested by %s\n", requester)
				fmt.Printf("  Enter this code on the client to authorize it:  %s   (valid ~%d min)\n\n", code, mins)
			},
		})
		if err != nil {
			return err
		}

		// Startup banner — this is the information the user needs to connect.
		fmt.Println("viaduct server starting")
		fmt.Printf("  Listening:   %s\n", addr)
		fmt.Printf("  Server key:  %s\n", id.PublicKeyString())
		if created {
			fmt.Println("               (newly generated — clients learn it automatically when pairing)")
		}
		fmt.Printf("  Authorized:  %d client key(s)\n", len(config.Server.AuthorizedKeys))
		fmt.Printf("  Tokens:      %s\n", tokenStatus(len(config.Server.Accounts)))
		fmt.Println()
		fmt.Printf("To pair a client, run on it:  viaduct remote pair --name vps --server <this-host>:%d\n", portOf(config.Server))
		fmt.Println("A code will appear here when it connects; enter that code on the client.")
		fmt.Println()

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()

		// Run the monitor scheduler alongside the HTTP server.
		go srv.MonitorScheduler(ctx)

		httpSrv := &http.Server{Addr: addr, Handler: srv.Handler()}
		go func() {
			<-ctx.Done()
			shutCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
			defer c()
			_ = httpSrv.Shutdown(shutCtx)
		}()

		fmt.Println("Ready. Waiting for clients. Press Ctrl+C to stop.")
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return err
		}
		fmt.Println("\nServer stopped.")
		return nil
	},
}

var serveKeysCmd = &cobra.Command{
	Use:   "keys",
	Short: "List the client keys paired with this server",
	Run: func(cmd *cobra.Command, args []string) {
		config := loadOrCreateConfig()
		if len(config.Server.AuthorizedKeys) == 0 {
			fmt.Println("No clients paired yet. Run `viaduct serve` and pair one with its code.")
			return
		}
		for _, k := range config.Server.AuthorizedKeys {
			fmt.Println(k)
		}
	},
}

func tokenStatus(accounts int) string {
	switch accounts {
	case 0:
		return "none yet (clients push their own)"
	case 1:
		return "1 account"
	default:
		return fmt.Sprintf("%d accounts", accounts)
	}
}

func portOf(s cfg.ServerConfig) int {
	if s.Port == 0 {
		return cfg.DefaultPort
	}
	return s.Port
}

func init() {
	serveCmd.Flags().IntVar(&servePort, "port", 0, fmt.Sprintf("Port to listen on (default %d)", cfg.DefaultPort))
	serveCmd.Flags().StringVar(&serveListen, "listen", "", "Bind address (default all interfaces)")
	serveCmd.AddCommand(serveKeysCmd)
	rootCmd.AddCommand(serveCmd)
}
