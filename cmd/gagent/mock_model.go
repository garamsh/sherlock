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
// real model replaces it without interface changes.
//
// After ADR 0015 the memory tool is search-only: the canned script calls
// memory search, the shell_exec call computes the answer (echo 42), the
// message_send sends it, and the writeback records the question as an
// episodic memory — the writeback is the single write path.
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
