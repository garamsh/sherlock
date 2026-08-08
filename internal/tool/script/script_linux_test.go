//go:build linux

package script_test

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/shell"
	"github.com/garamsh/gagent/internal/tool/script"
)

// TestToolScript_Call_RunsAsAgentUID exercises the real executor: the
// script tool's child runs under the agent UID, not the platform UID
// (ADR 0010). Requires root for the privilege drop.
func TestToolScript_Call_RunsAsAgentUID(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("the privilege drop requires root; run with `sudo -E go test ./internal/tool/script/...`")
	}
	ts := script.New(script.Spec{
		Name: "whoami",
		Exec: script.ExecSpec{Command: []string{"id", "-u"}},
	}, shell.NewProcessExecutor(shell.DefaultAgentUID, 5*time.Second))

	out, err := ts.Call(context.Background(), json.RawMessage(`{}`))

	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(shell.DefaultAgentUID), strings.TrimSpace(string(out)))
}
