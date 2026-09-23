package tests

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	agentmemory "github.com/garamsh/gagent/internal/agent/memory"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
	"github.com/garamsh/gagent/internal/tool"
)

// workProbe observes the real reasoning and recall inputs, not model competence.
type workProbe struct {
	inner     agent.Model
	reasoning [][]agent.Message
	recall    [][]agent.Message
}

func (m *workProbe) Chat(ctx context.Context, messages []agent.Message, descriptors []tool.Descriptor) (agent.Response, error) {
	reasoning := false
	for _, d := range descriptors {
		if d.Name == agent.MemoryWriteTool {
			reasoning = true
		}
	}
	snapshot := append([]agent.Message(nil), messages...)
	if reasoning {
		m.reasoning = append(m.reasoning, snapshot)
	} else {
		m.recall = append(m.recall, snapshot)
	}
	return m.inner.Chat(ctx, messages, descriptors)
}

func currentAccount(status memory.WorkStatus, goal, remaining string) memory.WorkAccount {
	return memory.WorkAccount{Status: status, Progress: goal + "; " + remaining}

}

func workCheckpoint(t *testing.T, account memory.WorkAccount, reconciliation string) agent.ScriptedStep {
	t.Helper()
	doc := map[string]any{"status": account.Status, "progress": account.Progress, "inputs": []any{}, "resolved_attempts": []any{}}
	if reconciliation != "" {
		doc["resolved_attempts"] = []any{map[string]any{"id": 3, "evidence": reconciliation}}
	}
	data, err := json.Marshal(doc)
	require.NoError(t, err)
	return agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: agent.WorkingContextTool, Input: data}}}
}

func workFinish(t *testing.T, account memory.WorkAccount, reconciliation string) agent.ScriptedStep {
	t.Helper()
	checkpoint := workCheckpoint(t, account, reconciliation)
	return agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: agent.MemoryWriteTool, Input: json.RawMessage(`{"reply":"checkpoint saved","memory_ops":[],"working_context":` + string(checkpoint.ToolCalls[0].Input) + `}`)}}}
}

func workFactory(path string, model agent.Model, client agent.ToolClient) *gateway.Factory {
	return &gateway.Factory{MemoryPath: path, NewModel: func(string) agent.Model { return model }, Tools: client, MaxHistoryMessages: 3}
}

func workRecord(t *testing.T, messages []agent.Message) memory.WorkingContext {
	t.Helper()
	for _, message := range messages {
		if _, data, ok := strings.Cut(message.Content, "Current-work record:\n"); ok {
			work, err := memory.ParseWorkingContext([]byte(data))
			require.NoError(t, err)
			return work
		}
	}
	t.Fatal("reasoning did not receive durable current work")
	return memory.WorkingContext{}
}

// workUpdate supplies protocol operations without retranscribing owner instructions.
func workUpdate(t *testing.T, terminal bool, update string) agent.ScriptedStep {
	t.Helper()
	name := agent.WorkingContextTool
	if terminal {
		name = agent.MemoryWriteTool
		update = `{"reply":"recorded","memory_ops":[],"working_context":` + update + `}`
	}
	return agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: name, Input: json.RawMessage(update)}}}
}

