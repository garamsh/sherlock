// Command gagent is the CLI entry point for one agent pod (ADR 0017):
// "gagent agent" runs the agent's self — the HTTP gateway, the rolling
// session, memory, the mouth, and the tool client that spawns the
// operator's installed tools — and "gagent workspace" serves the
// agent's workspace, the gRPC data plane (files and exec). Settings
// come from flags, GAGENT_* environment variables, and gagent.yaml
// through viper.
package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/garamsh/gagent/internal/workspace/shell"
)

func main() {
	// The workspace's executor execs this binary as its resource-limit
	// shim for every untrusted child (ADR 0008). The verb short-circuits
	// the command tree: the shim caps itself and execs the child's
	// command, so it never returns to run a subcommand.
	shell.ShimIfRequested()
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
	root.AddCommand(newAgentCommand(), newWorkspaceCommand())
	return root
}
