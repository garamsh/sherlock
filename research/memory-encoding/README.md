# Context-enriched and chained memory encoding

Records the source survey for issue #887. It covers how published memory structures encode context and association when a memory is written, so that an old memory later brings its associations with it. For each structure it records the unit written, the context the writer sees, what is written besides the text, how the unit is retrieved, the model calls, compression and the reported numbers. From that it narrows sherlock's candidates to three and fixes a test plan. It makes no production decision and made no model call.

## Contents
- Question and method
- Sherlock at `d8dc900`
- The structures
- Comparison
- What write-time context buys against read-time association
- What the sources settle and what they do not
- Candidates for sherlock
- Test plan
- Limits

## Question and method

The owner wants old memories recalled together with what they were associated with: painting should bring back the scene, the person's age and job, and one's own painting style. The owner proposes building each mid episode from the context held when it was written, chaining episodes on earlier ones, and any better structure that compresses memory while keeping associations. The questions:
- How do published structures put context and association into a memory at write time?
- What does each cost in model calls, and how much does it compress?
- Against the read-time association sherlock already measured (#874, #885), what can write-time context add?
- Which two to four changes should sherlock's next run test, and how?

Method:
- **Reading primary sources only.** These are arXiv papers at a stated version, one blog post at its printed date, and repositories at a stated commit. No system was run and no model was called.
- **Access date.** Every source below was accessed on 2026-09-27.
- **How numbers were taken.** Each paper was downloaded as `https://arxiv.org/pdf/<id>v<n>` and converted with `pdftotext -layout`. Every number quoted below was found in that text of the version named, and is given with its setting. Nothing is recomputed. A value that is plotted and not printed is marked *(figure only)*, and is described rather than quoted.
- **Repository claims** name a commit and a file, read through the GitHub API. Where a repository differs from its paper, both are named.
- **What a source could not settle** is listed under each structure. A property absent from a subsection was not reported by its source.
- **Already surveyed, not repeated.** `../associative-memory/README.md` (#877) covers HippoRAG, GraphRAG, RAPTOR, Zep, Mem0, A-MEM's links, Generative Agents, MemGPT, IRCoT and Self-Ask. It is cited here where it bears on the question.
- **Sherlock.** Claims about this repository hold for `d8dc900` (origin/dev) and name the file read.

## Sherlock at `d8dc900`

What ships (`../../architecture/memory.md`, ADR 0006, ADR 0019):

- **The unit and its writer.** Each full batch of four visible messages becomes one immutable mid episode. `Accumulate` reads the four sources after the last checkpoint (`internal/memory/sqlite/accumulate.go`). It passes them, as `role: content` strings, to one session-model call (`internal/agent/memory/summary.go`).
- **What the writer sees.** The call has a 557-character system instruction and the four messages, and nothing else. It does not see the rolling window before the batch, the note, earlier episodes, or anything recalled. ADR 0006 states this as a decision: "Episodes are independent: the model receives the batch, not every previous mid summary". Among its rejected alternatives is "Recursively summarizing every prior mid state", because it "would make episodes less specific to retrieval and propagate earlier synthesis errors".
- **What is written.** One summary of at most 2,000 characters, the source interval, and later one `BAAI/bge-base-en-v1.5` embedding of the summary. Nothing else is written: no entities, keys, links or context string.
- **What the store holds when synthesis runs.** The note (`conversation_note`, at most 2,000 characters) and every earlier mid are in the same SQLite file (`internal/memory/sqlite/conversation.go` `Note`, `RecentMid`). A turn commits its sources before `Accumulate` runs, and saves the note after them. So the stored note at synthesis time is the one the agent held while the batch's messages were exchanged.
- **Reading.** Recall offers the two newest mids plus five by cosine to the raw task text, with no model call (`internal/agent/memory/conversation.go`). During work the model may call `memory_search` and `memory_inspect`.
- **The embedder's window.** `bge-base-en-v1.5` at revision `a5beb1e3e68b9ab74eb54cfd186867f64f240e1a` has `max_position_embeddings` 512 and `model_max_length` 512. Its pooling is the CLS token: `pooling_mode_cls_token: true`, `pooling_mode_mean_tokens: false` (`config.json`, `tokenizer_config.json` and `1_Pooling/config.json` on huggingface.co at that revision).

### What #874 and #885 measured

From `../chained-recall/README.md`, §Offline results, §Live results and §Confirmation (#885):

| Measure | S (shipped) | L (entity walk) | L+A (label and instruction) |
|---|---:|---:|---|
| #874 live, clean chains k ≥ 2 applied, of 114 | 4 | 47 | — |
| #885 live, C2 and C3 applied, of 100 | 9 | 56 | 57 |
| #885 absent (N) abstained, of 60 | 57 | 39 | 42 |
| #885 absent answered with the associated value, of 60 | 1 | 19 | 13 |
| #885 corrections (K) given the corrected value, of 40 | 28 | 25 | 21 |
| #885 implicit chains IC2 and IC3 applied, of 50 | 0 | 0 | 1 |
| #885 stored characters per source character | 0.731 | 0.814 | 0.814 |
| #885 context characters per turn | 3,279 | 3,362 | 3,490 |

- **Applying follows seeing.** In every arm a chain was applied almost only where each of its hops was in view: S 9 of 9, L 56 of 57, L+A 57 of 57 seen.
- **Implicit chains fail at reach, not at use.** Offline, L delivered IC2's rule hop in 3 of 25 (#874).
- **Model-directed chaining (M) was not significant** (10 against 4 of 114, p = 0.11).
- **Summary levels (H) do not chain and do not compress.** At groups of 4 and 8 they raised stored characters to 1.107 and 0.943 per source character, against S's 0.705.
- **Batch 8 compresses and loses.** It stores 0.466 characters per source character against batch 4's 0.705, and exact-query reach falls from 17 to 11 of 20.
- **Per-call synthesis cost.** In #874 the shipped call read 142,242 input tokens and wrote 87,252 output tokens over 425 batch-4 calls (§Cost), a mean of about 335 and 205. L's extended call read 61 and wrote 63 more tokens per batch.
- **Corpus geometry.** Hops of one chain sit at least 16 exchanges apart, so each lies in a different mid at batch 4 and 8 (§The corpus). A mean source message is 187 characters: 897,180 characters over the base's 4,800 messages (`../associative-memory/README.md` §Compression of what ships). So a batch of four holds about 748 source characters.

## The structures

Each subsection gives the source, then:
- the unit written;
- the context the writer sees;
- what is written besides the text;
- how the unit is retrieved;
- model calls at write and at read time;
- compression;
- reported numbers;
- what the source could not settle.

### Contextual Retrieval (Anthropic)

Source: "Introducing Contextual Retrieval", Anthropic, published Sep 19, 2024, `https://www.anthropic.com/engineering/contextual-retrieval` (the former `/news/contextual-retrieval` redirects there). Companion notebook: repository `anthropics/claude-cookbooks` (formerly `anthropic-cookbook`) at `813fbeec03cdedfda7808529438d1c7af71f26eb`, file `capabilities/contextual-embeddings/guide.ipynb`.

- **Unit.** A document chunk, "usually no more than a few hundred tokens". The cost example assumes "800 token chunks, 8k token documents".
- **Context at write time.** The whole source document and the chunk. The prompt places `{{WHOLE_DOCUMENT}}` and `{{CHUNK_CONTENT}}` in tags and asks: "Please give a short succinct context to situate this chunk within the overall document for the purposes of improving search retrieval of the chunk." No other document or memory is shown.
- **Written besides the text.** A context string, "usually 50-100 tokens", which "is prepended to the chunk before embedding it and before creating the BM25 index". The post's example turns "The company's revenue grew by 3% over the previous quarter." into a chunk opening "This chunk is from an SEC filing on ACME corp's performance in Q2 2023; the previous quarter's revenue was $314 million." The notebook stores the context and the original content as separate fields beside the combined text.
- **Retrieval.** Hybrid: the top chunks from embeddings and from BM25 are combined "using rank fusion techniques". An optional reranker scores the top 150 and keeps 20. The notebook fuses them by weighted reciprocal rank, 0.8 semantic and 0.2 BM25.
- **Calls.**
  - Write: one LLM call per chunk (Claude 3 Haiku in the post), with the document held in the prompt cache.
  - Read: a query embedding, and a reranker call if one is used.
- **Size.** Expansion, not compression: 50 to 100 tokens are added per chunk. The one-time cost is "$1.02 per million document tokens", assuming "800 token chunks, 8k token documents, 50 token context instructions, and 100 tokens of context per chunk".
- **Numbers** (averaged across the post's domains, Gemini Text 004 embeddings, metric "1 minus recall@20"):
  - "Contextual Embeddings reduced the top-20-chunk retrieval failure rate by 35% (5.7% → 3.7%)."
  - "Combining Contextual Embeddings and Contextual BM25 reduced the top-20-chunk retrieval failure rate by 49% (5.7% → 2.9%)."
  - With reranking, "by 67% (5.7% → 1.9%)".
  - Per-domain and per-embedder results are *(figure only)*.
  - The post says "adding generic document summaries to chunks (we experimented and saw very limited gains)", and gives no figure for it.
- **Notebook** (9 codebases, 737 chunks, 248 queries, `voyage-2`). Its summary table gives Pass@10 87.15% for baseline and 92.34% with contextual embeddings.
  - Its hybrid row (86.43/93.21/94.99 at Pass@5/10/20) disagrees with the printed output of the cell that computes it (88.86/92.31/95.23).
  - It does not say which model produced the saved outputs.
- **Not settled.** Per-domain numbers, dataset sizes, chunk size in the experiments, and the fusion method in the post. No answer accuracy, no multi-hop, no abstention, and no update handling are reported.

### Late chunking (Jina AI)

Source: Günther et al., "Late Chunking: Contextual Chunk Embeddings Using Long-Context Embedding Models", arXiv 2409.04701v3. Repository `jina-ai/late-chunking` at `1d3bb02bf091becd0771455e4e7959463935e26c`.

- **Unit.** One chunk vector. The chunk text is unchanged, and no text is generated.
- **Context at write time.** The whole document passes through a long-context embedder, and each chunk's vector is mean-pooled over its own token span: "chunking applied after the transformer model and just before mean pooling". The window example is "8192 tokens for jina-embeddings-v2-small".
  - Longer documents are split into "macro chunks" with an overlap ω. The paper's macro-chunk length and ω for its experiment are not printed. The repository defaults to an overlap of 256.
- **Written besides the text.** Nothing: one vector per chunk.
- **Retrieval.** kNN over chunk vectors.
- **Calls.** None. One embedder pass per document at write time and one query embedding at read time. The paper notes it "does not require using an additional large language model".
- **Size.** One vector per chunk, as naive chunking stores. No ratio is reported.
- **Numbers** (Table 2, BeIR SciFact, NFCorpus, FiQA and TRECCOVID, nDCG@10, average over jina-embeddings-v2-small, jina-embeddings-v3 and nomic-embed-text-v1):
  - fixed 256-token chunks: naive 52.2, late 54.0;
  - 5-sentence chunks: 52.4 and 54.3;
  - semantic boundaries: 52.4 and 53.8.
  - The text calls the sentence-boundary gain "a 3.63% relative improvement (1.9% absolute)".
  - Anaphora example (Table 1, query "Berlin"): "Its more than 3.85 million inhabitants ..." rises from 0.7084 to 0.8249 cosine.
  - Against contextual retrieval on one fictional five-chunk document (Table 4), the relevant chunk scores 0.8516 late, 0.8590 contextual and 0.6343 naive.
- **Stated limit.** On the synthetic Needle and Passkey sets "late chunking is not useful, as the additional context from the document is totally irrelevant".
- **Not settled.** The chunk-size and long-late-chunking results are *(figure only)*.
  - §4.1 says the chunks never overlap. Yet Table 2's fixed-size jina-embeddings-v2-small cells equal Table 6's "w/ Overlap" column.
  - No multi-hop, abstention or update result is reported.
- **Transfer to sherlock.** The method needs a mean-pooling embedder whose window holds the context. Sherlock's embedder pools the CLS token over 512 positions (§Sherlock at `d8dc900`), so the method cannot run on it unchanged.

### ReadAgent gist memory

Source: Lee et al., "A Human-Inspired Reading Agent with Gist Memory of Very Long Contexts", arXiv 2402.09727v3 (ICML 2024). Repository `read-agent/read-agent.github.io` at `569dff3e47bb708ad084d267ac554f2534d21756`, `assets/read_agent_demo.ipynb`. It is a demo notebook, not the paper's experiment code.

- **Unit.** A "page", an episode of contiguous paragraphs whose end the LLM chooses, and its "gist". Page bounds (Table 8): QuALITY and QMSum 600/280 maximum/minimum words, NarrativeQA Gutenberg 3000/500.
- **Context at write time.**
  - Pagination sees the text from the previous pause point up to the maximum words. Appendix F: including the previous page "only benefits our QMSum experiments". The demo notebook passes it.
  - Gisting sees only its own page: "each page is gisted independently". The prompt reads "Please shorten the following passage." The paper chose "shorten" over "summarize", which "tended to produce a restructured summary".
- **Written besides the text.** A page tag on each gist, and the raw page kept for look-up. There are no embeddings or links.
- **Retrieval.** The whole gist memory is in context. The LLM names pages to expand, and their raw text replaces the gists: ReadAgent-P in one call, ReadAgent-S one page at a time.
- **Calls.**
  - Write: about one pagination call per page, and one gist call per page.
  - Read: one look-up call (ReadAgent-P) or up to the cap (ReadAgent-S), then the answer.
- **Compression.** The compression rate is "100 ∗ (1 − word-count(in-context text) / word-count(full-context text))".
  - Gists alone reach 85.53% on QuALITY, "96.80% for Gutenberg texts and 91.98% for movie scripts" on NarrativeQA, and 83.13% on QMSum validation.
  - Table 6: raising the maximum page size from 400 to 1,200 words moves gist compression from 81.81% to 91.38%, and gist-only accuracy from 78.91% to 73.97%.
- **Numbers** (QuALITY dev, PaLM 2-L, Table 1, accuracy):
  - full text 85.83%;
  - gists only 77.52%;
  - ReadAgent-P with one page looked up 84.13%;
  - ReadAgent-S up to 6 pages 87.17% at 58.53% compression.
  - LLM pagination against uniform pages (Table 5): 86.83% against 85.71%.
- **Not settled.** Gists are never written with a neighbour's context, so the effect of doing so is untested. There are no multi-hop, abstention or update metrics.
  - The impact statement flags "an increased tendency of the LLM to hallucinate when working with gist memories rather than full text" as untested.
  - Conditional (task-aware) gisting is described but "We only explore the unconditional setting".

### SeCom segment memory with compression

Source: Pan et al., "On Memory Construction and Retrieval for Personalized Conversational Agents", arXiv 2502.05589v3 (ICLR 2025). Repository `microsoft/SeCom` at `1738e563b5dc7c51df762247e3d0379f1132ad23`, files `secom/secom.py`, `secom/configs/*.yaml`, `experiment/run.sh`.

- **Unit.** A topical segment: a run of consecutive turns within one session. It is compared with turn, session and summary units.
- **Context at write time.**
  - The segmenter sees one whole session, with turn indices and roles, and returns every boundary in one output.
  - The segment's text is the raw turns: "bypassing summarization".
- **Written besides the text.** An LLMLingua-2 compressed copy, which is the retrieval key. In the code, `build_memory` indexes the compressed text and keeps the raw unit in metadata, and `retrieve` returns the raw unit. The paper does not state in prose which text reaches the generator.
- **Retrieval.** BM25 or MPNet (`multi-qa-mpnet-base-dot-v1`) over the compressed keys. Units are delivered in time order under a budget: "4k tokens (∼ 5 sessions, 10 segments, or 55 turns) on LOCOMO".
- **Calls.**
  - Write: one segmentation call per session (GPT-4-0125 in the paper; the repository defaults to `gpt-4o-mini`), then one LLMLingua-2 pass per unit, which is a token classifier, not generation.
  - Read: none before the answer.
  - Per question on Long-MT-Bench+ including construction (Table 5): SeCom 1,722 input and 135 output tokens, against MemoChat's 7,233 and 229.
- **Compression.** "LLMLingua-2 ... with a compression rate of 75%" on the keys. The repository's default is 0.9.
- **Numbers** (GPT-35-Turbo answering, GPT4Score, Table 1):
  - LOCOMO: SeCom (BM25) 71.57, turn-level BM25 65.58, session-level BM25 63.16, SumMem 53.87, RecurSum 56.25, full history 54.15.
  - Long-MT-Bench+: SeCom (MPNet) 88.81.
  - Denoising ablation (Table 2, MPNet): LOCOMO 69.33 with the compressed keys against 59.87 without.
  - On the official LOCOMO pairs (Table 6, MPNet, GPT-35-Turbo): SeCom 84.21, turn-level 81.52, session-level 74.20.
  - The text: "SumMem and RecurSum fall behind turn-level or session-level baselines".
- **Not settled.** The main LOCOMO questions were generated by GPT-4 per session, and no LOCOMO category (multi-hop, adversarial) is reported. There is no update or correction handling. Segments never cross a session.

### EM-LLM event segmentation

Source: Fountas et al., "Human-inspired Episodic Memory for Infinite Context LLMs", arXiv 2407.09450v3 (ICLR 2025). Repository `em-llm/EM-LLM-model` at `edb2e4a6988c0c2a15637d380e81f04f1a1a531a`, `config/mistral.yaml`.

- **Unit.** An event: the key-value cache of a run of consecutive tokens, 8 to 128 tokens in the repository's configuration.
- **Context at write time.**
  - A boundary is placed where a token's surprise, "− log P(x_t | x_1, ..., x_{t−1}; θ)", exceeds a moving threshold.
  - Boundaries are then moved to maximise modularity, or minimise conductance, of the key-similarity graph inside the local window.
  - The writer is the model's own forward pass over everything before the event.
- **Written besides the text.** No text. The full key-value cache, and a few "representative tokens" per event (4 in the repository's configuration).
- **Retrieval.** Two buffers.
  - Similarity: k-NN between the current query and the representative tokens.
  - Contiguity: "When an event is retrieved, we also enqueue its neighboring events (within ±n positions in the original sequence)", with n = 1 and a contiguity share of 0.3.
- **Calls.** No generative call: segmentation "requires no additional computation" beyond the forward pass.
- **Size.** No compression: 20K tokens take 9.8 GB of key-value cache (Table 11, 32 layers, fp16).
- **Numbers.**
  - Mistral-7B, LongBench average (Table 1): InfLLM 41.9 against EM-LLM (surprise, refinement and contiguity) 43.7.
  - HotpotQA (Table 3): 36.56 against up to 40.27. MuSiQue: 17.68 against up to 18.77.
  - LLaMA-3.1-8B against RAG and full context (Table 9): LongBench average RAG (NV-Embed-v2) 36.44, full context 39.30, EM-LLM 51.58. On MuSiQue, full context 33.52 and RAG 30.90 beat EM-LLM's 25.37.
  - The significance claim covers the benchmark level only: "this isn't the case in the majority of individual tasks".
  - The contiguity buffer gave "the best performance in 44% of tasks".
- **Not settled.** The abstract claims "strong correlations" with human event boundaries, but the comparison is by Wasserstein distance *(figure only)*, and no correlation coefficient is printed. No conversational memory, abstention or update result is reported.

### MemoryBank

Source: Zhong et al., "MemoryBank: Enhancing Large Language Models with Long-Term Memory", arXiv 2305.10250v3. Repository `zhongwanjun/MemoryBank-SiliconFriend` at `cf61c4196e4cfdb0f2b7a0316249fa40312dc3a9`, files `memory_bank/summarize_memory.py`, `memory_bank/memory_retrieval/forget_memory.py`, `utils/prompt_utils.py`.

- **Unit.** Three layers per user:
  - the raw turns, "stored with timestamps";
  - a daily event summary rolled into "a global summary";
  - "daily personality insights" rolled into "a global understanding of the user's personality".
- **Context at write time.** Each daily summary and daily personality sees only that day's dialogue. The global summary and global personality see all the daily entries. Nothing is conditioned on retrieved neighbours.
- **Written besides the text.** The personality portrait, which in the code also carries a "response strategy". Per item: `memory_strength`, `last_recall_date` and an embedding (MiniLM for English).
- **Retrieval.** Dense retrieval over turns and daily summaries (top 6 in the code). The reply prompt also gets the global portrait and global summary in full.
- **Forgetting.** The paper uses R = e^(−t/S), where recall raises S by 1 and resets t. The code computes `math.exp(-t / 5*S)`, which Python parses as `(-t/5)*S`, so as written a stronger memory decays faster. The repository does not discuss this.
- **Calls.**
  - Write: two per user per day (summary and personality), plus two to refresh the global entries, with `gpt-3.5-turbo` in the code.
  - Read: none before the reply.
- **Size.** Not reported.
- **Numbers** (Table 2, 194 human-written probing questions over 10 simulated days for 15 users, human-scored):
  - SiliconFriend ChatGPT, English: retrieval accuracy 0.763, correctness 0.716, coherence 0.912.
  - The table has no no-memory row and no ablation of the portrait or forgetting.
- **Not settled.** Whether the reported runs used forgetting. There is no conflict handling: a changed fact is neither detected nor reconciled, and nothing measures correction or abstention quantitatively.

### Think-in-Memory

Source: Liu et al., "Think-in-Memory: Recalling and Post-thinking Enable LLMs with Long-Term Memory", arXiv 2311.08719v1 (the only version). The paper names no code repository.

- **Unit.** An "inductive thought", "the text which contains the relation between two entities". It is stored in a hash table keyed by locality-sensitive hashing of its embedding.
- **Context at write time.** After answering, the agent "post-think[s] upon Q-R pair". The insert prompt (Fig. 3) shows only the question-response pair and few-shot examples.
  - The abstract says the agent "incorporates both historical and new thoughts to update the memory". The printed prompt shows no historical thoughts, so which holds is not settled.
- **Written besides the text.** The hash index only: no time, no importance, no links.
- **Other operations,** within one hash group:
  - "forget" removes "counterfactual thoughts or contradictory thoughts";
  - "merge" folds thoughts that share a head entity.
  - In the paper's own forget example, "Michael likes to play football." and "Michael does not like to play football." are both dropped. Thoughts carry no time, so the newer cannot be kept.
- **Retrieval.** The query hashes to a group, and the top-k thoughts within it are returned (top 5 in Table 2).
- **Calls.** Write: at least one per question-response pair. How often forget and merge run is not stated. Read: none before the answer.
- **Size.** Not reported.
- **Numbers** (Table 2, human-scored, ChatGLM):
  - KdConv Film: correctness 0.657 without memory and 0.827 with TiM; retrieval accuracy 0.920.
  - The GVD English SiliconFriend row (0.809, 0.438, 0.680) equals MemoryBank's Table 2 ChatGLM row, and the paper does not say whether it was re-run.
- **Not settled.** Forget and merge have no ablation. There is no multi-hop, abstention or update metric, and no call or token cost.

### A-MEM note construction and evolution (encoding side)

Source: Xu et al., "A-Mem: Agentic Memory for LLM Agents", arXiv 2502.12110v11 (still the latest, the version #877 read). Repositories `WujiangXu/A-mem` at `0c8039f28fdcc08189a23c07a3437d9d2482f9c2` (the benchmark code; `WujiangXu/AgenticMemory` resolves to it), files `memory_layer.py` and `test_advanced.py`; `agiresearch/A-mem` at `ceffb860f0712bbae97b184d440df62bc910ca8d`, file `agentic_memory/memory_system.py`.

#877 covered A-MEM's links and one-hop expansion (`../associative-memory/README.md` §A-MEM). This covers how a note is written.

- **Unit.** A note of content, timestamp, LLM keywords, LLM tags, an LLM "contextual description", an embedding and links (Eq. 1). The benchmark writes one note per LoCoMo turn.
- **Context at write time.**
  - **Construction does not see neighbours.** Keywords, tags and context come from `LLM(c_i ∥ t_i ∥ P_s1)` (Eq. 2). The code's prompt holds only the content, not the timestamp. The context field is "one sentence summarizing" the note's topic, points and purpose.
  - **Neighbours enter afterwards.** The top k by cosine (k = 5 in the code) go to a link and evolution call.
- **Written besides the text.** Keywords, tags, the context sentence and links. The embedding covers all four: `f_enc[concat(c_i, K_i, G_i, X_i)]` (Eq. 3), with all-MiniLM-L6-v2.
  - `agiresearch/A-mem` at the commit read never calls `analyze_content` in `add_note`, and embeds the content alone.
- **Evolution.** The paper: "The evolved memory m_j* then replaces the original memory m_j", updating a neighbour's "context, keywords, and tags", with actions "strengthen", "merge" and "prune".
  - The code makes one combined call. Its `update_neighbor` rewrites a neighbour's `context` and `tags`, never its content, and the code has no merge or prune branch.
  - The benchmark rebuilds the retriever only every 100 evolutions (`evo_threshold=100`).
- **Calls.**
  - Write: two per note in the code; the paper describes three prompts.
  - Tokens: "approximately 1,200 tokens per memory operation".
- **Numbers** (LoCoMo, GPT-4o-mini, F1, Table 3 ablation):

  | Setting | Multi Hop | Temporal | Single Hop | Adversarial |
  |---|---:|---:|---:|---:|
  | without links and evolution | 9.65 | 24.55 | 13.28 | 15.32 |
  | without evolution | 21.35 | 31.24 | 39.17 | 44.16 |
  | full | 27.02 | 45.85 | 44.65 | 50.03 |

  - In Table 1 (GPT-4o-mini), the LoCoMo baseline's Adversarial F1 is 69.23 against A-MEM's 50.03.
  - The benchmark scores the adversarial category as a forced choice (`test_advanced.py`). The prompt offers "Not mentioned in the conversation" and the dataset's `adversarial_answer`, and scores against the latter. It is not a free-form abstention test.
- **Not settled.**
  - Which part of the multi-hop gain is construction, linking or evolution. The code's one-hop link expansion at read time is not in the paper.
  - Whether evolved neighbours were re-embedded before the reported queries.
  - A stale fact keeps its original content, and nothing marks it superseded.
  - Category labels. MemoryOS's Table 2 reprints A-MEM's "Multi Hop" as 45.85, which is A-MEM's Temporal figure. That is the cross-paper label drift #877 found.

### LD-Agent

Source: Li et al., "Hello Again! LLM-powered Personalized Agent for Long-term Dialogue", arXiv 2406.05925v2. Repository `leolee99/LD-Agent` at `af3c15ab63efcb4ab83d635670b316d63977d106`, files `Module/EventMemory.py`, `Module/Personas.py`, `DataLoader/MSC.py`.

- **Unit.** Two stores.
  - **Event memory.** One "brief summary" per ended session, with its time. The prompt asks for "brief sentences in English, within 20 words".
  - **Persona banks.** A user bank and an agent bank, with one trait string per utterance that has one ("No Trait" otherwise).
- **Context at write time.**
  - A session summary sees only that session's cached dialogue: no earlier memory and no persona.
  - Persona extraction sees one sentence ("Please extract the personal traits who said this sentence"), and not the existing bank.
  - A session closes on a gap of β = 600 s in the paper; the code uses 3,600 s.
- **Written besides the text.** The time, and the session's nouns as topics (spaCy). In the code, the embedded document is the noun string, and the summary sits in metadata. Traits are plain strings with no time and no embedding.
- **Retrieval.**
  - Paper: a semantic score plus noun overlap, times a time decay, above a semantic threshold of 0.5; otherwise "No relevant memory".
  - Code: the semantic term and the threshold are commented out. A memory needs at least one shared noun, and one memory is returned.
  - The persona banks are placed whole in the prompt.
- **Calls.**
  - Write: one summary call per session, and two persona calls per exchange (ChatGLM3-6B tuned, or `gpt-3.5-turbo-1106` zero-shot).
  - Read: none before the reply.
- **Numbers** (MSC, tuned ChatGLM, session 2, BLEU-2/BLEU-3/ROUGE-L, Table 2 add-one ablation):
  - baseline 5.48/1.59/17.65;
  - with event memory 7.57/2.49/19.50;
  - with the user persona 7.54/2.57/19.68;
  - full 10.70/5.63/23.31. The full row uses annotated personas and equals Table 1's `ChatGLM*_LDA`.
  - The text: "the event memory module contributing the most".
  - Zero-shot ChatGPT on CC, session 2: 10.57/5.50/22.10 without and 15.89/11.01/26.96 with LD-Agent.
- **Not settled.**
  - No recall accuracy, abstention or update metric; only generation overlap and a plotted human evaluation *(figure only)*.
  - The persona bank is append-only in the code, so a changed trait never replaces the old one.
  - Tables 1 and 2 disagree on session 4 and 5 columns.

### Semantic and profile memory: MemoryOS and Nemori

Two designs that keep facts about the user apart from episodes.

**MemoryOS.** Source: Kang et al., "Memory OS of AI Agent", arXiv 2506.06326v1. Repository `BAI-LAB/MemoryOS` at `587ed7755c7aed179965792830ff1b5ad9a6fa92`, files under `memoryos-pypi/` (`updater.py`, `prompts.py`, `long_term.py`, `mid_term.py`, `retriever.py`). The repository's `eval/` directory was not read.

- **Unit.**
  - **Short term.** Dialogue pages, each with "meta^chain".
  - **Mid term.** Topic segments with an LLM summary.
  - **Long-term persona memory.** A user profile, a user knowledge base, user traits ("90 dimensions across three categories"), and an agent profile and traits.
- **Context at write time.**
  - **Chained page meta.** An LLM first judges "a new page's contextual relevance to prior pages to determine chain linkage", then summarises "all chain pages into meta^chain" (§3.2). The code's prompt says "Start from the previous meta-summary", and rewrites the meta of every page in the chain.
  - **Profile.** The profile prompt includes the existing profile ("Maintain existing insights from the old profile while incorporating new observations"), and its output replaces the profile.
  - **Knowledge base.** Knowledge extraction does not see the existing entries.
  - **When it runs.** Long-term writes fire when a segment's heat exceeds 5.
- **Written besides the text.** Page links, keywords, embeddings, and segment heat. The knowledge base and agent traits are "a fixed-size queue (i.e., 100), employing a First-In-First-Out (FIFO) strategy". The code appends with no deduplication.
- **Retrieval.** Top 5 segments, then the top 10 pages within them. Top 10 entries each from the knowledge base and traits, with the profile whole. Retrieved pages carry "Conversation chain overview: {meta_info}".
- **Calls.** Table 3 gives "Avg. Calls" 4.9 per response, which seems to include writes; the split is not given.
- **Numbers** (LoCoMo, GPT-4o-mini, F1, Table 2): Single Hop 35.27, Multi Hop 41.15, Temporal 20.02, Open Domain 48.62. Table 3 gives average F1 36.23 at 3,874 retrieved tokens and 4.9 calls.
  - The ablation is *(figure only)*. Its text: "the Chain has the least impact".
- **Not settled.** The adversarial category is not reported. How contradicting knowledge-base entries are resolved. Error growth in the recursive chain meta.

**Nemori.** Source: arXiv 2508.03341v4, retitled in v4 as "What Deserves Memory: Adaptive Memory Distillation for LLM Agents" (v1 was "Nemori: Self-Organizing Agent Memory Inspired by Cognitive Science"). Repository `nemori-ai/nemori` at `d2a6dff6e5481214a0be6a2d10147feccfc16244`, files `nemori/llm/generators/semantic.py`, `nemori/llm/prompts.py`, `nemori/core/memory_system.py`, `nemori/llm/generators/merger.py`.

- **Unit.**
  - An episode: a title (the "episodic cue"), a third-person narrative, the raw messages and an embedding.
  - Separately, semantic knowledge statements.
- **Context at write time.**
  - **Boundaries.** One call partitions a buffer of 20 messages.
  - **Narrative.** It sees only its episode and the boundary reason. It converts relative times "to absolute dates".
  - **Integration.** The top 5 earlier episodes by cosine go to an LLM, which picks one with "episodic continuity" or none. A picked pair is rewritten into one episode that supersedes the old.
  - **Semantic distillation.** An anticipated episode is predicted from the title and up to 10 related semantic entries (similarity above 0.70). What the real episode adds beyond the prediction is then extracted.
  - **Consolidation (paper only).** Each insight is judged new, merge or conflict against 5 neighbours, and "conflict purges outdated entries and replaces" them. At the commit read, the code saves new statements without that step: a search of its 72 Python files for "conflict" finds only SQL upserts.
- **Retrieval.** The top 10 episodes and 20 semantic entries by similarity. Only "the top-2 episodic memories include their original conversation text".
- **Calls.** Several per episode. Table 3 (LoCoMo construction, gpt-4o-mini): Nemori 373.2 calls and 322.9k tokens, against Mem0's 1,602.2, MemoryOS's 1,016.1 and A-MEM's 1,175.5. The unit of the calls column is not printed.
- **Compression.** Answer context is "an 88% reduction compared with Full Context's 23,653 tokens" on LoCoMo.
- **Numbers.**
  - LoCoMo, LLM judge (Table 2). gpt-4o-mini: average 73.0 against full context 72.3; Multi-Hop 61.7 against 66.8. gpt-4.1-mini: 80.8 against 80.6; Multi-Hop 74.8 against 77.2.
  - Ablation (Table 5, gpt-4o-mini / gpt-4.1-mini): full 73.0 / 80.8; without semantic retrieval 54.7 / 76.9; without episodic retrieval 65.0 / 74.9; fixed 20-message chunks instead of boundaries 68.0 / 75.7.
  - LongMemEval_S (Table 8), Knowledge Update: 61.5 against full context 78.2 on gpt-4o-mini, and 79.5 against 76.9 on gpt-4.1-mini. Single-session Preference: 46.7 against 6.7 on gpt-4o-mini.
  - The paper says LoCoMo "rarely involves knowledge updates requiring consolidation".
- **Not settled.** The adversarial category and the LongMemEval abstention subset are not reported. How often conflict consolidation fires, and what errors the integration rewrite introduces.

### Chained summaries: recursive summarization

Source: Wang et al., "Recursively Summarizing Enables Long-Term Dialogue Memory in Large Language Models", arXiv 2308.15022v4. Repository `qingyue2014/Rsum` at `c06ebccc15c4ebfa5e3b487c4591af64a3b4c25c`, file `main_chatgpt.py`. Added beyond the issue's list because it is the published form of a memory chained on the previous one.

- **Unit.** One memory of "multiple sentences", capped by the prompt "ensuring it does not exceed 20 sentences". The new memory replaces the old.
- **Context at write time.** The previous memory and the whole ended session: `M_i = LLM(S_i, M_{i−1}, P_m)`. The prompt asks to "identify any new or changed personality" and to "Combine the old and new personality information".
- **Written besides the text.** Nothing.
- **Retrieval.** None. The latest memory is in every response prompt.
- **Calls.** One per session (`gpt-3.5-turbo-0301`). Read: none before the reply.
- **Numbers** (MSC session 5, Table 3): F1 20.48 against 19.41 without memory and 20.28 with MemoryBank. The text: "about +0.2% on the F1 score".
- **Error audit** (Table 6, 100 dialogues, memory of the last session): Fabricated Facts 2.7%, Incorrect Relationships 3.2%, Missing Details 3.9%. The text says "the incorrect/ inaccurate information does not exceed 10% of the summaries content".
  - Adding retrieved raw utterances on top of the memory raised F1 (Table 8): BM25 k = 5 from 20.91 to 21.81. The paper calls those utterances "evidence of event details".
- **Not settled.** The error rate is audited on the last of five sessions only, so its growth across rewrites is not measured. The paper proposes no fix for a memory that is wrong. Nothing is recoverable once overwritten.

## Comparison

"Neighbour" means the writer sees content beyond its own unit. "Recursive" means the writer's input includes its own earlier output.

| Structure | Unit | Writer sees beyond the unit | Written besides the text | Retrieval | Write calls | Read calls | Size | Headline number, in its setting |
|---|---|---|---|---|---|---|---|---|
| Contextual Retrieval | chunk | whole document | 50–100-token context, prepended | hybrid dense + BM25, optional rerank | 1 per chunk | 0 (rerank optional) | expands | recall@20 failure 5.7% → 2.9% (context + BM25) |
| Late chunking | chunk vector | whole document, in the embedder | nothing | kNN | 0 (embedder only) | 0 | as naive | BeIR nDCG@10 avg 52.4 → 54.3 (sentence chunks) |
| ReadAgent | page gist | pagination: window; gist: nothing | page tag; raw page | model reads all gists, expands pages | ~2 per page | 1 or more look-ups | gists 85.53% compression (QuALITY) | 77.52% gists, 87.17% with look-up, 85.83% full |
| SeCom | topical segment, raw | whole session (segmenter) | compressed key (75%) | BM25 / MPNet on keys, raw delivered | 1 per session + compressor | 0 | keys 75% | LOCOMO GPT4Score 71.57 vs turn 65.58 |
| EM-LLM | event KV | all preceding tokens (surprise) | representative tokens | k-NN + ±1 contiguity | 0 | 0 | none (full KV) | LongBench 41.9 → 43.7 (Mistral) |
| MemoryBank | turns, daily summary, portrait | day only; global: all dailies (recursive) | strength, recall date, portrait | dense + global portrait | 2 per day + 2 global | 0 | not reported | human retrieval acc 0.763 (ChatGPT, English) |
| Think-in-Memory | thought (relation) | question-response pair | hash index | LSH group, top-k | ≥ 1 per turn + forget/merge | 0 | not reported | KdConv Film correctness 0.657 → 0.827 |
| A-MEM (encoding) | note per turn | nothing (construction); 5 neighbours (links, evolution) | keywords, tags, context, links; rewrites neighbours | cosine, one-hop links (code) | 2 per note (code) | 1 keyword call | not reported | LoCoMo multi-hop F1 9.65 → 21.35 → 27.02 |
| LD-Agent | session summary; persona traits | session only; one sentence | time, nouns | noun overlap × time decay | 1 per session + 2 per exchange | 0 | ≤ 20 words per session | MSC S2 BLEU-2 5.48 → 7.57 (event memory) |
| MemoryOS | page, segment, profile, KB | previous page meta (recursive); old profile (recursive) | chain links, heat, FIFO KB | segments then pages; KB top 10; profile whole | several per page | 0 | not reported | LoCoMo multi-hop F1 41.15 (GPT-4o-mini) |
| Nemori | episode narrative; semantic statements | top-5 episodes (integration); 10 semantic entries (prediction) | title, absolute times, statements | top-10 episodes, top-20 statements | several per episode | 0 | answer context −88% | LoCoMo judge 73.0 vs full 72.3; Knowledge Update 61.5 vs 78.2 (gpt-4o-mini) |
| Recursive summarization | one rolling memory | previous memory (recursive) | nothing | none; always in prompt | 1 per session | 0 | ≤ 20 sentences | MSC F1 19.41 → 20.48; 2.7% fabricated facts |

What each source measured on the five properties the issue asks about:

| Structure | Chain reach | Implicit association | Absent-question abstention | Correction handling | Compression |
|---|---|---|---|---|---|
| Contextual Retrieval | no | example only (ACME) | no | no | expands |
| Late chunking | no | example only ("Berlin") | no | no | none |
| ReadAgent | no | case study only | no; risk flagged | no | yes, with accuracy |
| SeCom | no | motivating example only | no | no | keys only |
| EM-LLM | HotpotQA, 2WikiMQA, MuSiQue per task | no | no | no | none |
| MemoryBank | no | no | one example | no mechanism | no |
| Think-in-Memory | no | no | no | mechanism drops both sides | no |
| A-MEM | LoCoMo multi-hop | no | forced-choice adversarial | no; content never superseded | no |
| LD-Agent | no | no | threshold mechanism, unmeasured | append-only | cap only |
| MemoryOS | LoCoMo multi-hop | no | no | FIFO, no dedup | no |
| Nemori | LoCoMo multi-hop | no | no | Knowledge Update (aggregate) | answer context |
| Recursive summarization | no | no | no | error audit, last session | cap only |

## What write-time context buys against read-time association

Sherlock has measured one read-time association: L's entity walk. It raised chain application from 9 to 56 of 100 (#885), at the cost of abstention (57 to 39 of 60) and corrections (28 to 25 of 40). The question is what moving context or association into the write can change, property by property.

- **Chain reach.**
  - Every surveyed write-time context stays local to the unit: its own document (Contextual Retrieval, late chunking), its session (SeCom), a pagination window (ReadAgent), or its ±1 neighbours (EM-LLM's contiguity buffer).
  - Sherlock's chain hops sit at least 16 exchanges apart. So no local write-time context can reach a later hop. The evidence of hop k+1 is never in hop k's neighbourhood.
  - Only a write-time link to a distant unit can reach it. That means A-MEM's link generation over 5 neighbours, Nemori's integration over the top 5 episodes, or MemoryOS's chain judged against prior pages.
  - Of those, only A-MEM reports a multi-hop change it attributes to links (9.65 to 21.35 F1). Nemori stays below full context on multi-hop with both models. MemoryOS's chain "has the least impact" *(figure only)*.
  - A write-time link is L's mechanism with the link chosen by a model instead of by a shared name. Nothing surveyed predicts it will reach further than L.
- **Implicit association.**
  - Where the association is local, write-time context is the one mechanism with direct evidence. Contextual Retrieval's ACME example and late chunking's "Berlin" anaphora both name, in the unit, a referent the unit itself leaves implicit: 0.7084 to 0.8249 cosine in the latter. Neither is a benchmark.
  - This is the owner's painting case: a batch that says "the studio" while the previous exchange said whose studio.
  - Sherlock's implicit chains are not local. #874's IC hops are as far apart as its explicit ones, and they failed at reach.
  - For standing facts about the user, such as a style or a rule, the separate-store designs keep them in view whole: MemoryBank's global portrait, LD-Agent's persona banks, MemoryOS's profile. Removing Nemori's semantic retrieval moved its judge score from 73.0 to 54.7 on gpt-4o-mini. #874's "applying follows seeing" says that being in view is what counts.
- **Absent-question abstention.**
  - No source measures free-form abstention. A-MEM's adversarial score is a forced choice between two strings, and LD-Agent's threshold was commented out of its own code. So the literature offers no counter-evidence to #885.
  - #885 showed that associated material in view gets answered from, 19 of 60 times.
  - A context line written into a unit puts the association into the unit's own text. Similarity alone then carries it into view, without the walk and without a label to mark it.
  - The expected risk is therefore at least L's, unless the context is stored and shown apart from the summary. Contextual Retrieval's notebook keeps the two in separate fields.
- **Correction handling.**
  - Every mechanism that writes from earlier output mishandles updates somewhere:
    - TiM's forget drops both sides of a contradiction;
    - A-MEM's evolution rewrites a neighbour's context but never its content;
    - MemoryOS appends its knowledge base with no dedup;
    - LD-Agent's persona bank is append-only;
    - Nemori's conflict step is in the paper but not the code;
    - recursive summarization's last-session memory holds 2.7% fabricated facts and 3.2% wrong relationships.
  - Nemori's Knowledge Update score fell to 61.5 against full context's 78.2 on gpt-4o-mini.
  - Sherlock's current defence is immutable episodes, chronology and source inspection (ADR 0006, ADR 0018). A write-time context that restates an earlier value into a newer episode makes the stale value look newer. #885's K losses show corrections are already the fragile guard.
- **Compression.**
  - Write-time context adds storage in every design that writes text: 50–100 tokens per chunk (Contextual Retrieval), keywords and context per note (A-MEM), titles and statements (Nemori).
  - The sources that compress do it by holding less in view and looking up the rest: ReadAgent's gists at 85.53% compression answer 77.52% against 85.83%, and looking up pages recovers 87.17%.
  - Or they compress only the retrieval key and deliver the raw text: SeCom's 75% keys raised LOCOMO GPT4Score from 59.87 to 69.33.
  - Sherlock already has ReadAgent's shape: a summary as the lead and `memory_inspect` as the look-up. At batch 4 it stores 0.705 characters per source character, so its summaries barely compress. At batch 8 it compresses to 0.466 and loses exact reach (#874).
  - No surveyed source reports compression and association measured together.

## What the sources settle and what they do not

**Settled, within the settings named:**
- **Local write-time context helps retrieval of the unit itself.** Contextual Retrieval cut recall@20 failures from 5.7% to 3.7% with embeddings alone. Late chunking raised nDCG@10 by 1.5 to 1.9 points. SeCom's segments beat turn and session units on its own benchmarks.
- **A generic summary as context helps little.** Contextual Retrieval saw "very limited gains" from "adding generic document summaries to chunks". SeCom's summary baselines (SumMem, RecurSum) "fall behind turn-level or session-level baselines".
- **Recursive writing propagates errors, and nobody has measured how fast.** Recursive summarization found 2.7% fabricated facts in its last-session memory, and no source audits error growth across rewrites. This is the risk ADR 0006 cites.
- **User facts kept apart from episodes carry weight.** Nemori's ablation (73.0 to 54.7 without semantic retrieval, gpt-4o-mini) and LD-Agent's add-one ablation (BLEU-2 5.48 to 7.54 with the user persona) both show it. So does Nemori's single-session preference score (46.7 against full context's 6.7).

**Not settled:**
- **Whether write-time context reaches distant associations.** No source measures chains whose hops lie outside each other's neighbourhood.
- **Abstention under any write-time structure.** No source reports free-form abstention.
- **Correction under separate fact stores.** The designs mostly lack a working conflict step, or have one unmeasured. Nemori's aggregate Knowledge Update score is the one figure, and it moves with the model: below full context on gpt-4o-mini, above it on gpt-4.1-mini.
- **Transfer to MiniMax-M2 and a 512-position CLS embedder.** No source uses either.

## Candidates for sherlock

Excluded, with the reason each fails sherlock's constraints:
- **Late chunking.** It needs a mean-pooling embedder whose window holds the context. Sherlock's `bge-base-en-v1.5` pools the CLS token over 512 positions.
- **EM-LLM.** It stores key-value caches inside the model. Sherlock reaches its model over an API (ADR 0004).
- **ReadAgent's gist memory in context.** "the size of the gist memory is limited by the LLM's context length". Sherlock's summary-then-inspect path is already its look-up shape.
- **SeCom's compressed keys.** They need LLMLingua-2, a second local model. Sherlock's keys are summaries, not raw turns, so the denoising measured on raw turns does not transfer directly.
- **Think-in-Memory's forget and merge, and A-MEM's evolution.** They rewrite or delete stored memories, which conflicts with immutable episodes (ADR 0006). In their own examples they lose the newer side of a correction.
- **Recursive summarization as written.** One rolling memory that replaces itself is what ADR 0006 rejected. The note is sherlock's bounded rolling memory already.

Estimates below are net production lines before tests, read from the files at `d8dc900`; nothing was implemented. Costs assume MiniMax only, subscription-bound, with local embeddings: calls and characters, not dollars. The shipped call has a 557-character instruction and about 748 characters of batch, and read about 335 tokens in #874.

### C. Context at synthesis (the owner's first proposal)

- **Why.** Contextual Retrieval is the one design with a measured gain from showing the writer the unit's surroundings. Its gain comes from a short situating string, not from a summary of the surroundings. Sherlock's batch is cut every four messages regardless of topic, so a batch that says "the studio" often lost its referent to the batch before it.
- **Change to the input.** Besides the four messages, the call receives two things, both labelled as context and not to be summarised:
  - the W messages before the batch, raw from `conversation_sources`;
  - the stored note.

  Raw sources, not the previous summary, so that no synthesis output feeds the next synthesis.
- **Change to the output.** The `memory_summary` schema gains `context`: at most 300 characters naming who, where and what thread the batch belongs to, stated only from the context given. The summary stays about the batch alone.
- **Storage and use.**
  - The context goes in its own column.
  - It is embedded before the summary, as Contextual Retrieval prepends it.
  - Recall shows it labelled, apart from the summary: `Context when written: …`.
- **Deletes or replaces.** Nothing. The independent summary stays, and only its input grows.
- **Lines.**
  - `summary.go`: the input struct and the schema field. **+10 to +15.**
  - `internal/agent/conversation.go`: the summarizer signature. **+2 to +4.**
  - `accumulate.go`: read the preceding W sources and the note, and store the context. **+15 to +25.**
  - Schema: a nullable column plus its `ALTER` for existing files. **+5 to +10.**
  - Embedding text and the recall label. **+5 to +10.**
  - Net **+35 to +65**.
- **Cost.** No extra call.
  - The input grows by about 187 × W characters of messages, plus the note (up to 2,000). At W = 8 that is about 1,500 characters plus the note, against the shipped 1,300 or so.
  - The output grows by at most 300 characters.
  - Storage grows by at most 0.40 characters per source character at batch 4 (300 over 748).
  - Context plus summary may exceed the embedder's 512 positions. Putting the context first decides what a cut removes.
- **Tension with ADR 0006.** The ADR's decision says the model "receives the batch, not every previous mid summary". C gives no summary, but it does give raw neighbouring sources and the note. The note is model output, so an error in it can enter an episode, a bounded form of the propagation the ADR rejects. Adopting C revises the decision's wording.
- **Hypothesis.** On situated items, a batch that does not name its subject while the preceding W messages do, C reaches the evidence mid more often than S at equal context characters. It does not raise reach on #885-style chains, whose hops are at least 16 exchanges apart.
- **Expected risk.**
  - **Abstention: raised.** A query naming the context's subject now matches by similarity an episode about something else in that thread. The context line then puts the associated value in view unlabelled, unless the label holds.
  - **Correction: raised where the note holds an obsolete value.** The context line can restate it into a newer episode.

### P. Episodes chained on the previous episode (the owner's second proposal)

- **Why.** MemoryOS chains pages by a continuity judgement against the prior page. Nemori joins an episode to one of 5 earlier candidates. EM-LLM's contiguity buffer, delivering ±1 neighbours, was best in 44% of its tasks. Recursive summarization shows what a chain costs when it is carried in text: 2.7% fabricated facts, with the growth unmeasured. So P carries the chain in a link, not in text.
- **Change to the input.** Besides the batch, the call receives the previous mid's summary, labelled "earlier episode, for reference, do not restate".
  - Setting `k5`: instead, the top 5 earlier mids by cosine to the batch's text (one local embedding, no model call), each with its ID. This is A-MEM's and Nemori's k = 5.
- **Change to the output.** The schema gains `continues`: a boolean for the previous mid, or a list of the given IDs at `k5`. The summary stays about the batch alone.
- **Storage and use.**
  - A `mid_link` table (mid, linked mid) is written with the episode.
  - Recall and `memory_search` fill the historical slots in turn from the similar list and from the linked neighbours of mids already delivered, in both directions, as L's `alternate` does. So context stays equal by item count.
  - A linked mid is delivered with the kind `thread`.
- **Deletes or replaces.** Nothing.
- **Lines.**
  - `summary.go` input and schema. **+10 to +15.**
  - `accumulate.go` previous mid or top-5 lookup and link write. **+15 to +30.**
  - Link table and its reading. **+15 to +25.**
  - Recall and search slot filling, which L measured at about 43 lines. **+35 to +45.**
  - Net **+75 to +115**. The `prev` setting is at the low end.
- **Cost.** No extra call.
  - The input grows by the previous summary, 512 characters on average at batch 4, or by about 2,600 at `k5`.
  - The output grows by a boolean or a few IDs.
  - Storage adds one link row per link and no text.
- **Tension with ADR 0006.** P is exactly what the ADR's decision excludes: the model receives a previous mid summary. The ADR's reason, error propagation, is met here only in part.
  - The summary may not restate the earlier episode, and the link carries no text. So an earlier error cannot be copied forward by rule.
  - But the model reading the earlier summary may still take its wording, and nothing enforces the rule except the prompt.
  - Adopting P revises the ADR's decision and its rejected alternative, and the revision would need this run's evidence on correction.
- **Hypothesis.**
  - At `prev`, P reaches no more chain hops than S: hops are at least 16 exchanges apart, so the previous mid is never a later hop. It raises reach on threads that span consecutive batches.
  - At `k5`, the batch holding hop k+1's statement meets hop k's mid among its 5 candidates whenever they share names. So P-k5 is predicted to chain like L, through a model-chosen link instead of a shared name.
- **Expected risk.**
  - **Abstention: as L's at `k5`.** A linked mid puts associated values in view, and #885 found that structural. The `thread` kind is a label, which in #885 cut absent answers from 19 to 13 and cost corrections.
  - **Correction: lower than C's.** No text moves forward. But a linked stale episode is delivered beside its correction, as L's `K-01` listed both values.

### F. Standing facts written by the synthesis call

- **Why.** The owner's third example is one's own painting style: a fact about the user, not an episode. MemoryBank's portrait, LD-Agent's persona bank, MemoryOS's profile and Nemori's semantic memory all keep such facts apart, and Nemori's is the largest ablation effect surveyed. #874 found that what is applied is what is in view. The note could hold such facts, but it is written by the acting model and restated in full each turn.
- **Change to the input.** None.
- **Change to the output.** The schema gains `facts`: at most 5 statements per batch, each at most 200 characters, each typed `user` (the user's attributes, preferences, style, standing rules) or `other`. They are stated only from the batch. It is the same one call, as L's entity list is.
- **Storage and use.**
  - Facts go in a `mid_fact` table with their mid ID, embedded like mids.
  - Recall adds two things:
    - the newest `user` facts, up to 1,000 characters, always delivered as a profile block, each with its mid ID;
    - the top 3 facts by cosine to the task, beside the mids.
  - A later fact with the same meaning is not reconciled. Both show, newest first, and the contract's chronology rule applies (ADR 0018). That keeps immutability and adds no conflict step: the step the surveyed designs lack or get wrong.
- **Deletes or replaces.** Nothing. The note keeps its role.
- **Lines.**
  - `summary.go` schema and parsing. **+12 to +18.**
  - The fact table, written with the episode and embedded with the mids. **+35 to +50.**
  - Recall's profile block and fact list. **+40 to +60.**
  - Net **+90 to +130**.
- **Cost.** No extra call.
  - The output grows by at most 1,000 characters. L's entity list added 63 output tokens per batch, the nearest measured analogue.
  - Storage adds the facts and one 3,072-byte vector each.
  - Context grows by at most 1,000 characters for the profile block, plus the three facts.
- **Hypothesis.** On items where the user states a standing fact once and a later task needs it without naming it, F applies it more often than S. On explicit chains it does no better than S.
- **Expected risk.**
  - **Correction: the main one.** An older fact stays in the profile block until newer `user` facts push it out, and a corrected fact shows both values. Nemori's Knowledge Update loss on gpt-4o-mini (61.5 against 78.2) is the surveyed warning.
  - **Abstention: lower than L's.** Facts are about the user and are delivered as the user's. But a profile in view can be answered from on a question it does not settle.

## Test plan

It reuses the chained-recall harness: `study.Arm`, the shared stores, the proxy with its 2056 pause, `score.py`'s reach and applied checks, and the corpus rules of `../chained-recall/confirm/corpus.py`. The run is its own issue with its own preregistration, committed before any call.

- **Arms.**
  - S at `b4`, the baseline.
  - C at `b4-w8`.
  - P at `b4-prev` and `b4-k5`.
  - F at `b4`.
  - L at `b4-model`, offline only, as the measured reference for chain reach. It needs no new calls where #885's entities are recorded.
  - Each arm states its measured net diff.
  - **The note offline.** The stream harness synthesises without an act loop, so the note is empty offline, and C's offline setting measures the window part alone. C's note part can only be read from live probes, where the note starts empty too (#874 §Limits). So the first run tests C without the note, and says so.
- **Corpus.** A new seed, #885's templates and rules. Every evidence exchange fills its batch alone, and chain hops sit at least 16 exchanges apart.

  | Family | n | Role | What it tests |
  |---|---:|---|---|
  | C2, C3 | 50 each | primary for P | explicit chains, as #885 |
  | IC2, IC3 | 25 each | measured, secondary for every arm | implicit chains, now scored and paired rather than reported only |
  | SC | 50 | primary for C | situated: the evidence exchange names no subject; the exchange just before it (within W) does; the query names the subject |
  | UP | 50 | primary for F | user standing fact stated once; a later task needs it unnamed (the owner's painting style) |
  | C1 | 30 | guard | single hop |
  | E | 30 | guard | exact |
  | K | 40 | guard | correction, as #885 |
  | KU | 30 | guard | a user standing fact later changed; the task needs the newer |
  | N | 60 | guard | absent, #885's construction: the association states a value of the asked kind |
  | NC | 30 | guard | absent, situated: the context names the subject; the asked attribute is stated nowhere |

- **Chain check.**
  - No later hop, SC evidence or UP fact may rank in the raw query's top 20 at batch 4 under S, as #885 required.
  - The oracle must deliver 95% of evidence within 5 for its own statement.
- **Offline stage, local embedder only.**
  - Each arm synthesises the stream through the proxy once: about 875 calls per arm, as L's entity synthesis took in #885.
  - Per family it reports:
    - reach;
    - context characters per turn;
    - stored characters per source character;
    - vectors stored;
    - write-call input and output tokens against S's.
- **Mechanism controls,** fixed before the first offline run:
  - C: 90% of mids have a non-empty context, and the context names the subject in 80% of SC items.
  - P: `continues` is set on at least 10% of mids. At `k5`, at least half of the chain hop pairs are linked.
  - F: 90% of UP facts are written as `user`.
  - Mutation checks as `../chained-recall/mutation.py`.
- **Live stage.**
  - S always.
  - Of C, P and F, each arm whose control passed and whose offline reach on its primary family beats S's by the preregistered margin.
  - One live turn per probe on its own store copy, a 32-message window and an empty note, as #885.
- **Decision rule.**
  - Exact McNemar on applied, per arm on its primary family against S, Holm across arms.
  - Guardrails at #885's sizes: a loss of at most 3 of 60 on N, 2 of 40 on K, and 3 each on C1 and E. NC and KU at the same shares: 2 of 30.
  - IC is reported with its pair counts and p, but it is no gate.
  - Among valid arms, the smallest net diff is taken. If none is valid and every control passed, the shipped structure stands.
- **Compression figures.** Every arm reports its stored characters per source character and context characters per turn beside S's 0.731 and 3,279 (#885). A candidate that raises storage by more than L's 0.083 says so in the verdict.
- **Cost rules.** MiniMax only, through the proxy. Probe the key at most every 30 minutes under the 2056 pause, and re-run failed episodes once. Tokens are reported per phase. #885's run took 3,965 calls and 10.5M input tokens for three arms. This run has up to five live arms, so the live stage admits arms by the offline margin rather than running all.

## Limits

- **A survey, not a measurement.** No structure was run. Every number is the source's own, in its setting: other models, other embedders, documents or synthetic dialogue.
- **Selection.** The structures are the issue's list, plus recursive summarization and a second profile design. Others that write context, such as proposition indexing or hypothetical-question keys, were not read.
- **Papers against repositories.** They differ in A-MEM, SeCom, LD-Agent, MemoryBank, MemoryOS and Nemori, and both are named where they do. Several repositories are demos or post-paper packages, not the code behind the reported numbers.
- **Category labels.** LoCoMo categories are labelled differently across papers (MemoryOS against A-MEM), so cross-paper multi-hop figures are not comparable, as #877 found.
- **Estimates.** Net lines, character costs and storage bounds are read or computed from the files and #874's recorded corpus, not implemented. Token costs of the extended calls are not estimated, beyond L's measured +61 input and +63 output tokens, the nearest analogue.
- **The corpus decides what the run can see.** SC, UP, KU and NC are new templated families, whose names are invented words reused exactly, like #874's. Paraphrase is outside what they can measure.
