# Repository CLI trial

This study records issue #840's workspace CLI checks and single live repository task, measured on 2026-09-24 UTC.

## Question and method

Can the actual product agent inspect an authorized repository, choose a permitted substantive change, validate it, and publish a draft PR through ordinary Git and GitHub CLI in an isolated workspace?

- Product source and all three image tags: `09c278b5c7c57c2f926c88896fee7178b7334847`. Exact image IDs are in [results](results.json).
- Two built containers share a network namespace; workspace gRPC stays on loopback. Only workspace files enter the workspace volume. Agent config, memory and tool binaries have separate mounts.
- The agent runs one-shot `--message`; `cmd/gagent/agent.go` selects the turn path before gateway serving. This trial therefore tests no serving gateway or gateway mTLS deployment. Network-enabled serving deployments require [gateway authentication](../../architecture/adr/0012-gateway-mtls.md), including on shared loopback.
- Installed tools come from the built tools image: `file_read`, `file_write`, `file_list`, `shell_exec`, `message_send`. The normal six-call allowance per tool is unchanged.
- [Exact preregistered task](task.json), [study harness](qa.py), [sanitized actual tool trace](live-trace.json), and [local evidence custody index](evidence-index.json). Raw model requests/responses remain local because they contain private target repository content.
- Model: MiniMax-M2. Hard ceilings: 40 HTTP calls, 150,000 input tokens, 20,000 output tokens, estimated USD 0.10. Before each request the meter reserves UTF-8 request bytes plus 4,096 input tokens and up to 3,072 output tokens. Missing, malformed, negative or boolean usage stops further payment. Cost uses USD 0.30/1M input and 1.20/1M output, the [published M2 rates](https://www.minimax.io/news/minimax-m2); this is an estimate, not an invoice.
- No task changes or paid retry followed the result. Worker fixture setup and read-only verification are not agent task success.

## Evidence and results

| Measurement | Result |
|---|---|
| `make ci` at measured source | Passed: lint, formatting, tests and 23 packages built; local `ci-measured.log` |
| `make build-images` | Agent, workspace and tools images built; local `images-measured2.log` |
| Deterministic product loop | Passed in `preflight/`: six shell calls, file read/write, real local Git clone/commit/push, real `gh api` draft-PR POST to a synthetic TLS fixture |
| Failure controls | Missing Git ref returned 128; 100ms shell timeout reported `timed_out`; marker-authenticated synthetic PR request succeeded |
| Runtime isolation | UID 10001; CPU 60s, address space 2GiB, file size 100MiB, descriptors 1024, processes 64; no host home, agent mount, Docker socket or model/agent marker variables |
| Model access | Workspace HTTP request received 403; unauthenticated POST control received 403; actual authenticated agent calls succeeded. The metering endpoint has a random agent-only credential, not a public placeholder |
| Grant tests | Config precedence, operator grant authority, RPC trust refusal, real Exec environment separation and invalid/missing grant refusals passed; removing the corresponding behavior produced recorded test failures |
| Privileged checks | Focused capability, missing-capability refusal, UID drop, empty network namespace and timeout tests passed in the workspace image with the Compose capability list |
| Live trial | Failed before change selection; no edited target file, validation run, new branch, commit, push or PR |
| Live usage | 13 calls; 58,054 input and 2,376 output tokens; USD 0.0202674 estimated |
| Target | `garamsh/agent-test`, starting and ending HEAD `550dd5239c9cc42648ef116c3b9c772e1a483c66`; clean working tree, sole local branch `main` |
| Credential custody | Exact-value scan of 102 local evidence/workspace files and all checkout Git object contents found no credential matches; both trial containers removed and harness processes exited |

The live agent first tried unauthenticated `git clone`, then configured the normal `gh` credential helper and cloned successfully. It inspected the starting revision and instructions, but used absolute paths with workspace-relative file tools. Those calls were refused; it spent the remaining shell allowance on discovery, then received three shell-budget refusals and ended by saying it needed another turn. No further turn was authorized or run. The task's end-to-end acceptance remains unmet; a successful synthetic sequence does not show that the model can choose and finish a permitted repository change.

Free harness setup failures are retained separately: Docker context selection, config permissions/tool-directory symlinks, non-loopback workspace binding, dynamically linked host test binaries in a static agent image, read-only tool archive extraction, and `gh --hostname` rejecting a port. They were corrected locally before payment. The final fixture uses static image tools and an absolute HTTPS URL with a port for its fake server; no product listener gate or tool budget was relaxed.

The whole privileged test binary was also attempted in the workspace image: its Go-compilation test failed because that image intentionally ships no Go toolchain. The focused privileged suite then exercised the relevant kernel boundaries successfully. The unprivileged full suite passed through `make ci`. An early CI attempt lacked the lint tool on PATH; a second encountered the committed-source guard while implementation edits were uncommitted. Later measured-source CI passed. Intermediate removal inventory failures were corrected before the final check.

## Earlier API trial and custody

The superseded API trial remains a separate failed measurement at `d6a21e8789ec57ea00d9794335bf6f6c06e0e458`: 15 calls, 74,732 input tokens, 4,886 output tokens, USD 0.0282828 estimated, no target edits or PR. Its prompt included an ownership override later withdrawn by the coordinator; it is not an approved task for reuse. Its raw evidence, exact prompt, report and checksum index remain untouched under `.local/verification/github-840/`.

The CLI trial is under `.local/verification/github-840-cli/`; its `live/`, `preflight/`, failed free setup directories, build/test logs, measured harness and checksum index must be retained before deleting this worktree. [The custody index](evidence-index.json) records absolute paths and index hashes. Neither private target contents nor root credentials are copied into committed evidence.

## Reproduction and limits

- Read the current [operator grant decision](../../architecture/adr/0003-secret-free-execution-domain.md) and [deployment guidance](../../architecture/deployment.md). Workspace code deliberately receives repository credentials and networking; repository scope was not audited or proven here.
- Check out the measured source, run `make ci` and `make build-images`, then use the study harness's `preflight` mode on Docker Desktop with `host.docker.internal` access. The output directory must be absent; preserve previous evidence instead of overwriting it. The fixture requires host Python, OpenSSL and Docker, not paid model access.
- `live` is single-use measurement code, not an instruction to repeat this trial. A new paid study requires a new authorized task and budget. It parses only the two named assignments from the root credential file without sourcing it: `GH_TOKEN` goes only to workspace provisioning, and `MINIMAX_KEY` only to model access outside that workspace. No credential value enters prompts or configuration files.
- Preflight uses a scripted model and a fake GitHub endpoint; only local Git operations and the real `gh` executable are exercised. It does not prove real GitHub write permission, independent change selection, target validation, or successful PR creation. The live trial exercised authenticated clone and reads only.
- The offline validator checks Git whitespace and Markdown conflict markers. It is not semantic validation, and the live agent never reached it. Target-specific checks remain the agent's responsibility.
- Exact-value scanning cannot establish absence of encoded or transformed secrets. Container mount/UID checks and environment markers establish the tested topology, not security under arbitrary deployment changes. Workspace networking exposes shared loopback services; serving agents need mTLS, and model-meter authority stayed agent-only in this one-shot study.
