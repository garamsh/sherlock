# Retrieved context at write

Records issue #902's study: whether a mid written with retrieved older context carries associations that later recall can reach. Retrieved context means source messages and mids found by search when the batch is summarised. It uses #890's corpus and families (`../validation/`) and fixes its run in `preregistration.md`.

- **No paid call was made, and no arm is named.** A free screen of the search at write shows that writing alone can carry at most 7 of the 100 primary chains. Admission needs 10 of 100 more than S. So both arms, RW-key and RW, were cut before their paid phases (`preregistration.md` §Amendments, 1).
- **The search at write reaches the wrong things.**
  - For the primary chains, the later hop's search found the earlier hop in 28 of 150 neighbouring pairs.
  - A correction's search found the superseded statement in 30 of 30 KU items.
  - An SC evidence batch never found its subject's introduction (0 of 50).
- **The shipped recall stands.** The run cannot reach the decision rule, so this is a predictable null, not a measured one: the screen bounds reach from above, and no arm was built.

## Contents
- Question
- Method
- The screen
- The ceiling argument
- The correction risk
- S and C side by side
- Cost
- Limits
- Layout
- Reproduction

## Question

The owner's proposal: every n new messages, search the raw messages and the existing mids, then summarise the batch with what was found, so that each mid keeps its context as well as its messages. Does a mid written that way carry associations that later recall can reach, on chains and implicit association, without losing abstention, corrections or compression?

- **RW-key**, primary: S's summary, and a bounded "related to" line of names and earlier mid IDs in the embedding key only.
- **RW**, secondary and the owner's form: the summary may use what the search found.
- **Against S**, the shipped recall, with #890's C (8 adjacent messages at synthesis) as a reference.

## Method

- **The search at write** (`arms/retrieved/retrieve.go`, `preregistration.md` §Retrieval at write):
  - The query is the batch as the production store sends it to the summarizer.
  - It finds the 8 most similar earlier source messages and the 4 most similar earlier mids.
  - It ranks by cosine over `BAAI/bge-base-en-v1.5` vectors from a local TEI.
  - Nothing at or after the batch is a candidate.
- **The screen** (`retrieved screen`) runs that search for all 1,032 stream batches over the shared batch-4 store, with no model call. RW-key's mids are S's, so this is exactly the retrieval RW-key would have sent. RW's search over source messages is the same; its search over mids would differ only through RW's own summaries.
- **Links** (`score.py screen`).
  - For every pair of neighbouring hops of a chain, the pair is linked when the search that wrote the later hop found the earlier one: its statement among the messages, or a mid covering it.
  - A chain is linked when every pair is.
  - The same test is applied to an SC item's introduction from its evidence, a KU item's first statement from its correction, and an NC item's introduction from its second exchange.
- **Run.** One local TEI 1.9.4 container, image digest as #890, capped at 1 GB and at client batches of 16. It was stopped after the screen.

## The screen

`evidence/screen.json`, from `arms/retrieved/evidence/screen-b4.jsonl`.

| Family | Neighbouring pairs | Linked by a message | Linked by a mid | Linked either way | Chains with every pair linked |
|---|---:|---:|---:|---:|---:|
| C2 | 50 | 2 | 5 | 6 | 6 of 50 |
| C3 | 100 | 14 | 16 | 22 | 1 of 50 |
| IC2 | 25 | 0 | 1 | 1 | 1 of 25 |
| IC3 | 50 | 0 | 0 | 0 | 0 of 25 |
| SC, introduction from evidence | 50 | 0 | 0 | 0 | — |
| KU, first statement from correction | 30 | 30 | 30 | 30 | — |
| NC, introduction from second exchange | 30 | 0 | 0 | 0 | — |

