package main

import (
	"encoding/json"

	"github.com/garamsh/gagent/internal/agent"
)

// mockModel is the MVP placeholder model (ADR 0005): a canned script that
// drives the PLAN §5.7 scenario — compute in the sandbox, reply to the
// sender through message_send, then write the result back as a new
// episodic memory. It exhausts after one message per environment; a real
// model replaces it without interface changes.
func mockModel(_ string) agent.Model {
	return agent.NewScriptedModel(
		agent.ScriptedStep{
			ToolCalls: []agent.ToolCall{{
				Name:  "sandbox_exec",
				Input: json.RawMessage(`{"command":"echo $((6 * 7))"}`),
			}},
		},
		agent.ScriptedStep{
			ToolCalls: []agent.ToolCall{{
				Name:  "message_send",
				Input: json.RawMessage(`{"channel":"http","target":"sender","content":"The sandbox computed 42."}`),
			}},
		},
		agent.ScriptedStep{
			Content: "Computed the answer in the sandbox.\n" +
				"```memory\n" +
				`{"add":[{"content":"The sandbox computed 6 * 7 = 42 in the environment container","type":"episodic"}]}` +
				"\n```",
		},
	)
}
