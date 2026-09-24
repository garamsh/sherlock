# Correction and retention across restart

This record describes the addressed-memory instrument at the revision it measured. Issue #823 replaced the active runtime with source-linked conversational memory and retired the addressed free and paid entry points from `tests/`; commands naming those entry points are historical and require a new instrument before reuse.

Publishes the correction and valid-old-fact retention measurement for issue #815, checked against `bf619b716b42edebf9f4dce03c501219f4d6c3ef` (attempts 1–2) and `21f1d6a696aee4e4f6ee5f5875f4888da477623c` (attempt 3). The complete MiniMax-M2 run produced **1 accepted / 4 exact-output reasoning failures / 5 invalid premises / 0 recall failures**; it did not successfully retire the intervening weekday value at reversal.

## Method

- Instrument: `tests/memory_correction_test.go`; free controls: `tests/memory_correction_controls_test.go`. They own their helpers and change no product source or existing experiment helper.
- The built `gagent agent` runs its HTTP gateway, rolling session, model protocol, writeback, SQLite store and recall. A local scripted HTTP model substitutes for the external provider in free controls. Workspace tools are built and registered and the operator instructs the model not to use them; no workspace process runs. Attempt 3 nevertheless tried `file_list` and `shell_exec` on each Neri probe, and all four tool calls failed with connection refused (`run/phase-{0,1}/restart-unstated.json`). The resulting qualified abstentions do not test successful workspace search.
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
env -u MINIMAX_KEY -u GAGENT_CORRECTION_PAID \
  GAGENT_CORRECTION_FREE_RESULTS=/tmp/correction-free \
  go test ./tests -run '^TestMemoryCorrection_' -count=1 -v
```

Use an absent output directory; the test refuses to mix runs. [Original free controls](results/free-controls.tar.gz) and [their checks](results/checks.txt) are unchanged: their source records revision `712eca891a45d8065e16e0ab72315d272bbb7763` and instrument hashes matching committed `bf619b7`, with 260 calls / $2.00 bounds. [Fresh free controls](results/free-controls-21f1d6a.tar.gz) were run against committed `21f1d6a` with both the key and paid flag removed from the environment; `free-test.log` records the paid test skipping and all free tests passing. Its `free/<arm>/source.json` records the revised 333 calls / $4.00 bounds and updated instrument hashes. Both captures yield the table below. These are binary boundary controls with a substituted model, not an end-to-end actual-model capability result.

| Arm | Cases | Accepted | Recall failure | Invalid premise | Interpretation |
|---|---:|---:|---:|---:|---|
| Intact | 10 | 10 | 0 | 0 | Both compaction probes and all restart probes meet the instrument's conditions |
| Recall removed | 10 | 2 | 8 | 0 | Every positive memory case fails; both unstated-fact controls still answer `UNKNOWN` |
| Write omitted | 10 | 0 | 0 | 10 | Plants do not reach the store; no correction or retention score is valid |

- The recall removal is a Go build overlay replacing the recall hook's `if !address.Placed()` with `if true`. The actual resolver still runs, but no store result reaches reasoning. No checked-out product file is edited. The intact acceptance criterion is false on all eight positive cases in this binary.
- The scripted provider derives probe answers only from the current recall frame. It neither reads the store itself nor answers from the expected-case table. Its plant declarations are scripted, so its success says nothing about a real model's extraction or address choice.
- Original per-case local latencies are intact: 1–2 ms, recall-removed: 0–1 ms, write-omitted: 1–1 ms; the fresh run measures 1–7 ms, 1–6 ms and 1–9 ms respectively (`free/<arm>/judgments.json`). Each arm makes 222 scripted model requests across 111 turns; all three arms cost $0 in model charges. Scripted usage counters and local latencies are instrumentation data, not MiniMax usage or latency estimates.
- Additional accepted/rejected evidence controls catch answer-bearing history, absent recall events, an absent stored fact, a wrong answer and missing reasoning input. The unstated-fact control independently rejects a missing recall event, an invented active Neri fact, a recalled Neri fact, and Neri evidence injected without matching recall IDs; an accepted control demonstrates that unrelated Ivo recall still permits `UNKNOWN`. Local proxy controls verify a preflight refusal does not reach upstream, an output cap reaches the upstream request, original response bytes are retained, and missing usage blocks further spending. The revised free gate control forwards 333 requests of 24,000 bytes before refusing the next two, and separately forwards 184 requests of 60,000 bytes before monetary refusal; both exercise the local HTTP upstream and preserve original response bytes. Usage controls also close forwarding when reported input exceeds its reservation or output exceeds the cap.

## Published originals and verification

| Archive | Original root within archive | Source revision | Contents |
|---|---|---|---|
| [Attempt 1](results/attempt-1-bf619b7-originals.tar.gz) | `run/` | `bf619b716b42edebf9f4dce03c501219f4d6c3ef` | Interrupted capture, `test.log`, `exit-code`, supervisor timestamps |
| [Attempt 2](results/attempt-2-bf619b7-originals.tar.gz) | `run-2/` | `bf619b716b42edebf9f4dce03c501219f4d6c3ef` | Budget-limited capture, `test-2.log`, `exit-code-2`, supervisor timestamps |
| [Attempt 3](results/attempt-3-21f1d6a-originals.tar.gz) | `run/` | `21f1d6a696aee4e4f6ee5f5875f4888da477623c` | Complete capture, `test.log`, `exit-code`, supervisor timestamps and original PM `audit.md` |

- [Manifest](results/manifest.json) records every archive SHA256 and every regular member's path, size and SHA256, including the unchanged original free archive. Paid members also map to their immutable local source paths. All original run-directory files are included byte for byte: raw per-turn request/response bytes, events, before/after rows, frozen SQLite snapshots, model usage, source identities, configs and process logs. No original JSON is rewritten; decode `request_bytes` and `response_bytes` from base64 to recover payloads.
- Source records retain fixed prompts, model, limits, instrument and binary hashes. The temporary paid binaries were not retained; their recorded hashes alone cannot reproduce those executable bytes. Attempt 1 has neither a final `calls.json` nor `judgments.json`; its usage totals cover only calls retained in completed per-turn captures, not any interrupted request that never reached a capture.
- The [publication checks](results/publication-checks.txt) record verification of member bytes against originals, instrument hashes against Git revisions, response usage and call accounting, SQLite integrity, and a pattern-based exposure scan including decoded payloads. This worker did not read `.env` or a provider key; the scan therefore cannot prove absence by comparison with the private key. The archived PM audit reports that separate exact-key sweep for attempt 3.
- To verify the archive/member manifest from this directory without extracting files:

```sh
python3 - <<'PYVERIFY'
import hashlib, json, pathlib, tarfile
root = pathlib.Path('results')
manifest = json.loads((root / 'manifest.json').read_text())
for archive in manifest['archives']:
    path = root / archive['path']
    assert hashlib.sha256(path.read_bytes()).hexdigest() == archive['sha256']
    with tarfile.open(path) as tar:
        members = [m for m in tar if m.isfile()]
        assert len(members) == len(archive['members'])
        assert {m.name for m in members} == {m['path'] for m in archive['members']}
        for member in archive['members']:
            data = tar.extractfile(member['path']).read()
            assert len(data) == member['bytes']
            assert hashlib.sha256(data).hexdigest() == member['sha256']
