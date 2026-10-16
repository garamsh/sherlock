# Chained recall across mid memories

Records issue #874's study. For the memory structures `research/associative-memory/README.md` §Test plan for #874 selected, it measures how well the agent follows a chain of associated memories, and how much each structure compresses.

- **Arms.** S and M were built under #874. L (#880, PR #882) and H (#881, PR #883) were built on the shared basis by other workers and integrated here.
- **The structure that chains is L:** entity links written by the synthesis call and followed by a PageRank walk.
  - **Offline:** L reaches every hop of 74 of the 150 chains of k ≥ 2, against 0 for the shipped structure. That costs 2% more context characters per turn and 11% more stored characters.
  - **Live:** the agent applied the answer in 47 of the 114 clean chains with L, against 4 with the shipped structure (46 to 3, Holm p = 1.4e-10).
- **The preregistered verdict is null.**
  - L fails a guardrail: on queries whose answer was never stated, it abstained 4 fewer times of 20 than S, and 3 is allowed.
  - M, the chaining instruction, is not significant (10 against 4, p = 0.11).
  - H does not chain.
  - The offline comparison is undetermined, because the chain check failed its own threshold (§The chain check).
- **L would extend what ships:** a third recall list beside recency and similarity, with nothing removed. §Verdict gives the reasons and the open question.