- **Chains.** A hop's search rarely finds the hop it continues. Hops sit at least 16 exchanges apart and share only one entity, and the batch text also carries the hop's other entity and its acknowledgements.
- **SC.** The evidence batch ("The best line for them is extension 4864") does not name its subject, so similarity does not lead back to the introduction. C reached SC through adjacency (#890); retrieval does not.
- **KU.** A correction names the old value ("from now on it's Sofia Cukais, not Nadia Bogea"), so its search finds the first statement every time, both the message and its mid.

## The ceiling argument

- Under carried reach (`preregistration.md` §Offline stage and admission), a chain is reached only when a delivered item states every hop.
- Writing can put an earlier hop into a later mid only when the later hop's search found it. So a chain none of whose mids could carry its missing hops is reached by an arm exactly when S would reach it.
- S reaches 0 of 100 C2 and C3 chains, carried or strict.
- So an arm's gain over S on the primary families is at most the 7 chains whose every pair is linked. Admission needs a gain of at least 10 of 100 with p < 0.05. Even 7 of 7 would give 7 against 0, exact McNemar p = 0.016, and still miss the margin.
- **The bound is RW-key's exactly.** For RW, the message half of the search is identical, while the mid half ranks RW's own summaries. Those differ from S's only after a batch has itself been written with retrieved context. The screen does not bound that compounding part (§Limits).

## The correction risk

- In all 30 KU items, the correction's search brings the superseded statement into the write.
- RW-key would write only names into a key. RW would let the summary use the old value.
- #890's C, which saw only adjacent messages, already shows the pattern. Its stored stream text restates a superseded value after the correction in 26 of 30 KU items, 33 mids in all, against S's 0. In 8 of 30 KU probes a mid written after the correction and stating the old value is delivered (S 0).
- Retrieval at write makes that exposure certain rather than incidental.

## S and C side by side

`evidence/offline/scores.json`, from `python3 score.py offline` over #890's committed S and C records at `c830c8b`. Neither was re-run. RW-key and RW have no records: they were cut.

| Family (n) | S carried | S strict | C carried | C strict |
|---|---:|---:|---:|---:|
| C2 (50) | 0 | 0 | 0 | 0 |
| C3 (50) | 0 | 0 | 0 | 0 |
| IC2 (25) | 0 | 0 | 0 | 0 |
| IC3 (25) | 0 | 0 | 0 | 0 |
| SC (50) | 0 | 0 | 44 | 39 |
| UP (50) | 0 | 0 | 4 | 1 |
| C1 (30) | 27 | 27 | 27 | 24 |
| E (30) | 26 | | 27 | |
| K (40) | 35 | | 35 | |
| KU (30) | 27 | | 20 | |
| KU, stale-copy failure delivered | 0 | | 8 | |
| NC, near-miss beside the entity (30) | 0 | | 24 | |
| N, near-miss beside the entity (60) | 0 | | 0 | |
| Oracle, hops within 5 (505) | 493 | | 420 | |

| Measure | S | C |
|---|---:|---:|
| Stream summary, mean characters | 171.5 | 268.7; 366.3 with its context line |
| KU items with a stored stale copy after the correction | 0 | 26 |
| Stored stale copies | 0 | 33 |
| Stored characters per source character | 0.745 | 0.933 |

- Carried reach counts a hop stated by any delivered item. Strict reach, #890's rule, also requires the item to cover the hop's statement.
- E, K, KU and the near-miss rows are #890's rules for both arms, so they have one column each.

## Cost

- No MiniMax call and no other paid call. The ledger is empty.
- One local TEI container. `harness build -batch 4` took 29.6 minutes and the screen 44.6 minutes of wall time on this host, which the other studies' embedders shared.
- **The estimates that were not spent** (`preregistration.md` §Paid phases):
  - RW-key: about 1.1M input and 0.2M output tokens;
  - RW: about 1.3M and 0.5M;
  - the live stage: about 1.85M and 0.11M per arm.

## Limits

- **A screen, not a run.** No mid was written with retrieved context. The ceiling bounds carried reach through the mid that holds the later hop. It does not bound what a live agent would do with RW's longer summaries.
- **RW's compounding is unmeasured.** RW's own summaries, once written with retrieved context, could make a later mid search find an earlier hop that S's summaries do not lead to. The screen measures RW-key's retrieval exactly and RW's message half exactly. The PM cut RW on the shared ceiling and the correction risk.
- **One embedder, one k.** bge-base at 8 messages and 4 mids. A larger k, another query (such as the batch's entities alone) or lexical search could link more. None was preregistered.
- **A templated corpus.** Chain hops sit at least 16 exchanges apart and share one invented name. In real conversation a continuation may repeat more of what it continues.
- **The KU comparison with C** reads #890's C, whose context was adjacent, not retrieved. It shows the pattern, not RW's size.

## Layout

- `preregistration.md`: the arms, the search at write, the prompts, controls, measures, decision rule and cost, fixed before the first call. Amendment 1 records the cut.
- `arms/retrieved/`: the arm command, whose settings are `b4-rw-key` and `b4-rw`.
  - `retrieve.go`: the search at write.
  - `synthesis.go`: RW's request and RW-key's `memory_related` call.
  - `main.go`: recording, the store, and the shipped recall over it.
  - `screen.go`: the screen.
  - `evidence/screen-b4.jsonl`: each stream batch's retrieved message IDs and mids.
- `score.py`: `screen`, `offline`, `equivalence`, `live` and `tokens`, over #890's matching. `score_test.py` tests it.
- `mutation.py`: removes each behaviour this study added, one at a time. `evidence/mutation.txt` records that a named test fails for each of the 33.
- `evidence/screen.json` and `evidence/offline/scores.json`: the scored screen, and S and C side by side.
- `corpus.json` and `evidence/synthesis`: symbolic links to #890's corpus and recorded stream syntheses, so every command takes this directory as `-dir`.

## Reproduction

From the repository root, with Docker, Go and Python 3. `<tei>` is a free local port, and `<scratch>` is outside the repository.

```sh
D=research/memory-encoding/retrieved-context
docker run -d --name tei-902 --memory 1g -p 127.0.0.1:<tei>:80 \
  ghcr.io/huggingface/text-embeddings-inference@sha256:2538ea1c9640d3763b15af668039d24172d063b42337b0c27796fc2be180c78d \
  --model-id BAAI/bge-base-en-v1.5 --revision a5beb1e3e68b9ab74eb54cfd186867f64f240e1a --max-client-batch-size 16 --max-batch-requests 16
go run ./research/chained-recall/harness build -batch 4 -dir $D -work <scratch> -embed-url http://127.0.0.1:<tei>/v1
go run ./$D/arms/retrieved screen -dir $D -work <scratch> -embed-url http://127.0.0.1:<tei>/v1
python3 $D/score.py screen
python3 $D/score.py offline
go test ./$D/...
(cd $D && PYTHONDONTWRITEBYTECODE=1 python3 -m unittest score_test)
python3 $D/mutation.py
```

TEI 1.9.4 on CPU reports `max_batch_requests` 8 in `/info`, whatever the flag asks.