func TestWorkingContext_RestartAndCompaction(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	original := `Deliver using supplier Cedar only after arrival. Output exactly {"supplier":"Cedar","units":7}; write a receipt; never repeat a released order.`
	// Deliberately omit every exact output and no-repeat constraint from progress.
	steps := []agent.ScriptedStep{{Content: "no address"}, workUpdate(t, false, `{"status":"active","progress":"waiting","inputs":[{"index":0,"action":"amend"}]}`), workUpdate(t, true, `{}`)}
	for range 3 {
		steps = append(steps, agent.ScriptedStep{Content: "no address"}, workUpdate(t, true, `{"inputs":[{"index":0,"action":"dismiss"}]}`))
	}
	probe := &workProbe{inner: agent.NewScriptedModel(steps...)}
	runtime, err := workFactory(path, probe, workTools{}).New(ctx, "test", nil)
	require.NoError(t, err)
	_, err = runtime.Session.Run(ctx, original)
	require.NoError(t, err)
	for _, input := range []string{"still waiting", "weather update", "schedule unchanged"} {
		_, err = runtime.Session.Run(ctx, input)
		require.NoError(t, err)
	}
	for _, message := range runtime.Session.History() {
		require.NotContains(t, message.Content, original)
	}
	require.LessOrEqual(t, len(runtime.Session.History()), 5)
	latest := workRecord(t, probe.reasoning[len(probe.reasoning)-1])
	assert.Equal(t, []string{original}, latest.Source)
	assert.Equal(t, "waiting", latest.Account.Progress)
	require.NoError(t, runtime.Close(ctx))
	effects := 0
	resumed := &workProbe{inner: workModel(func(messages []agent.Message, descriptors []tool.Descriptor) agent.Response {
		if !hasWorkTool(descriptors) {
			return agent.Response{Content: "no address"}
		}
		current := workRecord(t, messages)
		require.Equal(t, []string{original}, current.Source)
		require.Equal(t, "waiting", current.Account.Progress)
		require.Equal(t, []string{"arrival notification"}, current.Inputs)
		if effects == 0 {
			return agent.Response{ToolCalls: []agent.ToolCall{{Name: "artifact", Input: json.RawMessage(`{"supplier":"Cedar","units":7}`)}}}
		}
		return agent.Response{ToolCalls: workUpdate(t, true, `{"status":"completed","progress":"receipt verified","inputs":[{"index":0,"action":"dismiss"}]}`).ToolCalls}
	})}
	restarted, err := workFactory(path, resumed, workTools{call: func(_ string, input json.RawMessage) (json.RawMessage, error) {
		effects++
		require.JSONEq(t, `{"supplier":"Cedar","units":7}`, string(input))
		return json.RawMessage(`{"receipt":"one order"}`), nil
	}}).New(ctx, "test", nil)
	require.NoError(t, err)
	defer restarted.Close(ctx)
	require.Empty(t, restarted.Session.History())
	_, err = restarted.Session.Run(ctx, "arrival notification")
	require.NoError(t, err)
	assert.Equal(t, 1, effects)
	require.Len(t, resumed.recall, 1)
	assert.Equal(t, []agent.Message{
		{Role: agent.RoleSystem, Content: agent.ReadInstruction},
		{Role: agent.RoleUser, Content: "arrival notification"},
	}, resumed.recall[0], "current work reaches reasoning without changing task-only fact resolution")
}

type workModel func([]agent.Message, []tool.Descriptor) agent.Response

func (m workModel) Chat(_ context.Context, messages []agent.Message, descriptors []tool.Descriptor) (agent.Response, error) {
	return m(messages, descriptors), nil
}
func hasWorkTool(descriptors []tool.Descriptor) bool {
	for _, d := range descriptors {
		if d.Name == agent.MemoryWriteTool {
			return true
		}
	}
	return false
}

