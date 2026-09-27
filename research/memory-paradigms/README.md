# Memory paradigms beyond retrieve-into-context

Records the source survey for issue #897. Every structure sherlock has measured since #874 is one paradigm: store mid summaries, retrieve some of them, put them in context, and let the acting model answer. This survey reads four others: full context, a recall sub-agent, idle-time consolidation into a structured memory document, and parametric memory. It measures whether sherlock's validation corpora fit MiniMax-M2's documented window. It compares the paradigms with what sherlock measured, names three candidates for a validation run, and fixes a test plan that always includes a full-context ceiling. It makes no production decision and made no model call.

## Contents
- Question and method
- Sherlock's one paradigm at `38b0af0`
- MiniMax-M2 and whether the corpora fit
- Full context as memory
- A recall sub-agent
- Idle-time consolidation into a memory document
- Parametric memory
- Comparison
- What the sources settle and what they do not
- Candidates for a validation run
- Test plan
- Limits

## Question and method

The questions, from the issue:
- Which paradigms other than retrieve-into-context could beat it on associative recall, abstention and corrections, under sherlock's constraints?
- What do MiniMax's documents say about M2's context window, caching and pricing, and do sherlock's corpora fit that window?
- What should the next validation run test, including the full-context ceiling sherlock has never measured?

The constraints are the issue's: MiniMax's API only, within the subscription; local embeddings; no OpenAI or other paid API.

