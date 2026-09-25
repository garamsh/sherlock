package tests

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/tool"
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

// optionalFieldToolFixture installs a fixture whose manifest declares
// one optional field on each side: `optional_field` is absent from the
// input schema's `required`, `detail` is absent from the output
// schema's. The script ignores its input and answers with the required
// output field alone. The fixture is what makes the two gates
// measurable against the manifest rather than against a projection of
// it.
func optionalFieldToolFixture(t *testing.T, root string) (manifestHash string) {
	t.Helper()
	toolDir := filepath.Join(root, "demo")
	require.NoError(t, os.MkdirAll(toolDir, 0o755))
	manifest := []byte(`{
		"name": "demo",
		"description": "one optional field on each side",
		"command": "demo.sh",
		"input_schema": {
			"type": "object",
			"properties": {
				"required_field": {"type": "string"},
				"optional_field": {"type": "string"}
			},
			"required": ["required_field"],
			"additionalProperties": false
		},
		"output_schema": {
			"type": "object",
			"properties": {
				"status": {"type": "string"},
				"detail": {"type": "string"}
			},
			"required": ["status"],
			"additionalProperties": false
		}
	}`)
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, "manifest.json"), manifest, 0o644))
	script := []byte("#!/bin/sh\ncat >/dev/null\nprintf %s '{\"status\":\"ok\"}'\n")
	require.NoError(t, os.WriteFile(filepath.Join(toolDir, "demo.sh"), script, 0o755))
	hash, err := tool.ManifestHashOfFile(filepath.Join(toolDir, "manifest.json"))
	require.NoError(t, err)
	return hash
}

// optionalFieldRegistry builds the registry over the optional-field
// fixture with the fixture pinned.
func optionalFieldRegistry(t *testing.T) *tool.Registry {
	t.Helper()
	root := t.TempDir()
	hash := optionalFieldToolFixture(t, root)
	reg, err := tool.NewRegistry(tool.RegistryOptions{
		Dir:      root,
		Pins:     tool.ManifestPins{"demo": hash},
		Required: []string{"demo"},
	})
	require.NoError(t, err)
	return reg
}

// TestScriptTool_ManifestGateAcceptsOmittedOptionalInput holds ADR 0009
// invariant 3 to the manifest: `optional_field` is optional there, so a
// call that omits it satisfies the contract and must reach the tool.
func TestScriptTool_ManifestGateAcceptsOmittedOptionalInput(t *testing.T) {
	reg := optionalFieldRegistry(t)

	out, err := reg.Call(context.Background(), "demo", json.RawMessage(`{"required_field":"x"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"status":"ok"}`, string(out))
}

// TestScriptTool_ManifestGateReadsNullAsAbsent keeps issue #319's
// outcome on the manifest gate: the model-facing projection promotes an
// optional property to required + nullable, so the null the model then
// emits for it reads as absence instead of as a type violation.
func TestScriptTool_ManifestGateReadsNullAsAbsent(t *testing.T) {
	reg := optionalFieldRegistry(t)

	out, err := reg.Call(context.Background(), "demo",
		json.RawMessage(`{"required_field":"x","optional_field":null}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"status":"ok"}`, string(out))
}

// TestScriptTool_ManifestGateRejectsNullOnRequiredInput bounds the rule
// above: absence is only what the manifest already permits. A null on a
// property the manifest requires is the type violation it looks like.
func TestScriptTool_ManifestGateRejectsNullOnRequiredInput(t *testing.T) {
	reg := optionalFieldRegistry(t)

	_, err := reg.Call(context.Background(), "demo", json.RawMessage(`{"required_field":null}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrValidation)
}

// TestScriptTool_ManifestGateAcceptsOmittedOptionalOutput holds ADR
// 0009 invariant 4 to the manifest: `detail` is optional there, so an
// output carrying only `status` conforms and the call succeeds.
func TestScriptTool_ManifestGateAcceptsOmittedOptionalOutput(t *testing.T) {
	reg := optionalFieldRegistry(t)

	out, err := reg.Call(context.Background(), "demo", json.RawMessage(`{"required_field":"x"}`))
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(out, &body))
	assert.NotContains(t, body, "detail")
}

// TestScriptTool_DescriptorCarriesManifestSchemas states which schemas
// leave the registry: the manifest's own, on both sides. A provider
// that needs a different shape projects one at its own boundary.
func TestScriptTool_DescriptorCarriesManifestSchemas(t *testing.T) {
	reg := optionalFieldRegistry(t)

	// List is the whole descriptor surface agent.ToolClient declares, so
	// the assertion reads the tool through the path the session does.
	var desc tool.Descriptor
	for _, d := range reg.List() {
		if d.Name == "demo" {
			desc = d
		}
	}
	require.Equal(t, "demo", desc.Name)
	assert.JSONEq(t, `{
		"type":"object",
		"properties":{"required_field":{"type":"string"},"optional_field":{"type":"string"}},
		"required":["required_field"],
		"additionalProperties":false
	}`, string(desc.InputSchema))
	assert.JSONEq(t, `{
		"type":"object",
		"properties":{"status":{"type":"string"},"detail":{"type":"string"}},
		"required":["status"],
		"additionalProperties":false
	}`, string(desc.OutputSchema))
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

	_, err = reg.Call(context.Background(), "echo", json.RawMessage(`{"wrong":42}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrValidation)
}

// TestScriptTool_NonConformingOutputFailsTheCallAndTheToolStays covers
// the output boundary and states its whole extent (#564): a script
// whose output does not match its manifest's output_schema fails the
// call, the error names the tool, and the output never reaches the
// model — but the tool is not taken out of service. It stays listed
// and the next call runs.
//
// The platform documented a quarantine for years and never implemented
// one. Building it would take the agent's only way to speak on one
// malformed response, since message_send is required and the required
// set is not operator-configurable, and the state would not survive a
// restart — a control that cannot be recovered from and expires on its
// own is not a boundary. What is a boundary is the output being
// dropped, and that is what this test holds.
func TestScriptTool_NonConformingOutputFailsTheCallAndTheToolStays(t *testing.T) {
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

	out, err := reg.Call(context.Background(), "broken", json.RawMessage(`{}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrExecution)
	assert.Contains(t, err.Error(), "output failed schema validation")
	assert.Contains(t, err.Error(), "broken", "the error names the tool")
	assert.Nil(t, out, "the non-conforming output does not reach the caller")
	assert.NotContains(t, err.Error(), "quarantin",
		"the error must not name a mechanism the platform does not have")

	// The tool is still installed and still callable: no quarantine.
	names := make([]string, 0, 1)
	for _, d := range reg.List() {
		names = append(names, d.Name)
	}
	assert.Contains(t, names, "broken", "a tool that emitted a bad output stays advertised")

	_, err = reg.Call(context.Background(), "broken", json.RawMessage(`{}`))
	require.Error(t, err, "the next call runs and fails the same way")
	assert.ErrorIs(t, err, tool.ErrExecution)
}

// TestScriptTool_ManifestFixtureSchemaConforms confirms the integration
// fixture is a valid manifest: the loader the runtime uses accepts it.
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

	// The registry is the path the runtime loads every manifest through,
	// so the fixture is checked under the real one. The tree holds the
	// fixture and nothing else, so the required set is explicitly empty
	// rather than the default that expects the mouth.
	reg, err := tool.NewRegistry(tool.RegistryOptions{Dir: root, Required: []string{}})
	require.NoError(t, err)

	names := make([]string, 0, 1)
	for _, d := range reg.List() {
		names = append(names, d.Name)
	}
	assert.Equal(t, []string{"echo"}, names)
}