print('All archive and member hashes match')
PYVERIFY
```

## Measured attempts

All three attempts used MiniMax-M2 through `https://api.minimax.io/v1`. These are successive attempts at the same fixed sequence, not independent success-rate samples. The [PM publication adjudication](https://github.com/garamsh/gagent/issues/815#issuecomment-5807775994) and attempt 3's archived `audit.md` are the interpretation basis; the originals are the measurement basis.

| Attempt | Go exit | Recorded turns | HTTP records | Reported input / output tokens | Assumed-rate USD | Limit reached |
|---|---:|---:|---|---:|---:|---|
| 1 | 1 | 55 | 139 × 200 | 592,067 / 33,888 | 0.218286 | Default 10-minute Go timeout; no final score |
| 2 | 0 | 89 (83 successful, 6 failed) | 222 × 200; 6 × 402 | 905,505 / 60,109 | 0.343782 | $2.00 conservative reservation, starting at `phase-1/filler-25` |
| 3 | 0 | 111 | 280 × 200 | 1,105,985 / 71,173 | 0.417203 | Neither bound; all ten probe rows recorded |

- Attempts 1–2 allowed 260 calls and $2.00; attempt 3 allowed 333 calls and $4.00. All capped output at 2,048 tokens per request. The revision changed only the experiment bounds and their free controls/method documentation; plants, probes, scoring and product source stayed fixed. Input reservations use request bytes plus 4,096 tokens and output reservations use the full cap before forwarding, so the conservative ceiling can stop a run well below the reported-usage cost.
- Costs use assumed $0.30/M input and $1.20/M output tokens, not a verified current MiniMax-M2 tariff or provider invoice. No missing-usage or reservation-overrun record occurs in attempt 3. All 280 response usage pairs match the originals, and all sent requests carry `max_tokens=2048`.
- Attempt 3's PM command was `go test ./tests -run '^TestMemoryCorrection_Paid$' -count=1 -timeout=30m -v`, with paid opt-in, a private PM-supplied key and a fresh output directory. Supervisor timestamps measure 1,149,582 ms wall time. Ten probe latencies span 5,337–16,331 ms, median 9,497.5 ms; these measure complete probe turns, not just provider time.
- Attempt 1 retained only three probe captures: compacted Ivo returned exact `LARCH-731` (5,736 ms), restarted weekday returned the correct value in a sentence (17,450 ms), and restarted weekend likewise (8,177 ms). There is no aggregate verdict to publish or infer for the interrupted sequence. Its timeout log is retained rather than presented as a passing measurement.
- Attempt 2's `run-2/judgments.json` reports 1 accepted / 4 reasoning failures / 5 invalid premises / 0 recall failures. All four phase-0 positive answers contain the correct value in sentences and fail exact scoring; phase-0 Neri returns exact `UNKNOWN` and is the accepted negative control, not a positive memory-use pass. Their latencies are respectively 6,253 ms (compacted old), 9,465 ms (weekday), 29,406 ms (weekend), 10,131 ms (old) and 17,964 ms (unstated). Each phase-1 probe has an empty answer and 0 ms recorded duration after preflight refusal, not zero-latency successful work; the failed filler, refused call and absent reasoning invalidate all five. The unstated case also lacks an observed recall event. The 260-call ceiling was not reached.

