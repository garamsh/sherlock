package main

import (
	"encoding/json"

	"github.com/garamsh/gagent/internal/agent"
)

// mockModel is the MVP placeholder model (ADR 0005): a canned script that
// drives the PLAN §5.7 scenario through shell_exec + message_send +
// memory — compute the answer with the shell, reply to the sender
// through message_send, then end the turn with a writeback block. It
// exhausts after one message per environment; a real model replaces it
// without interface changes.
//
// After ADR 0012 the canned script exercises the four built-in tools:
// the shell_exec call computes the answer (echo 42), the message_send
// sends it, the memory tool records the question, and the writeback
// persists the outcome as an episodic memory.
func mockModel(_ string) agent.Model {
	return agent.NewScriptedModel(
		agent.ScriptedStep{
			ToolCalls: []agent.ToolCall{{
				Name: "memory",
				Input: json.RawMessage(
					`{"op":"store","content":"user asked: 6 * 7","type":"episodic","importance_score":0.5,"source":"mock"}`,
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
			Content: "Replied via message_send and stored the result.\n" +
				"```memory\n" +
				`{"add":[{"content":"answered 6 * 7 = 42","type":"episodic"}]}` +
				"\n```",
		},
	)
}
