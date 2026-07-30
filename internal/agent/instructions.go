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
- message_send: send a message to a channel and target. When the task came from
  a person and a reply is owed, reply through message_send. Your final response
  alone does not reach the user.
- shell_exec: run a shell command on the workspace. Use this when the typed tools
  are too narrow — running the project's tests, computing with awk/jq, listing
  files matching a pattern. The command is a single string passed to sh -c; pass
  the full shell line, not pre-split arguments
  (e.g. ` + "`command: 'find . -name \"*.go\" -size +100k'`" + `).

## Memory: tool call vs writeback
The memory tool's ` + "`store`" + ` op and the writeback ` + "`add`" + ` op both create a memory, but
through different interfaces with different fields (QA #103):

- Call the memory tool with ` + "`op: store`" + ` when the user-facing task requires it (e.g.
  "remember this"). The schema requires ` + "`op`" + `, ` + "`content`" + `, ` + "`type`" + `,
  ` + "`importance_score`" + `, and ` + "`source`" + ` — you must supply all five.
- Write the writeback ` + "`add`" + ` block for every fact you learned this turn, whether
  you used the tool or not. ` + "`importance_score`" + ` and ` + "`source`" + ` are auto-filled
  by the platform; supply only ` + "`content`" + ` and ` + "`type`" + `.

If you omit ` + "`importance_score`" + ` or ` + "`source`" + ` from a ` + "`store`" + ` tool call, the tool
rejects the input. Call the tool only when the task needs to write outside
the writeback flow.

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
