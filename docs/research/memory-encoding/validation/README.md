# Write-time encoding validation

Records issue #890's study. It measures the memory-encoding survey's write-time candidates (`../README.md` §Candidates for sherlock) against the shipped recall, offline and live, under the survey's test plan and `preregistration.md`:
- C, context at synthesis;
- P, an episode linked to the one it continues;
- F, standing facts written by the synthesis call.

This file holds the shared basis every arm is built on. The results and the verdict are added when the run ends.

## Contents
- Layout
- The corpus
- Writing an arm
- Shared services
- Reproduction

## Layout

- `corpus.py`: `assemble` writes `candidates.json` and `corpus.json` (candidate stage); `select` keeps the candidates the check passes. `corpus_test.py` checks the corpus rules.
- `screen.py`: the free pre-screen on raw exchange text, before any synthesis (`evidence/screen.json`).
- `score.py`: `check`, `offline`, `live` and `tokens`. It extends #874's matching (`../../chained-recall/score.py`) with SC, UP, KU and NC, each arm's primary family, the live-admission margin and the decision rule. `score_test.py` tests it.
- `preregistration.md`: fixed before the first model call.
- `arms/<name>/`: one command per arm.
  - This issue writes `arms/shipped/` (S) and `arms/facts/` (F).
  - `arms/context/` (C, #891) and `arms/prev/` (P, #892) are the other workers'.
- `evidence/`:
  - `screen.json`;
  - `synthesis/stream-b4.jsonl`, the shipped call's summaries of the stream;
  - `selection/check-b4.json`, the check over the candidates;
  - `check-b4.json` and `check-summary.json`, the check over the selected corpus;
  - `offline/<arm>/<setting>/`, `live/<arm>/<setting>/` and their `scores.json`;
  - `ledger/`, every model call, and `tokens.json`.

## The corpus

`python3 corpus.py assemble` writes every candidate into one stream of 4,128 messages after the replication's 4,800 (1,032 batches of four). Seed 890, #874's chain templates and #885's rules (`../../chained-recall/corpus.py`, `../../chained-recall/confirm/corpus.py`).

| Family | Candidates | Kept | What a probe asks |
|---|---:|---:|---|
| C1, C2, C3 | 30, 70, 70 | 30, 50, 50 | a value reached through k hops, as #885 |
| IC2, IC3 | 30 each | 25 each | an artifact that must obey a rule reached through k hops, as #885 |
| SC | 70 | 50 | a subject's value, stated in the exchange after the one naming the subject |
| UP | 106 | 50 | a task that needs a standing fact about the user without naming it |
| E, K | — | 30, 40 | the replication's exact and correction queries, as #885 |
| KU | 30 | 30 | who holds a role for the user now, after a change |
| N | 60 | 60 | an absent value, whose associate's value is stated, as #885 built it |
| NC | 30 | 30 | an absent value, where the exchange after the subject's introduction states someone else's |

- **Each exchange is four messages and fills one batch alone.** Two exchanges of one item sit at least 16 exchanges apart, in random order.
  - The exceptions: an SC or NC introduction is the exchange right before its second, so it lies within the eight messages C reads, and a KU change follows its first statement.
- **Where evidence lives.** SC and UP are in `chains`, each with one hop: the evidence, or the fact. `intro` holds an SC item's introduction. KU, N and NC are in `guards`, with `sources` naming their exchanges.
- **Selection.** After the shipped call has summarised the stream, `harness check` over the candidates writes `evidence/selection/check-b4.json`. `corpus.py select` keeps, in candidate order, the first candidates whose needed sources the raw query ranks beyond the top 20: every chain hop but the first, the SC evidence, the UP fact.
- **UP templates.** Each fact is a slot of its own (`corpus.py` `STANDING`), so no two contradict.
  - The first draft stated each fact alone, and the facts sat too close to their tasks: the pre-screen (`screen.py`) passed 19 of 60. Framing the facts apart from their tasks passed 37 of 75.
  - Each fact now comes as an aside after an unrelated opening sentence (`OPENINGS`), as such facts come up in conversation, and 31 slots were added. All of this was settled before any call. `evidence/screen.json` is the pre-screen of the committed candidates.

## Writing an arm

An arm is a command under `arms/<name>/` whose main is `study.Main(New())`, with `study` the chained-recall package (`../../chained-recall/study/`; its `arm.go` doc comments are the contract). Everything `../../chained-recall/README.md` §Writing an arm says holds, with these differences:

- **Every command takes `-dir docs/research/memory-encoding/validation`.** Without it, `study.Main` reads #874's corpus. Records then land under this directory's `evidence/offline/<arm>/<setting>/` and `evidence/live/<arm>/<setting>/`.
- **Ports.** `-embed-url http://127.0.0.1:8100/v1` for the local TEI, and `-proxy http://127.0.0.1:8101` for the proxy (§Shared services).
- **The shared store.** `env.SharedStore(4)` is the production store over base and stream from the recorded summaries. It is built into a worker's own `-work` directory with `harness build -batch 4 -dir <this dir> -work <scratch>`, free, once `evidence/synthesis/stream-b4.jsonl` is committed.
- **Base and stream.** An arm adds its structure to the stream only, and the base's 1,200 mids keep their recorded summaries (`preregistration.md` §Arms).
  - The corpus's `stream` is what an arm synthesises: 1,032 batches, `env.Corpus.Stream` in groups of four.
  - The messages before a batch are `env.Corpus.Messages()`.
- **Names and settings.** The arm names are `S`, `C`, `P`, `F` and `L`, and the settings are those `preregistration.md` fixes: `b4`, `b4-w8`, `b4-prev`, `b4-k5`, `b4-model`. `score.py` maps an arm's name to its primary family.
- **Paid phases.** Name them by phase in `env.Model(episode, phase)`, so `score.py tokens` counts them. Run one at a time on the shared key, with at most two requests at once.
- **The proxy caches synthesis responses.** It keys on the request, and caches every response whose `memory_summary` summary is well-formed (and, for L, whose `entities` is an array of strings). A malformed extra field is therefore cached, and every identical retry returns it. An arm whose extended call can come back malformed varies the request on retry.
- **Scoring.** `python3 score.py offline` reads every arm's records under `evidence/offline/`. An arm's controls live in its own directory.

## Shared services

Run by #890's worker for all three workers:
- **Embedder:** text-embeddings-inference 1.9.4 image, `BAAI/bge-base-en-v1.5` at revision `a5beb1e3e68b9ab74eb54cfd186867f64f240e1a`, container `tei-890` on `127.0.0.1:8100`.
- **Proxy:** `harness proxy` of `../../chained-recall/harness`, on `127.0.0.1:8101`, ledger `evidence/ledger/`, ceiling USD 30, `MINIMAX_KEY` in its environment only. On HTTP 429 code 2056 it pauses every request and probes at most every 30 minutes, and `/budget` reports `"paused": true`.

## Reproduction

From the repository root, with Docker, Go 1.26 and Python 3:

```sh
docker run -d --name tei-890 -p 127.0.0.1:8100:80 \
  ghcr.io/huggingface/text-embeddings-inference@sha256:2538ea1c9640d3763b15af668039d24172d063b42337b0c27796fc2be180c78d \
  --model-id BAAI/bge-base-en-v1.5 --revision a5beb1e3e68b9ab74eb54cfd186867f64f240e1a
python3 docs/research/memory-encoding/validation/corpus.py assemble        # byte-identical candidates.json
python3 docs/research/memory-encoding/validation/screen.py <scratch>       # evidence/screen.json; free
D=docs/research/memory-encoding/validation
go run ./docs/research/chained-recall/harness synthesize -batch 4 -dir $D -proxy http://127.0.0.1:8101 -embed-url http://127.0.0.1:8100/v1   # paid
go run ./docs/research/chained-recall/harness build -batch 4 -dir $D -work <scratch> -embed-url http://127.0.0.1:8100/v1
go run ./docs/research/chained-recall/harness check -batch 4 -dir $D -work <scratch> -embed-url http://127.0.0.1:8100/v1
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s $D -p '*_test.py'
```
