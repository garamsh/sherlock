# Chained and associative memory structures

Records the source survey for issue #877. It covers how published memory structures let a system follow a chain of associated memories: recall one memory, recall what it leads to, then recall again. For each structure it records what was measured, what was found, what it costs and how much it compresses. From that it narrows sherlock's candidates to three and fixes the test plan issue #874 runs. It makes no production decision and made no model call.

## Contents
- Question and method
- Sherlock at `eaecbcc`
- The structures
- Comparison
- What the sources settle and what they do not
- Candidates for sherlock
- Test plan for #874
- Limits

## Question and method

Which published structures support recall that follows associations step after step? What does each cost in model calls at write and at read time? How much does it compress what it stores and what it shows? Which two or three of them should #874 test against what ships?

- **Method.** Reading primary sources only: arXiv papers at a stated version, and project repositories at a stated tag or commit. No system was run and no model was called.
- **Access date.** Every source below was accessed on 2026-09-26.
- **How numbers were taken.** Each paper was downloaded as `https://arxiv.org/pdf/<id>v<n>` and converted with `pdftotext -layout`. Every number quoted from a paper below was found in that text of the version named, and is given with its setting. Nothing is recomputed. Ratios the papers print as ratios are quoted; none is derived here.
  - The exception is a figure marked *(figure only)*. Such a value is plotted and not printed, so it is described, not quoted.
- **Repository claims** name a tag or commit and a file, read through the GitHub API. For IRCoT and Self-Ask only the commit was recorded, and no file was read.
- **What a source could not settle** is listed under each structure. A property absent from a subsection was not reported by its source, unless the subsection says it was looked for.
- **Sherlock.** Claims about this repository hold for `eaecbcc` (origin/dev) and name the file read.

## Sherlock at `eaecbcc`

What ships (`docs/architecture/memory.md`, ADR 0006, ADR 0019):

- **Memory unit.** Each batch of four visible messages becomes one immutable mid episode. One session-model call synthesizes it (`internal/agent/memory/summary.go`), and its summary is embedded locally with `BAAI/bge-base-en-v1.5` over text-embeddings-inference.
- **No links and no levels.** Episodes are independent: ADR 0006 rejected "recursively summarizing every prior mid state".
- **Reading, pre-turn.** Recall offers the 2 newest mids plus 5 more by cosine to the raw task text, with no model call (`internal/agent/memory/conversation.go`).
- **Reading, during work.** The model may call `memory_search` with its own query, at most 5 results per call and at most 6 calls of one tool per turn (`internal/agent/search.go`; `maxToolCallsPerTurn` in `internal/agent/service.go`). It may call `memory_inspect` to read a source by ID.
- **So the only chaining sherlock has today is model-directed re-retrieval:** a later search whose query the model writes after reading an earlier result. The contract tells the model to "search again with a different query when the first misses" (`internal/agent/instructions.go` §Source-linked memory). Nothing asks it to follow what a result points to.
- **The note.** 2,000 characters, restated by the model each turn.

### Compression of what ships

Computed for this survey from the #856 replication's recorded syntheses (`docs/research/mid-memory-rag/evidence/replication/`). The chars/source column is summary characters over the 897,180 characters of the 4,800 source messages. The last column is delivered recall on the replication's 280 primary queries (`docs/research/mid-memory-rag/README.md` §Replication).

| Batch | Mids | Mean summary chars | Stored summary chars / source chars | Delivered hits of 280 |
|---|---:|---:|---:|---:|
| 4 | 1,200 | 512 | 0.684 | 209 |
| 8 | 600 | 673 | 0.450 | 194 |
| 16 | 300 | 838 | 0.280 | 164 |

- **Batch 4 barely compresses.** A 4-message summary keeps two thirds of the source's characters. Batch 16 keeps 28%, and delivers 45 fewer queries.
- **Context per recall grows with batch size.** Seven mids at the mean summary length are about 3,600, 4,700 and 5,900 characters at batch 4, 8 and 16.
- **Storage is mostly vectors.** Each mid also stores a 768-dimension float32 vector, 3,072 bytes, which is several times the summary's characters at every batch size.
- **Reproduction:** `python3 -c` over `synthesis-n{4,8,16}.jsonl` and `corpus.json`, summing the trimmed `summary` and `content` lengths.

### Sherlock's own evidence on chaining

- **#862, #864 and #870** (`docs/research/agent-context/README.md`) measured single-evidence probes only. Given `memory_search`, MiniMax-M2 searched in 39, 39 and 23 of 70 probes whose referent lay beyond the window. The probe's evidence was reachable in one step.
- **Recursive retrieval was simulated in the offline memory-architecture harness** (`docs/research/memory-architecture/README.md`, G7 to G9). It matched single-hop, 0.79 against 0.87 for reflection on the diverse workload, "because independent sessions give it no path to walk". That harness hashes words instead of embedding them, and its corpus had no chains.
- **Blending buried relevance.** The issue attributes this to `memory-architecture`. The measurement is in `docs/research/memory-retrieval/README.md` §Which conclusions may be quoted across embedders: under the blended read path, "the top ten is settled by age and salience before the query is read". Any structure that folds recency or importance into one score with relevance repeats that design.
- **No sherlock study has measured a chain,** where step 2's evidence can only be identified from what step 1 recalled. #874 exists for that.