func TestWorkingContext_LatestIntentAndTerminalControls(t *testing.T) {
	for _, status := range []string{"canceled", "completed"} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "memory.db")
			original := "Use Cedar; output JSON with units and supplier; issue one receipt; never repeat release."
			amendment := "Change supplier to Maple only."
			replacement := "Replace the task: inspect a new fixture and return text."
			terminal := "Cancel this fixture."
			if status == "completed" {
				terminal = "The fixture is delivered; record completion."
			}
			expectedSource := []string{}
			effects := 0
			run := func(input, update string, want []string, effect bool) {
				t.Helper()
				probe := &workProbe{inner: workModel(func(messages []agent.Message, descriptors []tool.Descriptor) agent.Response {
					if !hasWorkTool(descriptors) {
						return agent.Response{Content: "no address"}
					}
					current := workRecord(t, messages)
					require.Equal(t, expectedSource, append([]string{}, current.Source...))
					require.Equal(t, []string{input}, current.Inputs)
					return agent.Response{ToolCalls: workUpdate(t, false, update).ToolCalls}
				})}
				// The scripted decision is conditional on the actual supplied record, not an enum round trip.
				checked := false
				first := probe.inner
				probe.inner = workModel(func(messages []agent.Message, descriptors []tool.Descriptor) agent.Response {
					if !hasWorkTool(descriptors) || !checked {
						response, _ := first.Chat(ctx, messages, descriptors)
						if hasWorkTool(descriptors) {
							checked = true
						}
						return response
					}
					current := workRecord(t, messages)
					require.Equal(t, want, current.Source)
					require.Empty(t, current.Inputs)
					if effect {
						effect = false
						return agent.Response{ToolCalls: []agent.ToolCall{{Name: "artifact", Input: json.RawMessage(`{"inspect":"new fixture"}`)}}}
					}
					return agent.Response{ToolCalls: workUpdate(t, true, `{}`).ToolCalls}
				})
				runtime, err := workFactory(path, probe, workTools{call: func(_ string, input json.RawMessage) (json.RawMessage, error) {
					effects++
					require.JSONEq(t, `{"inspect":"new fixture"}`, string(input))
					return json.RawMessage(`{}`), nil
				}}).New(ctx, "test", nil)
				require.NoError(t, err)
				require.Empty(t, runtime.Session.History())
				_, err = runtime.Session.Run(ctx, input)
				require.NoError(t, err)
				require.NoError(t, runtime.Close(ctx))
				expectedSource = want
			}
			run(original, `{"status":"active","progress":"waiting","inputs":[{"index":0,"action":"amend"}]}`, []string{original}, false)
			run(amendment, `{"inputs":[{"index":0,"action":"amend"}]}`, []string{original, amendment}, false)
			run(terminal, `{"status":"`+status+`","progress":"None - terminal","inputs":[{"index":0,"action":"amend"}]}`, []string{original, amendment, terminal}, false)
			run("arrival notification", `{"inputs":[{"index":0,"action":"dismiss"}]}`, expectedSource, false)
			require.Zero(t, effects, "terminal identity and governing cancellation were supplied; no effect requested")
			st, err := sqlite.Open(path)
			require.NoError(t, err)
			saved, err := st.LoadWorkingContext(ctx)
			require.NoError(t, err)
			assert.Equal(t, memory.WorkStatus(status), saved.Account.Status)
			require.NoError(t, st.Close())
			run(replacement, `{"status":"active","progress":"inspect new fixture","inputs":[{"index":0,"action":"replace"}]}`, []string{replacement}, true)
			assert.Equal(t, 1, effects, "only genuinely new scripted work acts")
		})
	}
}

type workTools struct {
	call func(string, json.RawMessage) (json.RawMessage, error)
}

func (workTools) List() []tool.Descriptor {
	return []tool.Descriptor{{Name: "artifact", InputSchema: json.RawMessage(`{"type":"object"}`)}}
}
func (c workTools) Call(_ context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	if c.call == nil {
		return nil, errors.New("unexpected tool invocation")
	}
	return c.call(name, input)
}

