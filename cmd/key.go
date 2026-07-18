package cmd

import (
	"fmt"
	"github.com/inherentescapade/viaduct/auth"
	"github.com/inherentescapade/viaduct/cfg"

	"github.com/spf13/cobra"
)

var keyForce bool

var keyCmd = &cobra.Command{
	Use:   "key",
	Short: "Manage your client identity key (for connecting to a viaduct server)",
	Long: `Your client identity is a small X25519 keypair. The private half stays on this
machine and is used to encrypt and authenticate every request you send. You
normally don't need to touch this: ` + "`viaduct remote pair`" + ` creates the identity for
you and authorizes it with the server via a one-time code.`,
}

var keyGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate your client identity keypair",
	RunE: func(cmd *cobra.Command, args []string) error {
		path := cfg.IdentityPath()
		id, err := auth.GenerateIdentity()
		if err != nil {
			return err
		}
		if err := auth.SaveIdentity(id, path, keyForce); err != nil {
			return err
		}
		fmt.Printf("Generated your client identity.\n\n")
		fmt.Printf("  Private key: %s (keep this secret)\n", path)
		fmt.Printf("  Public key:  %s\n\n", id.PublicKeyString())
		fmt.Println("To authorize this client with a server, pair it (no key copying):")
		fmt.Println("  viaduct remote pair --name vps --server HOST:21776")
		return nil
	},
}

var keyShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print your client public key",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := auth.LoadIdentity(cfg.IdentityPath())
		if err != nil {
			return fmt.Errorf("no client identity yet — run `viaduct key generate` first")
		}
		fmt.Println(id.PublicKeyString())
		return nil
	},
}

var keyPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the path to your client identity key file",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(cfg.IdentityPath())
	},
}

func init() {
	keyGenerateCmd.Flags().BoolVar(&keyForce, "force", false, "Overwrite an existing identity key")
	keyCmd.AddCommand(keyGenerateCmd)
	keyCmd.AddCommand(keyShowCmd)
	keyCmd.AddCommand(keyPathCmd)
	rootCmd.AddCommand(keyCmd)
}
