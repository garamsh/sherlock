package tests

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/tool"
)

// scriptToolFixture installs a tiny script-tool fixture under root
// and returns the manifest hash for the drift-pin test. The fixture
// is the smallest tool that exercises the contract: a one-line
// input, a one-line output, no env.
func scriptToolFixture(t *testing.T, root, name string) (manifestHash string) {
	t.Helper()
	toolDir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(toolDir, 0o755))
	manifest := []byte(`{
		"name": "` + name + `",
		"description": "echo the input back",
		"command": "echo.sh",
		"input_schema": {
			"type": "object",
			"properties": {"text": {"type": "string"}},
			"required": ["text"]
		},
		"output_schema": {
			"type": "object",
			"properties": {"echo": {"type": "string"}},
			"required": ["echo"]
		}
	}`)
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, "manifest.json"), manifest, 0o644))
	// The script reads the input JSON line from stdin and writes
	// the value of the "text" field as the value of an "echo" key.
	// The result is a JSON object that conforms to the manifest's
	// output schema: {"echo": "<text>"} with echo as a string.
	script := []byte("#!/bin/sh\n" +
		"read -r line\n" +
		"text=$(echo \"$line\" | sed 's/.*\"text\":\"\\([^\"]*\\)\".*/\\1/')\n" +
		"echo \"{\\\"echo\\\":\\\"$text\\\"}\"\n")
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, "echo.sh"), script, 0o755))
	hash, err := tool.ManifestHashOfFile(filepath.Join(toolDir, "manifest.json"))
	require.NoError(t, err)
	return hash
}

// TestScriptTool_RoundTripIntegration installs a manifest+script
// fixture under a tmpdir, registers the fixture with the new
// registry, and asserts a full roundtrip call: the input validates
// against the manifest's input schema, the script runs, the output
// validates against the output schema, and the registry returns the
// validated object to the caller.
func TestScriptTool_RoundTripIntegration(t *testing.T) {
	root := t.TempDir()
	hash := scriptToolFixture(t, root, "echo")

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir: root,
		// Pin the hash so the test catches drift at the boundary.
		Pins:     tool.ManifestPins{"echo": hash},
		Required: []string{"echo"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { reg.Close() })

	list := reg.List()
	require.Len(t, list, 1)
	desc := list[0]
	assert.Equal(t, "echo", desc.Name)
	assert.Equal(t, "echo the input back", desc.Description)
	assert.NotEmpty(t, desc.Hash, "the registry exposes the manifest hash for the model")
	assert.JSONEq(t,
		`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`,
		string(desc.InputSchema))

	out, err := reg.Call(context.Background(), "echo", json.RawMessage(`{"text":"hi"}`))
	require.NoError(t, err)
	assert.Contains(t, string(out), `"echo"`)
	assert.Contains(t, string(out), "hi")
}

// TestScriptTool_RequiredMissingFailsClosed mirrors the platform's
// required-tool invariant: a missing required tool fails startup.
// The registry never falls back to a partial surface.
func TestScriptTool_RequiredMissingFailsClosed(t *testing.T) {
	root := t.TempDir()
	_, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Required: []string{"message_send"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required tools missing")
	assert.Contains(t, err.Error(), "message_send")
}

// TestScriptTool_DriftPinRefusesLoad pins the manifest drift check
// (ADR 0009 invariant 2): a manifest whose hash does not match the
// pin is rejected at startup. The operator regenerates the pin on a
// deliberate change.
func TestScriptTool_DriftPinRefusesLoad(t *testing.T) {
	root := t.TempDir()
	_ = scriptToolFixture(t, root, "echo")

	_, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"echo": "deadbeef"},
		Required: []string{"echo"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "descriptor drift")
}

// TestScriptTool_UnpinnedToolRefusesLoad pins the other half of ADR
// 0009 invariant 2: the pin set is the operator's accepted tool set, so
// a tool present in the tree but absent from the set is refused. An
// unreviewed directory drop is otherwise indistinguishable from an
// installation.
func TestScriptTool_UnpinnedToolRefusesLoad(t *testing.T) {
	root := t.TempDir()
	hash := scriptToolFixture(t, root, "echo")
	_ = scriptToolFixture(t, root, "unreviewed")

	_, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"echo": hash},
		Required: []string{"echo"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unpinned tool "unreviewed"`)
}

// TestScriptTool_InputRejection covers the boundary: an input that
// does not match the manifest's input schema is rejected before the
// tool runs. The runner is never invoked.
func TestScriptTool_InputRejection(t *testing.T) {
	root := t.TempDir()
	hash := scriptToolFixture(t, root, "echo")

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"echo": hash},
		Required: []string{"echo"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { reg.Close() })

	_, err = reg.Call(context.Background(), "echo", json.RawMessage(`{"wrong":42}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrValidation)
}

// TestScriptTool_OutputQuarantine covers the output boundary: a
// script whose output does not match the manifest's output schema
// is quarantined. The platform never returns a non-conforming output
// to the model.
func TestScriptTool_OutputQuarantine(t *testing.T) {
	root := t.TempDir()
	toolDir := filepath.Join(root, "broken")
	require.NoError(t, os.MkdirAll(toolDir, 0o755))
	manifest := []byte(`{
		"name": "broken",
		"description": "returns broken output",
		"command": "broken.sh",
		"input_schema": {"type": "object"},
		"output_schema": {
			"type": "object",
			"properties": {"n": {"type": "integer"}},
			"required": ["n"]
		}
	}`)
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, "manifest.json"), manifest, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, "broken.sh"),
		[]byte("#!/bin/sh\ncat >/dev/null\necho '{\"text\":\"not an integer\"}'\n"), 0o755))
	hash, err := tool.ManifestHashOfFile(filepath.Join(toolDir, "manifest.json"))
	require.NoError(t, err)

	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"broken": hash},
		Required: []string{"broken"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { reg.Close() })

	_, err = reg.Call(context.Background(), "broken", json.RawMessage(`{}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrExecution)
	assert.Contains(t, err.Error(), "output failed schema validation")
}

// TestScriptTool_ManifestFixtureSchemaConforms confirms the
// integration fixture is a valid manifest: the tooltest conformance
// guard accepts it. The guard is shared between the legacy
// in-process tools and the new manifest-based tools, so a tool
// author's manifest must pass the same shape check.
func TestScriptTool_ManifestFixtureSchemaConforms(t *testing.T) {
	root := t.TempDir()
	toolDir := filepath.Join(root, "echo")
	require.NoError(t, os.MkdirAll(toolDir, 0o755))
	manifest := []byte(`{
		"name": "echo",
		"description": "echo the input",
		"command": "echo.sh",
		"input_schema": {
			"type": "object",
			"properties": {"text": {"type": "string"}},
			"required": ["text"]
		},
		"output_schema": {
			"type": "object",
			"properties": {"echo": {"type": "string"}},
			"required": ["echo"]
		}
	}`)
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, "manifest.json"), manifest, 0o644))

	// The conformance guard is exposed by the tooltest package. The
	// guard is the same path the manifest loader uses to validate
	// each schema, so a pass here proves the fixture compiles under
	// the same library the runtime will use.
	m, _, err := tool.LoadManifest(filepath.Join(toolDir, "manifest.json"))
	require.NoError(t, err)
	assert.Equal(t, "echo", m.Name)
}