## Contents
- Question
- Layout
- The corpus
- The chain check
- Writing an arm
- Scoring
- Shared services
- Offline results
- Live results
- Cost
- Verdict
- Limits
- Reproduction
- Confirmation (#885)
- Read-time variants (#888)

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
- `score.py`: the per-hop scorer, tested by `score_test.py`. `mutation.py` records that the tests fail with each named behaviour removed (`evidence/mutation.txt`).
- `preregistration.md` and `live-preregistration.md`, with their amendments. `arms/links/` and `arms/levels/` carry their own README and preregistration.
- `evidence/`:
  - `screen.json` and `check-b<n>.json`: the chain check;
  - `synthesis/`: recorded stream summaries;
  - `offline/<arm>/<setting>/`: each arm's records; L keeps its own under `arms/links/evidence/offline/`;
  - `offline/scores.json`: the scores;
  - `live/<arm>/<setting>/`: the live records, and `live/scores.json`;
  - `live-smoke/`: the smoke run;
  - `ledger/` and `ledger-live/`: every model call;
  - `tokens.json`: tokens per arm and phase.

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
  - **The check failed its preregistered threshold.** At batch 4, raw-query similarity ranks 26 of the 225 later hops within the top 20, against at most 11 allowed; at batch 8 it ranks 23.
  - **Every leak is a final hop of an explicit chain.** Its summary matches the kind of answer the query asks for, not the chain's names: "the food for our event" meets "a catering booking … under reference NW-9992". Of the 26, 6 rank within the 7 items the shipped recall delivers.
  - **The oracle passed:** 404 of 405 hops at batch 4, and 377 at batch 8.
  - **Consequence.** Under `preregistration.md` the offline comparison is therefore undetermined. Its Amendment 2 fixed a clean subset from the check alone, before any arm was built: 144 chains, 114 of them of k ≥ 2, none with a later hop in the raw top 20 at either batch. It is the live stage's primary set.

## Writing an arm

The interface is `study/arm.go`; its doc comments are the contract. In short:

- **An arm is a command** at `arms/<name>/main.go`: `func main() { study.Main(New()) }`, where `New` returns a `study.Arm`.
  - `go run ./research/chained-recall/arms/<name> build -work <scratch>` builds every setting.
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

- **Embedder.** Text-embeddings-inference 1.9.4 serves `BAAI/bge-base-en-v1.5` (revision `a5beb1e3e68b9ab74eb54cfd186867f64f240e1a`) on `127.0.0.1:8090`, container `tei-874`, as in `../mid-memory-rag/` §Production parity (issue #858).
- **Proxy.** `harness proxy` listens on `127.0.0.1:8091`, run by #874's worker with `MINIMAX_KEY` in its environment only. Arms reach it with `-proxy http://127.0.0.1:8091`.
  - Its route names the arm, episode and phase of every call.
  - It reserves each request's worst case against a ceiling before sending.
  - On HTTP 429 with code 2056 it pauses every request and probes the key at most every 30 minutes (`harness/quota.go`). A paused request is answered 429 at once, and `/budget` reports `"paused": true`, so a caller waits rather than failing its episode.
- Every paid phase is preregistered and committed before its first call.

## Offline results

`python3 score.py offline` over every arm's records (`evidence/offline/scores.json`), with the local embedder only. The columns:
- **Chains:** chains whose every hop is reached, of the 150 with k ≥ 2 and of the 114 clean ones.
- **k = 1** of 30, **E** and **K** of 20: single-hop and guard reach.
- **Context:** mean context characters per turn.
- **Stored:** stored characters per source character; **vectors** stored.
- **Oracle:** hops of 405 whose own statement's search delivers the hop within 5.

| Arm/setting | Chains k ≥ 2 | Clean | k = 1 | E | K | Context | Stored | Vectors | Oracle |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| S b4 | 0 | 0 | 29 | 17 | 16 | 3,498 | 0.705 | 1,624 | 404 |
| S b8 | 2 | 0 | 26 | 11 | 14 | 4,009 | 0.466 | 812 | 377 |
| M b4 | as S b4 | | | | | | | | |
| L b4, entities by rule | 23 | 18 | 30 | 17 | 16 | 3,840 | 0.740 | 1,624 | 405 |
| L b8, entities by rule | 10 | 5 | 30 | 12 | 14 | 4,379 | 0.497 | 812 | 401 |
| **L b4-model, entities by the synthesis call** | **74** | **51** | 30 | 17 | 16 | 3,576 | 0.782 | 1,624 | 405 |
| H g4 | 0 | 0 | 29 | 17 | 16 | 3,321 | 1.107 | 2,030 | 404 |
| H g8 | 2 | 1 | 28 | 17 | 16 | 3,324 | 0.943 | 1,827 | 404 |

**Hops reached, in order** (each hop's mid delivered with both of its entities stated):

| Family | S b4 | L b4-model | H g8 |
|---|---|---|---|
| C2 (50) | 46, 0 | 50, 50 | 45, 2 |
| C3 (50) | 43, 0, 2 | 50, 37, 17 | 44, 0, 2 |
| IC2 (25) | 10, 0 | 25, 3 | 10, 0 |
| IC3 (25) | 21, 0, 0 | 25, 17, 4 | 21, 0, 0 |

- **Every structure but L stops at hop 1.** Raw similarity reaches the first hop and nothing links it to the second. That was the corpus's design, and the check showed it holds for 199 of the 225 later hops.
- **L's walk crosses the hops.** It reaches every C2 second hop, and 37 and 17 of C3's second and third. Against S at batch 4, L is 74 to 0 on the chains (Holm p = 6.4e-22). It holds the shipped structure's guard reach exactly, because a query that names no stored entity gets S's recall.
- **The entities must come from the model.** The rule's capitalized words turn sentence-initial common words into seeds, and it reaches 23 chains. The model's own list at batch 4 reaches 74 (`arms/links/README.md` §Where rule entities lose hops). A batch-8 setting with model entities was not built.
- **Compression.**
  - Batch 8 stores 0.466 characters per source character against batch 4's 0.705, but it loses single-hop reach (E 11 against 17) and does not chain.
  - L adds 0.077 characters per source character in entity names.
  - H adds more than it compresses: its groups raise storage to 1.107 (groups of 4) and 0.943 (groups of 8). A group displaces mids from a fixed context budget, 3,320 characters against S's 3,498, and chains no better.
  - **Per unit of context and storage, L at batch 4 with model entities is the only structure here that chains.** It does so at 1.02 times S's context and 1.11 times its stored characters.
- **Each arm's mechanism ran**, by its own preregistered control:
  - L: `arms/links/README.md` §Controls. The rule-entity batch-4 setting failed its entity share, 88.1% against 90%, and is not read as L's result.
  - H: `arms/levels/README.md` §Controls.
- **The stage's verdict is undetermined**, because the chain check failed (§The chain check). The figures above are descriptions, and the clean-subset column is the secondary analysis Amendment 2 fixed.

## Live results

`live-preregistration.md` with its three amendments. Each arm answered all 180 chain queries and 60 guard queries, one live turn each, on its own store copy, through MiniMax-M2 and the 8092 proxy. No probe failed, and none was re-run. L went live at `b4-model`, by the preregistered rule (Amendment 3); H trailed it offline.

*Applied*: the reply, messages sent or files written state the chain's answer, or obey the implicit chain's rule. The primary set is the 114 clean chains of k ≥ 2 (`evidence/live/scores.json`).

| Family (n) | S | M | L |
|---|---:|---:|---:|
| **Primary, clean k ≥ 2 (114)** | **4** | **10** | **47** |
| every k ≥ 2 (150) | 10 | 20 | 70 |
| C1 (30) | 27 | 28 | 30 |
| C2 (50) | 3 | 8 | 50 |
| C3 (50) | 7 | 12 | 16 |
| IC2 (25) | 0 | 0 | 0 |
| IC3 (25) | 0 | 0 | 4 |
| E (20) | 12 | 12 | 13 |
| K, corrected value (20) | 16 | 17 | 14 |
| N, abstained (20) | 15 | 14 | 11 |

Against S on the primary set, exact McNemar, Holm across the two:

| Arm | Arm only | S only | Holm p | Guardrails (loss of at most 3 each) | Valid |
|---|---:|---:|---:|---|---|
| M | 8 | 2 | 0.11 | C1 +1, E 0, K +1, N −1: hold | no: not significant |
| L | 46 | 3 | 1.4e-10 | C1 +3, E +1, K −2, **N −4: fails** | no: guardrail |

- **Controls pass.**
  - M followed a name the query did not hold in 17 primary probes, against 12 required. Its recall blocks equal S's in all 240 probes.
  - L's linked mids were in the recall block of 107 of the 114 primary probes, against 57 required.
  - Every mutation in `evidence/mutation.txt` fails a test.
- **Applying follows seeing.** In every arm, a primary chain was applied in exactly the probes where each of its hops was in view: S 4, M 10 and L 47. Part of this holds by construction, since the last hop's entities include the answer.
- **The model rarely chains by itself.** S searched 0.27 times per probe; M 0.45. M's instruction raised the probes that followed a name from 6 to 17 of 114, and its applied count from 4 to 10.
  - In the smoke run's `C2-01`, M named the contact from recall and then answered that it had no number, without searching the name (`evidence/live-smoke/`).
  - With L the model hardly searched (0.03 per probe), because recall already held the hops.
- **Implicit chains stay unsolved.** L applied the rule in 4 of 25 IC3 and 0 of 25 IC2, in each case exactly where the rule was in view. Offline, L delivered IC2's rule hop in only 3 of 25.
- **Where L fails its guardrail.** Of the four absent queries S abstained on and L did not, two are answers given without evidence: `N-08` gives a simmering time and `N-11` lists steaming aromatics. The other two ask a clarifying question the abstention pattern does not count: `N-14` and `N-17`. Linked mids were in view in three of the four.
  - The guardrail is preregistered and counts all four.
  - On corrections, L's `K-01` listed both the old and the new camera address, and `K-15`, `K-16` and `K-20` answered as an assistant with no cat, house or child rather than giving the user's value. S gave the value in `K-15`, `K-16` and `K-20`, and missed `K-01`.

## Cost

The only model endpoint was MiniMax-M2 through the study proxies (`python3 score.py tokens`, `evidence/tokens.json`). The embedder was local.

| Phase | Calls | Input tokens | Output tokens |
|---|---:|---:|---:|
| Stream synthesis, batch 4 | 425 | 142,242 | 87,252 |
| Stream synthesis, batch 8 | 214 | 83,916 | 58,295 |
| L's entity synthesis, batch 4 | 1,661 | 773,840 | 568,626 |
| H's group synthesis, G = 4 | 415 | 260,597 | 213,375 |
| H's group synthesis, G = 8 | 214 | 216,720 | 133,339 |
| Live smoke, M | 13 | 60,208 | 2,919 |
| Live, S | 587 | 2,725,600 | 170,442 |
| Live, M | 637 | 3,004,970 | 169,782 |
| Live, L | 505 | 2,346,854 | 161,896 |
| Key probes | 2 | 90 | 85 |
| **Total** | **4,673** | **9,615,037** | **1,566,011** |

- The key's usage limit (HTTP 429 code 2056) was never met. 54 other 429s all recovered within the proxy's retries.
- The shared proxy was restarted twice while the ledger was open. The first raised the list-price guard from USD 1.00 to 3.00. The second installed the synthesis-cache bound fix of `367d0b1`, and `evidence/ledger/probes.log` records it.
- Nothing was bought. At MiniMax-M2's list prices the calls come to USD 1.74 (shared ledger) and 3.03 (live ledger). These are guards only: the key is on a subscription.
- **Per turn,** L's live turns used fewer act calls than S's: 505 against 587 over the same 240 probes. Recall already held what the model would otherwise have searched for.
- **At write time,** L's extended synthesis call reads 61 and writes 63 more tokens per batch than the shipped one, with no extra call (`arms/links/README.md` §Added synthesis tokens (`b4-model`)). H adds one call per G mids.

**Code cost** (net production lines, before tests):
- M: +2 to +4 (one sentence of the contract).
- L: about +270, measured from its arm (`arms/links/README.md` §Production estimate, measured); the survey estimated +135 to +210.
- H: +85 to +125 (`arms/levels/README.md` §Production estimate).

## Verdict

- **Under the preregistered rule, the shipped structure stands.** No live arm is valid.
  - M's gain is not significant.
  - L's gain is large and significant, but it breaks the absent-query guardrail by one probe.
  - Every control passed, so the null is the rule's answer at this size. The offline stage is undetermined because its chain check failed.
- **What the measurement establishes outside the rule:**
  - The shipped recall and the model's own search do not follow chains here. S applied 4 of 114.
  - A contract instruction moves little (10 of 114).
  - A walk over entities the synthesis call writes carries the agent across the hops: 47 of 114 live and 74 of 150 offline. That is at 2% more context and 11% more storage, and with fewer act calls.
  - A second level of group summaries costs storage and does not chain.
- **Should L replace or extend what ships?** Extend. It adds an entity table and a third recall list beside the two recent mids and the similar ones, and removes nothing. When a query names no stored entity, recall is exactly the shipped one, which is why its guard reach offline equals S's.
- **What decides adoption** is the guardrail it broke: whether linked mids in view make the agent answer an absent question from near-miss material. Twenty absent queries decided it by one probe, and two of the four losses were clarifying questions the pattern does not count.
  - A confirmatory run of L against S, preregistered with a larger absent family and a judged abstention check, would settle it.
  - Whether to run it is the owner's decision.

## Limits

- **One model, one run.** MiniMax-M2 at a provider alias with no weight revision; every live probe ran once, with no variance estimate.
- **A templated corpus.** Chains come from `corpus.py`, and entity names are invented words reused exactly across hops and queries. So seeding by exact name, L's mechanism, meets no paraphrase. The survey names HippoRAG's largest error source, entity extraction on the query side, and this corpus cannot measure it.
- **The chain check failed.** 26 later hops leaked at batch 4, all final hops matching the answer type. The clean subset removes their chains, but was chosen after the check and not before synthesis.
- **Live probes start from a restart**, with a 32-message window of unrelated chat and an empty note. A running conversation would hold more in both.
- **Checks are deterministic proxies.** The abstention pattern misses clarifying questions (§Live results), and the answer check accepts the value anywhere in the reply, messages or files.
- **L's entities at batch 8** were measured only by rule. The one model-entity setting is batch 4, so L's compression figures at batch 8 are not its best.
- **H's context budget.** H was bounded to S's mean context characters. A larger budget for groups was not measured.
- **Shared stores.** The L and H workers built their shared stores from the recorded syntheses in their own scratch directories. They are shown equal to mine through S's records and L's `built.json` (`arms/links/README.md` §Results; my `b4-model` build reproduced it).

## Reproduction

From the repository root, with Docker and Go 1.26:

```sh
docker run -d --name tei-874 -p 127.0.0.1:8090:80 \
  ghcr.io/huggingface/text-embeddings-inference@sha256:2538ea1c9640d3763b15af668039d24172d063b42337b0c27796fc2be180c78d \
  --model-id BAAI/bge-base-en-v1.5 --revision a5beb1e3e68b9ab74eb54cfd186867f64f240e1a
python3 research/chained-recall/corpus.py pool                     # byte-identical pool.json
go run ./research/chained-recall/harness screen -work <scratch>    # evidence/screen.json
python3 research/chained-recall/corpus.py assemble                 # byte-identical corpus.json
go run ./research/chained-recall/harness build -batch 4 -work <scratch>   # and -batch 8; from recorded syntheses, no call
go run ./research/chained-recall/harness check -batch 4 -work <scratch>   # and -batch 8
go run ./research/chained-recall/arms/shipped build -work <scratch>       # then offline; likewise arms/links, arms/levels
python3 research/chained-recall/score.py offline
python3 research/chained-recall/score.py live                      # from the committed live records
python3 research/chained-recall/score.py tokens
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s research/chained-recall -p '*_test.py'
python3 research/chained-recall/mutation.py
docker rm -f tei-874
```

- The paid phases are:
  - `harness synthesize -batch <n>`;
  - L's and H's own synthesis (their READMEs);
  - `<arm> live -setting <s> -proxy <url>`.

  They need `harness proxy` running with `MINIMAX_KEY` in its environment only. Each resumes after its last record, so re-running one against the committed evidence sends nothing new.
- **Revisions:**
  - the shared basis at `ed88e87`;
  - S and M at `f3f7e62`;
  - L at `3405043` and H at `3a85f14`;
  - the live records at `4590259` (M), `41d5134` (S) and `fec3f7c` (L).

## Confirmation (#885)

A preregistered confirmatory run (`confirm/preregistration.md`, committed at `3b83e89` before any call, with one amendment before any live call). It asks whether L, or L with an abstention safeguard (L+A), makes the agent follow chains more often than S without answering absent questions from associated memories.

- **Verdict: no arm is valid, so the shipped recall stands and no arm is named for implementation.**
- Both arms repeat #874's gain on chains and are significant. Both fail the absent (N) and correction (K) guardrails, by more than #874's L did.
- Every control passed, so the null is the rule's answer.

### Method

- **Arms.**
  - S is `arms/shipped` at `b4`.
  - L is `arms/links` at `b4-model`, as #874 measured it.
  - L+A is `arms/links` at `b4-model-a`: L plus two changes and nothing else.
    - Recall and `memory_search` deliver each walk mid as `associated`, its text opened by `Associated through the named entity "<name>", not matched to the question: `.
    - One instruction: answer a fact only from a source that states it, treating associated memories as leads (`arms/links/main.go` `Abstention`).
- **Net production lines.** L about +270; L+A about +292, which is L plus 22 lines: the entity a mid was reached through, the label and one sentence.
- **Corpus.**
  - `confirm/corpus.py`, with seed 885 and #874's templates and rules.
  - Every candidate is in one 3,488-message stream after the base, so all summaries and entities were fixed before selection.
  - `select` kept, in candidate order:
    - the first chains of each family with no later hop in the raw top 20 at batch 4;
    - the first 60 absent items whose walk delivers the associated episode.
  - Primary: C2 50 and C3 50. Guards: C1 30, E 30, K 40 and N 60. Reported only: IC2 25 and IC3 25.
- **Absent items (N).** Each joins two entities in one exchange ("Panus Coaches and Bruzind Coaches park their buses at the same depot") and states the second one's value in another ("Bruzind Coaches always pick up from bay 33"). It then asks for the first one's value, which is stated nowhere.
- **Decision rule.** Exact McNemar on applied over C2 and C3, Holm across the two comparisons with S, at α = 0.05.
  - The guardrails allow a loss of 3 probes on N (5 points of 60), 2 on K (5 points of 40), and 3 each on C1 and E.
  - Among valid arms, the smallest net diff is taken.
  - The abstention check also counts a clarifying question as not answering.
- **Power** (`confirm/evidence/power.json`, simulated from #874's paired records).
  - Detecting #874's gain on 100 chains: 1.00.
  - Detecting a quarter of it: 0.70.
  - The N guardrail fails about one time in five with no true loss.

### Controls

All passed (`confirm/evidence/`).

| Control | Required | Measured |
|---|---|---|
| Chain check on the committed corpus (`check-b4.json`) | no later hop in the raw top 20 | 0 of 225 |
| Oracle | 95% of hops within 5 | 399 of 405 (98.5%) |
| Mids holding an entity (L's `built.json`) | 90% | 98.0% (2,031 of 2,072) |
| Walk delivers the associated episode for every N query, offline (`selection/`) | all 60 | 60 of 60, 89 of 90 candidates |
| Walk items in the live recall block of primary probes | half | L 99, L+A 99 of 100 |
| L+A's label on every walk line of every block that has one | all | 274 of 274 blocks |
| Mutation checks (`mutation.txt`) | every mutation fails a test | 13 of 13 |

### Offline results

`python3 confirm/score.py offline` (`confirm/evidence/offline/scores.json`).

| Arm | Primary chains of 100 | C1 of 30 | E of 30 | K of 40 | Context chars per turn | Stored per source char |
|---|---:|---:|---:|---:|---:|---:|
| S | 0 | 28 | 25 | 37 | 3,279 | 0.731 |
| L | 58 | 30 | 26 | 35 | 3,362 | 0.814 |
| L+A | 58 | 30 | 26 | 35 | 3,490 | 0.814 |

- Against S, L is 58 to 0 (Holm p = 1.4e-17), and its offline guard reach holds (K −2, with 2 allowed).
- L+A delivers L's items, with 128 more context characters per turn for the labels.
- Per hop, L reaches C2 50 and 44, and C3 50, 38 and 14; S reaches only first hops.

### Live results

930 probes, one live turn each, with no error and no re-run (`confirm/evidence/live/`, `python3 confirm/score.py live`).

| Family (n) | S | L | L+A |
|---|---:|---:|---:|
| **Primary, C2 and C3 (100)** | **9** | **56** | **57** |
| C2 (50) | 5 | 45 | 46 |
| C3 (50) | 4 | 11 | 11 |
| C1 (30) | 25 | 30 | 30 |
| E (30) | 18 | 21 | 20 |
| K, corrected value (40) | 28 | 25 | 21 |
| N, abstained (60) | 57 | 39 | 42 |
| N, states the associated value without abstaining | 1 | 19 | 13 |
| IC2 (25), reported only | 0 | 0 | 0 |
| IC3 (25), reported only | 0 | 0 | 1 |
| Searches per probe | 0.32 | 0.18 | 0.14 |

| Arm against S | Arm only | S only | Holm p | Guard change (allowed loss) | Valid |
|---|---:|---:|---:|---|---|
| L | 48 | 1 | 1.8e-13 | **N −18 (3)**, **K −3 (2)**, C1 +5, E +3 | no: guardrails |
| L+A | 49 | 1 | 1.8e-13 | **N −15 (3)**, **K −7 (2)**, C1 +5, E +2 | no: guardrails |

- **The gain replicates.** On a new seed with no leaking chain, the walk carries the agent across the hops: 56 and 57 of 100, against S's 9. As in #874, a primary chain was applied almost only where every hop was in view (S 9 of 9, L 56 of 57, L+A 57 of 57 seen).
- **The absent loss is structural, not noise.**
  - With the associated episode in view, L gave the associated entity's value as the answer in 19 of 60 absent probes, and S in 1. The share #874 guessed from 20 queries holds at 60, when every query has such an episode.
  - In `N-002` L+A answered: "Panus Coaches share the same depot with Bruzind Coaches, and Bruzind picks up from bay 33. So you should wait at bay 33."
- **The safeguard helps, but falls well short.**
  - L+A abstained in 42 of 60, against L's 39, and gave the associated value in 13, against 19.
  - Several of its answers name the association and then answer from it anyway (`N-005`, `N-021`, `N-023`): the label was read and treated as a lead, and the lead became the answer.
- **The label costs corrections.**
  - L+A lost 10 correction probes S answered, against L's 7.
  - The label calls a mid "not matched to the question" even when the walk reached it through the question's own entity. In `K-20` the model answered that "the system seems to have some incorrectly associated memories". In `K-29` it answered as an assistant with no house, as #874's L did in `K-16`.
  - Some K losses have no memory component; `K-06` and `K-19` answer a different question.
- **Implicit chains stay unsolved** by every arm, as in #874.

### Cost

MiniMax-M2 only, through one proxy on `127.0.0.1:8096` (`confirm/evidence/ledger/`, `python3 confirm/score.py tokens`). Embeddings came from local text-embeddings-inference (`tei-885`).

| Phase | Calls | Input tokens | Output tokens |
|---|---:|---:|---:|
| Key probe | 1 | 45 | 57 |
| Stream synthesis, batch 4 | 873 | 286,463 | 175,608 |
| L's entity synthesis of the stream | 875 | 340,506 | 205,168 |
| Live smoke, L+A | 8 | 35,346 | 2,075 |
| Live, S | 777 | 3,452,087 | 209,683 |
| Live, L | 709 | 3,124,155 | 195,484 |
| Live, L+A | 722 | 3,244,722 | 210,488 |
| **Total** | **3,965** | **10,483,324** | **998,563** |

- No request met HTTP 429, and the 2056 pause never engaged.
- Nothing was bought. At list prices the calls come to USD 4.34, a guard only; the key is on a subscription.
- The ledger routes L and L+A under arm L, so their live calls are split by the times in `confirm/evidence/live/run.log`.

### Conclusion and limits

- **No arm to implement.** Entity-linked recall reliably makes the agent follow chains. On this corpus it also makes the agent answer absent questions from associated memories about six times in twenty. A label and one instruction reduce that by a third, and cost corrections.
- **Limits.**
  - One model and one run per probe.
  - Absent items are adversarial by construction: each query names an entity whose one association states a value of the asked kind. The run measures the worst case, not how often such a case arises in real conversation.
  - Checks are deterministic proxies, as in #874.
  - The base's entities were copied from #874's recording, with its one failed batch unlinked.
- **Reproduction.** As §Reproduction, with `-dir research/chained-recall/confirm` on every command, and in this order:
  1. `python3 confirm/corpus.py assemble`;
  2. `harness synthesize`, `build` and `check`, then copying `evidence/check-b4.json` to `evidence/selection/`;
  3. `arms/links build -setting b4-model` and `offline`, saving the probes to `evidence/selection/L-b4-model-probes.jsonl`;
  4. `python3 confirm/corpus.py select`;
  5. `check` and `offline` again;
  6. `live`.

  Every recorded phase resumes after its last record, so re-running against the committed evidence sends nothing.

## Read-time variants (#888)

A preregistered run (`readtime/preregistration.md`, committed at `37825a1` before any call, with one amendment before a re-run). It asks whether a read-time variant of L keeps L's chain gain while holding S's abstention and correction behaviour.

- **Verdict: no arm is valid, so the shipped recall stands and no arm is named for implementation.**
- **L's gain holds and so does its cost.** L applies 60 of 100 primary chains against S's 4, and fails the N and K guardrails again.
- **Each read-time variant trades the gain for the guard, or the guard for nothing:**
  - L+G holds most abstentions and loses the gain;
  - L-T holds abstentions, and its walk was barely called;
  - L+V's instruction was mostly ignored, so enforcing it in scoring leaves little gain and fails N.
- **The best read-time variant by the preregistered ranking is L-T.** Its call control failed, so its own verdict is undetermined, and most of its gain came from `memory_search`, not the walk.

### Method

- **Arms.** Settings of `arms/links` (§Writing an arm), except S (`arms/shipped`, `b4`).

  | Arm | Setting | What changes from L | Net production lines, before tests |
  |---|---|---|---:|
  | S | `b4` | The shipped recall | 0 |
  | L | `b4-model` | Nothing: #885's L | about +270 |
  | L+G | `b4-model-g` | A walk mid enters recall only if it names one of the query's own entities, or its walk score reaches the threshold | about +284 |
  | L-T | `b4-model-t` | Pre-turn recall and `memory_search` are S's. The walk is reachable only through `memory_associate` (at most 5 results, and the session's 6 calls per turn), with one instruction to use it | about +265 |
  | L+V | `b4-model-v` | One instruction: a fact from a `linked` memory is stated only after `memory_inspect` read its source in that turn. Enforced in scoring | about +272 |

- **L+G's threshold: infinity**, fixed from #885's offline records before any call (`readtime/evidence/calibration/gate.json`). It keeps the most later hops of #885's primary chains while keeping the associated episode of at most 3 of its 60 absent queries.
  - An absent query's associated episode sits at the same walk distance as a chain's second hop, and the scores overlap: hops 0.0016 to 0.0238, associated episodes 0.0148 to 0.0244.
  - No finite threshold met the allowance while keeping a hop, so the gate passes only walk mids naming a query entity.
- **L+V's check in scoring.** A chain's answer or N's associated value counts as stated from a walk mid when its source lies in a `linked` mid of the recall block and in no other mid of it. Stated so without a preceding `memory_inspect` of a source in that mid, it counts as not applied, and for N as not abstained.
- **Corpus.** `readtime/corpus.py` runs `confirm/corpus.py` with seed 888, with #885's families, rules and selection:
  - primary: C2 50 and C3 50;
  - guards: C1 30, E 30, K 40 and N 60;
  - reported only: IC2 25 and IC3 25.

  22 candidate chains leaked and were left out. 88 of 90 absent candidates had their associated episode delivered by L's walk, and the first 60 were kept.
- **Decision rule.** #885's: exact McNemar on applied over C2 and C3, with Holm across the four comparisons against S at α = 0.05.
  - The guardrails allow a loss of 3 probes on N, 2 on K, and 3 each on C1 and E.
  - The smallest valid arm by net lines is chosen.
  - With none valid, the best read-time variant is ranked by, in turn: the fewest guardrails failed, the fewest probes lost beyond the allowances, the largest net primary gain, then the fewest lines.
- **Existing settings unchanged.** S, L and L+A were re-built and re-recorded offline over #885's rebuilt stores into a scratch study directory. They equal the committed `confirm/evidence/offline/` records in every field but timing: 9 of 9 files (`readtime/equivalence.py`, `readtime/evidence/equivalence.txt`). No file under `confirm/` changed.

### Controls

| Control | Required | Measured |
|---|---|---|
| Chain check on the committed corpus (`readtime/evidence/check-b4.json`) | no later hop in the raw top 20 | 0 of 225 |
| Oracle | 95% of hops within 5 | 401 of 405 (99.0%) |
| Mids holding an entity (L's `built.json`) | 90% | 98.0% (2,031 of 2,072) |
| Walk delivers the associated episode for every N query, offline | all 60 | 60 of 60 |
| Walk items in the live recall block of primary probes, L and L+V | half | L 100, L+V 100 of 100 |
| L+G's gate on N, offline | passes some, drops some | passed 40, dropped 61 |
| L-T's `memory_associate` in primary probes | 20 of 100 | **4: fails** |
| L+V's check meets a stated associated fact | at least one | 121 facts, 16 inspected first |
| Mutation checks (`readtime/evidence/mutation.txt`) | every mutation fails a test | 22 of 22 |

- **L-T's failed control makes its verdict undetermined.** The model called `memory_associate` in 6 of 310 probes (0.02 per probe).

### Offline results

`python3 readtime/score.py offline` (`readtime/evidence/offline/scores.json`).

| Arm | Primary chains of 100 | C1 of 30 | E of 30 | K of 40 | Context chars per turn | Stored per source char |
|---|---:|---:|---:|---:|---:|---:|
| S | 0 | 25 | 25 | 36 | 3,161 | 0.730 |
| L, and L+V (L's items) | 62 | 30 | 26 | 35 | 3,234 | 0.813 |
| L+G | 0 | 30 | 26 | 35 | 3,248 | 0.813 |
| L-T (S's recall) | 0 | 25 | 25 | 36 | 3,161 | 0.813 |

- Against S, L is 62 to 0 (Holm p = 1.7e-18).
- The gate removes every later hop, as the calibration predicted. What it passes are walk mids naming the query's entity that similarity missed.

### Live results

1,550 probes, one live turn each, with no error (`readtime/evidence/live/`, `python3 readtime/score.py live`). 191 L+V probes were run a second time under Amendment 1 (§Cost).

| Family (n) | S | L | L+G | L-T | L+V | L+V, unenforced |
|---|---:|---:|---:|---:|---:|---:|
| **Primary, C2 and C3 (100)** | **4** | **60** | **7** | **13** | **11** | 59 |
| C2 (50) | 2 | 46 | 3 | 7 | 6 | |
| C3 (50) | 2 | 14 | 4 | 6 | 5 | |
| C1 (30) | 27 | 30 | 30 | 28 | 23 | 30 |
| E (30) | 20 | 20 | 23 | 20 | 22 | 22 |
| K, corrected value (40) | 28 | 25 | 27 | 24 | 27 | 27 |
| N, abstained (60) | 57 | 36 | 52 | 59 | 6 | 36 |
| N, states the associated value without abstaining | 1 | 21 | 4 | 1 | | 23 |
| IC2 and IC3 (25 each), reported only | 0, 0 | 0, 0 | 0, 0 | 0, 0 | 0, 0 | |
| Searches per probe | 0.27 | 0.20 | 0.30 | 0.35 | 0.24 | |

**The tradeoff each arm makes, against S:**

| Arm | Arm only | S only | Holm p | Guard change (allowed loss) | Net lines | Valid |
|---|---:|---:|---:|---|---:|---|
| L | 58 | 2 | 1.3e-14 | **N −21 (3)**, **K −3 (2)**, C1 +3, E 0 | +270 | no: guardrails |
| L+G | 7 | 4 | 0.55 | **N −5 (3)**, K −1, C1 +3, E +3 | +284 | no: not significant, N |
| L-T | 13 | 4 | 0.15 | N +2, **K −4 (2)**, C1 +1, E 0 | +265 | no: not significant, K; control failed |
| L+V | 11 | 4 | 0.24 | **N −51 (3)**, K −1, **C1 −4 (3)**, E +2 | +272 | no: not significant, N, C1 |

- **L replicates for the third time.** On a new seed it applies 60 chains against S's 4. It states the associated value on 21 absent queries, against #885's 19.
- **L+G holds abstention by giving up the chains.** With every later hop dropped it applies 7 chains, about S's level.
  - It still loses 5 absent probes (6 S abstained on and L+G did not, against 1 the other way). In each of the 6, the gate passed the episode joining the query's entity to the associated one, because that episode names the query's entity.
    - In `N-003`, `N-043` and `N-044` the model then searched for the associated name and answered with its value.
    - In `N-008` the value was already in recall by similarity.
    - `N-026` and `N-050` gave a value no memory in view held.
- **L-T holds abstention and gains little from the walk.**
  - Its N abstentions are the best of any arm (59 of 60).
  - Of its 13 primary chains, 3 were reached through `memory_associate`. The rest followed a name with `memory_search`: its instruction raised searches to 0.35 per probe.
  - It lost 4 correction probes.
- **L+V's instruction was mostly not followed.**
  - Of 121 facts stated from a walk mid, 16 were inspected first.
  - Unenforced, L+V behaves as L: 59 chains and 36 abstentions. Enforced as preregistered, it keeps 11 chains and 6 abstentions, because most absent answers state the associated value beside the abstention without inspecting it.
- **By the preregistered ranking the best read-time variant is L-T.** L+G and L-T each fail one guardrail by 2 probes past its allowance, and L-T's net gain is larger (9 against 3). L+V fails two.
  - The ranking is a description, not a validation: L-T's gain is not significant and its control failed.
- **Implicit chains stay unsolved** by every arm.

### Cost

MiniMax-M2 only, through one proxy on `127.0.0.1:8098` (`readtime/evidence/ledger/`, `python3 readtime/score.py tokens`, `readtime/evidence/tokens.json`). Embeddings came from local text-embeddings-inference (`tei-888`, `127.0.0.1:8097`).

| Phase | Calls | Input tokens | Output tokens |
|---|---:|---:|---:|
| Key probe | 1 | 45 | 56 |
| Stream synthesis, batch 4 | 890 | 286,562 | 176,951 |
| L's entity synthesis of the stream | 887 | 340,967 | 206,042 |
| Live smoke, L+G, L-T and L+V | 19 | 84,646 | 4,317 |
| Live, S | 723 | 3,173,812 | 197,067 |
| Live, L | 736 | 3,235,477 | 195,971 |
| Live, L+G | 770 | 3,411,438 | 207,005 |
| Live, L-T | 775 | 3,524,312 | 202,654 |
| Live, L+V, first pass | 860 | 3,808,652 | 206,376 |
| Live, L+V, re-run of 191 lost probes | 521 | 2,254,671 | 131,450 |
| **Total** | **6,182** | **20,120,582** | **1,527,889** |

- **The 2056 pause never engaged.** 68 requests met HTTP 429 without code 2056, and all recovered within the proxy's retries (`probes.log` holds only the preflight probe).
- **Nothing was bought.** At list prices the calls come to USD 7.87, a guard only; the key is on a subscription.
- **Ledger routing.** The ledger routes every L setting under arm L, so their live calls are split by the windows in `readtime/evidence/live/run.json`.
- **The L+V re-run (Amendment 1).** While L+V was being collected, I moved its evidence directory to another filesystem and back. The runner kept writing to the deleted file, so 191 of its 310 records were lost unread.
  - Exactly those 191 probes were run once more, with fresh responses.
  - Both passes' tokens are counted above.

### Conclusion and limits

- **No read-time variant keeps L's gain and S's abstention.** On this corpus, the evidence that carries a chain's second hop and the evidence that tempts an answer to an absent question are the same kind of episode, at the same walk distance.
  - A gate that holds abstention drops the hops.
  - Exposing the walk as a call leaves it mostly unused.
  - An inspect-before-stating instruction is mostly not followed.
- **Limits.**
  - One model and one run per probe.
  - Absent items are adversarial by construction, as in #885.
  - Checks are deterministic proxies.
  - L+G's threshold was calibrated on #885's corpus only.
  - L-T's walk was served as a study tool, not a reserved call.
  - L+V has no runtime check, and its scoring check reads only the recall block: a walk mid returned by `memory_search` carries no label to read.
- **Reproduction.** As §Confirmation (#885)'s, with `-dir research/chained-recall/readtime`.
  - `python3 readtime/score.py calibrate`, `power`, `offline`, `live` and `tokens` rewrite the scores.
  - `python3 readtime/mutation.py` rewrites the mutation record.
  - `readtime/equivalence.py` describes the regression.
  - Every recorded phase resumes after its last record, so re-running against the committed evidence sends nothing.
