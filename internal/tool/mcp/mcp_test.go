package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/tool"
	"github.com/garamsh/gagent/internal/tool/mcp"
)

// startFakeServer starts the fake MCP server (TestHelperProcess, re-exec of
// the test binary) with the given extra allowlisted variables and timeout.
func startFakeServer(t *testing.T, name string, env []string, timeout time.Duration) *mcp.Server {
	t.Helper()
	t.Setenv(fakeServerMarker, "1")
	srv, err := mcp.Start(context.Background(), mcp.Config{
		Name:    name,
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess"},
		Env:     append([]string{fakeServerMarker}, env...),
		Timeout: timeout,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

// lookup returns the named tool from the server.
func lookup(t *testing.T, srv *mcp.Server, name string) tool.Tool {
	t.Helper()
	for _, tt := range srv.Tools() {
		if tt.Name() == name {
			return tt
		}
	}
	t.Fatalf("tool %q not discovered", name)
	return nil
}

func TestStart_DiscoversToolsWithSchemas(t *testing.T) {
	srv := startFakeServer(t, "fake", nil, 0)

	names := make([]string, 0, len(srv.Tools()))
	for _, tt := range srv.Tools() {
		names = append(names, tt.Name())
	}
	assert.ElementsMatch(t, []string{"echo", "fail", "sleep", "getenv", "die"}, names)

	echo := lookup(t, srv, "echo")
	assert.JSONEq(t, `{
		"type": "object",
		"properties": {"text": {"type": "string"}},
		"required": ["text"],
		"additionalProperties": false
	}`, string(echo.InputSchema()))
	assert.Contains(t, string(echo.OutputSchema()), "echo")
}

func TestCall_ForwardsInputAndMapsResult(t *testing.T) {
	srv := startFakeServer(t, "fake", nil, 0)
	registry := tool.NewRegistry()
	for _, tt := range srv.Tools() {
		require.NoError(t, registry.Register(tt))
	}
	echo, err := registry.Lookup("echo")
	require.NoError(t, err)

	out, err := tool.Invoke(context.Background(), echo, json.RawMessage(`{"text":"hi"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"echo":"hi"}`, string(out))

	_, err = tool.Invoke(context.Background(), echo, json.RawMessage(`{"text":42}`))
	require.ErrorIs(t, err, tool.ErrValidation)
}

func TestCall_MapsErrorResult(t *testing.T) {
	srv := startFakeServer(t, "fake", nil, 0)
	fail := lookup(t, srv, "fail")

	_, err := tool.Invoke(context.Background(), fail, json.RawMessage(`{}`))
	require.ErrorIs(t, err, tool.ErrExecution)
	assert.Contains(t, err.Error(), "fake failure")
}

func TestCall_TimeoutIsTyped(t *testing.T) {
	srv := startFakeServer(t, "fake", nil, 50*time.Millisecond)
	sleep := lookup(t, srv, "sleep")

	_, err := tool.Invoke(context.Background(), sleep, json.RawMessage(`{"ms":5000}`))
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestCall_EnvironmentIsScrubbed(t *testing.T) {
	t.Setenv("GAGENT_FAKE_SECRET", "hunter2")
	t.Setenv("GAGENT_FAKE_VISIBLE", "shown")
	srv := startFakeServer(t, "fake", []string{"GAGENT_FAKE_VISIBLE"}, 0)
	getenv := lookup(t, srv, "getenv")

	out, err := tool.Invoke(context.Background(), getenv, json.RawMessage(`{"name":"GAGENT_FAKE_SECRET"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"set":false}`, string(out))

	out, err = tool.Invoke(context.Background(), getenv, json.RawMessage(`{"name":"GAGENT_FAKE_VISIBLE"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"set":true}`, string(out))
}

func TestCall_DeadServerFailsTyped(t *testing.T) {
	srv := startFakeServer(t, "fake", nil, 0)
	die := lookup(t, srv, "die")
	echo := lookup(t, srv, "echo")

	// The die call takes the server down mid-run; it may or may not return
	// before the process exits.
	_, _ = tool.Invoke(context.Background(), die, json.RawMessage(`{}`))

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		_, err := tool.Invoke(context.Background(), echo, json.RawMessage(`{"text":"hi"}`))
		assert.ErrorIs(c, err, mcp.ErrServerUnavailable)
	}, 5*time.Second, 10*time.Millisecond)
}

func TestStart_BadServerDoesNotAffectOthers(t *testing.T) {
	_, err := mcp.Start(context.Background(), mcp.Config{
		Name:    "broken",
		Command: "gagent-test-nonexistent-command",
	})
	require.Error(t, err)

	srv := startFakeServer(t, "fake", nil, 0)
	echo := lookup(t, srv, "echo")
	out, err := tool.Invoke(context.Background(), echo, json.RawMessage(`{"text":"hi"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"echo":"hi"}`, string(out))
}

func TestStart_InvalidConfig(t *testing.T) {
	_, err := mcp.Start(context.Background(), mcp.Config{Name: "no-command"})
	require.ErrorIs(t, err, mcp.ErrInvalidConfig)

	_, err = mcp.Start(context.Background(), mcp.Config{Command: "true"})
	require.ErrorIs(t, err, mcp.ErrInvalidConfig)
}

// --- fake MCP server (helper process) ---

// fakeServerMarker marks the re-executed test binary that runs the fake MCP
// server instead of the test.
const fakeServerMarker = "GAGENT_FAKE_MCP_SERVER"

// TestHelperProcess is not a test: re-executed with the marker variable set,
// it serves the fake MCP server over stdio and exits.
func TestHelperProcess(t *testing.T) {
	if os.Getenv(fakeServerMarker) != "1" {
		return
	}
	if err := runFakeServer(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

// runFakeServer serves the fake MCP server on stdio until the client
// disconnects.
func runFakeServer() error {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "fake", Version: "dev"}, nil)

	type echoIn struct {
		Text string `json:"text"`
	}
	type echoOut struct {
		Echo string `json:"echo"`
	}
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "echo", Description: "echo the text back"},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, in echoIn) (*mcpsdk.CallToolResult, echoOut, error) {
			return nil, echoOut{Echo: in.Text}, nil
		})

	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "fail", Description: "always fails"},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, any, error) {
			return nil, nil, errors.New("fake failure")
		})

	type sleepIn struct {
		MS int `json:"ms"`
	}
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "sleep", Description: "sleep until done or cancelled"},
		func(ctx context.Context, _ *mcpsdk.CallToolRequest, in sleepIn) (*mcpsdk.CallToolResult, any, error) {
			select {
			case <-time.After(time.Duration(in.MS) * time.Millisecond):
				return nil, nil, nil
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			}
		})

	type getenvIn struct {
		Name string `json:"name"`
	}
	type getenvOut struct {
		Set bool `json:"set"`
	}
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "getenv", Description: "report whether a variable is set"},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, in getenvIn) (*mcpsdk.CallToolResult, getenvOut, error) {
			_, ok := os.LookupEnv(in.Name)
			return nil, getenvOut{Set: ok}, nil
		})

	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "die", Description: "kill the server process"},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, any, error) {
			os.Exit(42)
			return nil, nil, nil
		})

	return server.Run(context.Background(), &mcpsdk.StdioTransport{})
}
