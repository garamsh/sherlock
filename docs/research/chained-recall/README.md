# Chained recall across mid memories

Records issue #874's study: for the memory structures `docs/research/associative-memory/README.md` §Test plan for #874 selected, how well the agent follows a chain of associated memories, and how much each structure compresses. This revision holds the shared basis every arm is built on; results follow when the arms land.

## Contents
- Question
- Layout
- The corpus
- The chain check
- Writing an arm
- Scoring
- Shared services
- Status

## Question

Recall one memory, recall what it leads to, then recall again. The question is which structure reaches every hop's evidence, and applies it, per unit of context and storage. It is measured for four arms:
- **S**, the shipped structure at `03cbdff`;
- **M**, S with an instruction to follow what a search result names;
- **L**, entity links written by the synthesis call and followed by a Personalized PageRank walk;
- **H**, a second level of contiguous group summaries.

S and L run at batch 4 and 8, H at groups of 4 and 8 batch-4 mids. M shares S's store.

## Layout

- `corpus.py`: generates `pool.json` (candidate chains) and, after screening, `corpus.json`. `corpus_test.py` checks the corpus rules.
- `study/`: the Go package every arm imports.
  - `arm.go`: the arm interface.
  - `main.go`: the runner, with `build` and `offline`.
  - `store.go`: the shared stores built from recorded summaries.
  - `adapt.go`: how items reach the production session.
  - `corpus.go`: the corpus types.
- `harness/`: the shared phases: the proxy, the screen, stream synthesis, shared-store builds and the chain check.
- `arms/<name>/`: one command per arm; its `main` is `study.Main(New())`.
  - S is `arms/shipped/`, M is `arms/chain-contract/`, L is `arms/links/` and H is `arms/levels/`.
- `score.py`: the per-hop scorer, tested by `score_test.py`.
- `evidence/`:
  - `screen.json` and `check-b<n>.json`: the chain check;
  - `synthesis/`: recorded stream summaries;
  - `offline/<arm>/<setting>/`: each arm's records;
  - `offline/scores.json`: the scores.

## The corpus

`python3 corpus.py pool`, then `harness screen`, then `python3 corpus.py assemble`. Both Python steps are deterministic, and re-running them rewrites byte-identical files.

- **Base.** The #856 replication's 4,800 messages (source IDs 1 to 4,800), whose batch-4 and batch-8 summaries are already recorded (`../mid-memory-rag/evidence/replication/`).
- **Chains.** 180 primary chains follow the base as a 1,696-message stream:

  | Family | k | n | Answer |
  |---|---:|---:|---|
  | C1 | 1 | 30 | a value |
  | C2 | 2 | 50 | a value |
  | C3 | 3 | 50 | a value |
  | IC2 | 2 | 25 | a rule the written artifact must obey |
  | IC3 | 3 | 25 | a rule the written artifact must obey |

  - Explicit chains follow six relation paths, for example project → supplier → contact → extension.
  - Implicit chains follow deliverable → office → house rules → a reference the artifact must include.
  - Every entity is an invented word that occurs nowhere in the base.
- **Each hop is one exchange that fills one batch of four alone:** statement, acknowledgement, a same-topic follow-up and its acknowledgement. This is the rule #870 needed for the oracle to pass or fail on ranking alone.
  - Hops of one chain sit at least 16 exchanges apart, in random order.
  - So they fall in different mids at batch 4 and 8, and in different groups of 4 and 8 batch-4 mids.
- **Hop words.** Hop k+1's identifying words, its entities and its answer, appear in hop k's statement and in no earlier hop or query (`corpus.py` `lexical_ok`, tested on every chain with a leaking control).
- **Tail.** 76 messages of unused replication chat close the stream on a multiple of 32 messages. So the restored window and the newest mids hold no evidence.
- **Guards.** 20 each of the replication's exact (E), correction (K) and absent (N) queries, whose evidence is in the base.

## The chain check

- **Before synthesis** (`harness screen`, `evidence/screen.json`). Every candidate exchange's text and every base mid's recorded summary are embedded with the local embedder. A chain is kept only when its raw query ranks none of its hop-2 or hop-3 exchanges within the top 20, deeper than any arm's budget.
  - 95 of 100 C2 and 91 of 100 C3 candidates passed, and every IC candidate passed.
  - `assemble` takes the first passing candidates of each family in pool order.
  - Median raw-query rank of the kept chains' later hops:
    - C2 hop 2: 254.5;
    - C3 hop 2: 146; hop 3: 201;
    - IC2 hop 2: 816;
    - IC3 hop 2: 844; hop 3: 946.
  - Hop 1 of IC2 has a median rank of 22, so the task wording alone often misses even the first hop.
