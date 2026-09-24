# Repository CLI comparison after removing the tool allowance

This report records the separately authorized #840 comparison on 2026-09-24 UTC; the [first CLI trial](README.md) and earlier API evidence remain unchanged.

## Change and method

- Product and three image tags: `a0cbffbd144fbaa95699d930bcb23f590f4186c4`; exact image IDs are in [comparison results](comparison-results.json). The loop has no per-tool counter; the default 16 model iterations and identical-failure guard remain ([ADR 0021](../../architecture/adr/0021-turn-iteration-bound.md)).
- The new eight-successful-call test failed against the old six-call implementation, then passed after removal. Mutating the remaining iteration ceiling and identical-failure check made their behavior tests fail. Accepted controls cover terminal response on iteration 16 and distinct/successful tool calls.
- `make ci` passed, and `make build-images` built all three new images before payment. The real product-loop synthetic fixture passed with **eight shell calls**, including its seventh/eighth markers, real Git/gh operations, isolation markers, nonzero status and timeout controls. This establishes the changed loop policy without claiming autonomous task success.
- The [exact comparison task](comparison-task.json) differs from the first CLI task only in the new branch name `docs/gagent-cli-840-comparison-20260925` and replacing its six-call batching sentence with the 16-iteration/identical-failure statement. No file, solution or ownership override was prescribed. The target, model, credentials, tool images, topology, validator and independent budget policy otherwise stayed the same, with fresh workspace and agent memory directories.
- Budget: 40 paid HTTP calls, 150,000 input tokens, 20,000 output tokens and estimated USD 0.10, reserved conservatively before calls. This comparison was explicitly authorized after the first failure; no further paid retry ran.

## Result

**Failed before selecting or editing a change.** The comparison made 9 paid calls with 105,727 input and 1,034 output tokens, estimated USD 0.0329589 at the same M2 rates as the first trial. The next conservative input reservation was refused with HTTP 402. The actual usage was below the input ceiling; reserving the next request's UTF-8 bytes plus framing headroom required more than the remaining allowance.

The agent first tried unauthenticated clone. `gh repo clone` then failed with `runtime/cgo: pthread_create failed: Resource temporarily unavailable`, exit 2, and **26,673 stderr bytes**. The error remained in subsequent model history; no output trimming changed the measured run. The agent recovered through `gh auth setup-git` and ordinary Git clone, inspected HEAD/branches, and again gave an absolute path to a workspace-relative file tool.

[The sanitized trace](comparison-trace.json) records six shell results, one rejected file-list call and a seventh requested shell command. The loop proceeded to its next model request after that command, consistent with dispatch completing, but the harness did not retain the refused request body containing the final tool result. Therefore this report does **not** count seven independently verified successful shell executions in the live comparison. Eight executions are directly evidenced by the free product-loop fixture.

Read-only checkout verification found HEAD `550dd5239c9cc42648ef116c3b9c772e1a483c66`, no working-tree changes and only local branch `main`. No new target branch, commit, push, validation run or PR was produced. Exact-value scanning covered 94 evidence/workspace files plus every checkout Git object and found no credential matches. Both trial containers and the harness processes exited.

This is a confounded failed comparison, not evidence that removing the gate fixes repository task completion: the thread failure changed the trajectory and inflated the context before the model could choose a change. The actual credential scope was not audited. The end-to-end acceptance remains unmet.

## Local diagnosis and accounting correction

- A local-only diagnostic invoked the same built-image resource shim as UID 10001 with the unchanged 2GiB address-space, 64-process, 60-second CPU, 100MiB file and 1024-descriptor caps. Five trials each used the default environment, `GOMAXPROCS=2`, or `MALLOC_ARENA_MAX=2`, with a synthetic enterprise token and local host destination, never the target repository or model service.
- All 15 reached the expected connection refusal without reproducing the thread abort. This early network failure does not reproduce the live GitHub request path or prove a mitigation. The failure's specific limiting resource remains unresolved; no production cap or output trimming policy changed. The host reported 12 Docker CPUs and 6,034,354,176 bytes of VM memory.
- The measured `live/meter.json` contains the last accounted request and says `stopped:false`; `live-summary.log` and `agent.log` show the subsequent reservation refusal and `stopped:true`. The original files and measured harness are hash-pinned and unchanged. A separately named `accounting-finalization.json` derives the final stop state without rewriting those originals.
- The study harness now writes a final meter snapshot after shutdown and retains refused request bodies. Free accepted/refused reservation and final-snapshot controls passed; **this observer correction was made after the comparison and was not paid-replayed**. It cannot recover the missing final tool result from the completed run.

## Evidence custody and reproduction

Preserve `.local/verification/github-840-comparison/` alongside both earlier evidence roots. Its `live/` and `preflight/` contain exact task/revision, raw model wire messages, process logs, tool traces in model history, marker/secret checks and workspace state. Root logs include CI/images, old-cap failure, retained-bound mutations, local diagnosis and the accounting correction. [The comparison custody index](comparison-evidence-index.json) pins the complete measurement snapshot and the original accounting files; private target contents remain local.

Use the committed study harness's `comparison-preflight` mode for the free fixture against the recorded image revision. `comparison-live` is the retained measurement path, not authorization for another paid run. No gateway was served in the one-shot topology; network-enabled serving deployments still require mTLS. The first trial's offline validator, synthetic-fixture limitations, exact-value scanning limitations and secret/network trust posture continue to apply.
