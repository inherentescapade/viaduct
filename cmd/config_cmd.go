package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/inherentescapade/viaduct/cfg"

	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show or edit configuration",
	Long:  "Display the current configuration. Use subcommands to modify it.",
	RunE: func(cmd *cobra.Command, args []string) error {
		config := loadOrCreateConfig()
		data, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	},
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the config file path",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(cfg.ConfigDir())
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a config value",
	Long: `Set a configuration value.

Keys:
  token       Your Discord token
  bot_mode    true/false - use bot token mode`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		config := loadOrCreateConfig()

		switch args[0] {
		case "token":
			config.Token = args[1]
		case "bot_mode":
			config.BotMode = args[1] == "true"
		default:
			return fmt.Errorf("unknown config key %q — valid keys: token, bot_mode", args[0])
		}

		if err := config.Save(); err != nil {
			return err
		}
		fmt.Printf("Set %s\n", args[0])
		return nil
	},
}

func init() {
	configCmd.AddCommand(configPathCmd)
	configCmd.AddCommand(configSetCmd)
	rootCmd.AddCommand(configCmd)
}