func TestWorkingContext_ProcessInterruption(t *testing.T) {
	if path := os.Getenv("GAGENT_WORK_CHILD"); path != "" {
		account := currentAccount(memory.WorkActive, "write release marker", "verify marker before any repeat")
		model := agent.NewScriptedModel(agent.ScriptedStep{Content: "no address"}, workCheckpoint(t, account, ""), agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: "artifact", Input: json.RawMessage(`{"action":"write","path":"release.marker"}`)}}})
		runtime, err := workFactory(path, model, workTools{call: func(string, json.RawMessage) (json.RawMessage, error) {
			require.NoError(t, os.WriteFile(path+".marker", []byte("released once"), 0o600))
			os.Exit(73)
			return nil, nil
		}}).New(context.Background(), "test", nil)
		require.NoError(t, err)
		_, err = runtime.Session.Run(context.Background(), "write release marker once, then verify it")
		require.NoError(t, err)
		t.Fatal("child did not stop at the effect boundary")
	}
	path := filepath.Join(t.TempDir(), "memory.db")
	executable, err := os.Executable()
	require.NoError(t, err)
	child := exec.Command(executable, "-test.run=^TestWorkingContext_ProcessInterruption$")
	child.Env = []string{"GAGENT_WORK_CHILD=" + path}
	output, err := child.CombinedOutput()
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit, string(output))
	require.Equal(t, 73, exit.ExitCode(), string(output))
	artifact, err := os.ReadFile(path + ".marker")
	require.NoError(t, err)
	require.Equal(t, "released once", string(artifact), "effect happened before process death")
	persisted, err := sqlite.Open(path)
	require.NoError(t, err)
	interrupted, err := persisted.LoadWorkingContext(context.Background())
	require.NoError(t, err)
	require.Len(t, interrupted.Attempts, 1, "effect must have durable attempt evidence before recovery dispatch")
	assert.Nil(t, interrupted.Attempts[0].Result)
	require.NoError(t, persisted.Close())

	account := currentAccount(memory.WorkActive, "write release marker", "verify marker before any repeat")
	completed := currentAccount(memory.WorkCompleted, account.Progress, "")
	probe := &workProbe{inner: agent.NewScriptedModel(
		agent.ScriptedStep{Content: "no address"}, workCheckpoint(t, account, ""),
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: "artifact", Input: json.RawMessage(`{"action":"read"}`)}}},
		workFinish(t, completed, "Read release marker: released once; do not repeat the write."),
	)}
	calls := 0
	runtime, err := workFactory(path, probe, workTools{call: func(_ string, input json.RawMessage) (json.RawMessage, error) {
		calls++
		require.JSONEq(t, `{"action":"read"}`, string(input))
		got, err := os.ReadFile(path + ".marker")
		require.NoError(t, err)
		require.Equal(t, "released once", string(got))
		return json.RawMessage(`{"marker":"released once"}`), nil
	}}).New(context.Background(), "test", nil)
	require.NoError(t, err)
	defer runtime.Close(context.Background())
	require.Zero(t, calls, "startup never replays a tool")
	_, err = runtime.Session.Run(context.Background(), "arrival notification")
	require.NoError(t, err)
	initial := workRecord(t, probe.reasoning[0])
	require.Len(t, initial.Attempts, 1)
	assert.Nil(t, initial.Attempts[0].Result, "effect has no durably observed result")
	assert.Contains(t, string(initial.Attempts[0].Input), "release.marker")
	assert.Equal(t, account, initial.Account)
	require.Len(t, workRecord(t, probe.reasoning[1]).Attempts, 1, "checkpoint omission cannot erase an unknown attempt")
	stored, err := sqlite.Open(path)
	require.NoError(t, err)
	defer stored.Close()
	final, err := stored.LoadWorkingContext(context.Background())
	require.NoError(t, err)
	require.Len(t, final.Attempts, 1, "unmentioned successful verification remains evidence")
	require.NotNil(t, final.Attempts[0].Result)
	assert.Equal(t, memory.WorkReturned, final.Attempts[0].Result.Outcome)
	assert.Contains(t, final.Account.Progress, "Read release marker")
	assert.Equal(t, memory.WorkCompleted, final.Account.Status)
	assert.Equal(t, 1, calls, "only the explicit verification ran")
}

// workFailure substitutes only the persistent boundary, retaining real store reads.
type workFailure struct {
	memory.WorkingContextStorer
	failAt, writes int
}

func (s *workFailure) SaveWorkingContext(ctx context.Context, work memory.WorkingContext) (int64, error) {
	s.writes++
	if s.writes == s.failAt {
		return 0, errors.New("injected durable write failure")
	}
	return s.WorkingContextStorer.SaveWorkingContext(ctx, work)
}

