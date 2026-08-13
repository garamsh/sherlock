package tool_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/tool"
)

// testtoolPath returns the on-disk path of the testtool binary. The
// test builds it once per test process; tests that need the binary
// reuse the same path.
var testtoolPath string

// TestMain builds the testtool binary so the protocol client tests can
// spawn it. The build is hermetic under the test's temp dir.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tool-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	bin := filepath.Join(dir, "testtool")
	build := exec.Command("go", "build", "-o", bin, "./cmd/testtool")
	build.Dir = moduleRoot()
	if out, err := build.CombinedOutput(); err != nil {
		panic("build testtool: " + err.Error() + ":\n" + string(out))
	}
	testtoolPath = bin
	os.Exit(m.Run())
}

func moduleRoot() string {
	// The test binary runs from the package's directory; the module
	// root has go.mod.
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("module root not found from " + dir)
		}
		dir = parent
	}
}

// writeTesttoolConfig writes a config file under t.TempDir for the
// testtool binary to read. name and description go into the descriptor;
// inputSchema and outputSchema gate the tool's call surface; behaviour
// is "echo" (return the input as the output), "fixed" (return the
// fixedOutput payload), or "error" (send a JSON-RPC error with the
// configured message and data payload).
type testtoolConfig struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema"`
	Behaviour    string          `json:"behaviour"`
	Fixed        json.RawMessage `json:"fixedOutput,omitempty"`
	ErrorMsg     string          `json:"errorMessage,omitempty"`
	ErrorData    json.RawMessage `json:"errorData,omitempty"`
}

func writeTesttoolConfig(t *testing.T, cfg testtoolConfig) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	body, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, body, 0o600))
	return path
}

