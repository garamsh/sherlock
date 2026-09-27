# Retrieved context at write

Records issue #902's study: whether a mid written with retrieved older context carries associations that later recall can reach. Retrieved context means source messages and mids found by search when the batch is summarised. It uses #890's corpus and families (`../validation/`) and fixes its run in `preregistration.md`.

- **Status: preregistered, not run.** No model call has been made and no embedding computed. Each paid phase waits for the PM's go (`preregistration.md` §Paid phases).
- **Arms:**
  - RW-key, primary: S's summary, with a bounded related line in the embedding key only;
  - RW, secondary and the owner's form: the summary may use what the search found;
  - both against S, with #890's C beside them as a reference.

## Layout

- `preregistration.md`: the arms, the retrieval at write, the prompts, controls, measures, decision rule and cost, fixed before the first call.
- `arms/retrieved/`: the arm command, whose settings are `b4-rw-key` and `b4-rw`.
  - `retrieve.go`: the search at write.
  - `synthesis.go`: RW's request and RW-key's `memory_related` call.
  - `main.go`: recording, the store, and the shipped recall over it.
  - `screen.go`: the retrieval RW-key will send, recorded with no model call.
  - `evidence/<setting>.jsonl`: each batch's retrieval and answer, once collected.
- `score.py`: `screen`, `offline`, `equivalence`, `live` and `tokens`, over #890's matching. `score_test.py` tests it.
- `mutation.py`: removes each behaviour this study added, one at a time, and records in `evidence/mutation.txt` that a named test fails.
- `corpus.json` and `evidence/synthesis`: symbolic links to #890's corpus and recorded stream syntheses, so every command takes this directory as `-dir`.

## Reproduction

From the repository root, with Docker, Go and Python 3. `<tei>` and `<proxy>` are the local ports chosen for the run.

```sh
D=docs/research/memory-encoding/retrieved-context
go test ./$D/...
(cd $D && PYTHONDONTWRITEBYTECODE=1 python3 -m unittest score_test)
python3 $D/mutation.py
# After the PM's go:
go run ./docs/research/chained-recall/harness build -batch 4 -dir $D -work <scratch> -embed-url http://127.0.0.1:<tei>/v1
go run ./$D/arms/retrieved screen -dir $D -work <scratch> -embed-url http://127.0.0.1:<tei>/v1
python3 $D/score.py screen
go run ./docs/research/memory-encoding/validation/arms/shipped build -dir $D -work <scratch> -embed-url http://127.0.0.1:<tei>/v1   # then offline
python3 $D/score.py equivalence
go run ./$D/arms/retrieved build -setting b4-rw-key -dir $D -work <scratch> -proxy http://127.0.0.1:<proxy> -embed-url http://127.0.0.1:<tei>/v1   # paid; then offline
go run ./$D/arms/retrieved build -setting b4-rw -dir $D -work <scratch> -proxy http://127.0.0.1:<proxy> -embed-url http://127.0.0.1:<tei>/v1       # paid; then offline
python3 $D/score.py offline
```

Each paid `build` resumes after its last recorded batch, so re-running it against a complete recording sends nothing.
