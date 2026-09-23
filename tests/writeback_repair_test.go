package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	agentmemory "github.com/garamsh/gagent/internal/agent/memory"
	"github.com/garamsh/gagent/internal/agent/openai"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
)

type workEvents []agent.Event

func (e *workEvents) Emit(_ context.Context, event agent.Event) { *e = append(*e, event) }

// The fixture retains actual response bytes and final reasoning records from
// 8556758 MiniMax calls.json, SHA256 5e95aca0a7e1371649cdde52755a32d5d0f14253ec982b33a61eaa1efd384967.
func TestWritebackRepair_RecordedPreparations(t *testing.T) {
	data, err := os.ReadFile("testdata/working-context-minimax.json")
	require.NoError(t, err)
	var preparations []struct {
		Label     string
		Responses [][]byte
		Work      memory.WorkingContext
	}
	require.NoError(t, json.Unmarshal(data, &preparations))
	require.Len(t, preparations, 2)
	for _, preparation := range preparations {
		for _, correction := range []string{"corrected", "still malformed", "operator call", "mixed calls"} {
			t.Run(preparation.Label+"/"+correction, func(t *testing.T) {
				ctx := context.Background()
				path := filepath.Join(t.TempDir(), "memory.db")
				store, err := sqlite.Open(path)
				require.NoError(t, err)
				defer store.Close()
				var terminal struct {
					Choices []struct {
						Message struct {
							ToolCalls []struct {
								Function struct{ Arguments string } `json:"function"`
							} `json:"tool_calls"`
						} `json:"message"`
					} `json:"choices"`
				}
				require.NoError(t, json.Unmarshal(preparation.Responses[len(preparation.Responses)-1], &terminal))
				arguments := terminal.Choices[0].Message.ToolCalls[0].Function.Arguments
				_, ops, warning, invalid := agent.ParseWriteback(arguments)
				require.NoError(t, warning)
				require.ErrorIs(t, invalid, agent.ErrMalformedWriteback)
				require.Nil(t, ops, "malformed fact keys are not an empty fact write")
				var fixed map[string]json.RawMessage
				require.NoError(t, json.Unmarshal([]byte(arguments), &fixed))
				fixed["memory_ops"] = json.RawMessage(`[]`)
				fixedBytes, err := json.Marshal(fixed)
				require.NoError(t, err)
				events := workEvents{}
				requests, effects := 0, 0
				endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var request struct {
						Messages []agent.Message
						Tools    []struct {
							Function struct {
								Name       string
								Parameters struct {
									Required   []string
									Properties map[string]struct{ Type json.RawMessage }
								}
							}
						}
					}
					require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
					for _, descriptor := range request.Tools {
						if descriptor.Function.Name == agent.MemoryWriteTool {
							assert.Contains(t, descriptor.Function.Parameters.Required, "working_context")
							assert.JSONEq(t, `["object","null"]`, string(descriptor.Function.Parameters.Properties["working_context"].Type))
						}
					}
					index := requests
					requests++
					w.Header().Set("Content-Type", "application/json")
					if index < len(preparation.Responses) {
						_, err := w.Write(preparation.Responses[index])
						require.NoError(t, err)
						return
					}
					require.Equal(t, len(preparation.Responses), index, "exactly one repair is allowed")
					require.Len(t, request.Tools, 1)
					assert.Equal(t, agent.MemoryWriteTool, request.Tools[0].Function.Name)
					assert.Contains(t, request.Messages[len(request.Messages)-2].Content, "no facts were written")
					facts, err := store.Active(ctx)
					require.NoError(t, err)
					require.Empty(t, facts)
					for _, event := range events {
						require.NotEqual(t, agent.EventWriteback, event.Kind)
					}
					input, name := string(fixedBytes), agent.MemoryWriteTool
					if correction == "still malformed" {
						input = arguments
					}
					if correction == "operator call" {
						name, input = "file_write", `{}`
					}
					calls := []map[string]any{{"id": "repair", "type": "function", "function": map[string]string{"name": name, "arguments": input}}}
					if correction == "mixed calls" {
						calls = append(calls, map[string]any{"id": "extra", "type": "function", "function": map[string]string{"name": "file_write", "arguments": "{}"}})
					}
					require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "tool_calls": calls}}}}))
				}))
				defer endpoint.Close()
				model, err := openai.New(openai.Config{BaseURL: endpoint.URL, Model: "recorded", APIKey: "local-fixture"})
				require.NoError(t, err)
				client := workTools{call: func(name string, input json.RawMessage) (json.RawMessage, error) {
					require.Less(t, effects, len(preparation.Work.Attempts), "repair must not repeat effects")
					attempt := preparation.Work.Attempts[effects]
					effects++
					require.Equal(t, attempt.Name, name)
					require.JSONEq(t, string(attempt.Input), string(input))
					if attempt.Result.Outcome == memory.WorkFailed {
						return nil, errors.New(attempt.Result.Content)
					}
					return json.RawMessage(attempt.Result.Content), nil
				}}
				session := agent.NewSession(model, client, agent.Options{WorkingContext: store, Writeback: agentmemory.Writeback{Svc: memory.NewService(store)}, Events: &events})
				reply, err := session.Run(ctx, preparation.Work.Source[0])
				if correction == "corrected" {
					require.NoError(t, err)
					assert.Contains(t, reply, "rates.json")
				} else {
					require.Error(t, err)
					assert.Empty(t, reply)
				}
				assert.Equal(t, len(preparation.Responses)+1, requests)
				assert.Equal(t, len(preparation.Work.Attempts), effects)
				saved, err := store.LoadWorkingContext(ctx)
				require.NoError(t, err)
				assert.Equal(t, preparation.Work.Source, saved.Source)
				assert.Equal(t, memory.WorkActive, saved.Account.Status)
				expectedAttempts, err := json.Marshal(preparation.Work.Attempts)
				require.NoError(t, err)
				actualAttempts, err := json.Marshal(saved.Attempts)
				require.NoError(t, err)
				assert.JSONEq(t, string(expectedAttempts), string(actualAttempts))
				facts, err := store.Active(ctx)
				require.NoError(t, err)
				assert.Empty(t, facts)
				failures, writes := 0, 0
				for _, event := range events {
					if event.Kind == agent.EventWritebackError {
						failures++
					}
					if event.Kind == agent.EventWriteback {
						writes++
						assert.Empty(t, event.Ops)
					}
				}
				assert.Equal(t, 1, failures)
				if correction == "corrected" {
					assert.Equal(t, 1, writes)
				} else {
					assert.Zero(t, writes)
				}
				require.NoError(t, store.Close())
				store, err = sqlite.Open(path)
				require.NoError(t, err)
				defer store.Close()
				probe := &workProbe{inner: agent.NewScriptedModel(workUpdate(t, true, `{"status":"canceled"}`))}
				session = agent.NewSession(probe, workTools{}, agent.Options{WorkingContext: store})
				_, err = session.Run(ctx, "Cancel the order; preserve the shortage report.")
				require.NoError(t, err)
				supplied := workRecord(t, probe.reasoning[0])
				assert.Equal(t, preparation.Work.Source, supplied.Source)
				assert.Equal(t, saved.Attempts, supplied.Attempts)
				assert.Equal(t, memory.WorkActive, supplied.Account.Status)
				canceled, err := store.LoadWorkingContext(ctx)
				require.NoError(t, err)
				assert.Equal(t, memory.WorkCanceled, canceled.Account.Status)
				assert.Equal(t, append(preparation.Work.Source, "Cancel the order; preserve the shortage report."), canceled.Source)
				assert.Equal(t, saved.Attempts, canceled.Attempts)
			})
		}
	}
}

