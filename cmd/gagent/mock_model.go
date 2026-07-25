package main

import (
	"encoding/json"

	"github.com/garamsh/gagent/internal/agent"
)

// mockModel is the MVP placeholder model (ADR 0005): a canned script that
// acknowledges each message through message_send and finishes the turn
// with a writeback block. It exhausts after one message per environment;
// a real model replaces it without interface changes.
func mockModel(_ string) agent.Model {
	return agent.NewScriptedModel(
		agent.ScriptedStep{
			ToolCalls: []agent.ToolCall{{
				Name:  "message_send",
				Input: json.RawMessage(`{"channel":"http","target":"sender","content":"Message received."}`),
			}},
		},
		agent.ScriptedStep{
			Content: "Acknowledged the inbound message.\n" +
				"```memory\n" +
				`{"add":[{"content":"exchanged a message over the http channel","type":"episodic"}]}` +
				"\n```",
		},
	)
}