Method:
- **Primary sources only.** arXiv papers at a stated version, official MiniMax documentation, one engineering post at its printed date, and repositories at a stated commit or tag. No system was run and no model was called.
- **Access date.** Every source below was accessed on 2026-09-27.
- **How numbers were taken.** Each paper was downloaded as `https://arxiv.org/pdf/<id>v<n>` at its latest version and converted with `pdftotext -layout`. Every number quoted below was found in that text, and is given with its setting and table. Nothing is recomputed. A value that is plotted and not printed is marked *(figure only)*.
- **MiniMax documentation** was read through the raw `.md` twin of each page on `platform.minimax.io/docs` and its `llms-full.txt`. The retired Coding Plan pages were read from Wayback Machine captures, whose timestamp is part of the URL.
- **Repositories** name a commit and the files read, through the GitHub or Hugging Face API.
- **Reading was split into five passes,** one per paradigm and one for MiniMax, whose notes were merged here. A sample of the quoted figures was re-read from the saved text: Zep's Table 2, Mem0's Table 2, Pollertlam's Table 3, MemGPT's Table 2, MemAgent's Table 1, Chain-of-Agents' Table 5 and OpenClaw's recall defaults. Each matched.
- **What a source could not settle** is listed under each paradigm.
- **Already surveyed, not repeated.** `../associative-memory/README.md` (#877) covers MemGPT, Generative Agents, Zep, Mem0, A-MEM and IRCoT. `../memory-encoding/README.md` (#887) covers Nemori, MemoryOS and recursive summarization. They are cited where they bear on a paradigm here.
- **Sherlock.** Claims about this repository hold for `38b0af0` (origin/dev) and name the file read. Arms C and P of #890 are on that study's branch at `46ca289`, not yet on dev, and are cited at that commit.
- **Corpus sizes** come from `tokens.py` beside this file, which applies MiniMax-M2's published tokenizer to the committed corpora (§MiniMax-M2 and whether the corpora fit).

## Sherlock's one paradigm at `38b0af0`

What ships (`../../architecture/memory.md`, ADR 0006, ADR 0018, ADR 0019):
- **Write.** Each full batch of four visible messages becomes one immutable mid episode, synthesized by one session-model call and embedded locally with `BAAI/bge-base-en-v1.5`. Each episode keeps its source interval.
- **Read, before the act loop.** The two newest mids and up to five more by cosine to the task, with no model call.
- **Read, during work.** `memory_search` (five per call) and `memory_inspect` (a source by ID), within six calls of one tool per turn (`maxToolCallsPerTurn`, `internal/agent/service.go`).
- **The note.** At most 2,000 characters, restated by the model in each terminal call and shown every turn.
- **The window.** The rolling session keeps 128 messages by default (`DefaultMaxHistoryMessages`, `internal/agent/memory/compaction.go`). A negative `MaxHistoryMessages` on `gateway.Factory` disables compaction and restores every saved visible message at startup (`internal/gateway/runtime.go`). `cmd/sherlock/agent.go` builds the factory without setting it, so no operator setting reaches that mode today. The studies' live probes restore a 32-message window.

### What has been measured in this paradigm

Every structure since #874, on its own corpus. Live: applied answers, one turn per probe on MiniMax-M2 (`../chained-recall/README.md`). Chains are the primary family of each run.

| Run | Arm | What it adds to S | Chains applied | N abstained | K corrected | C1 | E | Stored per source char | Net lines |
|---|---|---|---:|---:|---:|---:|---:|---:|---:|
| #874 | S | the shipped recall | 4 of 114 | 15 of 20 | 16 of 20 | 27 | 12 of 20 | 0.705 | 0 |
| #874 | M | an instruction to follow names | 10 | 14 | 17 | 28 | 12 | 0.705 | +2 to +4 |
| #874 | L | entity links and a PageRank walk | 47 | 11 | 14 | 30 | 13 | 0.782 | about +270 |
| #885 | S | | 9 of 100 | 57 of 60 | 28 of 40 | 25 | 18 of 30 | 0.731 | 0 |
| #885 | L | | 56 | 39 | 25 | 30 | 21 | 0.814 | about +270 |
| #885 | L+A | a label and an abstention instruction | 57 | 42 | 21 | 30 | 20 | 0.814 | about +292 |
| #888 | S | | 4 of 100 | 57 of 60 | 28 of 40 | 27 | 20 of 30 | 0.730 | 0 |
| #888 | L | | 60 | 36 | 25 | 30 | 20 | 0.813 | about +270 |
| #888 | L+G | a gate on the query's entities | 7 | 52 | 27 | 30 | 23 | 0.813 | about +284 |
| #888 | L-T | the walk as a tool only | 13 | 59 | 24 | 28 | 20 | 0.813 | about +265 |
| #888 | L+V | inspect before stating, enforced in scoring | 11 | 6 | 27 | 23 | 22 | 0.813 | about +272 |

Offline only:
- **H,** a second level of group summaries (#874): chains no better than S, and raises stored characters to 1.107 and 0.943 per source character.
- **C,** context at synthesis (#890 at `46ca289`, `../memory-encoding/validation/arms/context/README.md` there): reaches 39 of 50 situated items against S's 0. It puts the near-miss value in view with the entity in 24 of 30 NC items against S's 0, and leaves KU at 20 of 30 against 27. Two of its controls failed, so it was not admitted to the live stage. Its summaries took in the context messages in 49 of 50 SC items, against the prompt's rule.
- **P,** an episode linked to the previous one (#890 at `46ca289`, `arms/prev/README.md` there): reaches no chain, and puts the near-miss value in view for 17 of 30 NC items against S's 0.

What the runs establish, across seeds:
- **Association gains come with abstention losses.** L applied 47 to 60 chains where S applied 4 to 9, and lost 18 to 21 abstentions of 60 on #885's and #888's absent family (N). With the associated episode in view, L gave the associated entity's value in 19 and 21 of 60 absent probes, S in 1.
- **Applying follows seeing.** In every arm a chain was applied almost only where each of its hops was in view: #885's S 9 of 9, L 56 of 57, L+A 57 of 57.
- **Instructions barely move MiniMax-M2.** M's instruction: 10 against 4, p = 0.11. L+A's label and instruction recovered 3 of L's 18 lost abstentions. L-T's walk tool was called in 4 of 100 primary probes. L+V's inspect-before-stating was kept for 16 of 121 facts stated from a walk mid.
- **Implicit chains fail at reach.** No arm applied more than 4 of 25 IC3 or any IC2.

### Per-turn cost of what ships

Computed for this survey from the committed ledgers (`python3` over `calls.jsonl`, status 200, arm `S`, phase `live`):

| Measure | #885 S | #888 S |
|---|---:|---:|
| Act calls per probe, mean | 2.51 (777 over 310) | 2.33 (723 over 310) |
| Prompt tokens per act call, mean | 4,443 | 4,390 |
| Latency per act call, p50 and p90 | 3.4 s, 5.9 s | |
| Model time per probe, p50 and p90 | 9.0 s, 13.5 s | |

- **Write time:** one synthesis call per four messages. #885's 873 shared batch-4 calls averaged 328 prompt tokens, with a p50 latency of 2.8 s.
- **Idle time:** none. Nothing runs between turns.

## MiniMax-M2 and whether the corpora fit

### What MiniMax documents

Every page accessed 2026-09-27. The current documentation is written for MiniMax-M3 and lists M2 under legacy models.

- **Context window.**
  - The model table of `https://platform.minimax.io/docs/guides/text-generation.md` gives "MiniMax-M2 | 204,800". The Anthropic and OpenAI API pages carry the same row.
  - `https://platform.minimax.io/docs/guides/models-intro.md`: "Context Length: 200k tokens", "Maximum Output: 128k tokens (including CoT)".
  - The open weights (`MiniMaxAI/MiniMax-M2` on Hugging Face at `757303d492a50514c312788b5247a4f696a4c6a3`, `config.json`) set `"max_position_embeddings": 196608`. The model card states no window. Its evaluations ran "with 128k context length".
- **Maximum output** conflicts within the documents: "128k tokens (including CoT)" on the models page, and a `max_tokens` maximum of "204800 (200K)" for "other models" than M3 on `text-chat-anthropic.md` and `text-chat-openai.md`.
- **Caching.**
  - *Explicit* `cache_control` exists on the Anthropic-compatible endpoint only, and lists M2 (`anthropic-api-compatible-cache.md`). Cached content has "a lifetime of 5 minutes", refreshed on each hit. M2's row: input $0.3, output $1.2, cache read $0.03 and cache write $0.375 per million tokens.
  - *Automatic* caching works on both endpoints for calls "with 512 or more input tokens", by prefix match, with "No additional charge for cache writes" (`text-prompt-caching.md`). Its supported models are M3, M2.7, M2.5 and M2.1. M2 is not listed.
  - Sherlock can call either protocol (`openai-compatible` or `anthropic-compatible`, `cmd/sherlock/model_provider.go`). The studies call the OpenAI-compatible one (`../chained-recall/study/env.go`), and sherlock's Anthropic client sends no `cache_control` (`rg -i cache internal/agent/anthropic/` matches nothing; the same search over `docs/research/chained-recall/harness/` matches the proxy's synthesis cache). So on the path every study used, no caching is documented for M2.
- **Pricing basis.**
  - Pay-as-you-go (`pricing-paygo.md`): $0.3 per million input and $1.2 per million output tokens. Rate limits (`rate-limits.md`): 500 requests and 20,000,000 tokens per minute.
  - At M2's launch the subscription was the Coding Plan, metered in "100 prompts / 5 hours", "300" and "1000" for $10, $20 and $50 a month, where "1 "prompt" is roughly equivalent to 15 requests to the model" (Wayback captures `20251111035356` of `pricing-codingplan` and `20251111231539` of `coding-plan/intro`).
  - It has been replaced by the Token Plan (`token-plan/intro.md`), which "now uses usage-based deduction" at "the corresponding endpoint pricing", over "5-hour rolling and weekly windows" (`token-plan/faq.md`, `pricing-token-plan.md`). No numeric quota per tier is stated. Its model coverage names "M3 / M2.7 / image / speech", not M2.
  - Error 2056: "usage limit exceeded | Please wait for the resource release in the next 5-hour window" (`errorcode.md`).
- **Thinking.** M2's thinking must be passed back unchanged and cannot be disabled: "For M2.x models, thinking cannot be disabled" (`text-anthropic-api.md`). The model card: "it is important to retain the thinking content from the assistant's turns within the historical messages".
- **Fine-tuning.** None is documented for any MiniMax language model through the API (§Parametric memory).

### Whether sherlock's corpora fit

`python3 docs/research/memory-paradigms/tokens.py <tokenizer.json> <#890 corpus.json>` writes `tokens.json`.
- **Tokenizer.** MiniMax-M2's own byte-level BPE, from `tokenizer.json` at the revision above, applied in plain Python because the `tokenizers` package is not installed. Messages are counted as M2's chat template (`chat_template.jinja` at the same revision) frames them: `]~b]user\n<content>[e~[\n`, with `ai` for the assistant.
- **Validation against MiniMax's own counts.** #874's 424 distinct batch-4 synthesis requests differ only in their user message. For every one, the `prompt_tokens` MiniMax reported (`../chained-recall/evidence/ledger/calls.jsonl`) minus this count of that message is exactly 273. So the count is exact on that text. The base holds 1,342 non-ASCII characters that the validation did not exercise. Even a one-token error on each would move a corpus total by less than 1%.
- **The corpora.** Each is the #856 replication's 4,800 messages (the base) followed by its study's stream.

| Corpus | Messages | Characters | Content tokens | Templated tokens | Stream alone, templated |
|---|---:|---:|---:|---:|---:|
| #874 chained recall | 6,496 | 979,988 | 206,359 | 238,839 | 29,548 |
| #885 confirmation | 8,288 | 1,038,601 | 223,039 | 264,479 | 55,188 |
| #888 read-time variants | 8,288 | 1,038,164 | 223,130 | 264,570 | 55,279 |
| #890 encoding validation, at `46ca289` | 8,928 | 1,062,421 | 228,926 | 273,566 | 64,275 |

- **No corpus fits whole.** 238,839 to 273,566 templated tokens exceed both the API's 204,800 and the weights' 196,608, before instructions, tools and output.
- **Every stream fits.** Chains, SC, UP, N, NC and KU all have their evidence in the stream.
- **A truncated window.** Under a history budget of 180,000 tokens (§W. Full-context ceiling), the newest 5,207 to 6,890 messages fit. That holds the whole stream and the base's newest messages from ID 1,290 (#874), 1,840 (#885), 1,841 (#888) and 2,039 (#890).
  - E and K have their evidence in the base, so part of it falls outside: E 15 of 20 in view for #874, and 15, 18 and 17 of 30 for #885, #888 and #890. K is 20 of 20, then 39, 38 and 38 of 40.
- **Content runs 4.6 to 4.7 characters per token** over the whole corpus, and 3.7 to 3.9 over the streams alone, whose invented names split into more tokens.

## Full context as memory

The whole history in the prompt, with no memory structure. It is the ceiling most conversational-memory papers report against.

### Sources

- **LoCoMo** (Maharana et al., arXiv 2402.17753v1; repository `snap-research/locomo` at `3eb6f2c585f5e1699204e3c3bdf7adc5c28cb376`).
  - Conversations average 9,209.2 tokens and 304.9 turns (Table 1). The public release keeps "the longest conversations", 10 of the 50 (README), which later papers measure at about 16,000 to 26,000 tokens.
  - gpt-3.5-turbo-16k given 16K of history (Table 2), F1: single-hop 56.4, multi-hop 42.0, temporal 20.3, open-domain 37.2, adversarial 2.1, overall 37.8. RAG over observations, top 5 (Table 3): overall 41.4, multi-hop 30.6, adversarial 44.7.
  - Adversarial F1 falls as the window grows: 13.1 at 4K, 8.4 at 8K, 6.4 at 12K, 2.1 at 16K.
- **LongMemEval** (Wu et al., arXiv 2410.10813v2; repository `xiaowu0162/LongMemEval` at `9e0b455f4ef0e2ab8f2e582289761153549043fc`).
  - LongMemEval_S is "∼115k tokens/question", LongMemEval_M about 1.5M.
  - Full history against the evidence sessions alone (Figure 3b, values printed in the panel): GPT-4o 0.606 against 0.870, and with Chain-of-Note 0.640 against 0.924. Llama 3.1 70B 0.334 against 0.744. The abstract: "a 30% accuracy drop".
  - Retrieval over the 1.5M-token _M history beats full context over the 115K-token _S history for every model of Table 8. For example Llama-3.2-3B: 0.470 against 0.010.
  - Its 30 abstention questions get no per-type full-context score.
- **Mem0** (Chhikara et al., arXiv 2504.19413v1), LoCoMo's 10 conversations at "26000 tokens on average", adversarial category excluded (Table 2).
  - LLM-judge J: full context 72.90, Mem0g 68.44, Mem0 66.88, Zep 65.99, best RAG 60.97. The full-context model is not named; Mem0 runs on gpt-4o-mini.
  - Total latency p50 and p95: full context 9.870 s and 17.117 s, Mem0 0.708 s and 1.440 s. Context tokens: 26,031 against 1,764.
- **Zep** (Rasmussen et al., arXiv 2501.13956v1), LongMemEval_S at 115K tokens (Tables 2 and 3).
  - gpt-4o: full context 60.2% at 28.9 s, Zep 71.2% at 2.58 s with 1.6K tokens. gpt-4o-mini: 55.4% at 31.3 s against 63.8% at 3.20 s.
  - Full context wins single-session-assistant (94.6 against 80.4 on gpt-4o) and, on gpt-4o-mini, knowledge-update (76.9 against 74.4).
  - Zep ran remotely with network latency; the baselines did not.
- **Nemori** (arXiv 2508.03341v4, retitled "What Deserves Memory").
  - LoCoMo, LLM score (Table 2): gpt-4o-mini average 72.3 full context against 73.0, multi-hop 66.8 against 61.7. gpt-4.1-mini: 80.6 against 80.8, multi-hop 77.2 against 74.8.
  - LongMemEval_S at 101K tokens (Table 8): gpt-4o-mini 55.0 against 64.2; knowledge-update 78.2 against 61.5. gpt-4.1-mini: 65.6 against 74.6.
  - Response cost on LoCoMo (Table 4): full context 23,653 tokens and 5,806 ms, Nemori 2,745 tokens and 3,053 ms.
- **LightMem** (arXiv 2510.18866v4), LongMemEval_S at 105K tokens (Tables 2 and 7).
  - GPT-4o-mini: full text 56.80, LightMem 68.64. Multi-session 45.45 against 71.74; single-session-assistant 89.29 against 32.14. Qwen3-30B: 54.80 against 70.20.
  - GLM-4.6: full text 36.71 against 73.20. On LoCoMo (Table 3), Qwen3-30B's full text, 74.87, is the highest row.
- **A-MEM's full-context row** (arXiv 2502.12110v11, Table 1, 16,910 tokens): on LoCoMo with GPT-4o-mini, F1 temporal 18.41 against A-MEM's 45.85, adversarial 69.23 against 50.03.
- **Pollertlam and Kornsuwannawit,** "Beyond the Context Window" (arXiv 2603.04814v1), with tokens counted in `o200k_base`.
  - Accuracy (Table 3): GPT-5-mini with the long context 92.85 on LoCoMo and 82.40 on LongMemEval, against Mem0's 57.68 and 49.00. GPT-OSS-120B: 81.69 and 48.20.
  - With caching modelled as a 90% input discount from turn 2 (Table 5), full context stays cheaper than Mem0 up to 13 turns at 30K tokens, 10 at 100K and 9 at 200K.
- **Fidelity Before Structure** (arXiv 2601.00821v4). With a gpt-4o answerer on LoCoMo categories 1 to 3 (Table 1), full context 59.9 beats verbatim chunks at 43.9. With gpt-4o-mini, full context 40.5 loses to chunks at 47.9. On LongMemEval_S's 30 abstention questions (Table 2): chunks 46.7, RAG 70.0, extracted artifacts 43.3, and no full-context row.
- **Hindsight** (arXiv 2512.12818v1, Table 3), LongMemEval_S: full context on GPT-OSS-20B 39.0 against Hindsight's 83.6, under mixed judges.
- **Length studies, not conversational.**
  - RULER (arXiv 2404.06654v3, Table 3): GPT-4's effective length is 64K of 128K claimed; 81.2 at 128K against 96.6 at 4K.
  - Lost in the Middle (arXiv 2307.03172v3): GPT-3.5-Turbo "can drop by more than 20%" when the answer sits mid-context.
  - NoLiMa (arXiv 2502.05167v3, Table 3), needles with little lexical overlap with the question: GPT-4o's effective length is 8K, and its score falls from 99.3 to 69.7 at 32K; GPT-4o mini from 84.8 to 13.7.

### What the sources say about full context

- **Up to about 26K tokens,** full context matches or beats memory systems overall, and most consistently on multi-hop: Mem0's J 72.90 against 68.44, Nemori's multi-hop on both models, Fidelity's gpt-4o run.
- **At 100K to 115K tokens,** it loses overall on every model but one: gpt-4o, gpt-4o-mini, gpt-4.1-mini, GLM-4.6, Qwen3-30B, GPT-OSS-20B and, by 0.8 points, GPT-OSS-120B. The exception is GPT-5-mini, 82.40 against Mem0's 49.00.
- **Even there it keeps single-session-assistant,** in every per-type table read, **and often knowledge-update.**
- **Latency is several times higher:** 5.8 to 31 s per answer against 0.7 to 3.2 s (Mem0, Zep and Nemori's tables).
- **Not settled.**
  - No source reports full-context abstention on LongMemEval. On LoCoMo's adversarial category the direction depends on the model: 2.1 F1 for gpt-3.5-turbo-16k, above A-MEM for GPT-4o-mini and GPT-4o.
  - No source measures MiniMax-M2 at any history length. Its model card evaluates at 128K; no effective-length figure exists for it.
  - Sherlock's corpora are 239K to 274K tokens, above every conversational benchmark read. Truncated to 180K they sit above LongMemEval_S.
  - LoCoMo's category labels differ between papers (A-MEM and Mem0 print the same F1 under different names), and "LoCoMo" means 50 conversations in 2024 and 10 from 2025 on. Cross-paper category figures are not comparable, as #877 found.

## A recall sub-agent

A separate loop that searches memory, inspects sources and follows chains, and returns only what it found to the acting model.

### Sources

- **OpenClaw Active Memory** (`openclaw/openclaw` at `e1b0d1fbdfe933a1e8463a3f26cf0b232eece15e`; `docs/concepts/active-memory/*.md`, `extensions/active-memory/*.ts`).
  - *Two lanes.* A word-matching first pass injects up to 3 raw memory entries, at most 1,800 characters, when a match scores 0.65 or more (`trigger-recall.ts`). A code comment says 0.65 was "measured on a 20-trigger/50-unrelated synthetic corpus".
  - *When the sub-agent runs.* In the default `escalate` mode, only when the first pass has no strong hit and the message matches a recall-intent pattern such as "do you remember" or "last time" (`escalation.ts`). In `always` mode, every eligible turn.
  - *What it has.* `memory_search` and `memory_get`, which reads a memory file or line range. It sees the latest message, a recent tail or the whole thread, by `queryMode`. Thinking is off by default, "since active memory runs in the reply path".
  - *Bounds.* No step or tool-call cap in `recall-run.ts`. A timeout of 15,000 ms by default, 250 to 120,000 allowed (`types.ts`, `configuration.md`).
  - *What it returns.* `NONE`, or "one compact plain-text summary" of at most 220 characters (`DEFAULT_MAX_SUMMARY_CHARS`, `prompt.ts`), with no citations. Its prompt styles range from "Aggressively prefer NONE" (`precision-heavy`) to "lean toward returning memory instead of NONE" (`recall-heavy`).
  - *Numbers.* None: no recall, abstention or latency measurement.
- **MemGPT and Letta** (arXiv 2310.08560v2; `letta-ai/letta` at tag `0.16.8`, `1131535716e8a31c9a437f8695e25ac98f203a24`). Not a separate agent: the acting model searches, pages and chains its own calls (`request_heartbeat`), and sees raw results. Letta caps a turn at `DEFAULT_MAX_STEPS = 50` (`letta/constants.py`). Deep Memory Retrieval (Table 2): GPT-4 92.5% against 32.1% for GPT-4 given summaries. No count of calls, tokens or latency.
- **Anthropic, "How we built our multi-agent research system"** (`https://www.anthropic.com/engineering/multi-agent-research-system`, printed "Published Jun 13, 2025").
  - A lead agent spawns subagents that search in their own context and return condensed findings; a separate CitationAgent then attributes claims to sources.
  - "outperformed single-agent Claude Opus 4 by 90.2% on our internal research eval". "agents typically use about 4× more tokens than chat interactions, and multi-agent systems use about 15× more tokens than chats".
  - The eval is internal, with no absolute scores. The CitationAgent's effect is not measured separately.
- **Search-o1** (arXiv 2501.05366v1). The reasoner never sees raw documents: after each search a second call of the same model condenses them ("Reason-in-Documents"). QwQ-32B, EM (Table 3): HotpotQA 45.2 against 43.0 for an agent reading raw text, MuSiQue 16.6 against 13.6, 2WikiMultihopQA 58.0 against 58.4. No full-context baseline, no cost.
- **Chain-of-Agents** (arXiv 2406.02818v1). Workers read chunks in turn and pass evidence to a manager that answers; l + 1 calls per query.
  - NarrativeQA against Claude 3's 200K context (Table 5): claude-3-opus 23.96 against 6.56 for the full context and 5.86 for RAG.
  - text-bison, MuSiQue (Table 4): 37.09 against 33.83 for RAG and 26.87 for the 8K baseline. Without the manager (Table 7): 26.79.
- **MemAgent** (arXiv 2507.02259v2). A model trained with RL reads 5,000-token chunks into a 1,024-token memory it overwrites; one call per chunk. RULER-HotpotQA (Table 1): 75.78 at 896K tokens, against 0.00 for Qwen2.5-14B-1M reading the full context.
- **CRAG** (arXiv 2401.15884v3). A separate T5 evaluator judges retrieval before generation. It judges correctly 84.3% of the time, against ChatGPT's 58.0 (Table 4). PopQA accuracy 52.8 to 59.8 (Table 1). Generation time per instance rises from 0.363 s to 0.512 s (Table 6).

### What the sources say about a recall sub-agent

- **Separating the loop from the answer is common, and measured only on document QA.** Search-o1, Chain-of-Agents and MemAgent each keep raw text from the answering model and gain on multi-hop QA. None is a conversational-memory benchmark.
- **The chain-following evidence is the model-directed loop's:** MemGPT's nested lookups, IRCoT and Self-Ask (#877). MemGPT's weaker models failed "to perform enough lookups".
- **No source checks that a returned fact rests on a source the loop read.** OpenClaw returns 220 uncited characters. Search-o1 and Chain-of-Agents pass condensed text. Anthropic's CitationAgent adds citations after the fact, unmeasured.
- **The cost is calls and latency.** Anthropic reports about 4× chat tokens for one agent. OpenClaw budgets 3 to 5 s for its lightest mode and 15 s otherwise, and turns thinking off for latency.
- **Not settled.**
  - Abstention: OpenClaw designs `NONE` in and measures nothing. No source reports a sub-agent's effect on answering absent questions.
  - Whether a separate loop follows chains better than the acting model's own search on the same model. No source compares the two.
  - Corrections: no source measures them.

## Idle-time consolidation into a memory document

A background pass, between turns, that reads episodes and rewrites a structured memory: deduplicated, organized by entity or topic, and always in view of the acting model. "Sleep-time" in Letta's name; "dreaming" in OpenClaw's.

### Sources

- **Sleep-time compute** (Lin et al., arXiv 2504.13171v1, the only version).
  - At idle time the model rewrites a context `c` into `c'`, "prompting the model to draw inferences and re-write c in a way that might be useful at test-time" (§3). Test-time queries then read `c'`.
  - About "∼5×" fewer test-time tokens at equal accuracy on Stateful GSM-Symbolic and Stateful AIME. Up to 13% and 18% more accuracy from more sleep-time compute. Up to 2.5× lower cost per query at 10 queries per context, with test-time tokens priced at 10 times sleep-time ones. All *(figure only)*.
  - The reference rewrite (Appendix F) overwrites the memory block. No task involves conversation memory or corrections.
- **Letta's sleep-time agent** (`letta-ai/letta` at tag `0.16.8`; `letta/server/server.py`, `letta/groups/sleeptime_multi_agent_v4.py`, `letta/prompts/system_prompts/sleeptime_v2.py`).
  - A second agent shares the primary agent's memory blocks and copies its model configuration. It runs as a background task every 5 primary turns by default (`sleeptime_agent_frequency=5`), and does not block the reply.
  - It reads the transcript since its last pass and edits the blocks with `memory_replace`, `memory_insert`, `memory_rethink` and `memory_finish_edits`. Its prompt asks for blocks that are "comprehensive, readable, and up to date", with absolute dates.
  - The primary agent loses its own block-editing tools and keeps `conversation_search`, so raw messages stay reachable. Blocks are "always visible - no retrieval needed" (docs.letta.com, memory blocks). A block edit overwrites its value.
  - Letta publishes no measurement of it.
- **OpenClaw Dreaming** (`openclaw/openclaw` at `e1b0d1fbdfe933a1e8463a3f26cf0b232eece15e`; `docs/concepts/dreaming.md`, `src/memory-host-sdk/dreaming.ts`, `extensions/memory-core/src/dreaming-consolidation.ts`, `dreaming-phases.ts`).
  - On by default, as a daily cron job at 03:00 (`DEFAULT_MEMORY_DREAMING_FREQUENCY`). Three phases; only the last, "deep", writes `MEMORY.md`, which is "Loaded at the start of a session". Daily notes stay on disk and searchable.
  - A candidate is promoted only when all three gates pass: score at least 0.75, recalled at least 3 times, from at least 3 distinct queries.
  - The model does not write the memory. One tool-free call "Emit[s] exactly one operation per candidate: candidateKey, action (added, merged, or superseded)"; the host writes a sourced entry of at most 160 tokens, with a `Source: path#Lx-Ly` reference.
  - Before writing, the host rejects a supersession without a matching lineage key and a rewrite that removes more than 25% of prior entries, and it stores the previous `MEMORY.md`.
  - Nothing is evaluated.
- **Generative Agents' reflection** (arXiv 2304.03442v2). Triggered when summed importance passes 150, "roughly two or three times a day". It reads the 100 most recent records and appends insights citing them; nothing is overwritten.
  - Believability TrueSkill: 29.89 with reflection, 26.88 without.
  - 1.3% (6 of 453) of answers about other agents were hallucinated; embellishments were observed but not traced to reflections.
- **Nemori** (arXiv 2508.03341v4; `nemori-ai/nemori` at `d2a6dff6e5481214a0be6a2d10147feccfc16244`). It consolidates at write time, not idle time (#887 covers its design).
  - Its new/merge/conflict management gives no gain on LoCoMo: 64.6 against 65.0 without it on gpt-4o-mini, in the ablation without episodic memory (Table 5). The paper says LoCoMo "rarely involves knowledge updates".
  - On main, semantic memories are only appended (`nemori/core/memory_system.py` `save_batch`). A conflict step exists only on branch `feat/semantic_manager` at `333f827fead47d4561ff1979bc86badbbfeeea50`. Merged episodes delete their originals.
- **LightMem's offline "sleep-time update"** (arXiv 2510.18866v4, Table 2, LongMemEval_S). Each entry is updated only by later, similar entries, offline and in parallel.
  - GPT-4o-mini, before and after the offline pass: 67.78 to 65.39 and 68.64 to 67.07 in two settings, 64.29 to 64.69 in a third. Qwen3-30B: 70.20 to 65.14, 68.69 to 67.34, and 61.95 to 62.34.
  - So it lowered accuracy in 4 of 6 settings, by up to 5.06 points, while the paper's introduction says it "enhances long-term memory reliability".
- **ChatGPT's memory as an always-in-view document** (measured in Mem0, arXiv 2504.19413v1, Table 2). The memories ChatGPT generated from each LoCoMo conversation, given in full as context: J 52.90 with 4,437 tokens. Mem0's retrieval scored 66.88 with 1,764 and full context 72.90. Temporal J was "below 15%, primarily due to missing timestamps". OpenAI's own memory pages returned HTTP 403 and were not read.
- **Auto-Dreamer** (arXiv 2605.20616v1). A consolidator trained with GRPO rewrites a region of entries every 5 to 10 sessions, inspecting provenance-linked raw trajectories. The untrained rewrite alone makes the bank 6 to 11 times smaller (Table 2), and success rates rise, for example ALFWorld 48.0 to 60.2. Procedural tasks, not user facts.
- **Anthropic Managed Agents "Dreams"** (`https://platform.claude.com/docs/en/managed-agents/dreams`). An on-demand job over 1 to 100 sessions that writes a new memory store with "duplicates merged, stale or contradicted entries replaced with the latest value"; "The input store is never modified". No measurement is published.

### What the sources say about consolidation

- **The always-in-view document is designed, rarely measured, and where measured it lost.** ChatGPT's memory in full scored 52.90 against 66.88 for retrieval and 72.90 for full context. LightMem's offline pass lowered accuracy in 4 of 6 settings. Nemori's conflict management changed nothing on LoCoMo.
- **The safeguards that exist are structural, in the newest designs.** OpenClaw lets the model choose an operation and has the host write a sourced entry, bounds how much a rewrite may delete, and keeps the previous file. Anthropic's Dreams writes a new store and keeps the input. Letta keeps raw messages searchable.
- **Idle time moves calls off the reply path.** Letta's pass runs in the background every 5 turns, and OpenClaw's once a day.
- **Not settled.**
  - No source measures hallucination introduced by a consolidation pass, or abstention with a consolidated memory in view.
  - Correction evidence is indirect: Nemori's knowledge-update score on gpt-4o-mini fell to 61.5 against full context's 78.2, while LightMem's rose to 83.12 against 76.92.
  - How much a bounded document drops when the history outgrows it. OpenClaw truncates the injected copy past its budget and measures nothing.

### Against sherlock's addressed store

Sherlock has run a write-time structured store once: the addressed memory ADR 0018 first decided ("A fact is held at the address of the question it answers", at `b2e9ac8`), measured in `../recall-specificity/README.md` and retired by #823.
- **Where it failed.**
  - *At read.* The read step resolved an address from the question alone. With the subject left out of the question, every probe missed: 0 of 10 at L3, each placing an invented subject such as "someone". No L1 or L2 probe reached its address (0 of 20); every correct answer came from widening to the subject.
  - *At write.* 2 of 10 facts were stored with the fact inside the address and a date as the value.
  - *And it kept no source,* so exact wording could not be recovered (ADR 0018's context as revised).
- **What an always-in-view document changes.** There is no read-side address to resolve: the whole document is in context, and #874 found that what is in view is what gets applied. So the L3 failure cannot occur. If each line cites its mid, the source path ADR 0018 now requires is kept.
- **What it repeats.**
  - A model still decides at write what the fact is and where it goes, the step that misfiled 2 of 10 facts.
  - A pass that rewrites the document from itself and new mids is the recursive summary ADR 0006 rejected, because it "would make episodes less specific to retrieval and propagate earlier synthesis errors". Recursive summarization's audit found 2.7% fabricated facts in its last-session memory (#887).
  - A bounded document must choose what to drop once the history outgrows it.

## Parametric memory

Memory stored in the model's weights: fine-tuning, LoRA adapters or knowledge editing.

### Sources

- **ROME** (arXiv 2202.05262v5) rewrites one mid-layer MLP matrix per fact and needs weights and gradients. CounterFact, one edit per case (Table 4): efficacy 100.0 and 99.9 on GPT-2 XL and GPT-J. Its limits (§3.7): "it only edits a single fact at a time", and edited models "will guess plausible new facts that have no basis in evidence".
- **MEMIT** (arXiv 2210.07229v2) edits many facts at once. 10,000 CounterFact edits on GPT-J (Table 2): efficacy 98.9, neighbourhood specificity 73.7 against 83.5 unedited. ROME at the same scale scores 2.6 on zsRE (Table 1). "ROME performs well up to n = 10 but degrades starting at n = 32". 10,000 edits take 7.44 hours (Appendix B). Conflicting edits were filtered out of the evaluation (§5.2.2).
- **MQuAKE** (arXiv 2305.14795v3) asks multi-hop questions after edits.
  - GPT-J on MQuAKE-CF (Table 3): ROME and MEMIT recall 88.3 and 96.2 of edits, but answer 7.4 and 7.0 of multi-hop questions, against 40.5 unedited. MQuAKE-T (Table 4): 100.0 edit-wise, 0.3 multi-hop.
  - MeLLo keeps the model frozen, stores edits outside it and retrieves them; it runs on GPT-3.5 through its API. Multi-hop accuracy as the number of edited instances grows (Table 5): 52.1 at 1 to 30.9 at 3,000 on CF, 91.1 to 85.5 on T.
- **Ovadia et al.,** "Fine-Tuning or Retrieval?" (arXiv 2312.05934v3). Questions on events after the 7B models' cutoff (Table 2): RAG 0.875, 0.585 and 0.876 for Mistral, Llama2 and Orca2; fine-tuning 0.504, 0.219 and 0.511. Llama2's base was 0.353.
- **Gekhman et al.** (arXiv 2405.05904v3), PaLM 2-S on EntityQuestions. Fitting unknown facts lowers accuracy on other questions: βunk −8.3 in distribution and −3.0 out of distribution (Table 1). Training on unknown facts only: 25.8 at convergence against 43.5 for the natural mix (Table 2).
- **Architectural memory** (MemoryLLM, arXiv 2402.04624v2; Titans, arXiv 2501.00663v1; MemoRAG, arXiv 2409.05591v3) needs a custom architecture or memory model trained by its user: MemoryLLM took "8 A100-80GB GPUs for three days" on a 7B model.
- **MiniMax.**
  - No fine-tuning, LoRA or custom-training endpoint for any language model appears in `https://platform.minimax.io/docs/llms-full.txt` or its Chinese twin (searched for `fine-?tun|lora|custom model|training job|微调`). The file-upload `purpose` enum holds only `voice_clone`, `prompt_audio` and `t2a_async_input`. The only LoRA recipe is for the H3 video model on self-hosted weights.
  - M2's weights (Hugging Face at `757303d`): "230 billion total parameters with 10 billion active parameters", mixture of experts, FP8. The vLLM guide at that commit gives "220 GB for weights" for inference alone.

### What the sources say about parametric memory

- **Infeasible under sherlock's constraints.** Every method needs weights and GPUs. MiniMax documents no training endpoint, and self-hosting M2 needs 220 GB of GPU memory before any training; local compute here is for embeddings only.
- **It would also fail on what sherlock measures.** Edited facts do not carry into multi-hop questions (MQuAKE: 7.0 to 7.4 against 40.5), and fitting new facts raises wrong answers elsewhere (Gekhman).
- **Not settled.** Whether an undocumented enterprise fine-tuning offer exists. No source tests correcting the same fact twice, which is what a conversation correction is.

## Comparison

"Measured" rows are sherlock's own runs (§Sherlock's one paradigm at `38b0af0`). The others are what the sources report, in their settings; none uses MiniMax-M2.

| Paradigm | Chains | Implicit association | Abstention | Corrections | Compression | Per-turn calls and latency | Write-time and idle-time calls | Implementation cost |
|---|---|---|---|---|---|---|---|---|
| S, shipped (measured) | 4 to 9 of 100 | IC 0 of 50 | N 57 of 60 | K 28 of 40 | 0.73 stored per source char; about 3,300 context chars | 2.3 to 2.5 calls, 4,400 prompt tokens each; 9.0 s p50 per probe | 1 call per 4 messages; none idle | 0 |
| L, entity walk (measured) | 47 to 60 | IC 0 to 4 of 50 | N 36 to 39; associated value given in 19 to 21 | K 25 | 0.81 | fewer act calls than S (505 against 587 in #874) | same call, +61 input and +63 output tokens | about +270 |
| C, context at synthesis (measured offline) | 0 | SC 39 of 50 against 0 | near miss in view in 24 of 30 NC | KU 20 of 30 against 27 | 0.93 | as S; 3,990 context chars | same call, 521 input and 489 output tokens against 327 and 205 | +47 to +63 |
| Full context | wins multi-hop up to 26K tokens; loses overall at 100K to 115K on 7 of the 8 models read | latent needles fall with length (NoLiMa: GPT-4o 99.3 to 69.7 at 32K) | no conversational figure; LoCoMo adversarial depends on the model | often wins knowledge-update (Zep gpt-4o-mini 76.9 against 74.4; Nemori 78.2 against 61.5) | none | no extra call; whole history in every call; 5.8 to 31 s per answer at 24K to 115K | none | 0 to measure (`MaxHistoryMessages < 0`) |
| Recall sub-agent | model-directed loops gain on multi-hop QA (MemGPT; Search-o1 HotpotQA 45.2 against 43.0) | not measured | `NONE` designed in (OpenClaw), not measured | not measured | the actor sees only what the loop returns (OpenClaw: at most 220 chars) | +1 loop per turn; OpenClaw budgets 3 to 15 s; about 4× chat tokens (Anthropic) | none | §Candidates: +120 to +190 |
| Idle-time consolidation | not measured on conversational chains | standing facts in view weigh heavily (Nemori without semantic memory 73.0 to 54.7, #887) | not measured | indirect and mixed (Nemori 61.5 against full context's 78.2; LightMem 83.12 against 76.92) | Auto-Dreamer's rewrite: 6 to 11 times smaller; ChatGPT's memory 4,437 tokens at J 52.90 | no extra call; the document in every call | 1 pass per 5 turns (Letta) or per day (OpenClaw), off the reply path | §Candidates: +135 to +215 |
| Parametric | edited facts fail multi-hop (MQuAKE 7.0 to 7.4 against 40.5) | not measured | new facts raise wrong answers elsewhere (Gekhman) | repeated overwrite not tested | in weights | none | training per update | infeasible: no training endpoint; 220 GB to run M2 |

Which could beat retrieve-into-context on associative recall, abstention and corrections, under sherlock's constraints:
- **Full context cannot be adopted.** No corpus fits the window, and ADR 0006 rejected "Keeping every source message in the rolling prompt" because it "would remove the context bound". As a ceiling it answers the question every earlier run left open: with the associations in view in their original order and wording, does MiniMax-M2 still answer absent questions from them?
- **A recall sub-agent is the one paradigm that separates reaching evidence from what the acting model sees.** Every association gain so far came from putting associated text in the actor's view. The one measured live, L, cost abstention, and C and P put near-miss values in view offline. A loop that reads and returns only cited facts could keep the reach without the view. Its abstention and correction effects are unmeasured anywhere.
- **Idle-time consolidation could beat it on standing facts** and on chains whose hops share an entity line. It repeats the write-side risks of sherlock's addressed store and of recursive summarization. The measured always-in-view documents lost.
- **Parametric memory cannot run** under the constraints, and fails multi-hop in its own benchmark.

## What the sources settle and what they do not

**Settled, within the settings named:**
- **Full context is strongest at short histories and weakens with length.** Up to about 26K tokens it matches or beats memory systems, most consistently on multi-hop. At 100K to 115K it loses overall on seven of the eight models read, while keeping single-session-assistant and often knowledge-update. Sherlock's corpora are 239K to 274K tokens.
- **Model-directed loops gain on multi-hop questions,** at a model call per step, and separate condensing loops gain on document QA (Search-o1, Chain-of-Agents, MemAgent). MemGPT's weaker models stopped short, "failing to perform enough lookups".
- **Parametric memory is infeasible here,** and edited facts do not carry into multi-hop questions.
- **Always-in-view memory documents, where measured, lost or changed nothing:** ChatGPT's memory at J 52.90 against 66.88 for retrieval, LightMem's offline pass down in 4 of 6 settings, Nemori's conflict step neutral.
- **M2's documented window is 204,800 tokens on the API and 196,608 in the weights,** with no caching documented on the path sherlock's studies use.

**Not settled:**
- **Abstention under any of the three paradigms.** No source reports free-form abstention for full context on a conversational benchmark, for a recall sub-agent, or for a consolidated document. #885 and #888 are the only abstention evidence, and they are sherlock's.
- **MiniMax-M2 at long context.** No source measures it. Its model card evaluates at 128K.
- **A sub-agent whose citations are checked.** No source checks that a returned fact rests on a source the loop read.
- **Hallucination introduced by consolidation.** No source traces errors to a consolidation pass.
- **Transfer to sherlock's corpora,** whose invented names repeat exactly and whose chains are built so that step 2 is identifiable only from step 1. No benchmark read has that construction.

## Candidates for a validation run

Three candidates. W is always run, as a ceiling and not for adoption. Estimates are net production lines before tests, read from the files at `38b0af0`; nothing was implemented. Costs are MiniMax tokens at list price, which the Token Plan deducts "according to the corresponding endpoint pricing"; they are what a run uses of the subscription, not money spent.

### W. Full-context ceiling

- **Change to sherlock.**
  - *To measure:* none in production. The arm is S with its window raised to the newest messages under a 180,000-token history budget: 5,207, 6,449, 6,448 and 6,890 messages for #874's, #885's, #888's and #890's corpora (`tokens.json`). Recall and search stay S's, as production keeps them with compaction disabled.
  - *In the study:* `study/live.go` takes the window from `env.Corpus.Window`. An optional arm interface supplying it, as `SettingInstructor` supplies instructions, is about +10 to +15 lines of study code.
  - *In production, if ever wanted:* an operator setting for `MaxHistoryMessages` in `cmd/sherlock/agent.go`, +5 to +10. A token bound instead of a message count in `compaction.go`, +20 to +35.
- **The budget.** M2's 196,608 positions, less about 4,600 tokens of instructions, tools and recall (S's live prompts averaged 4,443 in #885, 4,390 in #888) and 12,000 for the turn's tool results and output (S's largest live prompt in #885 was 6,243 tokens, and its mean output 270 per call).
- **What it holds.** Every chain hop, SC, UP, N, NC and KU exchange of each corpus. E and K evidence where it falls in the window: E 15 to 18 of 30, K 38 to 40 of 40 (§Whether sherlock's corpora fit).
- **Cost.** About 184,600 input tokens per act call, with no caching on the studies' path. At two act calls per probe (S's median), about 369,000 per probe.
  - #888's corpus, 310 probes: about 114M input tokens, USD 34 at list price.
  - #890's corpus, 470 probes: about 174M, USD 52.
  - Together about 14 times #888's whole run (20.1M input tokens, USD 7.87). Leaving out the IC families, reported only, saves 50 probes on each corpus.
  - Explicit caching would bill the shared history at USD 0.03 per million once written, and refresh it on each hit within 5 minutes. It exists only on the Anthropic-compatible endpoint, so it needs `cache_control` in sherlock's Anthropic client (about +10 to +20 lines) and the harness on that protocol. It is not in the plan: the protocol would then differ from every earlier run's.
  - The Token Plan states no numeric quota, so how many 5-hour windows the run takes cannot be read from the documents.
- **Latency.** Not documented for M2 at this length. Zep's full context at 115K took 28.9 s per answer on gpt-4o. The study's model client allows 5 minutes per request (`study/env.go`); production's default is 60 s (`DefaultTimeout`, `internal/agent/openai/openai.go`).
- **Hypotheses.**
  - With every hop in view, W applies C2 and C3 at least as often as L did on the same corpus (#888: 60 of 100). If it applies fewer, length costs reach on this model.
  - On N, both the statement joining the two entities and the associated entity's value are in view, as with L. If W holds S's abstention (57 of 60 in #888), L's losses came from how memories are presented: out-of-context summaries delivered as recall. If W also loses N, association in view costs abstention whatever the structure, and no structure that shows associated text can hold the guard.
  - On K, the correction and what it supersedes are both in view in order. The sources' knowledge-update results predict W holds S's K.
  - On IC, every hop is in view. If W applies implicit chains, they failed at reach; if not, at use.

### R. A recall sub-agent whose citations are checked in code

- **Why.** In the sources, only a separate loop gains on multi-hop questions while the answering model never sees the raw text (Search-o1, Chain-of-Agents). The actor's own search rarely follows names: S searched 0.27 to 0.32 times per probe, M's instruction raised that to 0.45, and L-T's walk tool was called in 4 of 100 primary probes. No surveyed design checks its citations, and #888's L+V showed an instruction to inspect is mostly ignored (16 of 121). So the check is code, not an instruction.
- **Change.**
  - A new `agent.Recaller` (`internal/agent/service.go`) in `internal/agent/memory/`. Before the act loop, a loop of at most 6 model calls, with `memory_search` and `memory_inspect` over the store, given the task and the last 4 window messages.
  - It returns `{"facts": [{"statement", "sources": [<source IDs>]}]}`. Code keeps a fact only when every cited source was read by `memory_inspect` in that loop, and drops the rest.
  - The actor receives the kept facts with their source IDs, or a line saying no stored evidence was found, in place of the seven-mid recall block. It keeps its own `memory_search` and `memory_inspect`.
- **Lines.** Loop and tool dispatch, reusing the store's search and inspection: +80 to +120. Fact schema, parsing and the citation check: +25 to +40. Instructions: +8 to +12. Wiring in `internal/gateway/runtime.go` and a setting: +8 to +15. **Net +120 to +190.** It removes nothing while the shipped recall stays selectable.
- **Cost.** +1 to +6 calls per turn, each reading instructions, the task and earlier tool results: about +3,000 to +20,000 input tokens per turn. On #890's 470 probes, 1.4M to 9.4M input tokens, near one S live arm (3.2M to 3.5M). At #885's p50 of 3.4 s per call, +3.4 to +20 s per turn before the actor starts.
- **Hypothesis.** R applies C2 and C3 more often than S, and holds N and K within the guardrails, because a fact reaches the actor only through a source the loop read.
- **Expected risk.** The check proves reading, not support. On N the loop can read the associated entity's message and state its value for the queried entity, citing a real source. A scoring control counts returned facts whose cited sources do not state both the fact's entity and its value.

### D. An idle-time memory document with cited lines

- **Why.** The designs that keep standing facts in view carry weight in their ablations (#887), and idle time keeps the calls off the reply path. OpenClaw's pattern answers the risks of sherlock's addressed store: the model chooses an operation, code writes a line that cites its source, a pass may not delete much, and the previous version is kept.
- **Change.**
  - After every 8 new mids, and at startup catch-up, one call reads the current document and the new mids' summaries with their IDs. It emits operations only: add a line, supersede line n, or keep.
  - Code writes each line with its mid IDs, refuses a pass that removes more than a quarter of the lines without a supersession, and keeps every earlier version in a history table.
  - The document is bounded at 16,000 characters and shown every turn under the note. Episodes, recall and search are unchanged.
- **Lines.** The pass (call, schema, applying operations, bounds): +90 to +140. Table, history and migration: +25 to +40. The trigger in `internal/memory/sqlite/accumulate.go` or the runtime: +10 to +20. Showing it: +10 to +15. **Net +135 to +215.**
- **Cost.** No call on the reply path. About 3,400 more tokens in every act call at the bound (16,000 characters at #890's 4.64 characters per token). At write or idle time, one pass per 8 mids: #890's 2,232 mids take 279 passes of about 4,600 input tokens, so about 1.3M input and 0.15M to 0.3M output tokens.
- **Tension with the ADRs.** A pass that rewrites a document from itself and new mids is the recursive summary ADR 0006 rejected; operations over cited lines and a kept history are its answer to the ADR's reason, error propagation. `memory.md` names the store and the note as "the only active long-term memory", so D adds a third, and adoption revises both.
- **Capacity.** #890's hop statements alone are 29,684 characters, before the base's 1,200 mids. So D cannot hold every fact, and the run measures what its pass keeps.
- **Hypothesis.** D applies UP more often than S, and C2 and C3 where the kept lines join a chain's entities.
- **Expected risk.** K and KU from a stale line kept beside or instead of its correction, N from associations in view as with L, and facts dropped past the bound.

## Test plan

The runs reuse the chained-recall harness (`../chained-recall/study/arm.go`, `study.Main`, the proxy with its 2056 pause, one live turn per probe on its own store copy), #890's scorer for SC, UP, KU and NC (`../memory-encoding/validation/score.py`, on that branch), and #885's decision rule. Each run is its own issue with a preregistration committed before its first call.

- **Corpora.**
  - #888's (`../chained-recall/readtime/`), where S, L, L+G, L-T and L+V are recorded: W reads against every read-time arm.
  - #890's (`../memory-encoding/validation/`, after it merges), where S, C, P and F are measured: W, R and D.
  - No new seed. W adds no selection of its own, and R and D are compared with S on the same probes, as #890's arms are.
- **Arms and primary families.**

  | Arm | Setting | Primary family | Role |
  |---|---|---|---|
  | S | `b4` | | baseline, re-run live in the same run |
  | W | `w180k` | C2 and C3 | ceiling; reported beside every arm, not selected |
  | R | `b4-agent` | C2 and C3 | candidate |
  | D | `b4-doc16k` | UP | candidate; C2 and C3 secondary |

- **Guards,** at #890's sizes: N 60, NC 30, K 40, KU 30, C1 30, E 30. IC2 and IC3 are reported with pair counts and p, and gate nothing.
- **Decision rule,** #885's:
  - exact McNemar on applied over each candidate's primary family against S, Holm across R and D, α = 0.05;
  - guardrails: a loss of at most 3 on N, 2 on K, 3 each on C1 and E, and 2 each on NC and KU;
  - among valid candidates the smallest net diff is taken; with none valid and every control passed, the shipped recall stands.
  - W is outside the Holm family. Its pairs against S, L and every other recorded arm are reported per family. Its E is reported twice: items whose evidence is in its window, and the rest.
- **Controls,** fixed in the preregistration. A null for an arm is an answer only if its controls passed.
  - *W:* every act call's prompt tokens in the ledger lie between 180,000 and 196,608, and each probe's window starts at the ID `tokens.py` gives. A five-probe smoke run records per-call latency first. If its p90 passes the study's 5-minute request timeout, the run stops and the budget is revised before the full run.
  - *R:* the loop runs in at least 95% of probes. The citation check drops at least one fact somewhere, or it was never exercised. The share of kept facts whose cited sources state both their entity and their value is reported. Removing the check breaks a named test.
  - *D:* the document is within its bound after the stream; every line cites an existing mid; the history holds every earlier version. At least half the UP facts have a line, or the pass did not write them. Mutation checks as `../chained-recall/mutation.py`.
- **Stages.**
  - W has no offline stage: its reach is set by the window (`tokens.json`).
  - R has none: its loop is live.
  - D's consolidation pass runs over each corpus once through the proxy. Its offline reach is a fact present in a document line, by #874's matching, with admission by #890's margin.
- **Order.**
  - First, W on #888's corpus, after its smoke run: about 114M input tokens, and the reading every earlier structure lacks.
  - Then R, D and W on #890's.
  - The owner confirms before each that the subscription's windows can carry it (§W, cost).
- **Reported for every arm:** applied per family, context tokens per turn, stored characters per source character, act calls per probe, per-call and per-probe latency p50 and p90, and write-time and idle-time calls and tokens, beside S's.
- **The run** is issue #900, recorded in `validation/README.md`.

## Limits

- **A survey, not a measurement.** No paradigm was run. Every figure is the source's own, on other models, benchmarks and judges. No source uses MiniMax-M2.
- **Lengths differ.** The conversational benchmarks run to 115K tokens; sherlock's corpora are 239K to 274K. LoCoMo's size and category labels differ between papers, so cross-paper category figures are not comparable.
- **Designs without evaluations.** OpenClaw's Active Memory and Dreaming, Letta's sleep-time agent and Anthropic's Dreams publish no measurement. They are cited for their mechanisms.
- **Papers against repositories.** Nemori's conflict step is in the paper and on a side branch, not on main. LightMem's text and its Table 2 disagree. Both are named where they occur.
- **MiniMax's documents are written for M3.** The M2-era Coding Plan was read from Wayback captures. The Token Plan's model coverage does not name M2, though #888's ledger shows the key served MiniMax-M2 on 6,113 calls up to 2026-09-27T04:01Z.
- **Token counts.** The tokenizer is exact on the validated text: 424 of 424 requests at one constant offset. It was not exercised on the base's 1,342 non-ASCII characters. The per-message framing, 5 tokens a message and 32,480 to 44,640 per corpus, is read from the Hugging Face chat template and was not validated against the API. Without any framing, content alone (206,359 to 228,926 tokens) still exceeds both 196,608 and 204,800.
- **Estimates.** Net lines, call counts and token costs are read or computed from the files, the ledgers and `tokens.json`, not implemented.
- **Reading passes.** Sources were read in five passes and merged here. A sample was re-read from the saved text, not every figure.
- **The corpora decide what a run can see.** Templated families with invented names reused exactly; paraphrase is outside them, as #874's limits say.
