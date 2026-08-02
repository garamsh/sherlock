package main

import (
	"encoding/json"

	"github.com/garamsh/gagent/internal/agent"
)

// mockModel is the MVP placeholder model (ADR 0005): a canned script that
// drives a fixed scenario through memory search + shell_exec +
// message_send — search long-term memory, compute the answer with the
// shell, reply to the sender through message_send, then end the turn with
// a writeback block. It exhausts after one message per environment; a
// real model replaces it without interface changes. The writeback is
// the single write path: the memory tool is search-only (ADR 0015).
func mockModel(_ string) agent.Model {
	return agent.NewScriptedModel(
		agent.ScriptedStep{
			ToolCalls: []agent.ToolCall{{
				Name: "memory",
				Input: json.RawMessage(
					`{"op":"search","query":"6 * 7"}`,
				),
			}},
		},
		agent.ScriptedStep{
			ToolCalls: []agent.ToolCall{{
				Name: "shell_exec",
				Input: json.RawMessage(
					`{"command":"echo 42"}`,
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