## The structures

Each subsection gives the source, then:
- the memory unit and how links or levels form;
- how recall follows a chain;
- model calls at write and at read time;
- size relative to the source;
- reported numbers;
- what the source could not settle.

### HippoRAG

Source: Gutiérrez et al., "HippoRAG: Neurobiologically Inspired Long-Term Memory for Large Language Models", arXiv 2405.14831v3 (NeurIPS 2024). Repository `OSU-NLP-Group/HippoRAG` at `1438aba3fc44ff10573e5a5e1e7cc3c7f9794aff`.

- **Unit and links.** A schemaless knowledge graph (§3.4):
  - An LLM (GPT-3.5-turbo-1106) extracts noun-phrase nodes and relation edges from each passage by OpenIE.
  - Synonym edges join nodes whose retriever-embedding cosine is at least 0.8.
  - Table 1 (MuSiQue): 11,656 passages yield 91,729 nodes and 107,448 triples.
- **Chain.** A graph walk.
  - One LLM call extracts named entities from the query.
  - Each is matched to its nearest node.
  - One Personalized PageRank run from those seeds, damping 0.5, scores nodes. Passages are ranked by the scores of the nodes they contain.
  - The walk crosses several hops in one step, with no model call per hop.
- **Calls.**
  - Write: about one LLM extraction per passage.
  - Read: one NER call plus the walk.
  - Appendix G, Table 17 (1,000 queries, GPT-3.5 Turbo): online retrieval cost $0.1 and 3 minutes against IRCoT's $1–3 and 20–40 minutes. The text says "10 to 30 times cheaper and 6 to 13 times faster".
  - Indexing is "around 10 times slower and $15 more expensive for every 10,000 passages".
- **Size.** Not reported relative to the source.
- **Numbers.** Single-step retrieval, Recall@2/@5 (Table 2):

  | Retriever | MuSiQue | 2Wiki | HotpotQA |
  |---|---|---|---|
  | ColBERTv2 | 37.9/49.2 | 59.2/68.2 | 64.7/79.3 |
  | HippoRAG (ColBERTv2) | 40.9/51.9 | 70.7/89.1 | 60.5/77.7 |

  - With IRCoT as the multi-step driver (Table 3), IRCoT + HippoRAG reaches 45.3/57.6, 75.8/93.9 and 67.0/83.0, against IRCoT + ColBERTv2's 41.7/53.7, 64.1/74.4 and 67.9/82.0.
  - QA EM/F1 (Table 4): HippoRAG 19.2/29.8, 46.6/59.5, 41.8/55.0, against ColBERTv2 15.5/26.4, 33.4/43.3, 43.4/57.7.
- **Failure analysis** (Appendix F, 100 MuSiQue errors): NER limitations 48%, OpenIE errors 28%, PPR 24%.
- **Not settled.** No conversational benchmark. The gain concentrates in 2Wiki, and HotpotQA loses to plain ColBERTv2.

### HippoRAG 2

Source: Gutiérrez et al., "From RAG to Memory: Non-Parametric Continual Learning for Large Language Models", arXiv 2502.14802v2 (ICML 2025).

- **Unit and links.** HippoRAG's graph, plus a passage node for every passage, joined by a context edge to each phrase extracted from it (§3.2).
- **Chain.** Still Personalized PageRank at damping 0.5.
  - The query embedding retrieves top triples, and an LLM filter keeps "up to 4 relevant facts" (Appendix A prompt).
  - The phrases of those facts seed the walk. Every passage node also seeds it, weighted by embedding similarity times 0.05 (Table 5 sweeps 0.01 to 0.5).
  - With no triple left, it falls back to dense retrieval.
