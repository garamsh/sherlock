// Command sherlock is the CLI entry point for one agent pod (ADR 0017):
// "sherlock agent" runs the agent's self — the HTTP gateway, the rolling
// session, memory, the mouth, and the tool client that spawns the
// operator's installed tools — and "sherlock workspace" serves the
// agent's workspace, the gRPC data plane (files and exec). Settings
// come from flags, SHERLOCK_* environment variables, and configs/agent.yaml
// through viper.
package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/garamsh/sherlock/internal/config"
	"github.com/garamsh/sherlock/internal/workspace/shell"
)

func main() {
	// The workspace's executor execs this binary as its resource-limit
	// shim for every untrusted child (ADR 0008). The verb short-circuits
	// the command tree: the shim caps itself and execs the child's
	// command, so it never returns to run a subcommand.
	shell.ShimIfRequested()
	if err := NewRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}

// NewRootCommand builds the sherlock command tree with the shared config
// flags available to every subcommand. It is the package's one export:
// main runs it, and the package's tests drive the binary through it.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "sherlock",
		Short: "Run one agent pod: the agent process or its workspace data plane",
	}
	config.AddSharedFlags(root.PersistentFlags())
	root.AddCommand(newAgentCommand(), newWorkspaceCommand())
	return root
}