## Complete-run adjudication

Attempt 3's `run/judgments.json` is the exact automated score. `run/<case>.json` carries the full answer, recall event, raw reasoning request and before/after store rows for each row below. A correct value inside a sentence and qualified `UNKNOWN` are semantic observations, not exact-format passes.

| Case within `run/` | Answer observation | Recalled IDs | Automated verdict | Duration ms |
|---|---|---|---|---:|
| `phase-0/compacted-old` | Exact `LARCH-731` | 1 | accepted | 7,395 |
| `phase-0/restart-corrected` | Correct `BASALT-592` in a sentence | 4, 3 | reasoning failure | 9,469 |
| `phase-0/restart-coexisting` | Correct `QUARTZ-806` in a sentence | 4, 3 | reasoning failure | 9,973 |
| `phase-0/restart-old` | Correct `LARCH-731` in a sentence | 1 | reasoning failure | 5,337 |
| `phase-0/restart-unstated` | `UNKNOWN` plus explanation | None | reasoning failure | 11,747 |
| `phase-1/compacted-corrected` | Correct `CEDAR-214` in a sentence | 5, 4, 3 | invalid premise | 15,871 |
| `phase-1/restart-corrected` | Correct `CEDAR-214` in a sentence | 5 | invalid premise | 7,715 |
| `phase-1/restart-coexisting` | Correct `QUARTZ-806` in a sentence | 5, 4, 3 | invalid premise | 6,433 |
| `phase-1/restart-old` | Correct `LARCH-731` in a sentence | 1 | invalid premise | 9,526 |
| `phase-1/restart-unstated` | `UNKNOWN` plus explanation | None | invalid premise | 16,331 |

- Phase 0's four positive cases have active answer rows, recall of those rows, their IDs and values in the current reasoning frame, and no planted values in preceding history. Only compacted Ivo meets exact output. Neri has no active or recalled Neri fact and abstains with an explanation; this is an exact-output failure, not an invented-fact failure. An observed empty recall event is distinct from no event.
- All five phase-1 rows are invalid because obsolete `BASALT-592` remains active and unarchived. Their answers contain the expected values or qualified `UNKNOWN`, and the captured prior histories lack planted values, but neither observation repairs the failed correction premise. Only four of the eight positive questions have valid memory-use premises.

The address evidence in attempt 3 separates writeback from storage:

| Original member | Declared operation and address | Observed store effect |
|---|---|---|
| `run/plant-1.json` | Initial `CEDAR-214` at `(Mira, what is Mira's office code on weekdays, on weekdays)` | Active row 2 |
| `run/plant-3.json` | `replace` with `BASALT-592` at the same address | Row 2 archived with `superseded_by=4`; row 4 active; weekend `QUARTZ-806` row 3 survives |
| `run/reversal.json` | `replace` with `CEDAR-214` at `(Mira, what is Mira's weekday office, empty restriction)` | New row 5 active; original weekday `BASALT-592` row 4 stays active and unarchived |
| `run/phase-1/compacted-corrected.json` | Recall IDs 5, 4, 3 | Current frame exposes both `CEDAR-214` and obsolete `BASALT-592` |
| `run/phase-1/restart-corrected.json` | Recall ID 5 only | Correct value does not demonstrate retirement of row 4 |

- The reversal is an address-selection/writeback failure before retrieval. SQLite applied replacement at the declared address; it was not asked to replace row 4's different address. There is no successful retirement or phase-1 correction/retention result to claim.
- Each phase's four restarted probes begin from identical frozen rows, preserved in `run/phase-0/frozen/memory.db` and `run/phase-1/frozen/memory.db`; per-case `Before` records permit comparison. Probe writebacks do not repair subsequent probes.
- Attempt 2 additionally wrote duplicate active weekday and weekend values at empty restrictions in `run-2/phase-1/filler-06.json`. Attempt 3's 48 second-phase fillers contain no such additional writes. Variation on identical messages is observed model behavior, not a controlled estimate of reliability.

## Limits

- One short sequence, three planted facts and named subjects do not test capacity eviction, age-based decay, growing-store interference, implied subjects, sustained complex work or reliable long-term learning. A complete HTTP-200 run and Go exit 0 do not establish successful correction.
- Exact scoring is deliberately conservative. Semantic value correctness is reported separately without upgrading verdicts. Invalid plants, malformed declarations, address changes and format failures are distinct from recall failure.
- Token absence in prior messages establishes the instrument's compaction check, not absence of every possible semantic paraphrase. The complete requests are retained for independent review; this is not a general proof of history independence.
- The worker made no provider calls. PM owns final evidence review, publication approval and any decision about product changes; no product behavior or architecture decision changes in this publication.
