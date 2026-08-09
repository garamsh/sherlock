// Command gagent is the CLI entry point for one agent pod (ADR 0018):
// "gagent agent" runs the agent's self — the HTTP gateway, the rolling
// session, memory, the mouth, and the tools the agent imports at
// compile time — and "gagent workspace" serves the agent's workspace,
// the filesystem-only gRPC data plane. Settings come from flags,
// GAGENT_* environment variables, and gagent.yaml through viper.
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
		Short: "Run one agent pod: the agent process or its workspace data plane",
	}
	addConfigFlags(root.PersistentFlags())
	root.PersistentFlags().String("ego-file", "",
		"ego file for the agent's instructions (default: embedded EGO.md)")
	root.AddCommand(newAgentCommand(), newWorkspaceCommand())
	return root
}
