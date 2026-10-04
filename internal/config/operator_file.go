package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/pflag"
)

// readOperatorFile reads the operator file the named flag points at. The
// empty flag returns empty text; a named file that is missing or
// unreadable fails, its error labelled by the flag ("ego-file" → "ego file").
func readOperatorFile(fs *pflag.FlagSet, flag string) (string, error) {
	path, err := fs.GetString(flag)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", strings.ReplaceAll(flag, "-", " "), err)
	}
	return string(data), nil
}