func TestWorkingContext_FailedWritesStopEffectsAndSuccess(t *testing.T) {
	for _, failAt := range []int{0, 1, 2, 3, 4, 5, 6} {
		t.Run(string(rune('0'+failAt)), func(t *testing.T) {
			ctx := context.Background()
			store, err := sqlite.Open(filepath.Join(t.TempDir(), "memory.db"))
			require.NoError(t, err)
			defer store.Close()
			boundary := &workFailure{WorkingContextStorer: store, failAt: failAt}
			account := currentAccount(memory.WorkActive, "write marker", "verify marker")
			model := agent.NewScriptedModel(agent.ScriptedStep{ToolCalls: []agent.ToolCall{{ID: "first", Name: "artifact", Input: json.RawMessage(`{}`)}, {ID: "second", Name: "artifact", Input: json.RawMessage(`{"second":true}`)}}}, workFinish(t, account, ""))
			effects := 0
			probe := &workProbe{inner: model}
			session := agent.NewSession(probe, workTools{call: func(string, json.RawMessage) (json.RawMessage, error) { effects++; return json.RawMessage(`{}`), nil }}, agent.Options{WorkingContext: boundary, Compactor: agentmemory.Compactor{MaxHistoryMessages: 100}, MaxHistoryMessages: 100})
			reply, err := session.Run(ctx, "write marker")
			if failAt == 0 {
				require.NoError(t, err)
				assert.NotEmpty(t, reply)
				assert.Equal(t, 2, effects)
				return
			}
			require.ErrorContains(t, err, "injected durable write failure")
			assert.Empty(t, reply, "no false success")
			assert.Equal(t, max(0, (failAt-1)/2), effects)
			saved, err := store.LoadWorkingContext(ctx)
			require.NoError(t, err)
			if failAt == 3 {
				require.Len(t, saved.Attempts, 1)
				assert.Nil(t, saved.Attempts[0].Result)
			}
			if failAt == 4 {
				require.Len(t, saved.Attempts, 1)
				require.NotNil(t, saved.Attempts[0].Result)
			}
			probe.inner = agent.NewScriptedModel(workFinish(t, account, ""))
			before := effects
			_, err = session.Run(ctx, "check status after storage recovers")
			require.NoError(t, err)
			assert.Equal(t, before, effects, "recovery never replays a call")
			recovered := probe.reasoning[len(probe.reasoning)-1]
			pending := map[string]bool{}
			for _, message := range recovered {
				for _, call := range message.ToolCalls {
					pending[call.ID] = true
				}
				if message.Role == agent.RoleTool {
					delete(pending, message.ToolCallID)
				}
			}
			assert.Empty(t, pending, "failed batch leaves no unanswered protocol calls, including siblings")
			if failAt == 3 {
				require.Len(t, workRecord(t, recovered).Attempts, 1)
				assert.Nil(t, workRecord(t, recovered).Attempts[0].Result)
			}

		})
	}
}

// fixtureCheckpoint states the work in the existing offline messaging scenarios.
func fixtureCheckpoint(status string) agent.ScriptedStep {
	return agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: agent.WorkingContextTool, Input: json.RawMessage(`{"status":"` + status + `","progress":"follow the scripted scenario","inputs":[],"resolved_attempts":[]}`)}}}
}

func TestWorkingContext_PendingIntentSurvivesBeforeInterpretation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	account := currentAccount(memory.WorkActive, "prepare fixture", "await authorization")
	runtime, err := workFactory(path, agent.NewScriptedModel(agent.ScriptedStep{Content: "no address"}, workUpdate(t, true, `{"status":"active","progress":"prepare fixture; await authorization","inputs":[{"index":0,"action":"amend"}]}`)), workTools{}).New(ctx, "test", nil)
	require.NoError(t, err)
	_, err = runtime.Session.Run(ctx, "prepare fixture")
	require.NoError(t, err)
	require.NoError(t, runtime.Close(ctx))
	for _, input := range []string{"cancel the fixture", "do not send anything"} {
		runtime, err = workFactory(path, agent.NewScriptedModel(agent.ScriptedStep{Err: errors.New("interrupted before interpreting intent")}), workTools{}).New(ctx, "test", nil)
		require.NoError(t, err)
		_, err = runtime.Session.Run(ctx, input)
		require.ErrorContains(t, err, "interrupted before interpreting intent")
		require.NoError(t, runtime.Close(ctx))
	}
	canceled := currentAccount(memory.WorkCanceled, account.Progress, "")
	probe := &workProbe{inner: agent.NewScriptedModel(agent.ScriptedStep{Content: "no address"}, workFinish(t, canceled, ""))}
	runtime, err = workFactory(path, probe, workTools{}).New(ctx, "test", nil)
	require.NoError(t, err)
	defer runtime.Close(ctx)
	_, err = runtime.Session.Run(ctx, "arrival notification")
	require.NoError(t, err)
	current := workRecord(t, probe.reasoning[0])
	assert.Equal(t, account, current.Account)
	assert.Equal(t, []string{"cancel the fixture", "do not send anything", "arrival notification"}, current.Inputs)
}

