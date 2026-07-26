package mcp

import (
	"errors"
	"os"
	"time"
)

// DefaultCallTimeout bounds one tools/call when Config.Timeout is unset.
const DefaultCallTimeout = 30 * time.Second

var (
	// ErrInvalidConfig marks a server config that cannot be started
	// (missing name or command).
	ErrInvalidConfig = errors.New("invalid mcp server config")
	// ErrServerUnavailable marks calls against a server whose process has
	// exited. No automatic restart is attempted.
	ErrServerUnavailable = errors.New("mcp server unavailable")
)

// Config describes one stdio MCP server subprocess.
type Config struct {
	// Name labels the server in logs and errors.
	Name string
	// Command is the executable to run.
	Command string
	// Args are the command arguments.
	Args []string
	// Env allowlists platform environment variable names forwarded to the
	// server process; everything else is scrubbed.
	Env []string
	// Timeout bounds one tools/call; <= 0 uses DefaultCallTimeout.
	Timeout time.Duration
}

// scrubbedEnv builds the server process environment: exactly the allowlisted
// platform variables that are set, nothing else.
func scrubbedEnv(allowlist []string) []string {
	env := make([]string, 0, len(allowlist))
	for _, name := range allowlist {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}
