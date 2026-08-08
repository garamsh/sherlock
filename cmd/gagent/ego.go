package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// resolveEgo reads the ego file named by --ego-file. The empty
// flag returns an empty ego, which selects the agent package's embedded
// default; a named file that is missing or unreadable fails at startup,
// before any discovery or model work begins.
func resolveEgo(cmd *cobra.Command) (string, error) {
	path, err := cmd.Flags().GetString("ego-file")
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("ego file: %w", err)
	}
	return string(data), nil
}
