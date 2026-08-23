# Deployment

Governs what has to be true to run this workload as a pod: the two processes, what they bind, what they mount, what kills them at boot, and what an orchestrator must allow on shutdown. The architecture behind it is `environment.md` and `security.md`; this file states the operator-facing consequences and does not restate them. Rationale: ADR 0017, ADR 0012, ADR 0008, ADR 0010, ADR 0003, ADR 0020.

## Contents
- The two processes
- Listeners and the bind gate
- Volumes and ownership
- Configuration and secrets
- Capabilities
- Startup: what kills the pod and what degrades it
- Health and readiness
- Shutdown and the grace period
- Images and tags
- Load-bearing facts
- Open questions

## The two processes

One pod per agent, two containers (ADR 0017). Both are the same binary under different subcommands.

| | agent container | workspace container |
|---|---|---|
| Command | `gagent agent` | `gagent workspace` |
| Image built from | `build/agent.Dockerfile` | `build/workspace.Dockerfile` |
| Runs as | `nonroot`, uid 65532 | root |
| Shell in the image | none | yes |
| Serves | the HTTP gateway | the gRPC data plane (Read, Write, List, Exec) |
| Config file read | `gagent.yaml` | `workspace.yaml` |

**Neither base image is named here.** Each is pinned by digest in the Dockerfile the *Image built from* row names, and that is the only copy (§Images and tags). A row that restated the versions went stale the last time a base moved, and was read as current because its other cell still was (#591); what an operator plans around is the consequence below rather than the version.

Two consequences an operator plans around:

- **The agent image carries no shell and no interpreter** — the platform binary is the only executable file in it, because that is the image holding the secrets and the memory store (ADR 0017, `security.md`). `kubectl exec` into that container gets nothing; its diagnosis is logs and the gateway. The workspace image carries the shell its Exec path runs.
- **The workspace container needs root**, because it drops Exec children to another uid. The agent container never needs it.

## Listeners and the bind gate

| Listener | Default | Setting | Environment |
|---|---|---|---|
| Gateway HTTP | `127.0.0.1:8080` | `addr` | `GAGENT_ADDR` |
| Workspace gRPC | `127.0.0.1:8081` | `addr` | `GAGENT_ADDR` |

The agent finds the workspace through its own `workspace-addr` (default `127.0.0.1:8081`) and publishes it to every tool it spawns as `GAGENT_WORKSPACE_ADDR`. The agent never dials the workspace itself; the tools do (`environment.md`).

**The bind gate, as a deployment fact** (ADR 0012). Both listeners refuse to serve beyond localhost unless something authenticates the caller:

- **Gateway, no `tls:` section, non-loopback `addr`** — refuses at startup. The pod crash-loops. This is what a pod spec that sets `addr: 0.0.0.0:8080` to be reachable from a Service hits first.
- **Gateway, no `tls:` section, loopback `addr`** — starts, and a Service in front of it reaches nothing. Nothing in the pod's status says so; the startup log carries `auth="none: loopback binds only"` at INFO.
- **Workspace, non-loopback `addr`** — refuses at startup, with no TLS escape hatch: its gRPC listener is unauthenticated until contract authentication exists. **The workspace can never be fronted by a Service.** It is a same-pod sidecar reached over localhost, which is what makes the shared network namespace part of the deployment rather than a convenience.

Serving the gateway beyond localhost means setting the `tls:` section — `cert`, `key` and `client-ca`, all three or none.

**Replacing that material is a restart, and the restart drops the accepted queue.** All three files are read once, while the process starts (`internal/config/tls.go`); nothing re-reads them and no signal reloads them, so new files take effect only when the process starts again. That restart discards every message the gateway accepted and has not yet taken into a turn (§Shutdown and the grace period), so replacing a certificate is not transparent to senders.

## Volumes and ownership

| What | Setting | Default | Written by |
|---|---|---|---|
| Memory store | `memory-path` | `data/memory.db` | agent container |
| Outbox | derived, not separately settable | `outbox/` beside `memory-path` | the `message_send` tool |
| Workspace directory | `workspace` | `data/workspace` | workspace container |
| Tools tree | `tools-dir` | `./tools` | read only |

- The workspace directory is created on first use. When the workspace runs privileged it chowns that directory to `exec-uid` and gives every file it writes over the contract to the same uid, so contract writes and Exec children share one owner (ADR 0008). An unprivileged workspace owns the directory itself.
- The tools tree is read at startup only — one `manifest.json` per `tools/<name>/` directory, plus the executable the manifest names. It can be mounted read-only.
- **The memory store grows monotonically.** An insert retires nothing: a correction coexists with the fact it corrects, because no similarity threshold separates the two (`memory.md`, ADR 0018). Size the volume for growth and expect it to be reclaimed by the memory service's own compression and retention, not by writes replacing each other.

**Owner-only files.** Three files are mode-checked, and one of them is the workspace's: the agent's config file, the agent's `tls:` server key, and the workspace's config file. The check lives in the loader both processes share (`internal/config/`), so `workspace.yaml` carries the same rule as `gagent.yaml`. `0600` and `0400` pass; `0644` fails and the process exits. Kubernetes projects Secret and ConfigMap volumes at `0644` by default, so **all three** need an explicit `defaultMode` — a sidecar whose config is mounted at the default crash-loops exactly as the agent would. The certificate and the client CA are not checked.

## Configuration and secrets

Settings resolve in one precedence: **flags > `GAGENT_*` environment > config file > defaults.** The environment name is the setting with dashes and dots replaced by underscores, so `tools-dir` is `GAGENT_TOOLS_DIR` and `model.base-url` is `GAGENT_MODEL_BASE_URL`.

**A config-file key no setting reads is reported, not refused.** Both processes warn once per unread key, naming the key and the file, and start anyway — so a renamed key does not reject an operator's whole file. The warning is the only signal, and it reaches a log inside the container and nothing else (#476).

Two exceptions a pod spec meets immediately:

- **Both commands name their listener `addr`**, so a single `GAGENT_ADDR` set on the pod retargets both containers. Set it per container, or leave the workspace at its default.
- **`--agent-id` is a flag only.** It is read straight off the command line rather than through the settings layers, so setting `GAGENT_AGENT_ID` in the pod spec does not choose the agent's ID. The agent ID must appear in the container's args. The agent then publishes the ID it resolved under that same name for the tools it spawns, so a value already in the environment is replaced rather than read — `message_send` reads it there to stamp each delivered message (ADR 0016).
- **`tools.pins` has no environment form.** It is a nested map reachable through the config file only.

**The model API key never appears in configuration** (ADR 0003). `model.api-key-env` names the *environment variable* that holds it, defaulting to `OPENAI_API_KEY`. The key reaches the process through the environment and nowhere else, so it is injected as a Secret-backed environment variable rather than a mounted file.

**Every tool the agent spawns inherits the agent's whole environment**, the model key included — the runner starts the child from `os.Environ()` rather than from empty (`internal/tool/`). This is the decided posture, not a defect: security is drawn by environment and a tool is inside the agent's one (#543, `security.md` §Tool trust posture). An operator choosing what to put in the agent container's environment is choosing what every installed tool can read, so a credential meant for one tool is readable by all of them, and **installing a tool is a trust decision rather than a capability one**. **There is no per-tool credential scope, and no manifest field to reach for**: the manifest carried an `env` field that did nothing, and #550 removed it rather than document it (ADR 0009). A deployment that needs one tool to hold a credential the others cannot read needs a second agent, with its own environment and its own tool set.

**What the operator must put in the agent's environment for the shipped tools.** The platform sets `GAGENT_WORKSPACE_ADDR`, `GAGENT_OUTBOX_DIR` and `GAGENT_AGENT_ID` itself, so a deployment supplies none of them. What is left is `tools/github`, which reads `GH_TOKEN` and will not run without it, and optionally `GITHUB_API_BASE` to retarget an enterprise host. Both are the operator's to set, and both are readable by every other installed tool once set. This list is held against the tools' own reads by a test in `internal/tool/`, so a shipped tool that starts reading a new variable fails the build rather than starting up without a value.

The workspace's Exec children are unaffected: they get a scrubbed environment (ADR 0003).

**The agent stores no episodic memories, on any configuration.** The store holds an address and a value, and a summary of a conversation answers no question a read can ask for (ADR 0018), so context overflow — their last source — writes nothing and the oldest turns simply leave the window. Until #582 this depended on the configuration: the write was refused unless a model generated the summary, which the deterministic stand-in does not, so an operator running `deploy/local/gagent.yaml` already held none. The store still fills with what the writeback states as facts.

**A turn makes one model call more than its reasoning loop needs.** Recall resolves the turn's task into an address before the store is read, because an addressed store answers nothing asked in words (`memory.md`, ADR 0019). The call is unconditional — paid on turns that need no memory — and it is on the critical path, so nothing is recalled until it returns. Its outcome is on the monitoring stream as the recall event's `recall_outcome`: `unplaced` says the step could not place the question, which is a different fact from `empty`, and an operator watching whether memory is working reads that field rather than the count beside it.

**Every outbound destination is the operator's choice and none is the model's** (ADR 0013): replies through `message_send`, capabilities through the operator-installed tools, and the reasoning loop's and memory's model traffic through the operator-configured base URLs. A NetworkPolicy for this pod is written from the operator's own configuration, not from a list this repository holds.

**Outbound delivery is a directory, not a network hop.** `message_send` writes one JSON file per message into the outbox directory the gateway names for it, and nothing forwards it onward (`gateway.md`, ADR 0007). Whatever consumes the agent's replies reads that directory, so it is a volume the deployment has to mount and collect from — not a destination it configures.

## Capabilities

The workspace container needs five, and they divide on a line that decides the pod's Pod Security Standard:

| Capability | For | PSS `baseline` |
|---|---|---|
| `SETUID`, `SETGID` | dropping the Exec child to `exec-uid` | allowed |
| `CHOWN` | handing the workspace directory to `exec-uid` | allowed |
| `KILL` | signalling that child on expiry — the drop leaves it owning neither of the workspace's uids | allowed |
| `SYS_ADMIN` | `unshare(CLONE_NEWNET)` — the fresh, empty network namespace an Exec child gets per call | **not allowed** |

**`SYS_ADMIN` is netns creation, and it cannot be moved to an initContainer.** The unshare happens inside each Exec child immediately before the credential drop, not once at startup (`internal/workspace/shell/`), so the workspace container holds the capability for its whole life.

**Dropping it does not fail the pod.** Without `SYS_ADMIN` the workspace starts normally and every network-isolated Exec call fails closed with `agent_uid unavailable`. The only signal is a WARN at startup — `CAP_SYS_ADMIN missing; netns-isolated children will report agent_uid unavailable`. Nothing in the pod's status reports it, so an operator who drops the capability to reach `baseline` gets a running agent that cannot execute anything. That is the fail-closed posture ADR 0008 and ADR 0010 choose deliberately: a capability that is unavailable is better than one silently upgraded.

**`KILL` is the deadline, and it is the one that looks removable.** It grants nothing over the node: the container holds its own PID namespace, so the only processes it can reach are the workspace and the children it started. What it buys is the ability to signal them at all. `kill(2)` permits a signal only when the sender's real or effective uid matches the target's real or saved-set uid, or the sender holds `CAP_KILL`, and the drop leaves the child owning neither of the workspace's uids — being root is no substitute once the capability has left the bounding set. Until #612 the shipped set omitted it, and the consequence was invisible: the SIGKILL on expiry did nothing, `Wait` ran until the command finished on its own, and a 100ms deadline took as long as the command did. Single-account mode never needed it, which is part of why it went unnoticed — there the child owns the workspace's own uid.

**Dropping `KILL` now fails every Exec, by choice.** A privileged workspace that cannot signal its child refuses the call with `agent_uid unavailable` and a message naming the capability, rather than running a command whose deadline would pass unenforced. It warns at startup too — `CAP_KILL missing; every Exec will report agent_uid unavailable`. The alternative was to keep serving with an unenforceable bound, which is the shape the defect had.

## Startup: what kills the pod and what degrades it

The list below is the set an operator can trigger from a pod spec, grouped by cause. It is a selection, not the whole of it: the code carries roughly forty distinct startup error sites, and the ones left out are those no deployment choice reaches — a failed `setenv`, an unresolvable executable path, errors reachable only on a non-Linux build. If a pod fails at boot with a message not below, it is one of those.

Every fatal path exits **1** and prints `Error: <message>`; nothing distinguishes them by exit code, so the message is the only diagnosis.

**Kills the process at startup:**

| Cause | Process | Message shape |
|---|---|---|
| Config file group- or other-readable | both | `config file "…" is group- or other-accessible (mode 0644); it must be owner-only` |
| `--config` names a missing or malformed file | both | viper's parse error |
| `tls:` section incomplete (some of cert/key/client-ca) | agent | `tls: section is incomplete: missing key, client-ca` |
| `tls:` server key group- or other-readable | agent | `tls: server key "…" is group- or other-accessible (mode 0644)` |
| Certificate/key unparseable or mismatched; client CA unreadable or holding no PEM | agent | `tls: load server certificate: …` / `tls: client CA "…" holds no PEM certificates` |
| Non-loopback `addr` without a `tls:` section | agent | `addr "0.0.0.0:8080" is not a loopback bind: serving beyond localhost requires the tls: config section (ADR 0012)` |
| Non-loopback `addr` | workspace | `addr "0.0.0.0:8081" is not a loopback bind: the gRPC listener is unauthenticated and serves loopback only` |
| `provider: openai-compatible` with the named key variable unset or empty | agent | `model.provider "openai-compatible": environment variable OPENAI_API_KEY is not set or is empty` |
| Unknown `model.provider` value | agent | `unknown model.provider "…" (want "mock", or "openai-compatible" for any endpoint speaking the OpenAI chat-completions wire protocol)` |
| `provider: openai`, the value retired by #613 | agent | `model.provider "openai" names a vendor where it selects a wire protocol: set it to "openai-compatible", which is the same protocol and the same wiring` |
| Tools directory missing or unreadable | agent | `tool registry: tools directory "./tools" does not exist` |
| A manifest is unreadable, is not JSON, carries an unknown field, or omits `name`/`command` | agent | `load tools/foo/manifest.json: parse manifest: …` |
| A manifest carries the removed `env` field (#550) | agent | `load tools/foo/manifest.json: parse manifest: json: unknown field "env"` |
| Two manifests declaring the same name | agent | `duplicate tool name "files"` |
| A pin set is configured and a tool on disk is not named in it | agent | `unpinned tool "…": not named by the pin set; pin it as <sha256> or remove the tool` |
| A pin set is configured and a manifest's hash drifted | agent | `descriptor drift for "…": pinned=<sha256> fetched=<sha256>` |
| `tools.pins` present and naming no tool | agent | `config: tools.pins is present and names no tool; a pin set is the tools the operator accepted, so an empty one accepts nothing and no agent can start under it` |
| `message_send` is absent from the tools tree | agent | `tool registry: required tools missing: [message_send]` |
| `memory-path`'s directory cannot be created, or the store cannot be opened | agent | `mkdir …: read-only file system` / `open "…": …` |
| Workspace directory cannot be created, chowned or chmodded | workspace | `create workspace root "…": …` / `chown "…" to uid 1001: …` |
| Listen address already bound or rejected | both | `listen tcp …: bind: address already in use` |

**Starts, and degrades:**

| Cause | Effect |
|---|---|
| No `tools.pins` section | Drift pinning is off; the tree loads unreviewed. WARN: `tool drift pinning disabled: no pin set configured` |
| No `tls:` section on a loopback bind | The gateway serves unauthenticated on localhost. INFO field only: `auth="none: loopback binds only"` |
| `SYS_ADMIN` absent | Network-isolated Exec fails closed. WARN at startup, nothing afterwards |
| `KILL` absent, and `exec-uid` is not the workspace's own uid | **Every Exec fails closed** with `agent_uid unavailable: CAP_KILL is absent, …`. WARN at startup |
| `exec-uid` unreachable — the workspace runs unprivileged and `exec-uid` is not its own uid | **Every Exec fails closed** with `agent_uid unavailable`. No startup check, no warning |
| `exec-timeout: 0` | Every Exec without a per-call timeout fails: `shell: timeout is required`. No startup check |
| `workspace-addr` wrong or unreachable | Nothing dials it at startup; tools fail when first called |
| A tool's `command` names a missing executable | Startup is clean; the call fails |
| A directory in the tools tree without a `manifest.json` | Skipped silently, with no log line |

Two traps in that second table are worth naming separately, because they produce a healthy-looking pod that cannot work: **an unreachable `exec-uid`** and **a missing `SYS_ADMIN`**. Either leaves exec dead with nothing in pod status to show it. Run the workspace privileged, or set `exec-uid` exactly equal to the container's `runAsUser`. A missing `KILL` used to be a third and worse one, because it left exec *working* with its deadline unenforced; it now fails closed like the others.

**Choose `exec-uid` as a uid nothing else on the node runs as.** Containers share the node's uid space and `RLIMIT_NPROC` counts every process of the real uid, so a shared account spends the child's 64-process budget on processes that are not the child's. The code default is `1001`, which is the login user on many hosts; the sample deployment overrides it to `10001` for exactly this reason (`deploy/local/docker-compose.yml`).

**`tools.pins` absent and `tools.pins: {}` are different, and an empty section does not start.** An absent section leaves pinning off and warns. A present but empty section is rejected while the config resolves, because it is an unfinished configuration rather than a stricter one: "accept no tool" has no running form. `message_send` is required and the required set is not operator-configurable, so a pin set naming nothing refuses the mouth and the agent stops whatever else the tools tree holds. Name each tool and the hash the agent logs for it on the `tool manifest loaded` line, or remove the section to run unpinned.

**Upgrading with `tools.pins: {}` already in a config file will stop the pod from starting, and that pod was not pinned before.** Until #562 an empty section resolved to the same value as an absent one and quietly left pinning off, so a deployment carrying it has been running unpinned while reading as strict. The startup failure is the first accurate signal it has had.

**A one-shot `gagent agent --message` run skips the TLS and bind gates entirely**, because it never serves. A Job that smoke-tests configuration will not catch a broken `tls:` section or a non-loopback `addr`.

## Health and readiness

**There is no health or readiness endpoint.** The gateway registers two routes and no others — `POST /agents/{agent-id}/messages` and `GET /agents/{agent-id}/events` — and the workspace registers no gRPC health service. A probe has to use one of those two routes; a request naming any agent ID other than the process's own returns 404, so the probe path carries that exact value, and where `tls:` is set the probe must present a client certificate (the gateway requires and verifies one).

**`agent listening` is not a readiness signal.** The agent logs it before the listener binds, so a pod that cannot bind logs a successful-looking startup line and then fails. The workspace logs `workspace listening` after its bind, so the two containers' logs do not mean the same thing.

## Shutdown and the grace period

Both serving processes trap `SIGTERM` and `SIGINT`. A one-shot `gagent agent --message` run installs no signal handler at all — it runs to completion or dies.

- **Agent** — closes the listener, drains in-flight HTTP requests **bounded at 10s**, then closes the queue. Closing cancels the in-flight turn rather than draining it, waits for the runner to return, and then closes the memory store. The wait is unbounded; the close issues no network request, because the store has no provider to call (ADR 0018).
- **Workspace** — `GracefulStop`, which waits for in-flight RPCs with **no bound**. In practice an in-flight Exec is bounded by `exec-timeout` (default 30s). Exec children carry `Pdeathsig`, so they die with the workspace process.

Because the monitoring stream is long-lived by design, an agent with a connected SSE subscriber will normally spend the whole 10s drain before it gets to the queue.

**A draining agent refuses the connection; it does not decline the request.** The queue closes only after the listener is gone, so there is no window in which the pod accepts a post and answers it with a status. A sender posting during shutdown gets TCP connection refused, and one holding an open connection has it closed — neither distinguishable from a pod that crashed. A sender that needs to tell a drain from a failure has to learn it from the orchestrator, because the gateway has no way to say it.

**A restart discards the accepted queue.** Shutdown closes the queue instead of draining it, and the queue is held in memory with nothing writing it down (`internal/gateway/queue.go`, `gateway.md`), so every message accepted but not yet taken into a turn is lost. The sender holds its 202 and learns nothing more, because a message no turn took reaches no monitoring event — so a consumer treating the 202 as an end-to-end acknowledgement is counting messages the agent never read. Every way the process ends costs this: a rolling update, an eviction, a crash, and the `SIGKILL` at the end of the grace period alike. There is no drain-first option; an operator who needs the buffer empty stops posting and lets the agent work it off before the pod is stopped.

**No total shutdown deadline exists** — not as a flag, an environment variable, or a config key. The only explicit bound this repository states is the 10s HTTP drain; everything after it is unbounded, so `SIGKILL` at the end of the orchestrator's grace period is the real bound. Derive `terminationGracePeriodSeconds` from your own `exec-timeout` and provider timeouts rather than copying a number from here, and expect this document to state one when the code does.

## Images and tags

Registry: **ECR, `ap-northeast-2`, account `486152169996`.** Two repositories:

- `garam/gagent-agent`
- `garam/gagent-workspace`

**What gagent requires of those repositories, and what it does not** (ADR 0020). Stated as requirements rather than as settings, because nothing here can read that account and a description of it goes stale with nobody able to notice:

- **Create both `IMMUTABLE`, and never make either mutable.** The setting belongs to the repository rather than to a tag and applies retroactively, and ECR records nothing about the policy a tag was pushed under, so a repository that is ever mutable leaves every tag it holds a label rather than an identifier — the ones pushed while it was immutable included.
- **Push no moving tag.** A tag names one commit and never moves; there is no `dev` and no `latest`. garam's own bring-up repositories take the opposite choice, and the difference is deliberate.
- **Neither a lifecycle policy nor image scanning is required.** gagent depends on neither, and this section described both as settings the account holds until ADR 0020 was taken. Either control, run for that account's own hygiene, is its to decide — not this project's to require, and not this document's to describe.

**A consumer references an image by digest and sets `imagePullPolicy: Always`** (ADR 0020). The digest, because a tag's stability rests on a repository setting a consumer cannot check from outside. `Always`, because these repositories are emptied once a release path publishes, so the failure to be loud about is a reference that stops resolving — which the default `IfNotPresent` turns into a per-node stale-cache fault that presents as the node's.

**Tag scheme, decided and not yet built**: a `v`-prefixed git tag pushed to GitHub is to trigger the release, which will push the image tagged without the `v` — `v0.0.1` in git becoming `0.0.1` in ECR.

**Base images are pinned by digest.** Every `FROM` in `build/` names `tag@sha256:...`, which is what `docs/convention/ci.md` §Verify a pinned dependency before adopting it requires of a container image; the tag stays for readability and the digest is what resolves. Two builds of one commit therefore pull the same base bytes. What moves those digests is `.github/dependabot.yml`, and it covers less than the whole job — see §Open questions.

**The release workflow and its IAM role do not exist yet.** Nothing pushes to these repositories today, and both were empty when this was written. `make build-images` builds both images locally and pushes nothing; the `docker` job in `.github/workflows/ci.yml` builds them as a smoke check and pushes nothing. That workflow triggers on pull requests and pushes to `main` and carries no tag filter, so pushing `v0.0.1` today runs nothing at all. An operator can rely on the names and the tag rule; nothing in this repository has yet pushed an image behind them.

## Load-bearing facts

Everything else in this document is **descriptive**: true, useful, and free to change. The facts below are **load-bearing** — the `gagent-operator` team's correctness depends on them, so changing one is a contract change and they are the party to tell. This list is exactly the set they named; a fact nobody has said they depend on stays descriptive until someone says otherwise.

| Fact | Value today | Where | What breaks on their side |
|---|---|---|---|
| The bind gate | no `tls:` ⇒ loopback binds only, enforced at startup | `internal/config/loopback.go` | Their CRD decides Service and TLS wiring from it. Relaxing it changes what a pod without `tls:` exposes; tightening it crash-loops pods their CRD used to accept |
| Owner-only file mode | `0600`/`0400` pass, `0644` fails | `internal/config/owner_only.go` | Their generated Secret volumes set `defaultMode` because of it. Widening it silently loosens their posture; extending the check to more files fails deployments that were valid |
| `exec-uid` must be a dedicated account | default `1001`; `RLIMIT_NPROC` (64) counts every process of the real uid, and joins only in the privileged drop | `internal/workspace/shell/` | They allocate a uid nothing else on the node uses. If the cap stopped being per-uid, their allocation is unnecessary; if a shared uid became acceptable, their constraint is wrong |
| `MaxDrainBatch` | `8` | `internal/gateway/runner.go` | How many queued messages one turn takes. Their queue-depth alerting and backlog expectations are derived from it |
| `SubscriberBuffer` | `32` | `internal/gateway/events.go` | A monitoring subscriber loses events past this many undelivered, with nothing in the stream marking the gap (`gateway.md`). Their consumer's reconnect and gap handling assume this number |

## Open questions

- **What moves a base-image digest when the tag does not.** A digest receives no security patch; a tag does, and pinning gave that up deliberately. `.github/dependabot.yml` raises a pull request when a base image's tag moves to a new version, which is the covered half. The other half is not covered: Dependabot's docker ecosystem offers version updates and no security-update channel, and a digest refresh under an *unchanged* tag is conditional rather than promised. So a base republished with a CVE fix under the same tag may raise nothing, and the fallback is re-resolving the digest by hand. Determined on 2026-08-17 from GitHub's supported-ecosystems table and `dependabot-core`'s `docker/lib/dependabot/docker/update_checker.rb`; re-check it before relying on the covered half.
- **Who refreshes the ECR pull credential on-prem.** Relayed to us and unverified on both sides: the deployment target is an on-premises Talos cluster, ECR authorization tokens expire after 12 hours, and with no IRSA path on-prem the refresher is unassigned. Not this repository's to solve, but an image URI without a way for the cluster to authenticate is half an instruction.
- **Agent-to-sidecar authentication.** The workspace's gRPC listener ships pod-local and unauthenticated, which is what forces the loopback-only rule above (`security.md`, `environment.md`).
- **Whether the gateway should offer a durable accept.** What a restart drops is not open: the queue is held in memory, so a message accepted but not yet drained is lost (§Shutdown and the grace period, `gateway.md`). What is open is whether the 202 should promise more than "queued in this process", and what an accept that survives a restart would cost (#492).
