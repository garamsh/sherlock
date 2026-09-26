# Chained recall across mid memories

Records issue #874's study. For the memory structures `docs/research/associative-memory/README.md` §Test plan for #874 selected, it measures how well the agent follows a chain of associated memories, and how much each structure compresses.

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
- **At write time,** L's extended synthesis call reads 61 and writes 63 more tokens per batch than the shipped one, with no extra call (`arms/links/README.md` §Added synthesis tokens). H adds one call per G mids.

**Code cost** (net production lines, before tests):
- M: +2 to +4 (one sentence of the contract).
- L: about +270, measured from its arm (`arms/links/README.md` §Production estimate); the survey estimated +135 to +210.
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
python3 docs/research/chained-recall/corpus.py pool                     # byte-identical pool.json
go run ./docs/research/chained-recall/harness screen -work <scratch>    # evidence/screen.json
python3 docs/research/chained-recall/corpus.py assemble                 # byte-identical corpus.json
go run ./docs/research/chained-recall/harness build -batch 4 -work <scratch>   # and -batch 8; from recorded syntheses, no call
go run ./docs/research/chained-recall/harness check -batch 4 -work <scratch>   # and -batch 8
go run ./docs/research/chained-recall/arms/shipped build -work <scratch>       # then offline; likewise arms/links, arms/levels
python3 docs/research/chained-recall/score.py offline
python3 docs/research/chained-recall/score.py live                      # from the committed live records
python3 docs/research/chained-recall/score.py tokens
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s docs/research/chained-recall -p '*_test.py'
python3 docs/research/chained-recall/mutation.py
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