func TestWritebackRepair_OptionalCurationRetainsEvidence(t *testing.T) {
	for _, update := range []string{"", `null`, `[]`, `{"progress":3}`, `{"arrival":[]}`, `{"status":"idle"}`, `{"arrival":"dismiss"}`} {
		t.Run(update, func(t *testing.T) {
			ctx := context.Background()
			store, err := sqlite.Open(filepath.Join(t.TempDir(), "memory.db"))
			require.NoError(t, err)
			defer store.Close()
			input := `{"reply":"Blocked awaiting rates","memory_ops":[]`
			if update != "" {
				input += `,"working_context":` + update
			}
			input += `}`
			events := workEvents{}
			effect := agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: "artifact", Input: json.RawMessage(`{"once":true}`)}}}
			probe := &workProbe{inner: agent.NewScriptedModel(effect, agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: agent.MemoryWriteTool, Input: json.RawMessage(input)}}})}
			session := agent.NewSession(probe, workTools{call: func(string, json.RawMessage) (json.RawMessage, error) {
				return json.RawMessage(`{"receipt":"one"}`), nil
			}}, agent.Options{WorkingContext: store, Events: &events})
			original := "Use harbor only; never repeat release."
			_, err = session.Run(ctx, original)
			require.NoError(t, err)
			before := workRecord(t, probe.reasoning[1])
			require.Equal(t, []string{original}, before.Source)
			require.Len(t, before.Attempts, 1)
			require.Equal(t, memory.WorkActive, before.Account.Status)
			saved, err := store.LoadWorkingContext(ctx)
			require.NoError(t, err)
			assert.Equal(t, before.Source, saved.Source)
			assert.Equal(t, before.Attempts, saved.Attempts)
			assert.Equal(t, before.Account, saved.Account)
			diagnostic := false
			for _, event := range events {
				diagnostic = diagnostic || event.Kind == agent.EventWorkingContextError
			}
			assert.True(t, diagnostic)
			probe.inner = agent.NewScriptedModel(workUpdate(t, true, `{"progress":"supplier changed"}`))
			_, err = session.Run(ctx, "Change supplier to ridge only.")
			require.NoError(t, err)
			supplied := workRecord(t, probe.reasoning[2])
			assert.Equal(t, saved.Source, supplied.Source)
			assert.Equal(t, saved.Attempts, supplied.Attempts)
			saved, err = store.LoadWorkingContext(ctx)
			require.NoError(t, err)
			assert.Equal(t, []string{original, "Change supplier to ridge only."}, saved.Source)
			assert.Equal(t, before.Attempts, saved.Attempts)
		})
	}
}

