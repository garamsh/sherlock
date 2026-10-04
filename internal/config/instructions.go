package config

import (
	"fmt"
	"os"

	"github.com/spf13/pflag"
)

// ResolveOperatorInstructions reads the operator-instructions file named by
// --instructions-file. The empty flag returns empty instructions, which the
// agent package omits from the session instructions (ADR 0024); a named
// file that is missing or unreadable fails at startup, as --ego-file does.
func ResolveOperatorInstructions(fs *pflag.FlagSet) (string, error) {
	path, err := fs.GetString("instructions-file")
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("instructions file: %w", err)
	}
	return string(data), nil
}