func TestWorkingContext_CheckpointFeedbackAndRequiredFinalAccount(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "missing final account"}[malformed], func(t *testing.T) {
			ctx := context.Background()
			store, err := sqlite.Open(filepath.Join(t.TempDir(), "memory.db"))
			require.NoError(t, err)
			defer store.Close()
			account := currentAccount(memory.WorkActive, "inspect fixture", "report observation")
			effect := agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: "artifact", Input: json.RawMessage(`{}`)}}}
			final := workFinish(t, account, "")
			if malformed {
				final = agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: agent.MemoryWriteTool, Input: json.RawMessage(`{"reply":"done","memory_ops":[]}`)}}}
			}
			probe := &workProbe{inner: agent.NewScriptedModel(effect, workCheckpoint(t, account, ""), effect, final)}
			effects := 0
			session := agent.NewSession(probe, workTools{call: func(string, json.RawMessage) (json.RawMessage, error) { effects++; return json.RawMessage(`{}`), nil }}, agent.Options{WorkingContext: store})
			reply, err := session.Run(ctx, "inspect fixture")
			if malformed {
				require.Error(t, err)
				assert.Empty(t, reply)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, 2, effects, "direct calls run after durable source and attempt persistence")
			require.Len(t, probe.reasoning, 4)
			feedback := probe.reasoning[2]
			found := false
			for i, message := range feedback {
				if message.Role == agent.RoleTool && message.Name == agent.WorkingContextTool {
					require.Positive(t, i)
					require.Len(t, feedback[i-1].ToolCalls, 1)
					assert.Equal(t, feedback[i-1].ToolCalls[0].ID, message.ToolCallID)
					assert.Contains(t, message.Content, "Checkpoint saved")
					found = true
				}
			}
			require.True(t, found, "checkpoint receives coherent tool feedback")
			assert.Contains(t, feedback[len(feedback)-1].Content, "Source contains")
		})
	}
}

func TestWorkingContext_UncertainEffectsCannotBecomeCompleted(t *testing.T) {
	for _, outcome := range []memory.WorkOutcome{"", memory.WorkFailed} {
		t.Run(string(outcome), func(t *testing.T) {
			ctx := context.Background()
			store, err := sqlite.Open(filepath.Join(t.TempDir(), "memory.db"))
			require.NoError(t, err)
			defer store.Close()
			work, err := store.LoadWorkingContext(ctx)
			require.NoError(t, err)
			work.Account = currentAccount(memory.WorkActive, "release fixture", "verify effect")
			attempt := memory.WorkAttempt{ID: 3, Name: "artifact", Input: json.RawMessage(`{"action":"release"}`)}
			if outcome != "" {
				attempt.Result = &memory.WorkResult{Outcome: outcome, Content: "timeout after possible effect"}
			}
			work.Attempts = []memory.WorkAttempt{attempt}
			_, err = store.SaveWorkingContext(ctx, work)
			require.NoError(t, err)
			completed := currentAccount(memory.WorkCompleted, work.Account.Progress, "")
			probe := &workProbe{inner: agent.NewScriptedModel(workFinish(t, completed, ""))}
			session := agent.NewSession(probe, workTools{}, agent.Options{WorkingContext: store})
			_, err = session.Run(ctx, "arrival notification")
			require.ErrorContains(t, err, "requires reconciliation")
			saved, err := store.LoadWorkingContext(ctx)
			require.NoError(t, err)
			assert.Equal(t, memory.WorkActive, saved.Account.Status)
			assert.Equal(t, work.Attempts, saved.Attempts)
			probe.inner = agent.NewScriptedModel(workFinish(t, completed, "Inspection confirmed exactly one fixture receipt; no repeat is needed."))
			_, err = session.Run(ctx, "inspection result is available")
			require.NoError(t, err, "accepted explicit reconciliation control; content truth is the model's responsibility")
			saved, err = store.LoadWorkingContext(ctx)
			require.NoError(t, err)
			assert.Equal(t, memory.WorkCompleted, saved.Account.Status)
			assert.Empty(t, saved.Attempts)
		})
	}
}

