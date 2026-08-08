// Command gagent is the platform's single CLI entry point: "gagent gateway"
// serves the HTTP gateway, "gagent agent" runs one agent turn directly
// (docs/architecture/gateway.md), and "gagent sidecar" serves the agent's
// computer — workspace, shell_exec, and script tools — over the tool
// contract (ADR 0018). Settings come from flags, GAGENT_* environment
// variables, and gagent.yaml through viper.
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
		Short: "Run the gagent platform: HTTP gateway, one-shot agent turns, or the sidecar tool server",
	}
	addConfigFlags(root.PersistentFlags())
	root.PersistentFlags().String("ego-file", "",
		"ego file for the agent's instructions (default: embedded EGO.md)")
	root.AddCommand(newGatewayCommand(), newAgentCommand(), newSidecarCommand())
	return root
}
