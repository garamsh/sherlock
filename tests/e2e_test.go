package tests

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/environment"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
	"github.com/garamsh/gagent/internal/sandbox"
	"github.com/garamsh/gagent/internal/tool"
)

// e2eEnvID is this scenario's own environment ID; the matching container
// name is gagent-env-e2e.
const e2eEnvID = "e2e"

// e2eSeed is the memory planted before the turn; e2eQuestion asks about
// it. They share keywords so the hybrid recall ranks the seed first.
const (
	e2eSeed     = "Prior task: the sandbox computed the answer 6 * 7 = 42 for the user."
	e2eQuestion = "What answer did the sandbox compute in the prior task?"
)

// requireDockerClient gates daemon-dependent tests on GAGENT_DOCKER_TESTS,
// mirroring the sandbox package guard. It returns a client used for
// pre-run leftover cleanup and post-run container assertions.
func requireDockerClient(t *testing.T) *client.Client {
	t.Helper()
	if os.Getenv("GAGENT_DOCKER_TESTS") == "" {
		t.Skip("set GAGENT_DOCKER_TESTS=1 to run Docker daemon tests")
	}
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = cli.Ping(ctx)
	require.NoError(t, err, "GAGENT_DOCKER_TESTS is set but the Docker daemon is unreachable")
	return cli
}

// mvpScript drives the PLAN §5.7 scenario through the scripted mock model:
// compute in the sandbox, reply to the sender, then write the result back
// as a new episodic memory.
func mvpScript() *agent.ScriptedModel {
	return agent.NewScriptedModel(
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{
			Name:  "sandbox_exec",
			Input: json.RawMessage(`{"command":"echo $((6 * 7))"}`),
		}}},
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{
			Name:  "message_send",
			Input: json.RawMessage(`{"channel":"http","target":"sender","content":"The sandbox computed 42."}`),
		}}},
		agent.ScriptedStep{Content: "Computed the answer in the sandbox.\n" +
			"```memory\n" +
			`{"add":[{"content":"The sandbox computed 6 * 7 = 42 in the environment container","type":"episodic"}]}` +
			"\n```"},
	)
}

// seedMemory stores content in the environment's memory.db through the
// memory tool and returns the assigned ID.
func seedMemory(t *testing.T, envDir, content string) int64 {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(envDir, gateway.MemoryDBFile))
	require.NoError(t, err)
	svc := memory.NewService(store, memory.NewHashingEmbedder(), memory.NewConcatSummarizer())
	out, err := tool.Invoke(context.Background(), memory.NewTool(svc), json.RawMessage(
		`{"op":"store","content":`+strconvQuote(content)+
			`,"type":"episodic","importance_score":0.8,"source":"test-seed"}`))
	require.NoError(t, err)
	require.NoError(t, svc.Close())
	var stored struct {
		ID int64 `json:"id"`
	}
	require.NoError(t, json.Unmarshal(out, &stored))
	require.Positive(t, stored.ID)
	return stored.ID
}

// TestEndToEnd_MVPScenario proves the PLAN §5.7 flow across the real wired
// system in-process: HTTP ingress, memory recall, code execution in a real
// container, and writeback to memory.
func TestEndToEnd_MVPScenario(t *testing.T) {
	cli := requireDockerClient(t)
	rt, err := sandbox.NewDockerRuntime()
	require.NoError(t, err)

	root, envDir, seedID := setupMVPScenario(t, cli, e2eEnvID)

	factory := &gateway.Factory{
		Root:        root,
		NewModel:    func(string) agent.Model { return mvpScript() },
		Sandbox:     rt,
		Image:       "alpine:3.21",
		ExecTimeout: 30 * time.Second,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := gateway.NewRouter(factory, logger)
	t.Cleanup(func() { router.Close() })
	srv := httptest.NewServer(gateway.NewHandler(router, logger))
	t.Cleanup(srv.Close)

	runMVPScenario(t, srv.URL, root, e2eEnvID)

	// Router shutdown destroys the environment container.
	require.NoError(t, router.Close())
	_, err = cli.ContainerInspect(context.Background(), "gagent-env-"+e2eEnvID)
	assert.True(t, errdefs.IsNotFound(err),
		"environment container must be removed on shutdown, got: %v", err)

	assertMVPRecallAndWriteback(t, envDir, seedID)
}

// setupMVPScenario prepares the data root for one §5.7 run: a temp root
// under $HOME (Docker Desktop VMs cannot bind-mount /tmp, same constraint
// as the sandbox tests), no leftover container from previous failed runs,
// and the seed memory planted through the memory tool. It returns the
// root, the environment's data dir, and the seed memory ID.
func setupMVPScenario(t *testing.T, cli *client.Client, envID string) (root, envDir string, seedID int64) {
	t.Helper()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	root, err = os.MkdirTemp(home, "gagent-e2e-test-")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(root)) })

	_ = cli.ContainerRemove(context.Background(), "gagent-env-"+envID, container.RemoveOptions{Force: true})

	env, err := environment.Open(root, envID)
	require.NoError(t, err)
	seedID = seedMemory(t, env.Dir(), e2eSeed)
	return root, env.Dir(), seedID
}

