package tool_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/tool"
)

// tinyScript writes a small sh script to a temp directory and returns
// the absolute path. The script's body receives the validated input
// on stdin and writes one JSON line to stdout. The helper makes the
// runner test hermetic: no test-time binary is built, the test
// filesystem is the only state.
func tinyScript(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tool.sh")
	script := "#!/bin/sh\n" + body
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return path
}

// TestRunner_RoundTrip is the happy path: the script reads the input
// from stdin, writes one JSON line to stdout, and the runner returns
// the validated output.
func TestRunner_RoundTrip(t *testing.T) {
	script := tinyScript(t, `cat >/dev/null
echo '{"echo":"hello"}'`)
	manifest := tool.Manifest{
		Name:         "echo",
		Description:  "echo",
		Command:      script,
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	}
	runner := tool.NewRunner(manifest, t.TempDir())

	out, stderr, err := runner.Run(context.Background(), json.RawMessage(`{"a":1}`))
	require.NoError(t, err)
	assert.Empty(t, stderr)
	assert.JSONEq(t, `{"echo":"hello"}`, string(out))
}

// TestRunner_ReadsStdinAsJsonLine pins the contract: the platform
// writes one JSON line to the tool's stdin. The script that reads
// the first line with `read` (or `head -1`) sees the encoded object.
func TestRunner_ReadsStdinAsJsonLine(t *testing.T) {
	script := tinyScript(t, "read -r line\necho \"{\\\"in\\\":$line}\"")
	manifest := tool.Manifest{
		Name:         "echo",
		Description:  "echo",
		Command:      script,
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	}
	runner := tool.NewRunner(manifest, t.TempDir())

	out, _, err := runner.Run(context.Background(), json.RawMessage(`{"x":42}`))
	require.NoError(t, err)
	// The script wrote the raw input as a JSON value: {"in":{"x":42}}
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(out, &decoded))
	assert.EqualValues(t, 42, decoded["in"].(map[string]any)["x"])
}

// TestRunner_ExitNonZeroSurfacesStderr covers the failure path: a
// non-zero exit returns the exit error and the captured stderr. The
// caller (registry) wraps the error and surfaces stderr in the event
// payload.
func TestRunner_ExitNonZeroSurfacesStderr(t *testing.T) {
	script := tinyScript(t, `echo "boom" 1>&2
exit 7`)
	manifest := tool.Manifest{
		Name:         "boom",
		Description:  "boom",
		Command:      script,
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	}
	runner := tool.NewRunner(manifest, t.TempDir())

	out, stderr, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Nil(t, out)
	assert.Contains(t, stderr, "boom")
	assert.Contains(t, err.Error(), "exit status 7")
}

// TestRunner_InvalidJsonOutput covers the parse failure: a script that
// writes a non-JSON line is rejected at the contract boundary.
func TestRunner_InvalidJsonOutput(t *testing.T) {
	script := tinyScript(t, `echo "not json"`)
	manifest := tool.Manifest{
		Name:         "bad",
		Description:  "bad",
		Command:      script,
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	}
	runner := tool.NewRunner(manifest, t.TempDir())

	_, _, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode output")
}

// TestRunner_EmptyOutput covers the case where the tool writes
// nothing to stdout: the runner returns an empty-output error, so the
// call fails rather than an empty result reaching the caller.
func TestRunner_EmptyOutput(t *testing.T) {
	script := tinyScript(t, `cat >/dev/null
exit 0`)
	manifest := tool.Manifest{
		Name:         "silent",
		Description:  "silent",
		Command:      script,
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	}
	runner := tool.NewRunner(manifest, t.TempDir())

	_, _, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	require.Error(t, err)
}

// TestRunner_EnvIsForwarded pins the per-tool env scoping: the
// manifest's env entries are forwarded to the spawned process. The
// script echoes the env var to stdout.
func TestRunner_EnvIsForwarded(t *testing.T) {
	script := tinyScript(t, `echo "{\"got\":\"$MY_SCOPE_VAR\"}"`)
	manifest := tool.Manifest{
		Name:         "env",
		Description:  "env",
		Command:      script,
		Env:          []string{"MY_SCOPE_VAR=from-manifest"},
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	}
	runner := tool.NewRunner(manifest, t.TempDir())

	out, _, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Contains(t, string(out), "from-manifest")
}

// TestRunner_CommandRelativeToManifestDir confirms the resolver: a
// command path in the manifest is relative to the manifest's
// directory, not the working directory. The integration test in
// tests/ exercises the same path with a real install.
func TestRunner_CommandRelativeToManifestDir(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tool.sh")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho '{\"ok\":1}'"), 0o755))
	manifest := tool.Manifest{
		Name:         "echo",
		Description:  "echo",
		Command:      "tool.sh",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	}
	runner := tool.NewRunner(manifest, dir)

	out, _, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":1}`, string(out))
}

