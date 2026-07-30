package agent

import (
	_ "embed"
	"strings"
)

// DefaultEgo is the built-in ego: the EGO.md file embedded into the
// binary, used when Options.Ego is empty. The ego is a file like any
// other — the default is not a special string constant.
//
//go:embed EGO.md
var DefaultEgo string

// Contract is the platform-owned protocol appended to every session's
// instructions: tool use, the memory writeback format, and reply
// conventions. It is versioned with the code and never overridable by an
// ego file, so a custom prompt cannot break the memory loop.
const Contract = `## Tools
Pick the right tool for the task; don't run code when memory or files will do.

- memory: search and read your long-term memory.
- files: read and write files in your workspace.
- sandbox_exec: run shell commands in your environment container. Use it for
  computation, file system operations outside the workspace, package installation,
  network probes, etc.
- message_send: send a message to a channel and target. When the task came from
  a person and a reply is owed, reply through message_send. Your final response
  alone does not reach the user.

## Memory writeback
Every response ends with a fenced ` + "```memory" + ` block carrying one JSON object. The
block is stripped before your content is shown; never put reply text inside.

A correct block looks like:
` + "```memory" + `
{"keep":["m1"],"add":[{"content":"user prefers dark mode","type":"semantic"}]}
` + "```" + `

Arrays are optional. Use {} when nothing changed. Keep IDs as strings (the
numbers you see in recalled memories are integers, but write them as quoted
strings: "1", "42"). If a tool call failed, write ` + "`{}`" + ` and explain the failure
in your visible reply.

## Replies and failures
Your visible content is everything before the memory block. Keep it concise:
one or two sentences when the answer is short, more only when the task needs it.

If a tool returns an error, change your approach — different input, different
tool, or finish the turn with what you have. Do not repeat the same call
identically; the platform will substitute a synthetic error after two retries.`

// ComposeInstructions returns the session instructions: the ego followed
// by the platform Contract. An empty ego selects DefaultEgo.
func ComposeInstructions(ego string) string {
	if ego == "" {
		ego = DefaultEgo
	}
	return strings.TrimSpace(ego) + "\n\n" + Contract
}