func TestWorkingContext_IndividualRetirementAndInvalidReferences(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "memory.db"))
	require.NoError(t, err)
	defer store.Close()
	work, err := store.LoadWorkingContext(ctx)
	require.NoError(t, err)
	work.Account = memory.WorkAccount{Status: memory.WorkActive, Progress: "two effects need verification"}
	work.Source = []string{"Release two independent artifacts once."}
	work.Attempts = []memory.WorkAttempt{{ID: 1, Name: "artifact", Input: json.RawMessage(`{"path":"one"}`)}, {ID: 2, Name: "artifact", Input: json.RawMessage(`{"path":"two"}`)}}
	_, err = store.SaveWorkingContext(ctx, work)
	require.NoError(t, err)
	run := func(update string) error {
		t.Helper()
		session := agent.NewSession(agent.NewScriptedModel(workUpdate(t, true, update)), workTools{}, agent.Options{WorkingContext: store})
		_, err := session.Run(ctx, "inspection notification")
		return err
	}
	require.NoError(t, run(`{"resolved_attempts":[{"id":1,"evidence":"read artifact one; receipt present"}]}`))
	saved, err := store.LoadWorkingContext(ctx)
	require.NoError(t, err)
	require.Len(t, saved.Attempts, 1)
	assert.EqualValues(t, 2, saved.Attempts[0].ID)
	assert.Nil(t, saved.Attempts[0].Result)
	assert.Contains(t, saved.Account.Progress, "artifact one")
	for _, update := range []string{
		`{"status":"completed"}`,
		`{"resolved_attempts":[{"id":1,"evidence":"already retired"}]}`,
		`{"resolved_attempts":[{"id":999,"evidence":"unrelated"}]}`,
		`{"resolved_attempts":[{"evidence":"missing ID"}]}`,
		`{"resolved_attempts":[{"id":null,"evidence":"null ID"}]}`,
		`{"resolved_attempts":[{"id":2,"evidence":""}]}`,
		`{"resolved_attempts":[{"id":2,"evidence":"read two"},{"id":2,"evidence":"duplicate"}]}`,
	} {
		t.Run(update, func(t *testing.T) {
			require.Error(t, run(update))
			after, err := store.LoadWorkingContext(ctx)
			require.NoError(t, err)
			assert.Equal(t, saved.Attempts, after.Attempts)
			assert.Equal(t, saved.Account, after.Account)
		})
	}
	require.NoError(t, run(`{"status":"completed","resolved_attempts":[{"id":2,"evidence":"read artifact two; distinct receipt present"}]}`))
	after, err := store.LoadWorkingContext(ctx)
	require.NoError(t, err)
	assert.Empty(t, after.Attempts)
	assert.Equal(t, memory.WorkCompleted, after.Account.Status)
	require.Error(t, run(`{"resolved_attempts":[{"id":2,"evidence":"repeated retirement"}]}`))
}

func TestWorkingContext_InputReferencesAndReplacement(t *testing.T) {
	for _, update := range []string{
		`{"inputs":[{"action":"dismiss"}]}`,
		`{"inputs":[{"index":null,"action":"dismiss"}]}`,
		`{"inputs":[{"index":-1,"action":"dismiss"}]}`,
		`{"inputs":[{"index":1,"action":"dismiss"}]}`,
		`{"inputs":[{"index":0}]}`,
		`{"inputs":[{"index":0,"action":"dismiss"},{"index":0,"action":"dismiss"}]}`,
		`{"status":"active"}`,
		`{"status":"idle"}`,
		`null`,
	} {
		t.Run(update, func(t *testing.T) {
			ctx := context.Background()
			store, err := sqlite.Open(filepath.Join(t.TempDir(), "memory.db"))
			require.NoError(t, err)
			defer store.Close()
			work, err := store.LoadWorkingContext(ctx)
			require.NoError(t, err)
			work.Account = memory.WorkAccount{Status: memory.WorkCanceled, Progress: "old canceled task; stale plans"}
			work.Source = []string{"old task", "cancel old task"}
			work.Attempts = []memory.WorkAttempt{{ID: 1, Name: "artifact", Input: json.RawMessage(`{"old":true}`)}}
			_, err = store.SaveWorkingContext(ctx, work)
			require.NoError(t, err)
			session := agent.NewSession(agent.NewScriptedModel(workUpdate(t, true, update)), workTools{}, agent.Options{WorkingContext: store})
			_, err = session.Run(ctx, "Authorize a new fixture.")
			require.Error(t, err)
			saved, err := store.LoadWorkingContext(ctx)
			require.NoError(t, err)
			assert.Equal(t, work.Source, saved.Source)
			assert.Equal(t, work.Account, saved.Account)
			assert.Equal(t, work.Attempts, saved.Attempts)
			require.Equal(t, []string{"Authorize a new fixture."}, saved.Inputs)
			// The accepted reference names that preserved input; the new arrival is neutral.
			probe := &workProbe{inner: agent.NewScriptedModel(workUpdate(t, false, `{"status":"active","inputs":[{"index":0,"action":"replace"},{"index":1,"action":"dismiss"}]}`), workUpdate(t, true, `{}`))}
			session = agent.NewSession(probe, workTools{}, agent.Options{WorkingContext: store})
			_, err = session.Run(ctx, "weather notification")
			require.NoError(t, err)
			after := workRecord(t, probe.reasoning[1])
			assert.Equal(t, []string{"Authorize a new fixture."}, after.Source)
			assert.Empty(t, after.Inputs)
			assert.Empty(t, after.Account.Progress, "replacement retires stale subordinate progress")
			assert.Equal(t, work.Attempts, after.Attempts, "new work cannot erase an old uncertain effect")
		})
	}
}

