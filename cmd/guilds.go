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

var guildsJSON bool

var guildsCmd = &cobra.Command{
	Use:   "guilds",
	Short: "List your Discord servers",
	Long:  "Fetches and displays all Discord servers you're a member of.",
	RunE: func(cmd *cobra.Command, args []string) error {
		config := loadOrCreateConfig()
		applyFlagOverrides(config)

		if config.Token == "" {
			return fmt.Errorf("no token configured — run `viaduct` to set up, or use --token")
		}

		client := discord.NewClient(config.Token, config.BotMode, http.DefaultClient)

		guilds, err := client.GetGuilds()
		if err != nil {
			return fmt.Errorf("failed to fetch guilds: %w", err)
		}

		_ = cfg.SaveGuildCache(guilds)

		if guildsJSON {
			data, _ := json.MarshalIndent(guilds, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "NAME\tID\tOWNER\n")
		fmt.Fprintf(w, "----\t--\t-----\n")
		for _, g := range guilds {
			owner := ""
			if g.Owner {
				owner = "yes"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", g.Name, g.Id, owner)
		}
		w.Flush()

		return nil
	},
}

func init() {
	guildsCmd.Flags().BoolVar(&guildsJSON, "json", false, "Output as JSON")
	rootCmd.AddCommand(guildsCmd)
}