// runMVPScenario drives the PLAN §5.7 message flow against a serving
// gateway: HTTP ingress, the full turn lifecycle on the monitoring stream,
// sandbox execution in the real container, and the reply in the outbox.
func runMVPScenario(t *testing.T, baseURL, root, envID string) {
	t.Helper()

	// Subscribe to the monitoring stream before the message so no event
	// is missed.
	events := subscribeSSE(t, baseURL+"/envs/"+envID+"/events")

	resp := postJSON(t, baseURL+"/envs/"+envID+"/messages",
		`{"content":`+strconvQuote(e2eQuestion)+`}`)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	resp.Body.Close()

	// Monitoring: the full turn lifecycle was published, in order.
	got := collectEvents(t, events, 7)
	var kinds []string
	for _, e := range got {
		kinds = append(kinds, e["kind"].(string))
	}
	assert.Equal(t, []string{
		"turn_started", "writeback", "tool_call", "writeback", "tool_call", "writeback", "turn_finished",
	}, kinds)
	assert.Equal(t, e2eQuestion, got[0]["task"])

	// The agent ran a command via sandbox_exec in the real container and
	// observed the computed result.
	execEvent := got[2]
	assert.Equal(t, "sandbox_exec", execEvent["tool_name"])
	assert.NotContains(t, execEvent, "tool_failed")
	assert.Contains(t, execEvent["tool_result"], `"stdout":"42\n"`)
	assert.Contains(t, execEvent["tool_result"], `"exit_code":0`)

	assert.Equal(t, "message_send", got[4]["tool_name"])

	// The final writeback event carries the new episodic memory.
	ops, ok := got[5]["ops"].([]any)
	require.True(t, ok, "final writeback event must carry ops")
	require.Len(t, ops, 1)
	op := ops[0].(map[string]any)
	assert.Equal(t, "add", op["kind"])
	assert.Equal(t, "episodic", op["type"])
	assert.Contains(t, op["content"], "6 * 7 = 42")

	// Reply: message_send output landed in the environment outbox.
	require.Eventually(t, func() bool {
		return len(outboxMessages(t, root, envID)) == 1
	}, 5*time.Second, 10*time.Millisecond)
	replies := outboxMessages(t, root, envID)
	require.Len(t, replies, 1)
	assert.Equal(t, "The sandbox computed 42.", replies[0].Content)
}

// assertMVPRecallAndWriteback asserts the memory side of the §5.7 scenario
// on the environment's memory store, reopened after shutdown: the seeded
// memory was recalled this turn and the computed result was written back
// as a new episodic memory, queryable via the memory tool's search.
func assertMVPRecallAndWriteback(t *testing.T, envDir string, seedID int64) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(envDir, gateway.MemoryDBFile))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	svc := memory.NewService(store, memory.NewHashingEmbedder(), memory.NewConcatSummarizer())

	// Recall: the seeded memory was retrieved this turn (its access was
	// recorded by the recall step).
	seed, err := store.Get(context.Background(), seedID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, seed.AccessCount, int64(1),
		"recall must have recorded access on the seeded memory")
	assert.False(t, seed.LastAccessed.IsZero())

	// Writeback: the computed result is a new episodic memory, queryable
	// via the memory tool's search.
	found, err := tool.Invoke(context.Background(), memory.NewTool(svc), json.RawMessage(
		`{"op":"search","query":"sandbox computed 42","limit":10}`))
	require.NoError(t, err)
	var hits struct {
		Results []struct {
			ID      int64  `json:"id"`
			Content string `json:"content"`
			Type    string `json:"type"`
		} `json:"results"`
	}
	require.NoError(t, json.Unmarshal(found, &hits))
	var writebackFound bool
	for _, h := range hits.Results {
		if strings.Contains(h.Content, "6 * 7 = 42 in the environment container") {
			writebackFound = true
			assert.Equal(t, "episodic", h.Type)
			assert.NotEqual(t, seedID, h.ID, "writeback must be a new memory, not the seed")
		}
	}
	assert.True(t, writebackFound, "writeback memory must be queryable via search, got: %s", found)
}
