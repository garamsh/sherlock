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
	return memory.WorkAccount{Status: status, Goal: goal, Constraints: "use supplier Cedar; release only on arrival", Decisions: "retain the approved design", Progress: "design independently verified", Remaining: remaining, Uncertainty: "delivery time unknown"}
}

func workCheckpoint(t *testing.T, account memory.WorkAccount, reconciliation string) agent.ScriptedStep {
	t.Helper()
	doc := struct {
		memory.WorkAccount
		Reconciliation string `json:"reconciliation"`
	}{account, reconciliation}
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

func TestWorkingContext_RestartAndCompaction(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	account := currentAccount(memory.WorkActive, "deliver the approved fixture", "await arrival, then release")
	var steps []agent.ScriptedStep
	for range 4 {
		steps = append(steps, agent.ScriptedStep{Content: "no address"}, workCheckpoint(t, account, ""), workFinish(t, account, ""))
	}
	probe := &workProbe{inner: agent.NewScriptedModel(steps...)}
	runtime, err := workFactory(path, probe, workTools{}).New(ctx, "test", nil)
	require.NoError(t, err)
	original := "Deliver the approved fixture using supplier Cedar; release only on arrival."
	_, err = runtime.Session.Run(ctx, original)
	require.NoError(t, err)
	for _, arrival := range []string{"still waiting", "weather update", "schedule unchanged"} {
		_, err = runtime.Session.Run(ctx, arrival)
		require.NoError(t, err)
	}
	for _, message := range runtime.Session.History() {
		require.NotContains(t, message.Content, original, "original task has left the real conversation window")
	}
	require.LessOrEqual(t, len(runtime.Session.History()), 5)
	latest := workRecord(t, probe.reasoning[len(probe.reasoning)-1])
	assert.Equal(t, account, latest.Account)
	require.NoError(t, runtime.Close(ctx))

	restoredProbe := &workProbe{inner: agent.NewScriptedModel(agent.ScriptedStep{Content: "no address"}, workCheckpoint(t, account, ""), workFinish(t, account, ""))}
	restarted, err := workFactory(path, restoredProbe, workTools{}).New(ctx, "test", nil)
	require.NoError(t, err)
	defer restarted.Close(ctx)
	require.Empty(t, restarted.Session.History(), "new process-session history has no original messages")
	_, err = restarted.Session.Run(ctx, "arrival notification")
	require.NoError(t, err)
	restored := workRecord(t, restoredProbe.reasoning[0])
	assert.Equal(t, account, restored.Account)
	assert.Equal(t, []string{"arrival notification"}, restored.Inputs)
	require.Len(t, restoredProbe.recall, 1)
	recallJSON, err := json.Marshal(restoredProbe.recall[0])
	require.NoError(t, err)
	assert.Contains(t, string(recallJSON), account.Goal, "recall resolution also receives current work")
	assert.Contains(t, string(recallJSON), "supplier Cedar")
}

func TestWorkingContext_LatestIntentAndTerminalControls(t *testing.T) {
	for _, status := range []memory.WorkStatus{memory.WorkCanceled, memory.WorkCompleted} {
		t.Run(string(status), func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "memory.db")
			original := currentAccount(memory.WorkActive, "prepare the old fixture", "await arrival")
			changed := currentAccount(memory.WorkActive, "prepare the replacement fixture", "await inspection")
			terminal := currentAccount(status, changed.Goal, "")
			for _, step := range []struct {
				input   string
				account memory.WorkAccount
			}{
				{"prepare old fixture", original}, {"replace the task with the replacement fixture", changed}, {"owner terminates this work", terminal},
			} {
				model := agent.NewScriptedModel(agent.ScriptedStep{Content: "no address"}, workCheckpoint(t, step.account, ""), workFinish(t, step.account, ""))
				runtime, err := workFactory(path, model, workTools{}).New(ctx, "test", nil)
				require.NoError(t, err)
				_, err = runtime.Session.Run(ctx, step.input)
				require.NoError(t, err)
				require.NoError(t, runtime.Close(ctx))
			}
			probe := &workProbe{inner: agent.NewScriptedModel(agent.ScriptedStep{Content: "no address"}, workCheckpoint(t, terminal, ""), workFinish(t, terminal, ""))}
			runtime, err := workFactory(path, probe, workTools{}).New(ctx, "test", nil)
			require.NoError(t, err)
			_, err = runtime.Session.Run(ctx, "arrival notification")
			require.NoError(t, err)
			got := workRecord(t, probe.reasoning[0])
			assert.Equal(t, terminal, got.Account, "terminal identity survives; this is not an empty-context refusal")
			assert.NotEqual(t, original.Goal, got.Account.Goal)
			require.NoError(t, runtime.Close(ctx))

			authorized := currentAccount(memory.WorkActive, "newly authorized fixture", "inspect the new fixture")
			probe = &workProbe{inner: agent.NewScriptedModel(agent.ScriptedStep{Content: "no address"}, workCheckpoint(t, authorized, ""), workFinish(t, authorized, ""))}
			runtime, err = workFactory(path, probe, workTools{}).New(ctx, "test", nil)
			require.NoError(t, err)
			_, err = runtime.Session.Run(ctx, "I authorize a new fixture now")
			require.NoError(t, err)
			assert.Equal(t, terminal, workRecord(t, probe.reasoning[0]).Account)
			assert.Equal(t, authorized, workRecord(t, probe.reasoning[1]).Account)
			require.NoError(t, runtime.Close(ctx))
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

	account := currentAccount(memory.WorkActive, "write release marker", "verify marker before any repeat")
	completed := currentAccount(memory.WorkCompleted, account.Goal, "")
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
	assert.Empty(t, final.Attempts)
	assert.Contains(t, final.Account.Decisions, "Read release marker")
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
	for _, failAt := range []int{0, 1, 2, 3, 4, 5, 6, 7} {
		t.Run(string(rune('0'+failAt)), func(t *testing.T) {
			ctx := context.Background()
			store, err := sqlite.Open(filepath.Join(t.TempDir(), "memory.db"))
			require.NoError(t, err)
			defer store.Close()
			boundary := &workFailure{WorkingContextStorer: store, failAt: failAt}
			account := currentAccount(memory.WorkActive, "write marker", "verify marker")
			model := agent.NewScriptedModel(workCheckpoint(t, account, ""), agent.ScriptedStep{ToolCalls: []agent.ToolCall{{ID: "first", Name: "artifact", Input: json.RawMessage(`{}`)}, {ID: "second", Name: "artifact", Input: json.RawMessage(`{"second":true}`)}}}, workFinish(t, account, ""))
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
			assert.Equal(t, max(0, (failAt-2)/2), effects)
			saved, err := store.LoadWorkingContext(ctx)
			require.NoError(t, err)
			if failAt == 4 {
				require.Len(t, saved.Attempts, 1)
				assert.Nil(t, saved.Attempts[0].Result)
			}
			if failAt == 5 {
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
			if failAt == 4 {
				require.Len(t, workRecord(t, recovered).Attempts, 1)
				assert.Nil(t, workRecord(t, recovered).Attempts[0].Result)
			}

		})
	}
}

// fixtureCheckpoint states the work in the existing offline messaging scenarios.
func fixtureCheckpoint(status string) agent.ScriptedStep {
	remaining := ""
	if status == "active" {
		remaining = "send the scripted reply"
	}
	return agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: agent.WorkingContextTool, Input: json.RawMessage(`{"status":"` + status + `","goal":"reply to the test sender","constraints":"use the test channel","decisions":"follow the scripted scenario","progress":"","remaining":"` + remaining + `","uncertainty":"","reconciliation":""}`)}}}
}

func TestWorkingContext_PendingIntentSurvivesBeforeInterpretation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	account := currentAccount(memory.WorkActive, "prepare fixture", "await authorization")
	runtime, err := workFactory(path, agent.NewScriptedModel(agent.ScriptedStep{Content: "no address"}, workFinish(t, account, "")), workTools{}).New(ctx, "test", nil)
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
	canceled := currentAccount(memory.WorkCanceled, account.Goal, "")
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
			assert.Equal(t, 1, effects, "the call before checkpoint was refused; the later authorized call ran")
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
			assert.Contains(t, feedback[len(feedback)-1].Content, "Checkpoint saved; continue acting or finish.")
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
			attempt := memory.WorkAttempt{Name: "artifact", Input: json.RawMessage(`{"action":"release"}`)}
			if outcome != "" {
				attempt.Result = &memory.WorkResult{Outcome: outcome, Content: "timeout after possible effect"}
			}
			work.Attempts = []memory.WorkAttempt{attempt}
			_, err = store.SaveWorkingContext(ctx, work)
			require.NoError(t, err)
			completed := currentAccount(memory.WorkCompleted, work.Account.Goal, "")
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