// TestRunner_EmptyCommandFailsClosed asserts the runner's local guard:
// the runner rejects a manifest without a command path. The
// manifest-level validator already catches this, but the runner
// defends in depth.
func TestRunner_EmptyCommandFailsClosed(t *testing.T) {
	manifest := tool.Manifest{
		Name:         "x",
		Description:  "x",
		Command:      "",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	}
	runner := tool.NewRunner(manifest, t.TempDir())
	_, _, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command is empty")
}

// TestRunner_ExitBeforeReadingStdin pins the structural fix: a tool
// that exits before reading stdin causes the runner's stdin write to
// fail with EPIPE. The runner must not propagate that failure — the
// exit status (or the output on stdout) is the real signal. The
// test loop runs the script many times to cover the race between
// the runner's write and the script's exit.
func TestRunner_ExitBeforeReadingStdin(t *testing.T) {
	script := tinyScript(t, `echo '{"ok":1}'
exit 0`)
	manifest := tool.Manifest{
		Name:         "fast",
		Description:  "fast",
		Command:      script,
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	}
	runner := tool.NewRunner(manifest, t.TempDir())
	for i := 0; i < 50; i++ {
		out, _, err := runner.Run(context.Background(), json.RawMessage(`{"x":1}`))
		require.NoError(t, err, "iteration %d: EPIPE from a fast-exiting script must not fail the runner", i)
		assert.JSONEq(t, `{"ok":1}`, string(out))
	}
}

// TestReadErrorObject_DetectsErrorField pins the failure case where
// the tool communicates the failure as a JSON object on stdout. The
// runner returns the raw response; the caller (registry) calls
// ReadErrorObject to detect the error field and surface a typed
// error.
func TestReadErrorObject_DetectsErrorField(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantMsg string
		wantOk  bool
	}{
		{"string error", `{"error":"bad"}`, "bad", true},
		{"object error", `{"error":{"code":"x"}}`, `{"code":"x"}`, true},
		{"no error", `{"ok":1}`, "", false},
		{"not an object", `[1,2,3]`, "", false},
		{"empty", ``, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg, ok := tool.ReadErrorObject(json.RawMessage(tc.input))
			assert.Equal(t, tc.wantOk, ok)
			if tc.wantOk {
				assert.Equal(t, tc.wantMsg, msg)
			}
		})
	}
}

// TestRunner_StderrCapturedOnSuccess pins the contract: stderr is
// captured even on a successful run. The platform filters empty
// stderr at the event layer; the runner is conservative.
func TestRunner_StderrCapturedOnSuccess(t *testing.T) {
	script := tinyScript(t, `echo "diagnostic" 1>&2
echo '{"ok":1}'`)
	manifest := tool.Manifest{
		Name:         "diag",
		Description:  "diag",
		Command:      script,
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	}
	runner := tool.NewRunner(manifest, t.TempDir())
	_, stderr, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.True(t, strings.Contains(stderr, "diagnostic"),
		"stderr is captured even on success; the event layer decides whether to surface it")
}

// TestRunner_InputFallsBackToEmptyObject pins the contract: a nil or
// empty input is written as `{}` so the tool always sees a valid JSON
// object on stdin. Tools that read the request as a structured object
// never have to handle the empty-input case.
func TestRunner_InputFallsBackToEmptyObject(t *testing.T) {
	script := tinyScript(t, `read -r line
echo "{\"len\":\"${#line}\"}"`)
	manifest := tool.Manifest{
		Name:         "echo",
		Description:  "echo",
		Command:      script,
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	}
	runner := tool.NewRunner(manifest, t.TempDir())
	out, _, err := runner.Run(context.Background(), nil)
	require.NoError(t, err)
	// `read` strips the trailing newline when the input is `{}`:
	// the line becomes `{}` which is two characters.
	assert.Contains(t, string(out), `"len":"2"`)
}

// TestRunner_ToolInheritsTheAgentEnvironment pins the decided posture
// (#543): security is drawn by environment, and the environments are
// the two containers. A tool is the agent's own (ADR 0017), so it runs
// inside the agent's environment and inherits it whole rather than
// across a boundary from it.
//
// The manifest below declares no env at all, which is the case that
// matters: a tool that asks for no secret still receives every
// credential the agent holds. That is why installing a tool is a trust
// decision and not a capability one
// (docs/architecture/security.md §Tool trust posture).
//
// Nothing pinned this before, which is how the document and the code
// drifted apart with neither wrong on its own terms.
func TestRunner_ToolInheritsTheAgentEnvironment(t *testing.T) {
	t.Setenv("GAGENT_TEST_AGENT_SECRET", "the-model-key")
	script := tinyScript(t, `cat >/dev/null
printf '{"seen":"%s"}\n' "$GAGENT_TEST_AGENT_SECRET"`)
	manifest := tool.Manifest{
		Name:         "inherits",
		Description:  "inherits",
		Command:      script,
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	}
	runner := tool.NewRunner(manifest, t.TempDir())

	out, stderr, err := runner.Run(context.Background(), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Empty(t, stderr)
	assert.JSONEq(t, `{"seen":"the-model-key"}`, string(out),
		"a tool reads what the agent's environment holds, declared or not")
}
