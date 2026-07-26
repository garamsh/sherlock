package agent

import (
	_ "embed"
	"strings"
)

// DefaultPersona is the built-in persona: the AGENT.md file embedded into the
// binary, used when Options.Persona is empty. The persona is a file like any
// other — the default is not a special string constant.
//
//go:embed AGENT.md
var DefaultPersona string

// Contract is the platform-owned protocol appended to every session's
// instructions: tool use, the memory writeback format, and reply
// conventions. It is versioned with the code and never overridable by a
// persona file, so a custom prompt cannot break the memory loop.
const Contract = `## Tools
You act through the tools provided with each request. Call a tool when the task needs it:
- memory: search and read your long-term memory directly.
- files: read and write files in your workspace.
- sandbox_exec: run shell commands inside your environment container.
- message_send: send a message to a person on a channel. When the task comes from a person and a reply is owed, reply through message_send; your final response alone does not reach them.

## Memory writeback
Every response you produce — with or without tool calls — must end with a fenced ` + "```memory" + ` block carrying one JSON object with optional arrays:
- "keep": IDs of recalled memories that remain valid, unchanged.
- "update": [{"id": "...", "content": "..."}] supersedes a recalled memory.
- "delete": IDs of recalled memories to retire.
- "add": [{"content": "...", "type": "episodic|semantic"}] persists a new fact worth remembering.
Use an empty object {} when nothing changed. The block is stripped before your content is shown; never put reply text inside it.

## Replies
Your visible content is everything before the memory block. Keep it concise and factual.`

// ComposeInstructions returns the session instructions: the persona followed
// by the platform Contract. An empty persona selects DefaultPersona.
func ComposeInstructions(persona string) string {
	if persona == "" {
		persona = DefaultPersona
	}
	return strings.TrimSpace(persona) + "\n\n" + Contract
}
