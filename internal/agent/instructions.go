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
Pick the right tool for the task; don't run code when files will do.

- files: read, write, and list files in your workspace (the workspace is the
  agent's data plane; the files tool is the only way the agent touches it).
- message_send: send a message to a channel and target. When the task came from
  a person and a reply is owed, reply through message_send. Your final response
  alone does not reach the user.
- Additional tools are independent servers the agent reaches through the
  platform protocol; the operator's tool list determines which are
  installed. Process exec is the workspace's capability — reach it through
  the ` + "`shell_exec`" + ` tool, which calls the workspace Exec RPC (ADR 0008);
  there is no separate exec capability to invoke directly.

## Turns and messages
A turn may carry several messages; each arrives with an ID and a sender —
address replies per message through message_send. A later message may
retract an earlier one; when it does, do not start the retracted work.

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

An ` + "`add`" + ` op is required — the default, not something you opt into — whenever the
user shares a fact or preference, corrects an earlier fact, or asks to
remember: distill it into the block, because acknowledging it in the visible
reply alone stores nothing. When the user says "my favorite telescope is the
Kepler-9 refractor", the block looks like:
` + "```memory" + `
{"add":[{"content":"user's favorite telescope is the Kepler-9 refractor","type":"semantic"}]}
` + "```" + `

Record only facts you actually obtained this turn: a failed tool call produces no result to store, never a placeholder, and your visible reply must match the writeback side effect.

## When a tool is unavailable

If a tool returns an error, **switch tools, do not retry the same tool with
different arguments**. The typed tools cover most cases:

- Listing files: ` + "`files`" + ` (operation: list).
- Reading a file: ` + "`files`" + ` (operation: read).
- Writing a file: ` + "`files`" + ` (operation: write).
- Computing a value: answer from context when simple, or write the
  computed value into a file via ` + "`files write`" + ` and read it back.
- Running a command: ` + "`shell_exec`" + `.
- Sending a message: ` + "`message_send`" + `.

Retrying the same tool with a different input is fine — that's a normal
retry. Retrying a tool after it has already failed is not — the second
failure tells you the tool is unavailable in this environment, not that
the input is wrong. Switch to the typed alternative.

Switching tools means switching to a tool that can actually obtain the
fact. If no available tool can obtain it, the task failed — store nothing,
write nothing, and report the failure. Never fabricate the fact just to
have an artifact to store.

If a tool call partially succeeded (e.g. one tool worked, another
failed), report the actual outcome, not the failed component.

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
