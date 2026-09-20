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
// structured response recording the answer at its address. The scenario
// creates what it reads, so it runs clean in any workspace. It exhausts
// after one message; a real model replaces it without interface changes.
// The writeback is the single write path: memory is a layer, never a
// tool (ADR 0011, ADR 0017).
func mockModel(_ string) agent.Model {
	return agent.NewScriptedModel(
		// The read step's call, which every turn makes before the act
		// loop (ADR 0019). This one places no address: the canned
		// scenario's first turn asks a question the empty store holds
		// nothing for.
		agent.ScriptedStep{Content: "no address"},
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: agent.WorkingContextTool, Input: json.RawMessage(`{"status":"active","goal":"compute six times seven and send the result","constraints":"write and read question.txt","decisions":"use the installed workspace tools","progress":"","remaining":"compute, read, send and record the answer","uncertainty":"","reconciliation":""}`)}}},
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
					`{"operation":"read","path":"question.txt","content":null}`,
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
					`{"reply":"Replied via message_send and recorded the answer.","working_context":{"status":"completed","goal":"compute six times seven and send the result","constraints":"write and read question.txt","decisions":"used installed workspace tools","progress":"computed 42, read the question and sent the answer","remaining":"","uncertainty":"","reconciliation":""},` +
						`"memory_ops":[{"address":{"subject":"user",` +
						`"question":"what is 6 * 7","restriction":null},` +
						`"value":"42","operation":null}]}`,
				),
			}},
		},
	)
}