- **Calls.** Write as HippoRAG. Read: one LLM filter call per query.
- **Cost** (Table 12, MuSiQue's 11,656 passages, Llama-3.3-70B):
  - Indexing input tokens: 9.2M for HippoRAG 2, 1.7M for RAPTOR, 115.5M for GraphRAG.
  - Indexing time: 99.5 minutes.
- **Numbers.**
  - Recall@5 (Table 3): MuSiQue 74.7, 2Wiki 90.4 and HotpotQA 96.3, against the NV-Embed-v2 dense retriever's 69.7, 76.5 and 94.5.
  - QA F1 (Table 2, Llama-3.3-70B reader): MuSiQue 48.6 against 45.7, 2Wiki 71.0 against 61.5, HotpotQA 75.5 against 75.3.
  - Ablations (Table 4, average R@5): 87.1 full, 81.0 without passage nodes, 86.4 without the filter.
- **Not settled.** No conversational benchmark. As the corpus grows, the paper reports that both HippoRAG 2 and the dense retriever degrade at a similar rate *(figure only)*.

### GraphRAG

Source: Edge et al., "From Local to Global: A Graph RAG Approach to Query-Focused Summarization", arXiv 2404.16130v2. Repository `microsoft/graphrag` at `769542fbf1d8e5b4c6a8677fefc34621c87894c5` (release v3.2.0).

- **Unit and levels.**
  - An LLM extracts entities, relationships and claims from 600-token chunks, with "gleaning" rounds that re-ask for missed entities.
  - Entities merge by exact name.
  - Hierarchical Leiden communities form levels C0 to C3, and an LLM writes a summary per community (§3.1).
  - The repository default chunk is 1,200 tokens (`packages/graphrag/graphrag/config/defaults.py`).
- **Chain.** No walk. "Global" search maps every community summary at one level to a partial answer, then reduces them (§3.1.6). It answers sensemaking questions over a whole corpus.
- **Calls.**
  - Write: extraction per chunk plus gleanings, then one summary per community.
  - Read: one map call per summary chunk plus a reduce.
  - Indexing the 1M-token podcast corpus "took 281 minutes" with gpt-4-turbo (§4.1.3).
- **Compression** (Table 2). Root-level summaries are 2.6% (podcast) and 2.3% (news) of source-text tokens; C3 is 73.5% and 66.8%. The text says C0 needs "9x-43x" fewer tokens per query.
- **Numbers.** Win rates judged by an LLM against vector RAG, not accuracy: "global approaches achieved comprehensiveness win rates between 72-83% (p<.001) for Podcast transcripts and 72-80% (p<.001) for News articles" (§5.1).
- **Not settled.** No multi-hop QA benchmark of its own. The only such numbers are third-party: HippoRAG 2 Table 2 gives GraphRAG MuSiQue F1 38.5 against the dense retriever's 45.7.

### RAPTOR

Source: Sarthi et al., "RAPTOR: Recursive Abstractive Processing for Tree-Organized Retrieval", arXiv 2401.18059v1 (ICLR 2024). Repository `parthsarthi03/raptor` at `7da1d48a7e1d7dec61a63c9d9aae84e2dfaa5767`.

- **Unit and levels.**
  - 100-token leaf chunks.
  - Gaussian-mixture soft clustering over embeddings, each cluster summarized by gpt-3.5-turbo, recursively until clustering stops (§3).
- **Chain.** Hierarchical.
  - "Tree traversal" descends top-k per layer.
  - The "collapsed tree" ranks every node of every layer in one list up to a token budget. The collapsed tree "consistently performs better" (Figure 3), and is run at "2000 maximum tokens, which approximately equates to retrieving the top-20 nodes".
  - Between 18.5% and 57% of retrieved nodes are summaries, not leaves (appendix).
- **Calls.** Write: one summary call per cluster. Read: embedding search only.
- **Compression** (Appendix C, Table 10): "The average ratio of the summary length to the sum of child node lengths across all datasets is 0.28, indicating a 72% compression rate". A summary averages 131 tokens over 6.7 children of 85.6 tokens.
- **Contiguous groups nearly match clusters** (Appendix B, Table 9). A balanced tree summarizing contiguous windows of 7 nodes reached 55.8% on QuALITY against RAPTOR's clustered 56.6%, with SBERT and UnifiedQA. That setting is the closest in the literature to grouping sherlock's mids by time.
- **Numbers.** QuALITY accuracy (Table 4): RAPTOR 62.4 against DPR's 60.4 with GPT-3. QASPER F1 (Table 3): 55.7 against DPR's 53.0 with GPT-4. About 4% of 150 sampled summaries held minor hallucinations.
- **On multi-hop, RAPTOR loses to flat retrieval in third-party tables:**
  - HippoRAG Table 2: MuSiQue R@5 46.5 against ColBERTv2's 49.2.
  - HippoRAG 2 Table 3: MuSiQue 57.8 against NV-Embed-v2's 69.7.
- **Not settled.** RAPTOR itself reports no multi-hop QA benchmark and no conversational memory.

### Zep and Graphiti

Source: Rasmussen et al., "Zep: A Temporal Knowledge Graph Architecture for Agent Memory", arXiv 2501.13956v1. Repository `getzep/graphiti` at release v0.30.2.

- **Unit and levels.** Three tiers (§2):
  - raw episodes, the "non-lossy data store";
  - entity nodes joined by fact edges that carry validity intervals;
  - community summaries.
- **How links form.** Per message, LLM calls:
  - extract entities, with the previous four messages as context and a reflection pass;
  - resolve duplicates;
  - extract facts and deduplicate edges;
  - extract dates;
  - invalidate contradicted edges (§2.2).
  - Graphiti v0.30.2 makes these as separate calls (`graphiti_core/utils/maintenance/edge_operations.py`).
- **Chain.**
  - Search combines cosine, BM25 and breadth-first search "within n-hops" from seed nodes (§3.1). Reranking is by RRF, MMR, mentions, node distance or a cross-encoder (§3.2).
  - Graphiti caps depth at `MAX_SEARCH_DEPTH = 3` (`graphiti_core/search/search_utils.py`).
- **Calls.** Several per message at write time. Read time needs no LLM unless a reranker uses one.
- **Size.** On LongMemEval_s, Zep's context averages 1.6k tokens against the full conversation's 115k (Table 2).
- **Numbers.**
  - LongMemEval_s (Table 2): 63.8% with gpt-4o-mini and 71.2% with gpt-4o, against full context's 55.4% and 60.2%.
  - Multi-session category with gpt-4o: 57.9% against 44.3% (Table 3).
  - DMR: 94.8% against MemGPT's 93.4% with gpt-4-turbo (Table 1). The paper itself calls DMR's conversations small enough to "easily fit" in context.
- **Not settled.** Which search recipe the experiments used, and so whether the n-hop walk contributed. The paper says a "subset" of search features was used. There is no ablation of the walk.

### Mem0 and Mem0 graph

Source: Chhikara et al., "Mem0: Building Production-Ready AI Agents with Scalable Long-Term Memory", arXiv 2504.19413v1. Repository `mem0ai/mem0`: graph code read at release v1.0.11; the current release is v2.2.1.

- **Unit.** Natural-language facts.
  - Per message pair, one LLM call extracts candidate facts, with a conversation summary and the last 10 messages as context.
  - A second LLM call compares each fact with its 10 most similar memories and chooses ADD, UPDATE, DELETE or NOOP (§2.1).
- **Graph variant (Mem0g).**
  - An entity extractor and a relationship generator build a directed labelled graph.
  - An LLM resolver marks conflicting edges invalid (§2.2).
  - At v1.0.11, search matches query entities to nodes and follows one hop of edges each way (`mem0/memory/graph_memory.py`).
  - The graph code was removed from the repository by PR #4805 (merged 2026-04-14), which also replaced the update step with add-only extraction.
- **Size** (§4.5, LoCoMo conversations of about 26k tokens):
  - Stored footprint: about 7k tokens (Mem0) and 14k (Mem0g).
  - Retrieved context per query (Table 2): 1,764 and 3,616 tokens, against 26,031 for full context.
- **Numbers.** LoCoMo multi-hop, LLM-judge J (Table 1): Mem0 51.15, Mem0g 47.19, Zep 41.35. Overall J (Table 2): Mem0 66.88, Mem0g 68.44, full context 72.90.
  - **The graph did not help the multi-hop category.**
- **Dispute.** Zep's reply (blog post by Chalef, 6 May 2025, `blog.getzep.com`) reports Zep at 75.14 J under a corrected configuration. Mem0's CTO disputed the scoring in `getzep/zep-papers` issue #5. The two parties' runs of each other's system are not reconciled.
- **Not settled.**
  - The judge model is not named beyond "a separate, more capable LLM".
  - LoCoMo category labels differ between papers. Mem0 Table 1 places A-MEM's own multi-hop 27.02 under a different category, so cross-paper multi-hop figures are not comparable.

### A-MEM

Source: Xu et al., "A-Mem: Agentic Memory for LLM Agents", arXiv 2502.12110v11. Evaluation repository `WujiangXu/A-mem` at `0c8039f`.

- **Unit and links.**
  - A note holds the content, its time, and LLM-generated keywords, tags and context, with an embedding over all of them.
  - At write time the k nearest notes are retrieved, and an LLM decides links to them.
  - "Memory evolution" lets an LLM rewrite the neighbours' context and tags (§3.2–3.3).
  - The evaluation code does linking and evolution in one call over 5 neighbours (`memory_layer.py`).
- **Chain.** One hop. The paper's retrieval is cosine top-k (§3.4). The code appends each hit's linked notes.
- **Calls.**
  - Write: about two LLM calls per note.
  - Read: one keyword call plus the answer call in the evaluation code.
  - Tokens: "approximately 1,200 tokens per memory operation", against 16,900 for the LoCoMo and MemGPT baselines (§4.3).
- **Numbers.** Multi-hop F1 on LoCoMo with GPT-4o-mini (Table 3 ablation):
  - 9.65 without link generation and evolution;
  - 21.35 without evolution;
  - 27.02 with both.
- **Not settled.**
  - The linked-note expansion the code performs is not described in the paper, so which part of the gain is the link and which the rewritten neighbours cannot be separated.
  - Mem0's re-run gives A-MEM a multi-hop J of 18.85 (Mem0 Table 1).

### Generative Agents

Source: Park et al., "Generative Agents: Interactive Simulacra of Human Behavior", arXiv 2304.03442v2.

- **Unit and levels.**
  - A memory stream of observations, each with an LLM-rated importance from 1 to 10.
  - Reflection runs when summed importance "exceeds a threshold (150 in our implementation)". It reads the 100 most recent records, asks questions, and writes insights that cite the records they rest on (§4.2).
  - Reflections are themselves memories, so trees form.
- **Chain.** No walk. Retrieval scores recency (decay factor 0.995), importance and relevance with equal weights (§4.1). A reflection brings its conclusion into context, not its evidence.
- **Calls.**
  - Write: one importance call per memory, plus reflection passes.
  - Read: none beyond embedding.
- **Numbers.** Believability, TrueSkill: full architecture μ = 29.89; without reflection 26.88.
- **Not settled.**
  - Nothing on multi-hop recall, storage or cost.
  - The blended score is the design `docs/research/memory-retrieval/` found buries relevance.

### MemGPT and Letta

Source: Packer et al., "MemGPT: Towards LLMs as Operating Systems", arXiv 2310.08560v2. Repository `letta-ai/letta` at release 0.16.8 (`1131535716e8a31c9a437f8695e25ac98f203a24`).

- **Unit and tiers.** Main context holds:
  - a working context the model edits by function call;
  - a message queue whose head holds a recursive summary of evicted messages;
  - a memory-pressure warning at about 70% of the window.
  - Evicted messages go to recall storage, and archival storage holds arbitrary text (§2).
- **Chain.** Model-directed. Each search is a function call. `request_heartbeat` asks for another inference immediately, so a multi-hop lookup is a sequence of model steps.
  - Letta 0.16.8 keeps this shape: `conversation_search` is a hybrid text and vector search returning 5 by default (`letta/functions/function_sets/base.py`).
  - Letta has no links between passages.
- **Calls.** One model call per hop at read time. Writes are model function calls.
- **Numbers.**
  - Deep Memory Retrieval (Table 2): GPT-4 92.5% with MemGPT against 32.1% for the base model given summaries.
  - Nested key-value retrieval (§3.2.2), where a value can be the next key:
    - GPT-4 and GPT-4 Turbo alone "hit 0 percent accuracy by 3 nesting levels".
    - MemGPT with GPT-4 "is unaffected with the number of nesting levels".
    - With GPT-4 Turbo and GPT-3.5 it begins "to drop off in performance at 2 nesting levels as a result of failing to perform enough lookups".
  - Per-level values are *(figure only)*.
- **Not settled.** No count of calls, latency or cost.

### IRCoT

Source: Trivedi et al., "Interleaving Retrieval with Chain-of-Thought Reasoning for Knowledge-Intensive Multi-Step Questions", arXiv 2212.10509v2 (ACL 2023). Repository `StonyBrookNLP/ircot` at `3c1820f698eea5eeddb4fba3c56b64c961e063e4`.

- **Unit.** Wikipedia paragraphs under BM25. There are no links; the chain is built at read time.
- **Chain.** Re-retrieval driven by the model's reasoning. Each new chain-of-thought sentence becomes the next query.
  - At most 8 steps and 15 paragraphs.
  - It stops when the sentence contains "answer is:".
- **Calls.** "a separate call to an (L)LM for each sentence of CoT" (Limitations). None at write time.
- **Numbers.**
  - Retrieval recall rises "up to 21 points" over one-step retrieval (abstract); "11-21 recall points" in §1.
  - QA F1 with GPT3 and a chain-of-thought reader (Table 4): 60.7, 68.0 and 36.5 on HotpotQA, 2Wiki and MuSiQue, against one-step retrieval's 53.6, 54.8 and 29.4.
- **Not settled.** No latency or token cost. The comparisons with other systems are "not head-to-head".

### Self-Ask

Source: Press et al., "Measuring and Narrowing the Compositionality Gap in Language Models", arXiv 2210.03350v3. Repository `ofirpress/self-ask` at `559e56ec1ba8dc93cf63c4148ec1f4717caf4b60`.

- **Chain.** Model-directed follow-up questions. Each "Follow up:" question is sent to a search engine and its answer is inserted before the model continues.
- **Calls.** One search and one continuation per sub-question. No write-time structure.
- **Numbers** (Table 1, Davinci-002): self-ask with search 60.0 on Bamboogle, 40.1 EM on 2WikiMultiHopQA and 15.2 on MuSiQue 2-hop, against self-ask without search at 57.6, 30.0 and 13.8, and chain of thought at 46.4, 29.8 and 12.6.
- **Not settled.** No retrieval metric. No step limit. The search engine is not reproducible.

### Evaluation settings the structures report on

- **LoCoMo** (Maharana et al., arXiv 2402.17753v1):
  - 50 conversations of "300 turns and 9K tokens on avg." (Table 5: 304.9 turns, 9,209.2 tokens), with 1,104 multi-hop questions among its categories.
  - RAG with a GPT-3.5-turbo-16k reader (Table 3), multi-hop F1:
    - dialog turns top-50: 37.2;
    - per-turn observations top-5: 30.6;
    - session summaries top-10: 14.7, while their multi-hop recall is 82.3.
  - **Summaries reach the evidence and lose the detail.** The paper attributes it to "loss of information during the conversion of dialogs to summaries".
- **LongMemEval** (Wu et al., arXiv 2410.10813v2):
  - 500 questions over about 115k-token histories (LongMemEval_S).
  - "Fact decomposition" loses on most abilities, with one exception: "multi-session reasoning questions, where fact decomposition consistently improves performance".
  - Adding extracted facts to the index keys improves "recall@k" by 9.4% and "final accuracy" by 5.4% on average.

## Comparison

"Walk" means the structure crosses hops without a model call per hop; "model" means one model step per hop. The multi-hop gain column quotes the source's own best comparison against its flat baseline, in the setting named in the subsection above.

| Structure | Chaining mechanism | Write-time model calls | Read-time model calls | Compression | Reported multi-hop gain | Implementation complexity |
|---|---|---|---|---|---|---|
| HippoRAG | walk: PPR over an entity graph from query entities | 1 OpenIE per passage | 1 NER per query | not reported | 2Wiki R@5 68.2 → 89.1; HotpotQA 79.3 → 77.7 | graph store, synonym edges, PPR, NER |
| HippoRAG 2 | walk: PPR over entities and passages, filtered seeds | as HippoRAG | 1 filter per query | not reported; 9.2M indexing tokens for 11,656 passages | MuSiQue R@5 69.7 → 74.7 | HippoRAG plus passage nodes and filter |
| GraphRAG | none; map-reduce over community summaries | extraction per chunk, gleanings, 1 per community | 1 per summary chunk, plus reduce | C0 2.3–2.6% of source tokens | none reported on multi-hop QA | high: extraction, Leiden, reports |
| RAPTOR | hierarchical: collapsed tree over summary levels | 1 per cluster | none | summary 0.28 of its children | no multi-hop benchmark; loses to flat retrieval in third-party tables | clustering, recursion, levels |
| Zep / Graphiti | walk: BFS within n hops, reranked | several per message | none required | 1.6k context tokens vs 115k | LongMemEval multi-session 44.3% → 57.9% (gpt-4o) | high: temporal graph, invalidation |
| Mem0 | none | 2 per message pair | none | ~7k stored tokens per ~26k conversation | LoCoMo multi-hop J 51.15 | extraction and update |
| Mem0g | one-hop edge expansion | 3 per message (graph add at v1.0.11) | entity extraction | ~14k stored tokens | LoCoMo multi-hop J 47.19, below Mem0 | graph store; removed upstream |
| A-MEM | one-hop linked-note expansion | ~2 per note | 1 keyword call | not reported relative to source | LoCoMo multi-hop F1 9.65 → 27.02 with links and evolution | notes, links, neighbour rewrite |
| Generative Agents | none; reflections carry conclusions | 1 importance per memory plus reflection | none | not reported | none on multi-hop | blended scoring, reflection |
| MemGPT / Letta | model: one search per step | model function calls | 1 per hop | recursive summary of evicted messages | nested KV: GPT-4 unaffected by nesting; weaker models drop at 2 levels | tiers and paging |
| IRCoT | model: CoT sentence as next query | none | 1 per step, at most 8 | none | recall "up to 21 points"; MuSiQue F1 29.4 → 36.5 (GPT3) | loop only |
| Self-Ask | model: follow-up questions | none | 1 per sub-question | none | 2Wiki EM 30.0 → 40.1 | loop only |

## What the sources settle and what they do not

**Settled, within the settings named:**
- **Model-directed re-retrieval is the most repeated multi-hop gain.** IRCoT, Self-Ask and MemGPT each chain by one model step per hop, and each gains on multi-hop tasks. It costs a model call per hop and depends on the model:
  - MemGPT's weaker models stopped "failing to perform enough lookups";
  - IRCoT's QA beat one-step retrieval "for all sizes except the smallest (0.2B)" of its Flan-T5 readers.
- **A write-time graph with a walk can replace per-hop model calls.** HippoRAG's single PPR step matched or beat IRCoT's iterative retrieval on 2Wiki and MuSiQue at 10 to 30 times lower online cost. It still gains when the two are combined, but loses on HotpotQA. Almost half of its errors (48%) come from entity extraction.
- **Summary levels compress, but do not chain.**
  - RAPTOR's summaries are 0.28 of their children.
  - LoCoMo's session summaries reach the multi-hop evidence (recall 82.3) yet answer worst (F1 14.7).
  - Wherever RAPTOR meets a multi-hop benchmark, in third-party tables, it trails flat retrieval.
- **One-hop graph expansion on conversation data has not shown a multi-hop gain in its own paper.** Mem0g scored below Mem0 on LoCoMo multi-hop. A-MEM's gain mixes links with LLM rewriting of neighbours.

**Not settled:**
- **Conversational multi-hop has no shared baseline.**
  - LoCoMo category labels differ between the Mem0 and A-MEM papers.
  - The Zep and Mem0 scores of each other's system are disputed.
  - LongMemEval has no chain family built so that step 2's evidence is identifiable only from step 1.
- **No source isolates the walk in a conversational memory.** Zep's n-hop search has no ablation. HippoRAG's walk was measured on encyclopedic passages, not on model-written summaries of chat.
- **No source measures compression and chaining together.** RAPTOR reports compression without multi-hop. The multi-hop papers report no storage relative to the source.
- **No source uses MiniMax-M2 or a local embedder of `bge-base` size** for these structures. Transfer to sherlock's model and embedder is untested.

## Candidates for sherlock

Excluded, with the reason each fails sherlock's constraints:
- **GraphRAG.** It answers corpus-wide sensemaking, not a chain. HippoRAG 2 Table 12 puts its indexing at 115.5M input tokens for 11,656 passages, which a subscription-bound budget cannot carry.
- **Zep / Graphiti and Mem0g.** Several LLM calls per message, a second store with invalidation rules, no ablation of the walk (Zep), and no multi-hop gain in its own paper (Mem0g).
- **Mem0.** Per-message extraction replaces source-linked episodes and has no chaining mechanism.
- **A-MEM.** Its evolution step rewrites stored neighbours, which conflicts with immutable episodes (ADR 0006). Its links alone are covered by candidate L below, at no extra call.
- **Generative Agents' reflection.** Its read path is the blended score `docs/research/memory-retrieval/` found buries relevance. Reflection as a summary level is covered by candidate H.
- **IRCoT and Self-Ask as loops.** Sherlock's `memory_search` already is this loop; candidate M sharpens it.

Estimates below are net production lines before tests, read from the files at `eaecbcc`; nothing was implemented. Costs are under a MiniMax-only, subscription-bound budget with local embeddings: calls and tokens, not dollars.

### M. Follow the chain with the shipped search (smallest change)

- **Why.** IRCoT, Self-Ask and MemGPT all gain by letting the model write the next query from what the last result said, and sherlock's search is that mechanism already. The contract asks the model to search again only "when the first misses", not to follow what a result names. MemGPT's weaker models failed by stopping early, and MiniMax-M2 searched in only 23 to 39 of 70 beyond-window probes in the agent-context runs.
- **Change.** The contract paragraph in `internal/agent/instructions.go` gains one instruction: when a candidate names a person, place, project or rule the task depends on, search for that name next, and keep going until the task's own question is answered. Estimate **+2 to +4 lines**. The per-tool budget of six stays, bounding a chain at six searches.
- **Cost.** No write-time change. One act call per hop, at #862's estimate of about 7,500 input tokens per call (`docs/research/agent-context/README.md` §Experiment plan). Compression is unchanged: 0.684 stored per source character at batch 4.
- **Hypothesis for #874.** On k = 2 and k = 3 chains, the agent under M reaches and applies every hop's evidence more often than under the shipped contract, with no loss on single-evidence guards. The failure it would show is MemGPT's: stopping before the last hop.

### L. Entity links written by the synthesis call, followed by a walk at recall

- **Why.** HippoRAG shows that a graph walk from query entities crosses hops without a model call per hop, and cheaply: $0.1 against IRCoT's $1–3 per 1,000 queries. A-MEM's ablation, whatever its mix, is the one conversational result where links moved multi-hop, from 9.65 to 27.02 F1. HippoRAG's largest error source is a separate NER step. Sherlock already makes one model call per batch, so the entities can come from that call.
- **Change.**
  - `internal/agent/memory/summary.go`: the `memory_summary` schema gains an `entities` array of names the batch mentions, so there is no new call. **+10 to +15.**
  - `internal/memory/sqlite/`: an entity table keyed by normalized name and linking to mids, written with the episode, plus reading it back. **+40 to +60.**
  - A Personalized PageRank over the mid–entity bipartite graph at damping 0.5, HippoRAG's setting. It is seeded by the query's words that match stored entity names, with no pre-turn model call, which keeps ADR 0019. **+60 to +90.**
  - Recall adds the walk's top mids as a third list beside recent and similar, deduplicated. `memory_search` uses the same walk. **+10 to +20.**
  - Existing databases: mids without entities stay unlinked until re-synthesized, or are backfilled by one call each (`memory.md` §Existing databases). **+15 to +25.**
  - Net **+135 to +210**.
- **Cost.** No extra call. The synthesis output grows by the entity list, estimated at 20 to 60 tokens per mid. Storage adds one row per entity mention. Context grows by the walk's list, bounded like the similar list.
- **Hypothesis for #874.** Offline, the walk delivers hop 2 and hop 3 evidence that raw-query similarity cannot reach at equal context characters. Live, it raises chain application over S without extra act calls. The failure it would show is HippoRAG's: seed entities the query names differently from the summary.

### H. A second level of contiguous group summaries, ranked with the mids

- **Why.** Compression, not chaining. RAPTOR's summaries are 0.28 of their children, and a tree over contiguous windows nearly matched a clustered one (55.8% against 56.6%). The collapsed tree, all levels ranked in one list, beat traversal. A group summary lets the same context hold more history, which is the owner's compression question.
  - The literature also predicts the limit. Summaries reached LoCoMo's multi-hop evidence and answered worst, and RAPTOR trails flat retrieval on MuSiQue in both HippoRAG papers.
  - Sherlock differs from both: a summary is a lead to `memory_inspect`, not the answer.
- **Change.**
  - `internal/memory/sqlite/accumulate.go`: after every G mids, one synthesis call over their summaries writes a group episode with the union interval and an embedding. **+30 to +45.**
  - Search ranks mids and groups in one list; recall labels a group as such. **+20 to +35.**
  - Schema, the existing-database path and wiring. **+20 to +35.**
  - Net **+70 to +115**. This is the recursion ADR 0006 rejected only in its running form, "every prior mid state". A fixed window over G mids is not running, but the ADR would need to say so.
- **Cost.** One extra call per G mids, with G summaries as input: at batch 4's mean of 512 characters, about 2,000 characters of input at G = 4 and 4,100 at G = 8. Storage adds one summary and one vector per G mids.
- **Hypothesis for #874.** At equal context characters, collapsed two-level recall holds more history than mids alone. It reaches single-hop evidence at least as often as batch 8 at a lower stored-chars-per-source ratio. It does not raise chain reach beyond S; a rise would contradict the literature.

## Test plan for #874

This replaces the arm list in #874, which asked for S, L, R and H before this survey. R, automatic similarity re-retrieval without a model, is dropped. No surveyed source measures it, and sherlock's own offline analogue (memory-architecture G7–G9) matched single-hop on unconnected data. M covers re-retrieval with the model choosing the query, which is what the sources measured.

- **Arms.**
  - S, the shipped structure at `eaecbcc`, model free to chain `memory_search`.
  - M, S with the chaining instruction.
  - L, entity links with a walk.
  - H, two-level contiguous groups.
  - Each arm states its measured net diff.
- **Compression settings.**
  - Batch 4 and batch 8 for S and L.
  - Groups of G = 4 and G = 8 mids over batch 4 for H.
  - Every arm reports mean stored characters per source character and context characters per turn, beside chain accuracy.
  - M shares S's store; its compression is S's.
- **Corpus.** #874's criteria stand:
  - chains of k = 1, 2 and 3, each hop in its own mid;
  - hop k+1's words present only in hop k's evidence;
  - implicit chains that reach a rule through an association;
  - the replication's exact, correction and absent queries as guards.
  - The deterministic check that a raw-query top-k cannot reach hop 2 and 3 evidence is also what separates L's walk from similarity.
  - Every evidence exchange fills its batch alone at batch 4, the rule #870 needed for its oracle to pass.
- **Offline stage, local embedder only.**
  - Chain reach per hop for S, L and H at each setting, from recorded syntheses (L's entities come from the same synthesis call).
  - Oracle per hop: each hop's evidence ranks top-5 for its own sentence.
  - Mechanism controls: entities stored and walk steps taken for L; groups stored and delivered for H.
  - M has no offline stage: its mechanism is the model.
- **Live stage.**
  - S and M always. Of L and H, the one with higher offline full-chain reach on k ≥ 2 at equal context characters. H goes live only if it does not trail S there by more than the preregistered margin.
  - Primary endpoint: every hop's evidence reached and the final answer or rule applied, on k ≥ 2 chains.
  - Paired exact McNemar against S, Holm-corrected. Guardrails on k = 1 and on the exact, correction and absent guards.
  - M's control: searches per probe, and the share of probes that searched for a name taken from a result.
- **Cost rules.** MiniMax only, with the 429 code 2056 pause: probe at most every 30 minutes and re-run failed episodes once. Tokens are reported per phase. Synthesis at batch 8 and groups of 4 and 8 are the only new write-time calls; L adds none.
- **Decision.** Among valid arms, the smallest net diff wins unless a larger one beats it directly, as in #862. If none is valid and every control passed, the shipped structure stands. H is judged on the compression figures whether or not it chains.

## Limits

- **A survey, not a measurement.** No structure was run here. Every number is the source's own, in its setting: other models, other embedders, encyclopedic or synthetic corpora.
- **Selection.** The structures are the issue's list. Others that chain, such as FLARE or iterative retrieval-generation, were not read.
- **Papers against repositories.** Where they differ, both are named. Mem0's current code no longer has the graph variant or the update step it measured. Graphiti and HippoRAG repository defaults differ from the papers' settings.
- **Estimates.** Net lines, token figures for L's entity lists, and H's group input sizes are read or computed, not implemented. The batch-4 compression figures come from the replication's model-written corpus.
- **The issue's attribution.** The blended-score finding the issue credits to `memory-architecture` is recorded in `memory-retrieval`; this survey cites the latter.
