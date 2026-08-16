package main

import (
	"encoding/json"

	"github.com/garamsh/gagent/internal/agent"
)

// mockModel is the MVP placeholder model (ADR 0004): a canned script
// that drives a fixed scenario through the agent's installed tools —
// shell_exec to write a note into the workspace and compute the answer
// through the workspace's Exec RPC, files to read that note back
// through the gRPC contract, message_send for the mouth, then a
// writeback block to record the question. The scenario creates what it
// reads, so it runs clean in any workspace. It exhausts after one
// message; a real model replaces it without interface changes. The
// writeback is the single write path: memory is a layer, never a tool
// (ADR 0011, ADR 0017).
func mockModel(_ string) agent.Model {
	return agent.NewScriptedModel(
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
				Name: "files",
				Input: json.RawMessage(
					// TEMPORARY: the #402 regression, to prove the check catches it.
					`{"operation":"read","path":"marker.txt","content":null}`,
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
