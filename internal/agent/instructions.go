package agent

import (
	_ "embed"
	"strings"
)

// DefaultEgo is the built-in ego: the ego.md file embedded into the
// binary, used when Options.Ego is empty. The ego is a file like any
// other — the default is not a special string constant.
//
//go:embed ego.md
var DefaultEgo string

// addressContract is the platform's account of what an address is: the
// three fields a fact is held under, and the rules for choosing them.
//
// It is one constant because two halves read it. The writeback section
// below asks the model to form an address from a statement; the read
// step asks it to form the same address from a question, and ADR 0019
// requires that the read carry nothing the write does not — "a read step
// that addresses better than the write addresses lands on addresses the
// write never wrote". Sharing the text is that requirement made
// structural rather than left to discipline: neither half can grow a
// rule the other lacks.
const addressContract = `- ` + "`address.subject`" + ` — who or what the statement is about. In "Dmitri flies
  with the northern-arc airline" that is Dmitri and not the airline: the
  statement is about him, and the airline is what it says about him. The
  subject is never the value.
- ` + "`address.question`" + ` — the question this fact answers, phrased the way
  someone who does not yet know the answer would ask it. That is the whole
  test. Whoever looks for this fact later has the question and not the
  answer, so the answer never appears in the question, and the question is
  worded the way it will be asked rather than the way this statement
  happened to put it. Not a topic noun: "what is the user's favorite
  telescope" is an address, "telescopes" is not.
  It asks about the subject. "who leads the northern-arc project" is not a
  question about Dmitri even when Dmitri is the answer — it is a question
  about the project, and it belongs to a different address.
  Where the statement says what kind of thing the answer is, ask for that
  kind: "Dmitri flies with the northern-arc airline" answers "which airline
  does Dmitri fly with". Where it says no kind, take the question word from
  what the answer is — ` + "`who`" + ` for a person or an organisation, ` + "`where`" + ` for a
  place, ` + "`what`" + ` otherwise. Never take it from the statement's verb, which
  describes the doing and not the answer.
- ` + "`address.restriction`" + ` — what the statement narrows that question to, in its
  own words, or null. Most statements narrow nothing; a restriction is
  material limiting when or for what the fact holds, never the subject and
  never the value. It goes in this field and never inside the question: the
  question stays the one a reader would ask, and this is what narrows it.`

// Contract is the platform-owned protocol appended to every session's
// instructions: tool use, the memory writeback format, and reply
// conventions. It is versioned with the code and never overridable by an
// ego file, so a custom prompt cannot break the memory loop. The
// writeback section is the prose half of ResponseSchema, which the
// provider enforces; the shape is stated in both because a model reads
// one and the API the other.
const Contract = `## Tools
Pick the right tool for the task; don't run code when files will do.

- file_read: read a file in your workspace.
- file_write: create or replace a file in your workspace.
- file_list: list a directory in your workspace.
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
End every turn by calling ` + "`memory_write`" + `. Its arguments are ` + "`reply`" + `,
` + "`memory_ops`" + ` and the complete final ` + "`working_context`" + ` account. ` + "`reply`" + ` is what the person reads — nothing else reaches them.
` + "`memory_ops`" + ` is the list of facts this turn states, and it is never omitted:
an empty list is your answer that the turn stated none. A turn is not finished
until you make that call, so make it even when you have nothing to record.

A fact is held at the address of the question it answers. Each entry gives:

` + addressContract + `
- ` + "`value`" + ` — the answer to that question, on its own.
- ` + "`operation`" + ` — ` + "`\"replace\"`" + ` where the value ends what that address already
  holds, ` + "`\"join\"`" + ` where it stands beside it, or null where the statement
  answers a question of its own. When you are unsure, write ` + "`\"join\"`" + `: a wrong
  join is repaired by the next correct value at that address, and a wrong
  replacement is repaired by nothing.

Both examples below assume the current-work account is idle. An unrelated fact
must preserve an existing work account, including its status and constraints.

When the user says "my favorite telescope is the Kepler-9 refractor":

{"reply":"Noted.","working_context":{"status":"idle","goal":"","constraints":"","decisions":"","progress":"","remaining":"","uncertainty":"","reconciliation":""},"memory_ops":[{"address":{"subject":"user","question":"what is the user's favorite telescope","restriction":null},"value":"the Kepler-9 refractor","operation":null}]}

A statement about someone else, carrying a restriction, works the same way.
When the user says "In winter Dmitri flies with the northern-arc airline":

{"reply":"Noted.","working_context":{"status":"idle","goal":"","constraints":"","decisions":"","progress":"","remaining":"","uncertainty":"","reconciliation":""},"memory_ops":[{"address":{"subject":"Dmitri","question":"which airline does Dmitri fly with","restriction":"in winter"},"value":"the northern-arc airline","operation":null}]}

The subject is Dmitri because the statement is about him; the question asks
for an airline because the statement says that is what the answer is; "in
winter" is the restriction and is
not in the question, so the same question reaches the fact whether or not a
reader knows about the winter.

An entry is required — the default, not something you opt into — whenever the
user shares a fact or preference, corrects an earlier fact, or asks to
remember: acknowledging it in ` + "`reply`" + ` alone stores nothing. A correction is
another entry at the same address with ` + "`\"replace\"`" + `; two facts that are both
true get either different questions or different restrictions, and ` + "`\"join\"`" + `
where they genuinely answer the same one.

Record only facts you actually obtained this turn: a failed tool call produces no result to store, never a placeholder, and your reply must match the writeback side effect.

## When a tool is unavailable

If a tool returns an error, **switch tools, do not retry the same tool with
different arguments**. The typed tools cover most cases:

- Listing files: ` + "`file_list`" + `.
- Reading a file: ` + "`file_read`" + `.
- Writing a file: ` + "`file_write`" + `.
- Computing a value: answer from context when simple, or write the
  computed value into a file via ` + "`file_write`" + ` and read it back.
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
Your visible content is the ` + "`reply`" + ` field. Keep it concise: one or two
sentences when the answer is short, more only when the task needs it.

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

const workingContextContract = `## Durable current work
The current-work record supplied with each reasoning call is authoritative across
restart and compaction. Recalled facts and old conversation do not authorize work.
Call working_context alone before the first operator tool calls of a turn.
End the turn with memory_write, carrying the complete final account in its
working_context field; no separate final checkpoint is needed. Supply a complete replacement account of the actual goal,
constraints, decisions, verified progress, remaining work and uncertainty.
Incorporate every pending input in order: newer intent supersedes older intent.
Keep unfinished work active when a turn merely pauses or awaits an event. Record
canceled or completed work with its goal and no remaining work. A neutral arrival
notification does not authorize restarting terminal work; only an explicit new
authorization does. Idle means no work was authorized, not lost or canceled work.
Tool attempts with no result may already have changed the world. Verify surviving
artifacts before repeating effects. A returned error may also hide a partial effect.
Do not claim completion from an intended action or an absent result. Reconciliation
must name observed evidence resolving ALL unknown attempts; leave it empty until
that evidence exists. Unknown attempts then remain visible across checkpoints.
Do not copy every old event into the account. Keep only what continuation needs;
write distinct useful long-term facts through memory_write as usual.
`
