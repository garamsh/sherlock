package main

import (
	"context"
	"encoding/json"

	"github.com/garamsh/sherlock/internal/agent"
)

// mockModel is the MVP placeholder model (ADR 0004): a canned script
// that drives a fixed scenario through the agent's installed tools. It
// answers the understanding step, then runs shell_exec to write a note
// into the workspace and compute the answer through the workspace's Exec
// RPC, file_read to read that note back through the gRPC contract,
// message_send for the mouth, and ends the turn with the reply. The
// scenario creates what it reads, so it runs clean in any workspace. It
// exhausts after one message; a real model replaces it without interface
// changes.
func mockModel(_ string) agent.Model {
	return agent.NewScriptedModel(
		agent.ScriptedStep{
			ToolCalls: []agent.ToolCall{{
				Name:  agent.MemoryUnderstandTool,
				Input: json.RawMessage(`{"restatement":"Compute 6 * 7 and reply with it.","queries":["6 * 7"],"note":null}`),
			}},
		},
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
					`{"reply":"Replied via message_send and recorded the answer."}`,
				),
			}},
		},
	)
}

// mockEmbedder stands in for an embeddings endpoint offline: every text gets
// the same vector, so similarity ties and recall falls back to recency.
type mockEmbedder struct{}

func (mockEmbedder) Embed(context.Context, string) ([]float32, error) { return []float32{1}, nil }
