//go:build !linux

package shell

import (
	"context"
	"time"
)

// ProcessExecutor is the platform-specific executor; on non-Linux hosts
// it cannot drop privileges or apply Linux rlimits, so Run always returns
// ErrAgentUIDUnavailable. This keeps the package compilable on macOS and
// Windows for tooling; production deployment is Linux-only.
type ProcessExecutor struct {
	AgentUID       int
	DefaultTimeout time.Duration
}

// NewProcessExecutor returns a ProcessExecutor that will report
// ErrAgentUIDUnavailable on Run.
func NewProcessExecutor(agentUID int, defaultTimeout time.Duration) *ProcessExecutor {
	return &ProcessExecutor{AgentUID: agentUID, DefaultTimeout: defaultTimeout}
}

// Run always returns ErrAgentUIDUnavailable on non-Linux hosts.
func (e *ProcessExecutor) Run(_ context.Context, _ Request) (Result, error) {
	return Result{}, ErrAgentUIDUnavailable
}