func TestWritebackRepair_ValidFactsAreRequiredBeforePersistence(t *testing.T) {
	for _, malformed := range []string{`{}`, `{"reply":"stored"}`, `{"reply":null,"memory_ops":[]}`, `{"memory_ops":[]}`, `{"reply":"stored","memory_ops":null}`, `{"reply":"stored","memory_ops":{}}`, `{"reply":"stored","memory_ops":[{"value":"missing address"}]}`, `{"reply":1,"memory_ops":[]}`} {
		t.Run(malformed, func(t *testing.T) {
			ctx := context.Background()
			store, err := sqlite.Open(filepath.Join(t.TempDir(), "memory.db"))
			require.NoError(t, err)
			defer store.Close()
			correction := `{"reply":"Stored supplier","memory_ops":[{"address":{"subject":"depot","question":"which supplier"},"value":"harbor","operation":null}]}`
			events := workEvents{}
			probe := &workProbe{inner: agent.NewScriptedModel(
				agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: agent.MemoryWriteTool, Input: json.RawMessage(malformed)}}},
				agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: agent.MemoryWriteTool, Input: json.RawMessage(correction)}}},
			)}
			session := agent.NewSession(probe, workTools{}, agent.Options{WorkingContext: store, Writeback: agentmemory.Writeback{Svc: memory.NewService(store)}, Events: &events})
			reply, err := session.Run(ctx, "Our supplier is harbor.")
			require.NoError(t, err)
			assert.Equal(t, "Stored supplier", reply)
			require.Len(t, probe.reasoning, 2, "malformed data requires correction")
			facts, err := store.Active(ctx)
			require.NoError(t, err)
			require.Len(t, facts, 1)
			assert.Equal(t, "harbor", facts[0].Value)
			failures, writes := 0, 0
			for _, event := range events {
				if event.Kind == agent.EventWritebackError {
					failures++
				}
				if event.Kind == agent.EventWriteback {
					writes++
					require.Len(t, event.Ops, 1)
				}
			}
			assert.Equal(t, 1, failures)
			assert.Equal(t, 1, writes)
		})
	}
}
