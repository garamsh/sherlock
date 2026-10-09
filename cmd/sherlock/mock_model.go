package main

import (
	"context"
	"encoding/json"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/tool"
)

// mockModel is the MVP placeholder model (ADR 0004): a canned script
// that drives a fixed scenario through the agent's installed tools —
// shell_exec to write a note into the workspace and compute the answer
// through the workspace's Exec RPC, file_read to read that note back
// through the gRPC contract, message_send for the mouth, then the reply
// with an empty note. The scenario creates what it reads, so it runs
// clean in any workspace. It exhausts after one message; a real model
// replaces it without interface changes.
//
// A request offering memory_summary is episode synthesis, not a turn: it
// gets one fixed valid call and leaves the script where it was, so a
// batch's synthesis succeeds at any point.
type mockModel struct{ script agent.Model }

// mockSummaryCall is the one memory_summary call the mock answers every
// synthesis request with.
var mockSummaryCall = agent.ToolCall{
	ID:    "mock-summary",
	Name:  "memory_summary",
	Input: json.RawMessage(`{"summary":"A conversation with the offline mock model.","entities":[]}`),
}

func (m mockModel) Chat(ctx context.Context, messages []agent.Message, tools []tool.Descriptor) (agent.Response, error) {
	for _, d := range tools {
		if d.Name == mockSummaryCall.Name {
			return agent.Response{ToolCalls: []agent.ToolCall{mockSummaryCall}}, nil
		}
	}
	return m.script.Chat(ctx, messages, tools)
}

// newMockModel returns the mock with its scenario at the first step.
func newMockModel(_ string) agent.Model {
	return mockModel{script: agent.NewScriptedModel(
		agent.ScriptedStep{
			ToolCalls: []agent.ToolCall{{
				Name: "shell_exec",
				Input: json.RawMessage(
					`{"command":"echo '6 * 7' > question.txt; echo $((6 * 7))","timeout_nanos":null}`,
				),
			}},
		},
		agent.ScriptedStep{
			ToolCalls: []agent.ToolCall{{
				Name: "file_read",
				Input: json.RawMessage(
					`{"path":"question.txt"}`,
				),
			}},
		},
		agent.ScriptedStep{
			ToolCalls: []agent.ToolCall{{
				Name: "message_send",
				Input: json.RawMessage(
					`{"channel":"http","target":"sender","content":"42"}`,
				),
			}},
		},
		agent.ScriptedStep{
			ToolCalls: []agent.ToolCall{{
				Name: agent.MemoryWriteTool,
				Input: json.RawMessage(
					`{"reply":"Replied via message_send and recorded the answer.","note":""}`,
				),
			}},
		},
	)}
}

// mockEmbedder stands in for an embeddings endpoint offline: every text gets
// the same vector, so similarity ties and recall falls back to recency.
type mockEmbedder struct{}

func (mockEmbedder) Embed(context.Context, string) ([]float32, error) { return []float32{1}, nil }