func TestWorkingContext_OmittedCurationPreservesSource(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "memory.db"))
	require.NoError(t, err)
	defer store.Close()
	source := `Output exactly {"ok":true}, retain receipt, never repeat.`
	probe := &workProbe{inner: agent.NewScriptedModel(workUpdate(t, false, `{"progress":"working"}`), workUpdate(t, true, `{}`))}
	session := agent.NewSession(probe, workTools{}, agent.Options{WorkingContext: store})
	_, err = session.Run(ctx, source)
	require.NoError(t, err)
	supplied := workRecord(t, probe.reasoning[1])
	assert.Equal(t, []string{source}, supplied.Inputs)
	assert.Equal(t, "working", supplied.Account.Progress)
	saved, err := store.LoadWorkingContext(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{source}, saved.Inputs)
}

func TestWorkingContext_NoOpPreservesReturnedEvidenceOnRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	artifact := filepath.Join(t.TempDir(), "receipt")
	model := agent.NewScriptedModel(agent.ScriptedStep{Content: "no address"}, agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: "artifact", Input: json.RawMessage(`{"write":"receipt"}`)}}}, workUpdate(t, false, `{"status":null,"progress":null,"inputs":[],"resolved_attempts":[]}`), workUpdate(t, true, `{}`))
	runtime, err := workFactory(path, model, workTools{call: func(_ string, _ json.RawMessage) (json.RawMessage, error) {
		err := os.WriteFile(artifact, []byte("released once"), 0o600)
		return json.RawMessage(`{"receipt":"released once"}`), err
	}}).New(ctx, "test", nil)
	require.NoError(t, err)
	_, err = runtime.Session.Run(ctx, "Release once and retain receipt.")
	require.NoError(t, err)
	require.NoError(t, runtime.Close(ctx))
	bytes, err := os.ReadFile(artifact)
	require.NoError(t, err)
	require.Equal(t, "released once", string(bytes))
	probe := &workProbe{inner: agent.NewScriptedModel(agent.ScriptedStep{Content: "no address"}, workUpdate(t, true, `{"status":"completed","resolved_attempts":[{"id":2,"evidence":"saved tool result contains receipt released once"}]}`))}
	runtime, err = workFactory(path, probe, workTools{}).New(ctx, "test", nil)
	require.NoError(t, err)
	defer runtime.Close(ctx)
	require.Empty(t, runtime.Session.History())
	_, err = runtime.Session.Run(ctx, "status notification")
	require.NoError(t, err)
	initial := workRecord(t, probe.reasoning[0])
	require.Len(t, initial.Attempts, 1)
	require.NotNil(t, initial.Attempts[0].Result)
	assert.Contains(t, initial.Attempts[0].Result.Content, "released once")
	store, err := sqlite.Open(path)
	require.NoError(t, err)
	defer store.Close()
	final, err := store.LoadWorkingContext(ctx)
	require.NoError(t, err)
	assert.Empty(t, final.Attempts)
}
