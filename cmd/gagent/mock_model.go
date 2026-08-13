package main

import (
	"encoding/json"

	"github.com/garamsh/gagent/internal/agent"
)

// mockModel is the MVP placeholder model (ADR 0004): a canned script
// that drives a fixed scenario through the agent's installed tools —
// shell_exec to compute the answer through the workspace's Exec RPC,
// message_send for the mouth, then a writeback block to record the
// question. It exhausts after one message; a real model replaces it
// without interface changes. The writeback is the single write path:
// memory is a layer, never a tool (ADR 0011, ADR 0018).
func mockModel(_ string) agent.Model {
	return agent.NewScriptedModel(
		agent.ScriptedStep{
			ToolCalls: []agent.ToolCall{{
				Name: "shell_exec",
				Input: json.RawMessage(
					`{"command":"ls; echo $((6 * 7))"}`,
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
			Content: "Replied via message_send and recorded the question.\n" +
				"```memory\n" +
				`{"add":[{"content":"user asked: 6 * 7","type":"episodic"}]}` +
				"\n```",
		},
	)
}
