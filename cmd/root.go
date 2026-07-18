package cmd

import (
	"fmt"
	"github.com/inherentescapade/viaduct/cfg"
	"github.com/inherentescapade/viaduct/tui"
	"os"

	"github.com/spf13/cobra"
)

var (
	flagToken string
	flagBot   bool
)

// version is the build version reported by `viaduct --version`. It defaults to
// "dev" for local/source builds and is stamped with the release tag at build
// time via:
//
//	go build -ldflags "-X viaduct/cmd.version=v1.2.3"
var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "viaduct",
	Short: "Viaduct - Discord message deletion tool",
	Long: `Viaduct deletes your messages from Discord servers and DMs.

Run with no arguments for interactive mode, or use subcommands for CLI usage.`,
	Version: version,
	RunE: func(cmd *cobra.Command, args []string) error {
		config := loadOrCreateConfig()
		applyFlagOverrides(config)
		return tui.Run(config)
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagToken, "token", "", "Discord token (overrides config)")
	rootCmd.PersistentFlags().BoolVar(&flagBot, "bot", false, "Use bot token mode")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func loadOrCreateConfig() *cfg.Config {
	config := cfg.DefaultConfig()
	if err := config.Load(); err != nil {
		// Config doesn't exist yet, that's fine — TUI will handle setup
	}
	return config
}

func applyFlagOverrides(config *cfg.Config) {
	if flagToken != "" {
		config.Token = flagToken
	}
	if flagBot {
		config.BotMode = true
	}
}
