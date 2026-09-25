# How agents understand input and compose context

Records the source survey for issue #860: how five established agents understand a user's input and assemble their context, and the candidate structures for sherlock's query understanding and full-context layer that follow from it. It makes no production decision and runs no experiment; §Experiment plan is the input to the next issue's preregistration.

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
- **Estimate.** Additions of about +140 to +200: the call, its schema and parse, multi-query ranking, the note row and injection. Net about −100 to −40 before D2. Each turn adds one model call, about 4,000 input tokens with this context, and its latency: the context-free phrase call it resembles took 3.5 to 4.4 s ([mid-memory retrieval](../mid-memory-rag/README.md) §Cost).
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
  - Every arm runs the production session through a study harness, as #858's parity harness did (`docs/research/mid-memory-rag/parity/`).
  - Model `MiniMax-M2`. Embedder `BAAI/bge-base-en-v1.5` on text-embeddings-inference, as in §Production parity of the mid-memory study.
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
- **Power.** 280 paired primary queries is the size at which the replication's rule reached "valid" for a true 10-point gain with probability 0.817 at 33% discordance ([mid-memory retrieval](../mid-memory-rag/README.md) §Power at the observed disagreement). Answer-level discordance may differ; the preregistration recomputes power with `replication_score.py power` from a dry run.
- **Decision rule.**
  1. Each candidate is compared with S on *applied* over the 280 primary queries, by the exact McNemar test with Holm correction across the three comparisons at family-wise α = 0.05, two-sided.
  2. A candidate is valid when its difference is positive and significant and every guardrail holds:
     - no primary family loses more than 7 of 70;
     - E and N each lose no more than 3 of 30;
     - no R canceled case is resumed that S did not resume.
  3. Among valid candidates, take the one with the smallest measured net production diff. A larger valid candidate is taken only if it beats the smaller one directly on *applied*, by the same test at α = 0.05.
  4. If no candidate is valid and every control passed, the shipped structure stands. That null is an answer: at this size, neither layer nor search changes whether the agent reaches and applies the evidence.

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