- **After synthesis** (`harness check -batch <n>`, `evidence/check-b<n>.json`). The same test runs through the production similarity search over each shared store's real summaries. Beside it, the oracle ranks each hop's mid for the hop's own statement.

## Writing an arm

The interface is `study/arm.go`; its doc comments are the contract. In short:

- **An arm is a command** at `arms/<name>/main.go`: `func main() { study.Main(New()) }`, where `New` returns a `study.Arm`.
  - `go run ./docs/research/chained-recall/arms/<name> build -work <scratch>` builds every setting.
  - `... offline -work <scratch>` records what each setting delivers.
  - Neither command registers the arm anywhere else.
- **`Settings`** names the compression settings: "b4" and "b8" (batch) or "g4" and "g8" (group).
- **`Build(ctx, env, setting, dir)`** writes the arm's memory of the whole corpus into `dir` and returns a `study.Built`.
  - `Built` counts the characters the arm stores for retrieval beyond the source archive, its vectors and its units.
  - An arm may start from a shared store (`env.SharedStore(4)` or `(8)`: the production store over base and stream at that batch, from recorded summaries). It copies it with `study.CopyDir` and never writes to it.
  - Model calls go only through `env.Model(episode, phase)`, the study proxy, and the phase names what the report's token table counts. Embeddings go only through `env.Embedder`, the local TEI server.
- **`Open(ctx, env, setting, dir)`** returns a `study.Memory`:
  - **`Recall(task)`**: the items the arm puts in context before the act loop, most important first.
  - **`Search(query, limit)`**: what the model's `memory_search` returns.
  - **`Store()`**: the production conversation store under it, which the live session records turns to and reads sources, recent messages and the note from.
- **`Instructions()`** is text the live session appends to the default ego; M uses it, the others return "".
- **An item** (`study.Item`) carries:
  - its ID and level ("mid", or the arm's own, such as "group");
  - a kind shown to the model as the age label ("recent", "historical", or the arm's own, such as "linked");
  - its inclusive source interval;
  - the text the model reads.

  `study.Recalled` renders items exactly as production renders mids. `study.ContextChars` counts them as the session writes them.
- **Controls.** A memory whose mechanism leaves evidence per call (walk steps, seeds, groups delivered) implements `study.Tracer`. The runner records its trace beside every probe.
- **What `offline` writes** under `evidence/offline/<arm>/<setting>/`:
  - `built.json`;
  - `probes.jsonl`: `Recall` for every chain query and guard query;
  - `oracle.jsonl`: `Search` for every hop's own statement, top 5.

## Scoring

`python3 score.py offline` reads every arm's records and writes `evidence/offline/scores.json`.

- **A hop is reached** when a delivered item's interval holds the hop's statement and the item's text states both entities the hop joins. Values are matched as `../agent-context/score.py` matches them. Interval-only reach is reported beside it.
- **A chain is reached** when every hop is.
- Per arm and setting it reports:
  - chain reach by family and by k;
  - per-hop reach;
  - oracle reach;
  - guard reach;
  - mean context characters per turn;
  - stored characters per source character.
- **Pairing.** Each arm and setting is paired against S at batch 4 on the k ≥ 2 chains by the exact McNemar test.
- The live stage's scoring is added with its preregistration.

## Shared services

Three workers share one MiniMax key, so there is one proxy and one ledger.

- **Embedder.** Text-embeddings-inference 1.9.4 serves `BAAI/bge-base-en-v1.5` (revision `a5beb1e3e68b9ab74eb54cfd186867f64f240e1a`) on `127.0.0.1:8090`, container `tei-874`, as in `../mid-memory-rag/` §Production parity.
- **Proxy.** `harness proxy` listens on `127.0.0.1:8091`, run by #874's worker with `MINIMAX_KEY` in its environment only. Arms reach it with `-proxy http://127.0.0.1:8091`.
  - Its route names the arm, episode and phase of every call.
  - It reserves each request's worst case against a ceiling before sending.
  - On HTTP 429 with code 2056 it pauses every request and probes the key at most every 30 minutes (`harness/quota.go`). A paused request is answered 429 at once, and `/budget` reports `"paused": true`, so a caller waits rather than failing its episode.
- Every paid phase is preregistered and committed before its first call.

## Status

- **Done in this revision:** the corpus, the screen, the harness, the interface, the scorer and their tests. No model call has been made.
- **Next:**
  - the preregistration of the stream's synthesis at batch 4 and 8;
  - the shared stores;
  - arms S and M;
  - then the integration of L and H, and the live stage under its own preregistration.
