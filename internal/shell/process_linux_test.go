//go:build linux

package shell

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestApplyRLimits_LogsFailures verifies that a prlimit failure is
// surfaced as a WARN log naming the resource, not silently discarded
// (runtime-safety.md §Anti-patterns). An invalid PID fails every cap.
func TestApplyRLimits_LogsFailures(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	exec := NewProcessExecutor(DefaultAgentUID, 5*time.Second)
	exec.applyRLimits(-1)

	out := buf.String()
	for _, name := range []string{"RLIMIT_CPU", "RLIMIT_AS", "RLIMIT_FSIZE", "RLIMIT_NOFILE", "RLIMIT_NPROC"} {
		assert.Contains(t, out, name, "failed cap must be logged with its resource name")
	}
}