// echoConfig returns the standard config for an echo tool. The input
// is checked against inputSchema; the output is the input object.
func echoConfig(name string) testtoolConfig {
	return testtoolConfig{
		Name:        name,
		Description: "echo the input back",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
		OutputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{"text":{"type":"string"}},
			"required":["text"]
		}`),
		Behaviour: "echo",
	}
}

// newClientWithTools spawns one testtool per spec and returns the
// ready-to-use client. The testtool is the only consistent spawn target
// for the protocol client surface. Tests that don't care about the
// required-tool set pass `Required: []string{}` to opt out of the
// default check.
func newClientWithTools(t *testing.T, opts tool.Options, cfgs ...testtoolConfig) *tool.Client {
	t.Helper()
	if len(cfgs) == 0 {
		t.Fatal("newClientWithTools: at least one testtool config required")
	}
	// Default opts need not specify Required; the platform's
	// default-required list does not include the test-only "echo"
	// name, so we suppress it here.
	if opts.Required == nil {
		opts.Required = []string{}
	}
	specs := make([]tool.Spec, 0, len(cfgs))
	for _, c := range cfgs {
		cfgPath := writeTesttoolConfig(t, c)
		specs = append(specs, tool.Spec{
			Name:    c.Name,
			Command: testtoolPath,
			Env:     []string{"GAGENT_TESTTOOL_CONFIG=" + cfgPath},
		})
	}
	client, err := tool.NewClient(context.Background(), opts, specs)
	require.NoError(t, err)
	t.Cleanup(func() { client.Close() })
	return client
}

// TestClient_DiscoverAndList is the protocol client's discovery path:
// one spawn, one tools/list, one parsed descriptor. The descriptor the
// platform pinned is the descriptor the server returned.
func TestClient_DiscoverAndList(t *testing.T) {
	client := newClientWithTools(t, tool.Options{}, echoConfig("echo"))
	list := client.List()
	require.Len(t, list, 1)
	d := list[0]
	assert.Equal(t, "echo", d.Name)
	assert.Equal(t, "echo the input back", d.Description)
	assert.NotEmpty(t, d.Hash,
		"the platform pins a hash of every fetched descriptor")
	assert.JSONEq(t, `{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`, string(d.InputSchema))
}

// TestClient_HashIsDeterministic pins the protocol's hash determinism:
// two discovers of the same descriptor produce the same hash, so the
// pin can detect drift across restarts.
func TestClient_HashIsDeterministic(t *testing.T) {
	c1 := newClientWithTools(t, tool.Options{}, echoConfig("echo"))
	first := c1.List()[0].Hash
	c1.Close()

	c2 := newClientWithTools(t, tool.Options{}, echoConfig("echo"))
	second := c2.List()[0].Hash
	assert.Equal(t, first, second,
		"the same descriptor hashes to the same value across restarts")
}

// TestClient_HashIgnoresKeyOrder pins the canonical-JSON rule: the
// hash is independent of the field order the server returned, so a
// server that reorders its fields does not drift the pin.
func TestClient_HashIgnoresKeyOrder(t *testing.T) {
	a := testtoolConfig{
		Name:         "echo",
		Description:  "echo",
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		Behaviour:    "echo",
	}
	b := a
	// Same descriptor, different order in the inner JSON: the
	// canonical hash must match.
	a.InputSchema = json.RawMessage(`{"properties":{"a":{"type":"string"}},"required":["a"],"type":"object"}`)
	c1 := newClientWithTools(t, tool.Options{}, a)
	c2 := newClientWithTools(t, tool.Options{}, b)
	assert.Equal(t, c1.List()[0].Hash, c2.List()[0].Hash,
		"canonical JSON keys sort before hashing; field order is not signal")
}

// TestClient_DriftPinning_RefusesLoad is the boundary case: a pin set
// to a stale hash fails the startup handshake. The agent must regenerate
// the pin on a deliberate descriptor change (ADR 0018 §Tool contract).
func TestClient_DriftPinning_RefusesLoad(t *testing.T) {
	c1 := newClientWithTools(t, tool.Options{}, echoConfig("echo"))
	stale := c1.List()[0].Hash
	c1.Close()

	// The pin is stale (the hash changed between c1 and the second
	// start... wait, the hash is deterministic; the drift here is
	// simulated by passing a different descriptor under the same
	// name.
	c2 := newClientWithTools(t, tool.Options{Pins: tool.Pins{"echo": stale}}, echoConfig("echo"))
	defer c2.Close()
	// The same descriptor repeating has the same hash, so the pin
	// should pass. To prove the pin blocks drift, swap to a
	// different descriptor and confirm the next start fails.
	c2.Close()
	_, err := tool.NewClient(context.Background(),
		tool.Options{Pins: tool.Pins{"echo": "deadbeef"}},
		[]tool.Spec{{Name: "echo", Command: testtoolPath,
			Env: []string{"GAGENT_TESTTOOL_CONFIG=" + writeTesttoolConfig(t, echoConfig("echo"))}}},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "descriptor drift",
		"a stale pin blocks the next startup")
}

// TestClient_RequiredMissingFailsClosed pins the required-tool check
// (ADR 0018 §Required tools). A missing required tool fails closed:
// the agent does not start with a partial tool surface.
func TestClient_RequiredMissingFailsClosed(t *testing.T) {
	_, err := tool.NewClient(context.Background(),
		tool.Options{Required: []string{"message_send"}},
		[]tool.Spec{},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required tools missing")
	assert.Contains(t, err.Error(), "message_send")
}

// TestClient_RequiredMetStarts is the happy path of the same check.
func TestClient_RequiredMetStarts(t *testing.T) {
	client, err := tool.NewClient(context.Background(),
		tool.Options{Required: []string{"echo"}},
		[]tool.Spec{{Name: "echo", Command: testtoolPath,
			Env: []string{"GAGENT_TESTTOOL_CONFIG=" + writeTesttoolConfig(t, echoConfig("echo"))}}},
	)
	require.NoError(t, err)
	defer client.Close()
	require.Len(t, client.List(), 1)
}

// TestClient_InputSchemaRejectsInvalidInput is the boundary input
// validation: a tool call that does not match the descriptor's input
// schema is rejected before reaching the tool body.
func TestClient_InputSchemaRejectsInvalidInput(t *testing.T) {
	client := newClientWithTools(t, tool.Options{}, echoConfig("echo"))

	_, err := client.Call(context.Background(), "echo", json.RawMessage(`{"wrong":42}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrValidation)
}

