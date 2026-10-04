package config

import "github.com/spf13/pflag"

// ResolveEgo reads the ego file named by --ego-file. The empty
// flag returns an empty ego, which selects the agent package's embedded
// default; a named file that is missing or unreadable fails at startup,
// before any discovery or model work begins.
func ResolveEgo(fs *pflag.FlagSet) (string, error) {
	return readOperatorFile(fs, "ego-file")
}
