# How agents understand input and compose context

Records the source survey for issue #860: how five established agents understand a user's input and assemble their context, and the candidate structures for sherlock's query understanding and full-context layer that follow from it. It makes no production decision. §Experiment plan was the input to issue #862's preregistration; §Experiment (issue #862), §Confirmatory run (issue #864) and §Unmixed-batch run (issue #870) record the runs.

## Contents
- Question and method
- Sherlock at `ebecac8`
- The agents
- Comparison
- Candidate structures for sherlock
- Experiment plan
- Experiment (issue #862)
- Confirmatory run (issue #864)
- Unmixed-batch run (issue #870)
- Position on #810 and #812
- Limits

## Question and method

How do established agents interpret an input before or while they retrieve, which context layers do they keep beside the conversation, who writes each and when, and how do they keep the prompt within budget? What is the smallest change to sherlock that gives it both things the owner asked for in #860: a step that understands the question with its full context, and a full-context layer beside the conversation list?

- **Method.** Reading public documentation and source code only. No agent was run against a model and no paid call was made.
- **Checked against.** Each agent's subsection names the release tag and commit, or the installed version, its claims hold for. Every URL in this file was accessed on 2026-09-26. Code paths within an agent's subsection are relative to its repository at the stated tag.
- **Sherlock.** Claims about this repository hold for `ebecac8` (origin/dev, #859 merged) and name the file read.
- **What a source could not settle** is listed per agent under "Not settled". A statement absent from a subsection was not looked for, not found absent, unless the subsection says how it was searched.

## Sherlock at `ebecac8`

What a turn sees, in the order the session builds it (`internal/agent/service.go` `Run`, `internal/gateway/runtime.go`):

1. The leading system message: the operator ego followed by the platform contract (`internal/agent/instructions.go` `ComposeInstructions`). It is injected once into an empty history and kept by the compactor.
2. The rolling window: restored visible user and assistant messages after a restart, then everything the session appended since, including tool results. The compactor drops the oldest entries past 128 messages and writes nothing (`internal/agent/memory/compaction.go`).
3. Per turn, a system message holding the task and the recalled memories (`frameContext`): the two newest mid episodes, up to five more by cosine similarity between the raw task text and each summary, and up to four pending source previews (`internal/agent/memory/conversation.go`). Then the task as a user message.
4. Tool results in the act loop, including `memory_inspect`, which reads one source message by ID (`internal/agent/inspect.go`).
5. The terminal `memory_write` call. Its `memory_ops` field is still required by the schema, but the runtime wires no sink for it (`internal/agent/writeback.go`; `runtime.go` sets no `Writeback` option).

Four properties of that shape matter to the design section. Each was read from the code; none was measured.

- **Nothing interprets the input.** Recall embeds the raw task text before the model sees the turn, and the act loop has no call that searches again (ADR 0019 §Read-policy limits).
- **No layer outlives the window except recall.** There is no instruction file the agent writes, no user profile, no working-state record and no compaction summary.
- **Recall blocks accumulate.** Each turn appends a new frame block to history and none is removed, so earlier turns' candidates stay in the window until it drops them. They count against the 128-message bound. A restart restores only visible messages, so after a restart the blocks are gone: the model sees different context for the same conversation depending on whether the process restarted.
- **Two recall slots overlap the window.** With the default window, the two newest mid episodes and the pending previews cover the most recent visible messages, which the window usually still holds. That stops being true when tool results and frame blocks fill the 128 entries; how often that happens was not measured.

Two reserved paths are not wired into the runtime: the addressed read step (`internal/agent/recall.go`, 117 lines; `internal/agent/memory/recall.go`, 131 lines) and the addressed writeback sink (`internal/agent/memory/writeback.go`, 58 lines). `rg` over non-test Go files under `internal/` and `cmd/` finds no construction of `agentmemory.Recall` or `agentmemory.Writeback`; the same search finds the `ConversationRecall` and `Compactor` constructions in `runtime.go`, so it does match what is wired.

## The agents

### Claude Code

Checked against: the installed Claude Code 2.1.283 (`claude --version`; bundle `~/.local/share/claude/versions/2.1.283`, SHA-256 `1859583ce32920595c61ef868bee52e1b1594f7486db209935e01f1e5e804ae2`) and the docs at `https://code.claude.com/docs/en/`. Claude Code is closed source, so bundle claims come from strings found with `rg -a`.

- **Input.** There is no rewrite and no mandatory planning step. The loop is "gather context, take action, verify results", and file search and reads are tools the model calls ([how Claude Code works](https://code.claude.com/docs/en/how-claude-code-works)). Plan mode is a permission mode the user selects. Anthropic describes the split as "CLAUDE.md files are naively dropped into context up front, while primitives like glob and grep allow it to navigate its environment and retrieve files just-in-time" ([Effective context engineering for AI agents](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents)).
  - The bundle holds a per-prompt memory selector: a side model call that picks up to five memory files for the latest user message and attaches them as `relevant_memories`. It is gated by a remote flag or an environment variable.
- **Layers in order.**
  1. System prompt: core instructions, tool definitions and the environment block ([context window](https://code.claude.com/docs/en/context-window), [prompt caching](https://code.claude.com/docs/en/prompt-caching)).
  2. One user message wrapped as a system reminder: managed policy, user `~/.claude/CLAUDE.md`, project `CLAUDE.md` files from the filesystem root down to the working directory, `CLAUDE.local.md`, then the first 200 lines or 25 KB of the auto-memory `MEMORY.md` index. "CLAUDE.md content is delivered as a user message after the system prompt, not as part of the system prompt itself" ([memory](https://code.claude.com/docs/en/memory)).
  3. Conversation, with tool results and mid-session reminders (hook context, nested `CLAUDE.md` files as their directories are read, task reminders) inserted where they occur ([hooks](https://code.claude.com/docs/en/hooks)).
  4. After compaction: a user message "This session is being continued from a previous conversation…" carrying the summary, followed by re-read `CLAUDE.md`, auto memory, the plan file, up to five recent files and invoked skill bodies ([context window](https://code.claude.com/docs/en/context-window)).
  - There is no separate user-profile layer. The task list is kept through the model's Task or TodoWrite tools ([tools reference](https://code.claude.com/docs/en/tools-reference)), and "Tasks persist across context compactions" ([interactive mode](https://code.claude.com/docs/en/interactive-mode)).
- **Writers.** The user writes `CLAUDE.md` directly, through `/memory` or `/init`, or by asking. The model writes auto memory itself during the session: typed notes plus the `MEMORY.md` index, saved immediately when asked to remember ([memory](https://code.claude.com/docs/en/memory)). A post-turn memory extractor exists in the bundle behind a remote flag.
- **Budget.** Auto-compaction runs at the model's context limit, with documented exceptions such as 200K-window models ([model config](https://code.claude.com/docs/en/model-config)). "It clears older tool outputs first, then summarizes the conversation if needed" ([how Claude Code works](https://code.claude.com/docs/en/how-claude-code-works)). The summary prompt asks for nine sections, among them primary request and intent, all user messages, pending tasks and current work (bundle string "Your task is to create a detailed summary"). Bash output over about 30,000 characters is saved to a file with a 2,000-character preview ([tools reference](https://code.claude.com/docs/en/tools-reference)). There is no memory flush before compaction; the PreCompact hook can block but not add context ([hooks](https://code.claude.com/docs/en/hooks)).
- **Not settled.** Whether the flagged selector and extractor are live for any user: the survey read `false` as the flags' default in the bundle, and a second pass could not trace the gate to that default; whether git status sits in the system prompt or the reminder block (`claude --help` and the bundle disagree); the default number of tool results kept when clearing.

### OpenAI Codex CLI

Checked against: tag `rust-v0.157.0` of https://github.com/openai/codex, commit `00c972ed5d6ff6499317fd41b7f23605b8e6850d`, matching the installed `codex-cli 0.157.0`. Paths are under `codex-rs/`. The docs at `developers.openai.com/codex/` redirect to `learn.chatgpt.com/docs/`.

- **Input.** No rewrite and no planning call: `run_turn` checks compaction, records context changes, resolves explicit `$skill` mentions, runs prompt hooks and samples (`core/src/session/turn.rs`). A skill named in the input has its `SKILL.md` injected as a user message; hook output is recorded as developer messages (`core/src/hook_runtime.rs`). Everything else is retrieved by the model through tools. The `update_plan` tool is registered only when enabled in config, and is off by default at this tag (`core/src/config/mod.rs`).
- **Layers in order** (`core/src/session/mod.rs` `build_initial_context_with_world_state`):
  1. Base instructions in the request's `instructions` field.
  2. One developer message: developer instructions from config, extension fragments such as memory read instructions, permissions, collaboration mode.
  3. One user message: the `AGENTS.md` block, then `<environment_context>`. `AGENTS.md` order is the global file, then project files from the repository root down to the working directory; project files are capped at 32 KiB ([AGENTS.md guide](https://learn.chatgpt.com/docs/agent-configuration/agents-md), `core/src/agents_md.rs`).
  4. Admin-required developer instructions.
  5. Conversation history. Changed context is appended as a diff before the next user message; a changed `AGENTS.md` is re-emitted as a replacement.
  - No user-profile layer. The plan exists only as `update_plan` calls in history. When memories are enabled, `memory_summary.md`, truncated to 2,500 tokens, is injected at thread start as a developer fragment (`ext/memories/src/lib.rs`).
- **Writers.** Humans write `AGENTS.md` and config; Codex has no writer for them. Memories are off by default ([memories](https://learn.chatgpt.com/docs/customization/memories)). When enabled, a background job at session start extracts summaries from idle past sessions and a sandboxed sub-agent consolidates them into `~/.codex/memories/` (`memories/README.md`). A model-called note tool exists only behind a second option, off by default, and is for when "the user explicitly asks Codex to remember" (`ext/memories/src/tools/ad_hoc_note.rs`).
- **Budget.** Auto-compaction fires at the smaller of a configured limit and 90% of the window (`protocol/src/openai_models.rs`). The OpenAI provider compacts server-side into an opaque encrypted item. Other providers use a local "handoff summary" prompt covering progress and decisions, constraints and user preferences, next steps and critical references (`prompts/templates/compact/prompt.md`). Local compaction keeps the most recent user messages up to 20,000 tokens, then the summary as a user message, and drops assistant messages and tool output (`core/src/compact.rs`). Tool output is middle-truncated at 10,000 tokens when recorded. No memory flush; PreCompact and PostCompact hooks run if configured.
- **Not settled.** What the server-side compaction item contains; whether the 32 KiB cap covers the global `AGENTS.md` (the code excludes it, the docs say "combined size").

### Nous Research Hermes Agent

Checked against: tag `v2026.9.24` of https://github.com/NousResearch/hermes-agent, commit `f97608f178d1ffeca59860195ab7da295f7c8e5f`, and the docs at https://hermes-agent.nousresearch.com.

- **Input.** No rewrite or planning step on the main turn. Recall is model-called: `session_search` (SQLite FTS5 over past sessions, no model call; `tools/session_search_tool.py`), `skill_view` and the memory tools. The prompt tells the model to search "when the user references something from a past conversation" (`agent/prompt_builder.py`). Automatic retrieval runs only when an external memory provider such as Honcho is active: a prefetch once per turn, appended to the current user message in a `<memory-context>` block (`agent/turn_context.py`). A model rewrite of the query exists for that prefetch only and is off by default (`plugins/memory/query_rewrite.py`, `plugins/memory/honcho/README.md`).
- **Layers in order.** The system prompt is built once per session and rebuilt after compaction, in three tiers (`agent/system_prompt.py`; [prompt assembly](https://hermes-agent.nousresearch.com/docs/developer-guide/prompt-assembly)):
  1. Stable: identity (`SOUL.md`), guidance, pinned skills.
  2. Context: the caller's system message, one project file (first of `.hermes.md`, `AGENTS.md`, `CLAUDE.md`, `.cursorrules`), a git snapshot.
  3. Volatile: the skills index, `MEMORY.md`, `USER.md`, the external provider's header, a timestamp, the runtime environment.
  - `MEMORY.md` and `USER.md` are a snapshot taken at session start: writes during the session reach disk and enter the prompt at the next session or rebuild (`tools/memory_tool.py`).
  - After compaction the history is the first three messages, one summary message marked "[CONTEXT COMPACTION — REFERENCE ONLY]", then the verbatim tail (`agent/context_compressor.py`). The todo list lives in tool results and is re-injected as a user-role row after compaction (`agent/conversation_compression.py`).
- **Writers.** The model writes `MEMORY.md` and `USER.md` through the `memory` tool during the turn. Their caps are 2,200 and 1,375 characters; a write over the cap returns an error, so the model has to consolidate (`tools/memory_tool_store.py`, `hermes_cli/config_defaults.py`). After a reply is delivered, a forked background agent reviews the conversation every ten user turns and may write memory and skills (`agent/background_review.py`). Context files are the user's; an external provider such as Honcho builds its own user model after each turn ([Honcho](https://hermes-agent.nousresearch.com/docs/user-guide/features/honcho)).
- **Budget.** Compression fires at a configured share of the window, 0.50 by default, raised to 0.75 for windows under 512K and capped at 256,000 tokens (`hermes_cli/config_defaults.py`); the gateway adds a backstop at 85% (`gateway/run_turn.py`). One call, on the main model by default, writes a structured summary with Goal, Constraints & Preferences, Completed Actions, Active State, Key Decisions, Errors & Fixes and more, and updates the previous summary on re-compression (`agent/context_compressor.py`). Old tool results outside the protected tail are replaced by a placeholder first. The only pre-compaction hook serves external providers; built-in memory gets no flush turn (`agent/conversation_compression.py`).
- **Not settled.** Whether a `flush_memories()` routine still exists (named only in comments); the effective default of the skill-review interval (10 in code, 15 in the example config); the summary cap (10,000 tokens in code, 12,000 in the docs).

### OpenClaw

Checked against: tag `v2026.9.6` of https://github.com/openclaw/openclaw, commit `eb377ac59e6c9fd6c7705028034812becf00271b`. One live docs page, [system prompt](https://docs.openclaw.ai/concepts/system-prompt), was compared with its repository copy; every other docs claim is from `docs/` at the tag.

- **Input.** No rewrite or planning step. The prompt's Memory Recall section tells the model: "Before answering anything about prior work, decisions, dates, people, preferences, or todos: run memory_search", and the tool calls itself a "Mandatory recall step" (`extensions/memory-core/src/memory-tool-contract.ts`). Two lanes run before the model sees the turn and are prepended to the user message for the model only (`src/agents/embedded-agent-runner/run/attempt-llm-boundary.ts`):
  - Lane 1, no model call: trigger phrases stored on `MEMORY.md` and `USER.md` entries are matched against the message; up to three strong matches are injected (`docs/concepts/memory-search.md`). The strong-match score is 0.65 in code (`extensions/active-memory/trigger-recall.ts`, whose comment says 0.72 was tried and rejected) and 0.72 in `docs/concepts/memory-architecture.md`.
  - Lane 2, Active Memory: a blocking recall sub-agent that, in the default mode, runs only when the message refers to the past and lane 1 found nothing strong (`docs/concepts/active-memory/how-it-works.md`).
- **Layers in order** (`src/agents/system-prompt.ts`, `src/agents/system-prompt-context-files.ts`):
  1. Stable system prompt: tooling and safety, skills, memory-recall guidance, then the workspace files in the order `AGENTS.md`, `SOUL.md`, `IDENTITY.md`, `USER.md`, `TOOLS.md`, `BOOTSTRAP.md`, `MEMORY.md`. Files are capped at 20,000 characters each and 60,000 in total; `USER.md` has its own 4,000.
  2. Below the cache boundary: date and time, messaging and runtime sections.
  3. History: the compaction summary first, as a user message wrapped in `<summary>`, then the kept messages (`packages/agent-core/src/harness/messages.ts`).
  - Daily notes `memory/YYYY-MM-DD.md` are never injected, except on the first turn after `/new` (`docs/concepts/system-prompt.md`). `MEMORY.md` loads only in the main private session. Search results arrive as tool results, ranked 0.7 vector and 0.3 BM25 with recency decay (`src/agents/memory-search.ts`).
- **Writers.** The architecture document names the nightly "dreaming" consolidation pass as the one primary writer of `MEMORY.md` and `USER.md`, besides direct user requests (`docs/concepts/memory-architecture.md`). The agent writes daily notes with its file tools and writes `IDENTITY.md` during the first-run ritual. `AGENTS.md` is "Human only".
- **Budget.** Compaction triggers at the context window minus a reserve (default 20,000 tokens, at most a quarter of the window) and keeps the most recent 20,000 tokens verbatim (`docs/reference/session-management-compaction/compaction.md`). The summary must carry `## Decisions`, `## Open TODOs`, `## Constraints/Rules`, `## Pending user asks` and `## Exact identifiers`; an audit retries when pending asks or identifiers are lost, and keeps the history if the retries fail (`src/agents/agent-hooks/compaction-safeguard-quality.ts`). A silent memory-flush turn runs 4,000 tokens before the compaction threshold, on a detached copy of the conversation, may only append to today's daily note, and normally replies `NO_REPLY` (`extensions/memory-core/src/flush-plan.ts`, `docs/reference/session-management-compaction/housekeeping.md`). Tool-output pruning is off by default outside Anthropic setups (`docs/concepts/session-pruning.md`).
- **Not settled.** Whether Active Memory is on by default; the trigger threshold, where code and docs disagree (above); who may write `MEMORY.md` and `USER.md`, since the default `AGENTS.md` template tells the agent to edit both directly while the architecture document names dreaming as the one writer.

### opencode

Checked against: tag `v1.18.32`, commit `545f51d26cc39a907d2867492d498d9607ea5fa4`, matching the installed `opencode 1.18.32`. `sst/opencode` resolves to https://github.com/anomalyco/opencode. Docs at `https://opencode.ai/docs/`.

- **Input.** No rewrite and no automatic retrieval (`packages/opencode/src/session/prompt.ts`). `@file` mentions run the Read tool at once and paste the result into the user message. Planning is the user-selected `plan` agent, which appends a read-only reminder to the user message (`packages/opencode/src/session/reminders.ts`). Retrieval is otherwise model-called tools. Reading a file also attaches any unseen `AGENTS.md` or `CLAUDE.md` above it inside the tool result (`packages/opencode/src/session/instruction.ts`).
- **Layers in order.** One system message (`packages/opencode/src/session/system.ts`, `packages/opencode/src/session/llm/request.ts`):
  1. The agent's prompt, or a base prompt chosen by model.
  2. The environment block.
  3. Instruction files: one global file, the project `AGENTS.md` files from the working directory up to the worktree root, then configured `instructions` paths and URLs.
  4. MCP server instructions, then the skills list.
  - Then the conversation: the latest compaction summary, the kept verbatim turns, the newest messages (`packages/opencode/src/session/message-v2.ts`). The todo list is stored in SQLite and appears only as the model's own `todowrite` calls. No user-profile or memory layer.
- **Writers.** The user writes instruction files, which are re-read on every step. The model writes the todo list and, in plan mode, the plan file. A hidden `compaction` agent writes summaries.
- **Budget.** Compaction starts when usage reaches the input limit minus a reserve (by default the smaller of 20,000 and the model's maximum output), or on a provider overflow error (`packages/opencode/src/session/overflow.ts`). It keeps the most recent 25% of the usable window, clamped to 2,000–15,000 tokens (`packages/opencode/src/session/compaction.ts`), and summarizes the rest into a fixed template: Objective, Important Details, Work State, Next Move, Relevant Files. A new summary merges the previous one as `<prior-summary>` (`packages/core/src/session/compaction.ts`). Tool-output pruning is off by default ([config](https://opencode.ai/docs/config/)); outputs over 2,000 lines or 50 KB are cut to a preview and saved to a file (`packages/opencode/src/tool/truncate.ts`). No memory flush.
- **Not settled.** Whether the hidden `summary` agent ever calls a model; which path uses the second compaction implementation in `packages/core`. The docs have no compaction page (`https://opencode.ai/docs/compaction/` returned 404).

## Comparison

"Model" means the acting model during its own turn; "background" means a separate pass outside the turn. "Off" means present at the stated version but disabled by default.

| Aspect | Claude Code | Codex CLI | Hermes Agent | OpenClaw | opencode | sherlock `ebecac8` |
|---|---|---|---|---|---|---|
| Input interpretation before acting | none; selector call behind a remote flag | none | none; rewrite for external prefetch off | trigger match; recall sub-agent when the input refers to the past | none | none |
| Retrieval | model tools | model tools | model tools; external prefetch | model tool the prompt makes mandatory, plus two pre-turn lanes | model tools | once, pre-turn, raw text; no search tool |
| Human-written instruction files | `CLAUDE.md` hierarchy | `AGENTS.md` hierarchy | one project file, `SOUL.md` | `AGENTS.md` and persona files | `AGENTS.md` hierarchy, `instructions` | ego file |
| Model-written memory in the prompt | auto-memory index, 200 lines or 25 KB | `memory_summary.md`, 2,500 tokens, background, off | `MEMORY.md` 2,200 chars | `MEMORY.md` | none | none |
| User profile | none separate | none | `USER.md` 1,375 chars; Honcho | `USER.md` 4,000 chars | none | none |
| Placement of persistent layers | user message after system prompt | developer and user messages after base instructions | system prompt, snapshot per session | system prompt, stable prefix | system prompt | leading system message (ego, contract) |
| Working state | task tools; persists across compaction | `update_plan` calls, off | todo tool; re-injected after compaction | `progress_card` plan tool | `todowrite` calls | none |
| Compaction summary | nine sections, user message | handoff summary, user message | structured, merged on re-compression | five required sections, audited | fixed template, merged | none; oldest messages dropped |
| Memory flush before compaction | none | none | external providers only | silent flush turn to the daily note | none | none |

### Where they agree

- **Understanding is the acting model's own reasoning.** No agent runs a mandatory rewrite or planning call before the main turn. Every pre-turn model call found is off by default (Hermes's rewrite), behind a remote flag (Claude Code's selector) or gated to inputs that look like recall (OpenClaw lane 2). OpenClaw's lane 1 injects matches without a model call.
- **Retrieval is a tool the model calls with the conversation in view.** All five offer it, and OpenClaw's prompt makes it mandatory for questions about prior work.
- **A persistent layer sits at the head of the prompt, before the conversation, and survives compaction.** It is re-read or rebuilt after compaction in Claude Code, Hermes and opencode, and kept in the stable prefix in OpenClaw.
- **Persistent layers are small and bounded.** Hermes errors on an over-limit write so the model consolidates; Claude Code loads 200 lines or 25 KB; OpenClaw caps each file.
- **Compaction keeps a recent tail verbatim and summarizes the rest into goal, constraints, decisions, state and next step.**
- **Working state is free text or a flat list written by the model.** None keeps a typed lifecycle status or a per-effect attempt log in the prompt.

### Where they differ

- **Who writes durable memory.** The acting model during the turn (Claude Code, Hermes), a background pass (Codex, Hermes's review, OpenClaw's dreaming), or nobody (opencode).
- **Placement.** System prompt (Hermes, OpenClaw, opencode) against a user or developer message after it (Claude Code, Codex). Hermes freezes memory for a session to keep the prefix cacheable; opencode re-reads instruction files on every step.
- **Flush before compaction.** Only OpenClaw runs one.
- **User profile.** A separate file only in Hermes and OpenClaw.

## Candidate structures for sherlock

The owner's two requirements from #860 are:

- **R1:** a step where the agent understands the question with its full context, before or while it retrieves.
- **R2:** a full-context layer beside the conversation list.

Each candidate below states the step, the layer, how both combine with the window and mid recall, what it deletes, and a net estimate. Estimates are production lines before tests, read from the files at `ebecac8`; nothing was implemented.

Two deletions recur:

- **The addressed `memory_ops` field** (D1). It is the required, unapplied field of the terminal call: its schema, parse and operation types (`internal/agent/writeback.go`, `internal/agent/agent.go`), the `WritebackSink` hook and `applyWriteback` (`internal/agent/service.go`), and the unwired `agentmemory.Writeback`. About −200 to −240 lines. It also removes the `ops` field of the monitoring event (`internal/gateway/handler.go`), which an external consumer may read.
- **The unwired addressed read step** (D2): `internal/agent/recall.go` and `internal/agent/memory/recall.go`, plus the `unplaced` and `widened` recall outcomes. About −250 lines. Any candidate may take it. It is listed apart because it is dead code rather than a structural change.

### A. Search during work, and a note carried by the terminal call

The smallest structural change meeting both requirements.

- **Understanding (R1, while retrieving).** Add a reserved `memory_search` call to the act loop. The model writes its own query with the window, the note and the recall block in view. The call runs the existing similarity ranking and returns up to five mids with their source intervals, which `memory_inspect` then reads. Pre-turn recall on the raw text stays as the first candidate set. The contract tells the model to search with a standalone query that resolves references such as "that" or "what I said earlier". This is the shape all five agents share. The existing per-tool budget of six calls a turn bounds it.
- **Layer (R2).**
  - **Content:** one free-text note of at most 2,000 characters holding standing instructions and preferences, and the current work: its goal, constraints, status and next step.
  - **Writer:** the acting model, through the terminal call. `memory_ops` is replaced by `note`: null leaves the note unchanged, a string replaces it. A string over the bound fails the turn, so the model consolidates, as Hermes does.
  - **Storage:** one row in `memory.db`, written in the same transaction as the turn's sources.
  - **Placement:** appended to the leading system message on every turn, so the compactor never drops it and a restart restores it.
- **With the window and recall.** The window and pre-turn recall are unchanged. Search reaches mids the raw text missed, and the note is the only layer that accumulates.
- **Deletes.** D1; the search call takes the reserved-read slot D2 vacates.
- **Estimate.** Additions of about +105 to +150: the note row and its migration, the field and its parse, the injection, the search call and the contract text. Net about −135 to −50, or about −385 to −300 with D2.
- **Risk.** #812's live run found a model-maintained status unreliable: canceled work resumed while the record said active. A free-text note written by the same model carries the same risk, and §Experiment plan measures it.

### B. Understand before retrieving

The step closest to a classic query-understanding stage.

- **Understanding (R1, before retrieving).** One model call before the act loop receives the note, the last 16 visible messages and the new input. It answers through a reserved call with three things:
  - a standalone restatement of the request, with references resolved;
  - one to three search queries;
  - an updated note, or null.

  Recall ranks by the maximum cosine over the restatement and the queries, then merges with the two recent mids as today. The frame shows the restatement beside the original input.
- **Layer (R2).** The same note as A. Its writer is the understanding call, so the note records the request before work starts; the terminal call carries only the reply.
- **With the window and recall.** The window is unchanged. The recall query changes from raw text to the restatement and the queries.
- **Deletes.** D1. It reintroduces a pre-turn model call, the shape #858 deleted with the phrase call.
- **Estimate.** Additions of about +140 to +200: the call, its schema and parse, multi-query ranking, the note row and injection. Net about −100 to −40 before D2. Each turn adds one model call, about 4,000 input tokens with this context, and its latency: the context-free phrase call it resembles took 3.5 to 4.4 s ([mid-memory retrieval](../mid-memory-rag/README.md) §Per-turn cost and §Replication (issue #856), its §Cost).
- **Evidence so far.**
  - A context-free rewrite did not help implicit tasks ([memory read policy](../memory-read-policy/README.md), arm B).
  - Maximum cosine over context-free phrases matched raw-text similarity, 27 against 27 of 40 (mid-memory retrieval, arm C).
  - No surveyed agent runs such a call by default. The nearest are Hermes's rewrite for external prefetch, off by default, Claude Code's memory selector, behind a remote flag, and OpenClaw's gated recall sub-agent.
  - B differs from what was measured only in that the call sees the context.

### C. A handoff summary written when the window drops

The compaction pattern of the five agents, applied to sherlock's window.

- **Understanding (R1).** As A: `memory_search` during work.
- **Layer (R2).**
  - **Content:** a running handoff summary with the sections the surveyed templates share: goal, standing instructions and constraints, decisions, pending asks, exact identifiers and next step, bounded at 4,000 characters.
  - **Writer:** the session model, when the compactor drops messages. It merges the previous summary with the dropped span, as opencode and Hermes do.
  - **Storage:** one row.
  - **Placement:** directly after the leading system message, where the other agents place their summaries.
  - The compactor's trigger changes from "trim to 128 on every step past 128" to "drop to 64 when past 128". Otherwise a summary call would fire on nearly every step once the window is full.
- **With the window and recall.** The summary covers what left the window, so the two recent mids and the pending previews, which cover what the window still holds, leave recall. Recall becomes the five similarity candidates.
- **Deletes.** D1, the recent-mid and pending-preview slots in recall, and the silent drop in `compaction.go`.
- **Estimate.** Additions of about +110 to +170: the summary call, its template and merge, the row, the placement, the trigger change and the search call. Deletions of about −30 to −40 in recall, beside D1. Net about −170 to −70 before D2. Each compaction adds one model call.
- **Conflict.** ADR 0006 rejected recursively summarizing prior mid state because it "would … propagate earlier synthesis errors". A running summary is recursive in that sense. Codex warns the user that repeated compactions reduce accuracy (`core/src/compact.rs` at `rust-v0.157.0`), and OpenClaw audits identifiers for the same reason. Adopting C re-opens that decision.
- **Pending previews.** Removing them changes ADR 0006's guarantee that "compaction cannot hide the interval before a checkpoint". The summary takes over that job.

### Considered and not proposed

- **Memory as a file in the workspace** (Claude Code, Hermes, OpenClaw). Sherlock's workspace is the data plane where agent-influenced programs run under the unprivileged UID (ADR 0010, ADR 0017), so any command could rewrite the file. The agent container would also need its own workspace read, which only tools hold today.
- **A separate user profile** (Hermes, OpenClaw). It is folded into the note's standing-instructions part, keeping one writer and one bound. The experiment's implicit family shows whether that loses anything.
- **Background consolidation** (Codex memories, Hermes review, OpenClaw dreaming). It adds a second writer and a scheduled model call beside the acting model.
- **Ephemeral recall blocks.** Replacing the previous turn's frame block instead of accumulating them would make context the same across restarts (§Sherlock at `ebecac8`). It is independent of R1 and R2, so no candidate carries it, and the experiment holds the shipped behaviour constant across arms.

## Experiment plan

For the next issue to preregister. Figures marked "estimate" are to be fixed in the preregistration from a dry run.

- **Arms.**
  - S: the shipped recall at `ebecac8`.
  - A, B and C as above, each implemented on an experiment branch that is not merged.
  - Every arm runs the production session through a study harness, as #858's parity harness did (`research/mid-memory-rag/parity/`).
  - Model `MiniMax-M2`. Embedder `BAAI/bge-base-en-v1.5` on text-embeddings-inference, as in §Production parity (issue #858) of the mid-memory study.
  - Window 32 messages in every arm, instead of the default 128, so that beyond-window cases are affordable. The preregistration states this deviation.
- **Corpus.** Ten conversation streams of about 150 turns each.
  - Filler, the exact, correction and absent items, and the implicit items come from the replication corpus, whose recorded batch-4 syntheses replay without paid calls (`evidence/replication/`).
  - New items are written by the model under the replication's item checks.
  - Every arm processes each stream live, so its note or summary is its own. Each probe runs on a copy of the arm's store and session taken at the probe's position, so probes do not contaminate one another.
- **Query families.**

  | Family | n | Role | Evidence and final input |
  |---|---:|---|---|
  | F, follow-up in window | 70 | primary | Evidence within the last 32 messages; the final input refers to it anaphorically ("do that again for March", "use the other one"). |
  | G, follow-up beyond window | 70 | primary | Evidence older than the window. The input alone does not rank the evidence in the raw-text top 5 (a preflight check), but the input read with the window identifies it. |
  | I, implicit need | 70 | primary | A standing instruction or preference stated once, older than the window, that an artifact must obey (replication implicit items and new rules such as "indent with tabs"). |
  | K, correction | 70 | primary | A newer statement supersedes an older one (replication correction items). |
  | E, exact | 30 | guard | Replication exact items. |
  | N, absent | 30 | guard | The asked fact was never stated (replication absent items). |
  | R, continuation | 30 | guard | #810's cases after a restart and a neutral arrival: 10 continue, 10 changed, 10 canceled. |

- **Metrics, per cell.**
  - *Reached:* an evidence source was in the model's context at answer time — in the window, in pre-turn recall, returned by search, or stated in the note or summary. Computed from the recorded requests.
  - *Inspected:* `memory_inspect` read an evidence source.
  - *Applied (primary endpoint):* the final reply or artifact meets the item's check. The check is a required value, the newest value and not the obsolete one for K, a rule checked by code or regular expression for I, and the stated change for F and G.
  - *Abstained (N):* the reply says the fact is unavailable and states no value.
  - *Continuation (R):* canceled work is not resumed; changed work follows the newer constraint; continued work resumes without the user restating it.
  - *Layer fidelity (A, B, C):* at each K probe, whether the note or summary states the obsolete value as current.
  - *Cost:* model calls, tokens, USD and latency per turn.
  - Deterministic checks decide wherever an item allows one. The rest follow a preregistered rubric, and the report names who judged.
- **Controls, required before any verdict.** Without them a null result is a harness defect, not an answer.
  - Oracle reach: 266 of 280 primary evidence sources reachable through each arm's own path, as in the replication.
  - A and C call `memory_search` in at least 20% of G cells.
  - The note in A and B is non-empty after the turn that states an I item's rule, in at least half of the I streams.
  - B's understanding call returns its reserved call on at least 90% of turns.
  - C writes at least one summary per stream before the stream's first probe.
  - Mutation checks show each arm's new path failing a test when removed.
- **Budget, estimate.**
  - Each call carries about 7,500 input tokens: system prompt, 32-message window, recall block and note.
  - A probe averages four calls, about USD 0.011 at USD 0.30 per million input and USD 1.20 per million output tokens, the rates the mid-memory study used. 370 probes over 4 arms cost about USD 17.
  - Stream prefixes run live cost about USD 12: 1,500 turns over 4 arms at about USD 0.002 a turn.
  - New items and their conversations cost about USD 1.5, going by the replication's USD 1.27 for 1,161 chats. Their synthesis costs about USD 0.3.
  - B's understanding calls and C's summaries cost under USD 2.
  - Expected total about USD 33; ceiling USD 40, enforced by reservation before each call as in the earlier studies.
- **Power.** 280 paired primary queries is the size at which the replication's rule reached "valid" for a true 10-point gain with probability 0.817 at 33% discordance ([mid-memory retrieval](../mid-memory-rag/README.md) §Replication (issue #856), its §Results, "Power at the observed disagreement"). Answer-level discordance may differ; the preregistration recomputes power with `replication_score.py power` from a dry run.
- **Decision rule.**
  1. Each candidate is compared with S on *applied* over the 280 primary queries, by the exact McNemar test with Holm correction across the three comparisons at family-wise α = 0.05, two-sided.
  2. A candidate is valid when its difference is positive and significant and every guardrail holds:
     - no primary family loses more than 7 of 70;
     - E and N each lose no more than 3 of 30;
     - no R canceled case is resumed that S did not resume.
  3. Among valid candidates, take the one with the smallest measured net production diff. A larger valid candidate is taken only if it beats the smaller one directly on *applied*, by the same test at α = 0.05.
  4. If no candidate is valid and every control passed, the shipped structure stands. That null is an answer: at this size, neither layer nor search changes whether the agent reaches and applies the evidence.

## Experiment (issue #862)

The live test of candidates A and B against the shipped recall S, preregistered in [preregistration.md](preregistration.md) and pushed as `b0d71c1` before the first stage-2 request (author date 00:41:11Z; §Reproduction names the commit that carries it now). Evidence is under [evidence/](evidence/). Candidate C was not run: its running summary is the recursive synthesis ADR 0006 rejects, re-opening that decision is the owner's, and it was the most expensive arm.

### What differs from §Experiment plan

- **Arms.** S is `4e84d2c`. A (`61f4e4c`) and B (`b5bc7ed`) are their own commits on the study branch, written to production standard. The harness is built inside each arm's source tree and drives each arm through the same `gateway.Factory`, so every arm runs its own session, store, recall and client (`harness/`).
- **B's recall** ranks by the restatement and the queries joined as one text, not by the maximum cosine over them: the recall interface takes one text.
- **The note is a required string, restated whole on every terminal call (A) or understanding call (B).** The first pilot offered it as nullable, meaning "unchanged". MiniMax-M2 returned null on every turn in both arms, including right after "start every reply with 'Ahoy!'" (`evidence/pilot/v1-arms/`), so neither layer was exercised. Both commits were changed before the preregistration.
- **Episodes.** 370 episodes, from `corpus.py`, each starting from the #856 replication's 4,800 messages and 1,200 recorded batch-4 syntheses, replayed without a paid call. Only the inputs of each family's own turns ran live: 630 live turns per arm. Everything else entered the store as completed turns. The window is 32 messages in every arm, and every live input starts from what a restart restores.
- **Budget.** The plan's USD 40 ceiling became the issue's USD 30 gate. The pilot projected USD 10.49 in total, and the stage-2 ledger ceiling was USD 20.00.

### Pilot

- Every arm ran 41 pilot episodes drawn apart from stage 2 (`pilot-episodes.json`).
- It found three harness defects, each fixed before the preregistration:
  - MiniMax "Token Plan" 429s outlasted three attempts. Six attempts with backoff now.
  - A synthesis response without a `memory_summary` call was cached and failed its seed. It is now re-requested and never cached.
  - Reach and oracle keys were matched literally, where summaries paraphrase. They are now matched as the scorer matches values; an S re-check of 16 pilot K and I episodes then reached the oracle in all 16.
- It also found the nullable note above, and a correction checker that failed "68 Alder Road — you corrected it from 94 Garnet Road".
- The pilot spent USD 1.5911 over 7,253 ledger entries, counting the discarded runs. Its discordance against S on *applied* was 0.34 (A) and 0.44 (B), which leaves 280 primary pairs with 0.71 and 0.59 power for a 10-point gain at Holm's first-step level (`evidence/pilot/projection.json`).

### Controls

From [scores](evidence/stage2/scores.json) `controls`:

| Control | Required | S | A | B |
|---|---|---:|---:|---:|
| Oracle reach, of 280 primary | 266 | **261** | **264** | 272 |
| `memory_search` called in G probes, of 70 | 14 | — | 39 | — |
| Note non-empty after the I rule turn, of 70 | 35 | — | 58 | 70 |
| Understanding answered through its reserved call, of 630 live turns | 567 | — | — | 628 |
| Every mutation breaks a test ([mutation](evidence/mutation.txt)) | 10 of 10 | pass | pass | pass |

The oracle-reach control fails for S and A.
- Of S's 19 primary misses, 16 are I episodes. Of A's 16, all 16 are I episodes. B misses 8, 7 of them I.
- An I oracle asks whether a mid summary holding the rule's key word ("lowercase", "tabs", "TL;DR") ranks in the top five for the rule's own sentence. So a summary that rewords the rule counts as a miss.
- The four E misses (`E-10`, `E-17`, `E-18`, `E-28`) are the same in every arm.
- The per-episode stores were deleted after each episode, so which misses are reworded summaries and which are ranking misses was not separated.

### Results

[Scores](evidence/stage2/scores.json) hold every per-episode value; *applied* with its Wilson 95% interval, then *reached* and *inspected*:

| Family (n) | S | A | B |
|---|---|---|---|
| F, follow-up in window (70) | 70 [0.95, 1.00]; 70, 1 | 70 [0.95, 1.00]; 70, 0 | 70 [0.95, 1.00]; 70, 0 |
| G, follow-up beyond window (70) | 9 [0.07, 0.23]; 15, 1 | 49 [0.58, 0.79]; 51, 9 | 45 [0.53, 0.75]; 45, 13 |
| I, implicit need (70) | 1 [0.00, 0.08]; 5, 0 | 20 [0.19, 0.40]; 57, 0 | 35 [0.39, 0.61]; 70, 0 |
| K, correction (70) | 44 [0.51, 0.73]; 44, 0 | 70 [0.95, 1.00]; 70, 0 | 64 [0.83, 0.96]; 68, 0 |
| **primary (280)** | **124 [0.39, 0.50]** | **209 [0.69, 0.79]** | **214 [0.71, 0.81]** |
| E, exact (30) | 16 [0.36, 0.70] | 14 [0.30, 0.64] | 11 [0.22, 0.54] |
| N, absent: abstained (30) | 20 [0.49, 0.81] | 19 [0.46, 0.78] | 23 [0.59, 0.88] |
| R, continuation (30) | 13 [0.27, 0.61] | 23 [0.59, 0.88] | 29 [0.83, 0.99] |

- **R by case** (continue, changed, cancelled, of 10 each): S 7, 5, 1; A 9, 10, 4; B 10, 10, 9. After a cancellation, S still wrote a file listing the names in 9 of 10 cases, A in 6, B in 1.
- **Layer fidelity (K).** After the correction turn, the note stated the obsolete value without the current one in 0 of 70 episodes in both A and B. It stated the current value in 69 (A) and 66 (B).
- **Reached against applied in I.** B had the rule in view in all 70 I probes and applied it in 35; A had it in view in 57 and applied it in 20. For I, the note is the path: S reached the rule in 5.
- **Failures.** Two S probes (`I-60`, `I-62`) ended on a malformed terminal call and count as not applied. No turn in any arm failed on an infrastructure error, so nothing was re-run.

Paired against S on the 280 primary probes, exact McNemar, Holm across the two:

| Arm | Arm only | S only | Difference | Holm p | Guardrails |
|---|---:|---:|---:|---:|---|
| A | 86 | 1 | +0.304 | 2.3e-24 | hold (E −2, N −1, no cancelled case resumed where S did not) |
| B | 94 | 4 | +0.321 | 2.4e-23 | **fail**: E loses 5 of 30, above the 3 allowed |

B against A on the primary probes: B only 36, A only 31, p = 0.63.

### Cost per turn

From the [ledger](evidence/stage2/ledger/calls.jsonl), 34,672 attempts in total, of which 26,826 were cached syntheses, 368 were HTTP 429 and later retried, and 90 were syntheses re-requested for lacking a `memory_summary` call:

| Arm | USD, stage 2 | USD per live turn | Act calls per live turn | Mean act latency | Extra calls |
|---|---:|---:|---:|---:|---|
| S | 2.4176 | 0.0038 | 2.03 | 3.3 s | — |
| A | 2.5405 | 0.0040 | 2.08 | 3.7 s | `memory_search` inside act calls |
| B | 2.8552 | 0.0045 | 2.01 | 3.0 s | one understanding call per turn, 6.3 s mean |

Stage 2 spent USD 7.8132; with the pilot, USD 9.4043.

### Verdict

**Undetermined**, under the preregistered rule. The oracle-reach control fails for S (261 of 280) and A (264 of 280) against the 266 required, and the rule makes the verdict undetermined when any control fails.

What the collection shows, outside the rule:
- **Both candidates apply the evidence far more often than S** on the primary families: A 209 and B 214 of 280, against S's 124. S alone applied the evidence in 1 primary probe against A and in 4 against B.
- **The gains are in G, I and K**, the families that need either a search with the conversation in view or a layer that outlives the window. F, where the evidence is in the window, is 70 of 70 in every arm.
- **Had the oracle control passed,** A would be valid and B would not: B loses 5 of 30 exact queries, and the guardrail allows 3. Under the rule A would then be taken, and B does not beat it (36 against 31, p = 0.63).
- **Excluding the 24 primary episodes any arm missed the oracle on,** A leads S 79 to 1 and B leads 78 to 3 (256 episodes). This is a sensitivity check, not the preregistered endpoint.

### Code

The arms are not part of this study's branch. Each stays on its own branch, kept until a confirmatory run decides:
- A is `61f4e4c`, the tip of `sauce-git/feat/agent-context-arm-a`.
- B is `b5bc7ed`, the tip of `sauce-git/feat/agent-context-arm-b`, built on A.

Net production lines against S, read with `git diff --numstat 4e84d2c <commit>` over non-test files:
- **A, `61f4e4c`:** +264 −351, net **−87**. The preregistration's tie-break quotes −91, the count before the note became a required string. The ordering the rule uses is unchanged.
- **B, `b5bc7ed`:** +375 −361, net **+14** against S; it is built on A's note and replaces A's search call.

Neither arm won under the rule. The commits a later implementation PR would take are A's (`61f4e4c`) if the owner accepts the result outside the rule, since A leads on size and B fails a guardrail. The documents that change would change with it: `docs/architecture/agent.md` and `memory.md`, ADR 0011 (`memory_ops`), ADR 0019 (§Read-policy limits) and the `README.md` paragraph on the mock provider's `memory_ops`. They are not edited here.

### Limits

- **One model wrote every live answer.** MiniMax-M2 at a provider alias with no weight revision; every probe ran once, with no variance estimate.
- **The corpus is templated.** Events, values, rules and corrections come from `corpus.py`, not conversation; filler and the base are the replication's model-written chats. G was built so that the probe alone cannot pick the referent among 22 same-attribute statements, which favours any arm that reads the window.
- **Only live turns write the note.** Seeded history never passed through the arms' note step, so a real conversation would fill the note with more, and more competing, content.
- **Every live input starts from a restart.** Frame blocks never accumulate, and the window is 32 messages rather than 128.
- **Checks are deterministic proxies.** They are pinned in `score_test.py`, but a value in another format than those accepted, or a rule applied in spirit but not to the letter, fails. The abstention check is a pattern.
- **The synthesis cache** answers byte-identical syntheses from the first answer, so arms share summaries of identical batches. That fixes a source of variance the plan did not name.

### Reproduction

From a checkout of this branch, with Docker and Go 1.26:

```sh
python3 research/agent-context/corpus.py && python3 research/agent-context/corpus.py pilot   # byte-identical episodes
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s research/agent-context -p '*_test.py'
go test ./research/agent-context/harness/
python3 research/agent-context/score.py research/agent-context/evidence/stage2    # offline; rewrites scores.json
python3 research/agent-context/mutation.py                                            # offline; the mutation record
```

- **The arms' trees.** S is `4e84d2c` on `dev`. A and B come from their own branches: `git fetch origin sauce-git/feat/agent-context-arm-a sauce-git/feat/agent-context-arm-b`, then `git archive 61f4e4c` and `git archive b5bc7ed`.
- **The study commits were rebuilt onto `dev`** after review, so this branch carries no arm code. `docs/research/` is byte-identical to the collected tip `d49acd1`, and author dates are kept. Old SHAs cited in committed files map as follows:
  - `f32ba3a` → `8ae1f9b`, the harness, corpus and scorer.
  - `7c670ec` → `44e34e0`, the proxy fix. `evidence/mutation.txt` names `7c670ec`; `mutation.py` now archives `44e34e0`, whose `harness/` tree is identical.
  - `c095201` → `01b24d4`, the correction scoring.
  - `1cf6751` → `93ddc1f`, the key matching. The preregistration names `1cf6751` as the harness the binaries were built with; `93ddc1f`'s `harness/` tree is identical.
  - `b0d71c1` → `b8c1378`, the preregistration.
  - `d49acd1` → `e8cc29e`, the results.
- Collection needs the TEI container of §Production parity (issue #858) in the mid-memory study on `127.0.0.1:8090`.
- Each arm's binary is built from `git archive <commit>` with `docs/research/agent-context/harness/` copied in, as `go build ./docs/research/agent-context/harness`.
- The base store is `harness base -out base.db`.
- The proxy is `harness proxy -ceiling <USD> -dir <ledger>`, with `MINIMAX_KEY` in its environment only.
- Each arm runs `harness run -arm <S|A|B> -base base.db -out <dir> -proxy http://127.0.0.1:8091`.
- The paid phases are a new run with their own budget; the ledger stays in the directory given to the proxy, and a run resumes after its last recorded episode.

## Confirmatory run (issue #864)

A against S only, with the oracle moved to the source level, preregistered in [confirm-preregistration.md](confirm-preregistration.md). It was committed as `a466bf3` before the first paid call and amended in `e391516` before collection resumed. Evidence is under [evidence/confirm/](evidence/confirm/). #862's evidence is unchanged.

### What changed from #862

- **Arms.** S is `2e28064` (origin/dev; its production code is `4e84d2c`'s). A is `61f4e4c` on `sauce-git/feat/agent-context-arm-a`. The harness is `8cb0b1b`, which adds the source-level oracle, `-keep-stores` and `score.py confirm`.
- **Oracle.** A primary episode is reached when a mid whose source interval holds the evidence statement's stored message ranks among the top five for that statement, through the arm's own store.
  - Each miss is classified from the same store as unsummarized, reworded summary (the holding mid's summary lacks the key), or ranking miss (the summary states the key and still ranks below five).
  - Every episode's store was kept until scoring; stores are not committed.
- **Episodes.** `corpus.py confirm`, seed 864, same families and sizes. F, G and K are new. I repeats #862's 70 rule-task pairs, which the generator cannot vary, around new filler. N repeats the replication's absent queries. 12 of the 30 E facts recur.
- **One comparison** at α = 0.05; B and C were not run.

### Collection

- Collection started at 03:52 UTC. From about 04:05 UTC every non-cached request got HTTP 429 "Token Plan usage limit reached … (2056)", a quota on the key rather than a request rate.
- It was stopped at 04:23 UTC, with USD 0.3010 spent. At that point 25 of S's 50 recorded episodes and 26 of A's 51 had failed while seeding.
- One ledgered probe every 15 minutes returned 429 at 04:28 and 04:45 and 200 at 05:00:53 ([probes](evidence/confirm/ledger/probes.log)).
- The amendment was committed before collection resumed. It re-runs every episode that failed on 429, 5xx or a transport error from scratch, scores it once, and halves concurrency to two episodes per arm.
- **Re-run episodes: S 25, A 26,** every one of them a 429 failure. Their first records are in `results-attempt1.jsonl`; after the re-runs no episode failed on 429 again.
- Two S probes failed on a malformed terminal call and count as not applied: `R-15`, and `I-43`, where the failure hit the rule turn itself, so the rule never entered the store.
- Collection ended at 07:42 UTC.

### Controls

From [scores](evidence/confirm/scores.json) `controls`:

| Control | Required | S | A |
|---|---|---:|---:|
| Oracle reach, source level, of 280 primary | 266 | **257** | **257** |
| `memory_search` called in G probes, of 70 | 14 | — | 39 |
| Note non-empty after the I rule turn, of 70 | 35 | — | 60 |
| Every mutation breaks a test ([mutation](evidence/confirm/mutation.txt)) | 11 of 11 | pass | pass |

The oracle-reach control fails in both arms, 257 against the 266 required.

| Class of miss | S | A |
|---|---:|---:|
| Ranking miss: the holding mid's summary states the key but ranks below five | 22 (F 8, I 9, K 5) | 23 (F 8, I 12, K 3) |
| Reworded summary | 0 | 0 |
| Unsummarized | 0 | 0 |
| Not stored: the turn that states the evidence failed | 1 (`I-43`) | 0 |

- **No miss is a reworded summary.** Every holding mid states the evidence key; it ranks 6th to 851st (`scores.json` `oracle_misses`).
- **Why it ranks low.** Each evidence message shares its four-message batch with a neighbouring filler or pad pair, so the summary carries two topics. `F-05`'s mid reads "Two separate topics discussed: (1) Lena's charity run has 185 people … (2) … utility costs for a rental", and ranks 6th. `I-24`'s mixes the lowercase rule with an nginx configuration and ranks 70th.
- **F is structural.** Its referent sits among 21 same-attribute statements whose mids hold nothing else, so a mixed referent mid ranks below them. The oracle counts only mids, even though every F referent is in the restored window, where every arm applied it (70 of 70).
- **Without F**, oracle reach is 195 of 210 in both arms, again short of 95%.

### Results

*Applied* with its Wilson 95% interval, then *reached* and *inspected*:

| Family (n) | S | A |
|---|---|---|
| F (70) | 70 [0.95, 1.00]; 70, 3 | 70 [0.95, 1.00]; 70, 0 |
| G (70) | 4 [0.02, 0.14]; 7, 0 | 40 [0.45, 0.68]; 41, 10 |
| I (70) | 2 [0.01, 0.10]; 4, 0 | 17 [0.16, 0.36]; 54, 0 |
| K (70) | 46 [0.54, 0.76]; 48, 1 | 65 [0.84, 0.97]; 70, 1 |
| **primary (280)** | **122 [0.38, 0.49]** | **192 [0.63, 0.74]** |
| E (30) | 13 [0.27, 0.61] | 14 [0.30, 0.64] |
| N, abstained (30) | 20 [0.49, 0.81] | 20 [0.49, 0.81] |
| R (30) | 11 [0.22, 0.54] | 23 [0.59, 0.88] |

- **A against S.** Over the 280 primary probes, exact McNemar: A only 74, S only 4, difference +0.250, p = 1.0e-17.
- **Guardrails hold.** No primary family loses to S; E is +1 and N is 0 for A; no cancelled case is resumed where S did not resume it.
- **R by case** (continue, changed, cancelled): S 7, 3, 1; A 10, 9, 4.
- **Layer fidelity (K).** A's note stated the obsolete value without the current one in 0 of 70 episodes, and the current value in 69.
- **Cost.** USD 5.1781 in total, including the stopped run and USD 0.0001 of probes. S cost 2.5303 and A 2.6477, which is USD 0.0040 and 0.0042 per live turn. The ledger holds 23,574 entries: 18,009 cached syntheses, 357 HTTP 429s and 70 syntheses re-requested for lacking a `memory_summary` call.

### Verdict

**Undetermined**, under the preregistered rule: the source-level oracle reached 257 of 280 in each arm, against the 266 required.

What the run adds to #862's:
- **The oracle failure is not reworded summaries.** Every miss is a ranking miss of a mid that states the key, from batches that mix the evidence with a neighbouring exchange, plus one S episode whose rule turn failed. Neither the #862 keyword oracle nor this source-level one measures whether the evidence was reachable during a probe. The probe's own path for F is the window, and for G, I and K it is search, the note or recall of the probe's words, not of the evidence's sentence.
- **The effect replicates on a new seed.** A applied the evidence in 192 of 280 primary probes against S's 122. #862's figures were 209 against 124. The gain sits in the same families, G, I and K, and A's guardrails hold again.
- **Excluding the 29 primary episodes either arm missed the oracle on,** A leads 66 to 4 over 251 (p = 1.7e-15). This is a sensitivity check, not the preregistered endpoint.

A control that can pass whether the arms differ or not has to fit the corpus. Batches that do not mix evidence with filler, or an oracle over the probe's own retrieval, would fit this one. Choosing either is a design decision for the owner, not a result of this run.

### Reproduction

As §Experiment (issue #862) §Reproduction, with these differences:
- the corpus is `corpus.py confirm`;
- the binaries are built from `2e28064` (S) and `61f4e4c` (A) with `harness/` from `8cb0b1b`;
- the base store is #862's (SHA-256 in the preregistration);
- runs pass `-keep-stores <dir>`, and the proxy starts from `evidence/stage2/ledger/synthesis-cache.jsonl`;
- scoring is `python3 research/agent-context/score.py confirm research/agent-context/evidence/confirm research/agent-context/confirm-episodes.json`, which regenerates `scores.json` unchanged.

## Unmixed-batch run (issue #870)

A third run of A against S, on a corpus the oracle control fits, preregistered in [unmixed-preregistration.md](unmixed-preregistration.md) (`42cd349`, before the first paid call). Evidence is under [evidence/unmixed/](evidence/unmixed/). Earlier evidence is unchanged.

### What changed from #864

- **Arms.** S is `d045c0f` (origin/dev; production code as `4e84d2c`). A is `61f4e4c` on `sauce-git/feat/agent-context-arm-a`, which draft PR #867 carries. The harness is `aa56020`.
- **One corpus rule.** Every evidence statement's exchange fills one four-message batch alone.
  - A seeded statement is followed by a same-topic pair.
  - A live statement is closed by an `align` step, which seeds as many same-topic messages as the batch needs, however many messages the turn stored. The align steps replace the pad pairs that followed live turns in #864.
- **Checks of the rule.** `corpus_test.py` checks it statically. At run time the harness checks every stored evidence batch (`unmixedBatch`), and that is a new control.
- **Seed** 870; families and sizes as #864. Two workers per arm, with #864's re-run rule adopted up front.

### Collection

- One ledgered probe at 09:46:50 UTC returned 200 ([probes](evidence/unmixed/ledger/probes.log)).
- Collection ran from 09:47 to 11:45 UTC. The key's quota never stopped it: 21 HTTP 429s, all recovered within their retries.
- **No episode failed on 429, 5xx or a transport error, so none was re-run** (S 0, A 0).
- Three S episodes (`I-09`, `I-31`, `I-37`) ended their rule turn on a malformed terminal call. The rule was never stored, and the three count as not applied.

### Controls

| Control | Required | S | A |
|---|---|---:|---:|
| Oracle reach, source level, of 280 primary | 266 | 277 | 279 |
| Unmixed evidence batches, of the evidence stored | all | 277 of 277 | 280 of 280 |
| `memory_search` called in G probes, of 70 | 14 | — | 23 |
| Note non-empty after the I rule turn, of 70 | 35 | — | 62 |
| Every mutation breaks a test ([mutation](evidence/unmixed/mutation.txt)) | 13 of 13 | pass | pass |

Every control passes.

**Oracle misses.**
- S's 3 are the three I episodes whose rule was never stored.
- A's one is a ranking miss. In `I-36` the batch is unmixed and its summary states the rule ("all future replies must start with the word 'Ahoy!'"), but it also recounts other content and ranks 49th.
- No miss is a reworded summary.

### Results

*Applied* with its Wilson 95% interval, then *reached* and *inspected*:

| Family (n) | S | A |
|---|---|---|
| F (70) | 70 [0.95, 1.00]; 70, 1 | 70 [0.95, 1.00]; 70, 0 |
| G (70) | 31 [0.33, 0.56]; 32, 2 | 53 [0.65, 0.84]; 54, 5 |
| I (70) | 1 [0.00, 0.08]; 4, 0 | 19 [0.18, 0.39]; 57, 0 |
| K (70) | 69 [0.92, 1.00]; 69, 0 | 70 [0.95, 1.00]; 70, 0 |
| **primary (280)** | **171 [0.55, 0.67]** | **212 [0.70, 0.80]** |
| E (30) | 27 [0.74, 0.97] | 27 [0.74, 0.97] |
| N, abstained (30) | 15 [0.33, 0.67] | 17 [0.39, 0.73] |
| R (30) | 18 [0.42, 0.75] | 26 [0.70, 0.95] |

- **A against S.** Over the 280 primary probes, exact McNemar: A only 42, S only 1, difference +0.146, p = 1.0e-11.
- **Guardrails hold.** No primary family loses to S; E is 0 and N +2 for A; no cancelled case is resumed where S did not resume it.
- **R by case** (continue, changed, cancelled): S 10, 8, 0; A 10, 10, 6.
- **Layer fidelity (K).** A's note stated the obsolete value without the current one in 0 of 70 episodes, and the current value in 69.
- **Unmixed batches help S too.** Its G rose from 4 (#864) to 31 of 70 and its K from 46 to 69, because a mid holding only the evidence ranks higher. A's gain narrows accordingly, but stays in G (+22) and I (+18).
- **Cost.** USD 4.8412 in total: S 2.3839 and A 2.4572, USD 0.0038 and 0.0039 per live turn, plus USD 0.0001 of probe. The ledger holds 22,572 entries: 17,457 cached syntheses, 21 HTTP 429s and 82 syntheses re-requested for lacking a `memory_summary` call.

### Verdict

**Valid**, under the preregistered rule. Every control passes and every guardrail holds. A applies the evidence more often than S on the primary families: 212 against 171 of 280, A only 42 against S only 1, p = 1.0e-11.

This is the result the preregistered rule required for adopting A: the change in draft PR #867, whose merge waited on it. Across the three runs A led by 85, 70 and 41 discordant probes. The first two were undetermined because their oracle controls measured the corpus, not the arms.

### Reproduction

As §Confirmatory run (issue #864) §Reproduction, with these differences:
- the corpus is `corpus.py unmixed`;
- the binaries are built from `d045c0f` (S) and `61f4e4c` (A) with `harness/` from `aa56020`;
- the proxy starts from `evidence/confirm/ledger/synthesis-cache.jsonl`;
- scoring is `python3 research/agent-context/score.py confirm research/agent-context/evidence/unmixed research/agent-context/unmixed-episodes.json`, which regenerates `scores.json` unchanged.

## Position on #810 and #812

- **#810's goal: supported.** Every surveyed agent restores a durable layer that does not depend on retrieval finding it:
  - Claude Code re-reads `CLAUDE.md`, auto memory and the plan after compaction.
  - Hermes rebuilds `MEMORY.md` and `USER.md` into the system prompt and re-injects the todo list.
  - OpenClaw keeps its summary at the head of history.
  - opencode and Codex place their summaries ahead of the kept messages.
- **#812's shape: not supported.** Its record is a typed status (active, completed, canceled, idle), progress text, source list, arrival and a per-effect attempt log (`internal/memory/memory.go` and `internal/memory/working_context.go` at the PR head `8d77903`). No surveyed agent keeps a lifecycle status or an attempt log as prompt context. Working state in all five is free text or a flat list the model writes.
- **Position: candidate A's note replaces #812.**
  - #812 is cut from `712eca8`, 13 commits behind `ebecac8` and before the source-linked memory of #843.
  - It carries its record through the `memory_ops` writeback, which the runtime no longer applies.
  - It adds 650 production lines and removes 50 (`git diff --numstat 712eca8 <PR head>`, non-test files under `internal/` and `cmd/`).
  - Its own live run, recorded in its description, resumed canceled work while the record said active, so the structure did not prevent the failure it targets.
- **What follows.** #810's continuation cases become family R of the experiment. A structured record returns only if R shows a failure the note does not fix. Closing #812 is the PM's decision.

## Limits

- **A survey, not a measurement.** It records what the sources say each agent does. No claim here says a layer helps; §Experiment plan exists for that.
- **Versions.** Each agent was read at one release and changes quickly: Claude Code holds features behind remote flags whose server-side state is not visible, and Codex holds several that are off by default. Claims hold only for the versions named.
- **Closed source.** Claude Code's bundle strings establish that code is present, not that it runs by default.
- **Where the docs and the code disagree** (opencode's `plan` permissions, Hermes's skills placement and summary cap, Codex's `AGENTS.md` cap), the subsection follows the code and says so under "Not settled" or inline.
- **Estimates.** Code estimates are read, not implemented. The budget is an estimate from the earlier studies' token rates and a 7,500-token call; the dry run replaces it.
