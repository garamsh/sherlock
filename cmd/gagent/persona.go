package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// resolvePersona reads the persona file named by --agent-prompt. The empty
// flag returns an empty persona, which selects the agent package's embedded
// default; a named file that is missing or unreadable fails at startup,
// before any environment or model work begins.
func resolvePersona(cmd *cobra.Command) (string, error) {
	path, err := cmd.Flags().GetString("agent-prompt")
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("agent prompt file: %w", err)
	}
	return string(data), nil
}
