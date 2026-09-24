# Correction and retention across restart

Measures the running agent's correction and valid-old-fact retention for issue #815; this record publishes free instrument controls, with actual-model measurement pending PM execution.

## Method

- Instrument: `tests/memory_correction_test.go`; free controls: `tests/memory_correction_controls_test.go`. They own their helpers and change no product source or existing experiment helper.
- The built `gagent agent` runs its HTTP gateway, rolling session, model protocol, writeback, SQLite store and recall. A local scripted HTTP model substitutes for the external provider in free controls. Workspace tools are built and registered, but this conversation uses none; no workspace process is needed.
- Plants, in order: Ivo's locker code `LARCH-731`; Mira's weekday office `CEDAR-214`; her coexisting weekend office `QUARTZ-806`; a correction of only the weekday office to `BASALT-592`. The second phase reverses the weekday office to `CEDAR-214`.
- Each phase adds 48 transient conversation turns. Phase zero asks the old unrelated fact in the same process; phase one asks the corrected fact in the same process. Each phase also asks four probes in separate restarted processes: weekday office, weekend office, old locker code, and Neri's never-stated locker code. There are ten scored cases and one sequence, not ten independent samples.
- Probes contain subjects and questions, never answer values. The operator ego asks for only a value or `UNKNOWN`; it supplies no facts, addresses or expected answers. The exact prompts and source hashes are retained in each `source.json`.
- A read-only SQLite connection makes a transactionally consistent snapshot before either compaction probe. The snapshot is checked against the source rows; each restarted probe receives a separate copy. Probe writes therefore cannot refresh the store used by later probes or the reversal. No expected fact is inserted or repaired directly.
- Every plant is read back. Before probing, the current, coexisting and unrelated values must be active, the obsolete value must be archived rather than active, and its successor must occupy the same address. Coexisting values must occupy distinct addresses. A failed premise remains a reported invalid case, never a repaired plant.
- A memory-use acceptance needs the correct exact answer, an active row carrying it, the row ID in the recall event, and the value and ID in the current reasoning frame. Every earlier reasoning message must be free of all four planted value tokens. A history leak or missing request invalidates the premise even if the answer is correct. `UNKNOWN` is the separately identified negative control, not successful positive memory use: it also requires an observed recall event, no active row mentioning Neri, and no Neri evidence in the current recall frame; unrelated recalled facts do not invalidate the control. An invented fact invalidates that premise even when the answer is `UNKNOWN`.
- Per-turn files separate raw provider request/response bytes, gateway events (including writeback operations and recall selection), all active/archive row columns before and after the turn, final answer, process ID and latency. Byte arrays in JSON are base64; decoding them recovers the original payloads. Frozen SQLite files and process logs are retained too.

## Free evidence

The reproducible command is:

```sh
GAGENT_CORRECTION_FREE_RESULTS=/tmp/correction-free \
  go test ./tests -run '^TestMemoryCorrection_' -count=1 -v
```

Use an absent output directory; the test refuses to mix runs. `results/free-controls.tar.gz` contains the full captured run; `results/checks.txt` records the command and checks. Source and binary identities are inside the archive. These are binary boundary controls with a substituted model, not an end-to-end actual-model capability result.

| Arm | Cases | Accepted | Recall failure | Invalid premise | Interpretation |
|---|---:|---:|---:|---:|---|
| Intact | 10 | 10 | 0 | 0 | Both compaction probes and all restart probes meet the instrument's conditions |
| Recall removed | 10 | 2 | 8 | 0 | Every positive memory case fails; both unstated-fact controls still answer `UNKNOWN` |
| Write omitted | 10 | 0 | 0 | 10 | Plants do not reach the store; no correction or retention score is valid |

- The recall removal is a Go build overlay replacing the recall hook's `if !address.Placed()` with `if true`. The actual resolver still runs, but no store result reaches reasoning. No checked-out product file is edited. The intact acceptance criterion is false on all eight positive cases in this binary.
- The scripted provider derives probe answers only from the current recall frame. It neither reads the store itself nor answers from the expected-case table. Its plant declarations are scripted, so its success says nothing about a real model's extraction or address choice.
- Captured per-case local latencies are intact: 1–2 ms, recall-removed: 0–1 ms, write-omitted: 1–1 ms. Each arm makes 222 scripted model requests across 111 turns; all three arms cost $0 in model charges. Scripted usage counters and local latencies are instrumentation data, not MiniMax usage or latency estimates.
- Additional accepted/rejected evidence controls catch answer-bearing history, absent recall events, an absent stored fact, a wrong answer and missing reasoning input. The unstated-fact control independently rejects a missing recall event, an invented active Neri fact, a recalled Neri fact, and Neri evidence injected without matching recall IDs; an accepted control demonstrates that unrelated Ivo recall still permits `UNKNOWN`. Local proxy controls verify a preflight refusal does not reach upstream, an output cap reaches the upstream request, original response bytes are retained, and missing usage blocks further spending.

## PM-only bounded measurement and limits

- Freeze and commit both instrument files and the unchanged product source before running `TestMemoryCorrection_Paid`. It rejects dirty product/instrument paths and records the Git revision, instrument SHA-256 hashes, binary SHA-256 and all fixed inputs before the first request.
- PM alone supplies `MINIMAX_KEY` to the test process, sets `GAGENT_CORRECTION_PAID=1`, and chooses a new `GAGENT_CORRECTION_RESULTS` directory. The worker neither reads `.env` nor calls the paid endpoint. The child agent receives only an inert local-proxy key; the proxy holds the provider key and never records headers.
- Plan: one MiniMax-M2 sequence at `https://api.minimax.io/v1`, ten cases, 111 turns and an expected 222 requests if each turn takes one resolve and one reasoning request. Hard request ceiling: 260. Output cap: 2,048 tokens per request. Accounting ceiling: $2.00 at the preregistered rates of $0.30 per million input tokens and $1.20 per million output tokens. These rates are accounting assumptions, not a verified current tariff; PM must verify them before spending.
- The proxy reserves input at request bytes plus 4,096 tokens and output at the full cap before sending. Missing usage, usage exceeding the reservation, or a ceiling refusal prevents further upstream calls. The monetary bound depends on those rates and the provider honoring the output cap; a provider violating the reservation invalidates that premise and is recorded. PM audits original bytes and reported usage, rather than treating a proxy total as the invoice.
- The automatic score is intentionally exact and conservative. Paraphrases, semantic address changes, malformed operations, wrong declarations and store failures require independent examination of original writeback bytes and before/after rows. Invalid plants are preparation outcomes, not evidence of recall failure. The raw evidence preserves these distinctions even where automatic diagnosis cannot assign a unique cause.
- Compaction is established only when the captured request is free of planted tokens in earlier messages. Repeated factual replies during fillers can keep a value in the window; such a case is invalid. Token absence does not rule out a semantic paraphrase; PM must audit the complete request before accepting actual-model memory use.
- Three active facts and a short run do not approach a capacity or age threshold. This does not measure eviction, decay, long-term learning, large-store interference, implied subjects, general correctness, or statistical reliability. No product behavior or architecture decision changes with this instrument.