// TestClient_CallEchoesInput is the protocol round-trip: the input
// validates against the input schema, the server returns the
// identical shape, and the platform validates the output against the
// output schema before returning.
func TestClient_CallEchoesInput(t *testing.T) {
	client := newClientWithTools(t, tool.Options{}, echoConfig("echo"))

	out, err := client.Call(context.Background(), "echo", json.RawMessage(`{"text":"hi"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"text":"hi"}`, string(out))
}

// TestClient_OutputQuarantine is the platform's output boundary: a
// tool whose response does not match its declared output schema is
// quarantined; the call returns a typed error.
func TestClient_OutputQuarantine(t *testing.T) {
	// The tool reports an output schema of {"type":"object"} but
	// the behaviour is "fixed" returning a non-object payload. The
	// platform must reject the response.
	cfg := testtoolConfig{
		Name:         "broken",
		Description:  "returns broken output",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`),
		Behaviour:    "fixed",
		Fixed:        json.RawMessage(`"not an object"`),
	}
	client := newClientWithTools(t, tool.Options{}, cfg)

	_, err := client.Call(context.Background(), "broken", json.RawMessage(`{}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrExecution)
	assert.Contains(t, err.Error(), "output failed schema validation",
		"the platform quarantines the tool whose output violates its declared shape")
}

// TestClient_PermitPolicy allows the listed tools.
func TestClient_PermitPolicy(t *testing.T) {
	client := newClientWithTools(t, tool.Options{
		Policy: tool.AllowList("echo"),
	}, echoConfig("echo"))

	_, err := client.Call(context.Background(), "echo", json.RawMessage(`{"text":"hi"}`))
	require.NoError(t, err)
}

// TestClient_DenyPolicy blocks calls to tools not on the list.
func TestClient_DenyPolicy(t *testing.T) {
	client := newClientWithTools(t, tool.Options{
		Policy: tool.AllowList("other"),
	}, echoConfig("echo"))

	_, err := client.Call(context.Background(), "echo", json.RawMessage(`{"text":"hi"}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrPolicyDenied)
}

// TestClient_AllowAll policy permits every installed tool name.
func TestClient_AllowAll(t *testing.T) {
	assert.True(t, tool.AllowAll().Permitted("echo"))
	assert.True(t, tool.AllowAll().Permitted("anything"))
}

// TestClient_CloseIdempotent is the shutdown path: closing twice does
// not double-kill the spawned processes.
func TestClient_CloseIdempotent(t *testing.T) {
	client := newClientWithTools(t, tool.Options{}, echoConfig("echo"))
	require.NoError(t, client.Close())
	require.NoError(t, client.Close())
}

// TestClient_UnknownToolReturnsTypedError is the failure mode: a call
// to a tool that is not in the operator's list returns a typed error
// (the agent does not auto-discover).
func TestClient_UnknownToolReturnsTypedError(t *testing.T) {
	client := newClientWithTools(t, tool.Options{}, echoConfig("echo"))
	_, err := client.Call(context.Background(), "ghost", json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ghost")
	assert.Contains(t, err.Error(), "not installed")
}

// TestClient_DuplicateNameFailsClosed pins the operator's list: two
// specs with the same name fail startup — the platform has no
// precedence rule it could pick.
func TestClient_DuplicateNameFailsClosed(t *testing.T) {
	cfg := echoConfig("echo")
	_, err := tool.NewClient(context.Background(),
		tool.Options{},
		[]tool.Spec{
			{Name: "echo", Command: testtoolPath,
				Env: []string{"GAGENT_TESTTOOL_CONFIG=" + writeTesttoolConfig(t, cfg)}},
			{Name: "echo", Command: testtoolPath,
				Env: []string{"GAGENT_TESTTOOL_CONFIG=" + writeTesttoolConfig(t, cfg)}},
		},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate tool name")
}

// TestClient_DescriptorNameMismatchFailsClosed pins the spec ↔
// descriptor contract: the spec name must equal the descriptor name.
// A mismatch is a setup error and fails startup.
func TestClient_DescriptorNameMismatchFailsClosed(t *testing.T) {
	cfg := echoConfig("echo-server")
	_, err := tool.NewClient(context.Background(),
		tool.Options{},
		[]tool.Spec{{Name: "echo", Command: testtoolPath,
			Env: []string{"GAGENT_TESTTOOL_CONFIG=" + writeTesttoolConfig(t, cfg)}}},
	)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "does not match") || strings.Contains(err.Error(), "descriptor name"),
		"a spec whose descriptor name does not match fails startup")
}

// TestClient_SurfacesToolErrorDataPayload asserts the platform client
// propagates the JSON-RPC error's data payload into the surfaced
// execution error. Issue #303: files and shell_exec write the
// underlying gRPC detail (status code, gRPC message) into the data
// payload so the agent sees the cause, not just the high-level message.
func TestClient_SurfacesToolErrorDataPayload(t *testing.T) {
	cfg := testtoolConfig{
		Name:         "list",
		Description:  "list files",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		Behaviour:    "error",
		ErrorMsg:     "list failed",
		ErrorData:    json.RawMessage(`{"operation":"list","grpc_code":"InvalidArgument","error":"path escapes workspace: /"}`),
	}
	client := newClientWithTools(t, tool.Options{}, cfg)

	_, err := client.Call(context.Background(), "list", json.RawMessage(`{}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrExecution)
	assert.Contains(t, err.Error(), "list failed",
		"the surfaced error retains the high-level message")
	assert.Contains(t, err.Error(), "path escapes workspace",
		"the surfaced error carries the underlying gRPC reason from Error.Data (issue #303)")
	assert.Contains(t, err.Error(), "InvalidArgument",
		"the surfaced error carries the gRPC status code from Error.Data (issue #303)")
}
