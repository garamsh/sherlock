// Command gagent is the platform's single CLI entry point: "gagent gateway"
// serves the HTTP gateway and "gagent agent" runs one agent turn directly
// (docs/architecture/gateway.md). Settings come from flags, GAGENT_*
// environment variables, and gagent.yaml through viper.
package main

import (
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}

// newRootCommand builds the gagent command tree with the shared config
// flags available to every subcommand.
func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "gagent",
		Short: "Run the gagent platform: HTTP gateway or one-shot agent turns",
	}
	addConfigFlags(root.PersistentFlags())
	root.PersistentFlags().String("ego-file", "",
		"ego file for the agent's instructions (default: embedded EGO.md)")
	root.AddCommand(newGatewayCommand(), newAgentCommand())
	return root
}
