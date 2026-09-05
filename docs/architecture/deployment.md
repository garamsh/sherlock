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

**The *Runs as* row is what the image sets, not what the pod asserts.** Each cell is that container's Dockerfile `USER`, and an orchestrator admitting the pod reads neither Dockerfile. What a manifest has to assert about these two, and which Pod Security Standard the pair can reach, is §Capabilities.

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
| Outbox | derived, not separately settable | `outbox/` beside `memory-path` | the `message_send` tool, which the agent container spawns |
| Workspace directory | `workspace` | `data/workspace` | workspace container |
| Tools tree | `tools-dir` | `./tools` | nobody; read by the agent container |

- The workspace directory is created on first use. When the workspace runs privileged it chowns that directory to `exec-uid` and gives every file it writes over the contract to the same uid, so contract writes and Exec children share one owner (ADR 0008). An unprivileged workspace owns the directory itself.
- The tools tree is read at startup only — one `manifest.json` per `tools/<name>/` directory, plus the executable the manifest names. It can be mounted read-only.
- **The memory store grows monotonically, and nothing reclaims it.** An insert retires nothing: a correction coexists with the fact it corrects, because no similarity threshold separates the two (`memory.md`, ADR 0018). Nothing else retires a record either — there is no compression and no consolidation, and the retention pass drops nothing a writeback wrote (`memory.md`, #664). The one path that can retire one is capacity eviction, which discards the least recently read at the active index's ceiling and does not fire at the growth a decade of turns reaches (ADR 0018 §Forgetting is decided by use). **That ceiling counts memories and not bytes**, so it bounds no volume: size for growth that nothing reverses, and take the rate from the store you run rather than from that headroom.

**Two writable volumes, not one shared filesystem.** The agent container writes the memory store and the outbox beside it; the workspace container writes the workspace directory; and nothing the two containers exchange goes through a file. The tools that need the workspace dial its gRPC data plane instead — `files` over Read/Write/List and `shell_exec` over Exec (`environment.md`, ADR 0017) — and no agent-side path resolves under the workspace directory, `workspace` being a setting only `gagent workspace` reads (`internal/config/`). So the two sets may sit on separate volumes, and neither container needs a filesystem the other holds at the same time. The fourth row above adds no third volume of state: the tools tree is the operator's install rather than anything the agent writes. The sample deployment mounts one root into both, which is that file's convenience rather than a requirement (`deploy/local/docker-compose.yml`).

**What the workload requires of them is survival, not sharing: an agent's volumes must outlive a reschedule.** That is a durability property and not a capacity one, and it is the failure that looks like success — the pod runs, the data persists, and the loss happens when the pod moves. Which storage satisfies it is the orchestrator's. No storage class, default, or access mode is named here: this repository cannot read a cluster, so a claim about one would go stale with nobody in a position to notice.

**The outbox is on the agent's volume, and that decides where a consumer of replies can sit.** It is derived from `memory-path` rather than separately settable, so it lands wherever the memory store does. A consumer that reads it has to reach that volume — another container in this pod, or the agent container itself, whose one-shot `gagent agent --message` run echoes what the turn delivered. A consumer that watches the gateway's monitoring stream instead needs no volume at all, and the agent's stays private to the agent container; the two are not the same record, though, because that stream is lossy and carries a turn's outcome, where the outbox holds the addressed replies and is durable once each file is written (`gateway.md`). Which of them a deployment collects from is therefore a storage decision before it is a delivery one.

**Owner-only files.** Three files are mode-checked, and one of them is the workspace's: the agent's config file, the agent's `tls:` server key, and the workspace's config file. The check lives in the loader both processes share (`internal/config/`), so `workspace.yaml` carries the same rule as `gagent.yaml`. `0600` and `0400` pass; `0644` fails and the process exits. Kubernetes projects Secret and ConfigMap volumes at `0644` by default, so **all three** need an explicit `defaultMode` — a sidecar whose config is mounted at the default crash-loops exactly as the agent would. The certificate and the client CA are not checked.

## Configuration and secrets

Settings resolve in one precedence: **flags > `GAGENT_*` environment > config file > defaults.** The environment name is the setting with dashes and dots replaced by underscores, so `tools-dir` is `GAGENT_TOOLS_DIR` and `model.base-url` is `GAGENT_MODEL_BASE_URL`.

**A config-file key no setting reads is reported, not refused.** Both processes warn once per unread key, naming the key and the file, and start anyway — so a renamed key does not reject an operator's whole file. The warning is the only signal, and it reaches a log inside the container and nothing else.

Two exceptions a pod spec meets immediately:

- **Both commands name their listener `addr`**, so a single `GAGENT_ADDR` set on the pod retargets both containers. Set it per container, or leave the workspace at its default.
- **`--agent-id` is a flag only.** It is read straight off the command line rather than through the settings layers, so setting `GAGENT_AGENT_ID` in the pod spec does not choose the agent's ID. The agent ID must appear in the container's args. The agent then publishes the ID it resolved under that same name for the tools it spawns, so a value already in the environment is replaced rather than read — `message_send` reads it there to stamp each delivered message (ADR 0016).
- **`tools.pins` has no environment form, and `GAGENT_TOOLS_PINS` fails startup rather than being ignored.** It is a nested map — one variable carries one value — reachable through the config file only. Setting the variable is refused while the config resolves, empty value included, and the refusal names the file to write the pin set in (#745). A per-tool variant such as `GAGENT_TOOLS_PINS_FILES` names no key and is ignored.

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

**Target `baseline`, and do not target `restricted`.** `restricted` is reached only where every container in the pod runs non-root, and one here does not: the workspace drops Exec children to another uid, which is where ADR 0010 puts the execution boundary, so it runs as root (§The two processes). Admission evaluates the pod and the two containers share one (ADR 0017), so what the agent container could satisfy alone decides nothing — on its own it would pass `restricted`, which is what makes this workload read as one manifest away from a standard it cannot reach. Getting there means moving that boundary, which is a change to ADR 0010 rather than to a manifest, and nothing has proposed one. `baseline` is what is left, and the `SYS_ADMIN` row is what reaching it costs.

**A manifest asserts the agent container's non-root run; the image's `USER` does not.** Set `runAsNonRoot: true` on that container's security context. The *Runs as* row in §The two processes is the image — `build/agent.Dockerfile` declares `USER nonroot` — and an orchestrator enforcing non-root reads what the pod asserts rather than what the image ships, so a manifest that leaves the question to the image is refused at admission with nothing in the image and nothing in this repository having been wrong. A numeric `runAsUser` is not the assertion either: it states which uid to run as and asserts nothing about that uid, so a value that is not zero reads as non-root to a person and to nothing else.

**`SYS_ADMIN` is netns creation, and it cannot be moved to an initContainer.** The unshare happens inside each Exec child immediately before the credential drop, not once at startup (`internal/workspace/shell/`), so the workspace container holds the capability for its whole life.

**Dropping it does not fail the pod.** Without `SYS_ADMIN` the workspace starts normally and every network-isolated Exec call fails closed with `agent_uid unavailable`. The only signal is a WARN at startup — `CAP_SYS_ADMIN missing; netns-isolated children will report agent_uid unavailable`. Nothing in the pod's status reports it, so an operator who drops the capability to reach `baseline` gets a running agent that cannot execute anything. That is the fail-closed posture ADR 0008 and ADR 0010 choose deliberately: a capability that is unavailable is better than one silently upgraded.

**`KILL` is the deadline, and it is the one that looks removable.** It grants nothing over the node: the container holds its own PID namespace, so the only processes it can reach are the workspace and the children it started. What it buys is the ability to signal them at all. `kill(2)` permits a signal only when the sender's real or effective uid matches the target's real or saved-set uid, or the sender holds `CAP_KILL`, and the drop leaves the child owning neither of the workspace's uids — being root is no substitute once the capability has left the bounding set. Until #612 the shipped set omitted it, and the consequence was invisible: the SIGKILL on expiry did nothing, `Wait` ran until the command finished on its own, and a 100ms deadline took as long as the command did. Single-account mode never needed it, which is part of why it went unnoticed — there the child owns the workspace's own uid.

**Dropping `KILL` now fails every Exec, by choice.** A privileged workspace that cannot signal its child refuses the call with `agent_uid unavailable` and a message naming the capability, rather than running a command whose deadline would pass unenforced. It warns at startup too — `CAP_KILL missing; every Exec will report agent_uid unavailable`. The alternative was to keep serving with an unenforceable bound, which is the shape the defect had.

**The five above are the privileged path's, and a second isolation path needs a host rather than a capability.** `internal/workspace/shell/process_linux.go` carries both: where the workspace is root the Exec child unshares `CLONE_NEWNET` while `CAP_SYS_ADMIN` is still held, and where it is not the child clones `CLONE_NEWUSER | CLONE_NEWNET` with the uid mapped to itself (ADR 0010). The second is what makes "drop `SYS_ADMIN`, run unprivileged, keep the isolation" look available. It is not a shape this document supports — the workspace container needs root (§The two processes), and the path exists only in the single-account one, an unprivileged workspace whose `exec-uid` is its own uid, any other value failing closed before the namespace is reached. What follows states what that path would require, not that it is offered.

**It requires a host that permits unprivileged user namespaces.** Creatability without privilege is a property of the syscall and not of every host, so it is checked rather than assumed — checked where the workspace would run, as the account it would run as, under the security context it would run under:

- **`user.max_user_namespaces` reads non-zero.** The sysctl is itself namespaced, so a value read on the node answers for the node and not for the pod.
- **`unshare --user --net true` succeeds** with no capabilities added. The sysctl is necessary and not sufficient: it answers for the kernel, while a runtime's seccomp or LSM policy can refuse a clone the kernel would permit, so what settles the question is making the call rather than reading a value.
- **The shell package's single-account netns cases run rather than skip.** They make the call the workspace makes, and the helper they run through turns the fail-closed sentinel into a skip (`internal/workspace/shell/netns_linux_test.go`), so on a host that refuses the clone they skip and `make ci` still passes. A green pipeline is not evidence about this path; those cases running is.

**Unmet, it fails closed as a missing `SYS_ADMIN` does, with one difference.** The clone's `EPERM` becomes the same `agent_uid unavailable` every undeliverable boundary reports, so the call is refused rather than served with the host's network (ADR 0010). The difference is the signal: the `SYS_ADMIN` probe warns at startup only where the workspace is root, so in the single-account shape nothing is probed at startup and the first network-isolated Exec is the first thing to say so.

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
| `GAGENT_TOOLS_PINS` set, with any value | agent | `config: GAGENT_TOOLS_PINS is set, and the environment cannot express tools.pins: a pin set maps each tool name to its manifest SHA-256, and one variable carries one value` |
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
| The host refuses the unprivileged user namespace, and the workspace runs unprivileged | Network-isolated Exec fails closed with `agent_uid unavailable`. No startup check, no warning (§Capabilities) |
| `KILL` absent, and `exec-uid` is not the workspace's own uid | **Every Exec fails closed** with `agent_uid unavailable: CAP_KILL is absent, …`. WARN at startup |
| `exec-uid` unreachable — the workspace runs unprivileged and `exec-uid` is not its own uid | **Every Exec fails closed** with `agent_uid unavailable`. No startup check, no warning |
| `exec-timeout: 0` | Every Exec without a per-call timeout fails: `shell: timeout is required`. No startup check |
| `workspace-addr` wrong or unreachable | Nothing dials it at startup; tools fail when first called |
| A tool's `command` names a missing executable | Startup is clean; the call fails |
| A directory in the tools tree without a `manifest.json` | Skipped silently, with no log line |

Three traps in that second table are worth naming separately, because they produce a healthy-looking pod that cannot work: **an unreachable `exec-uid`**, **a missing `SYS_ADMIN`**, and **a host that refuses the unprivileged user namespace**. Each leaves exec dead — the third only for the calls that ask for isolation — with nothing in pod status to show it. Run the workspace privileged, or set `exec-uid` exactly equal to the container's `runAsUser`; the second of those is the single-account shape, where isolated Exec then rests on the host permitting unprivileged user namespaces (§Capabilities). A missing `KILL` used to be another and worse one, because it left exec *working* with its deadline unenforced; it now fails closed like the others.

**Choose `exec-uid` as a uid nothing else on the node runs as.** Containers share the node's uid space and `RLIMIT_NPROC` counts every process of the real uid, so a shared account spends the child's 64-process budget on processes that are not the child's. The code default is `1001`, which is the login user on many hosts; the sample deployment overrides it to `10001` for exactly this reason (`deploy/local/docker-compose.yml`).

**A stalled workspace is refused by its caller, and `exec-timeout` still decides how long a command runs.** `exec-timeout` bounds the command the workspace runs, not the RPC carrying it, so a workspace that accepts the connection and then answers nothing — a deadlocked handler, a process stopped mid-call — never reaches it, and the queue serialises runs, so one such call stops every later message for that agent with nothing in the pod's status to show it (§Health and readiness). Each tool therefore sets its own deadline on the data-plane call: `files` waits what a pod-local filesystem call needs, and `shell_exec` waits the per-call timeout it was handed plus a margin for the workspace's dispatch. **A call naming no timeout is bounded against the stall alone**, because the ceiling is then `exec-timeout`'s and a tool in the agent container cannot read it — so that bound is required to clear the ceilings `exec-timeout` is given, and raising `exec-timeout` needs no matching change here. gRPC carries a caller's deadline into the executor, so a bound taken from a command duration instead would cut short every command a longer `exec-timeout` allows; what keeps this one clear of it is a test rather than a line in this document.

**`tools.pins` absent and `tools.pins: {}` are different, and an empty section does not start.** An absent section leaves pinning off and warns. A present but empty section is rejected while the config resolves, because it is an unfinished configuration rather than a stricter one: "accept no tool" has no running form. `message_send` is required and the required set is not operator-configurable, so a pin set naming nothing refuses the mouth and the agent stops whatever else the tools tree holds. Name each tool and the hash the agent logs for it on the `tool manifest loaded` line, or remove the section to run unpinned.

**Upgrading with `tools.pins: {}` already in a config file will stop the pod from starting, and that pod was not pinned before.** Until #562 an empty section resolved to the same value as an absent one and quietly left pinning off, so a deployment carrying it has been running unpinned while reading as strict. The startup failure is the first accurate signal it has had.

**A one-shot `gagent agent --message` run skips the TLS and bind gates entirely**, because it never serves. A Job that smoke-tests configuration will not catch a broken `tls:` section or a non-loopback `addr`.

## Health and readiness

**There is no health or readiness endpoint.** The gateway registers two routes and no others — `POST /agents/{agent-id}/messages` and `GET /agents/{agent-id}/events` — and the workspace registers no gRPC health service. A probe has to use one of those two routes; a request naming any agent ID other than the process's own returns 404, so the probe path carries that exact value, and where `tls:` is set the probe must present a client certificate (the gateway requires and verifies one).

**Of the two, only `GET /agents/{agent-id}/events` may be used as the probe.**

- **`GET /agents/{agent-id}/events` subscribes and enqueues nothing.** The monitoring stream is a different structure from the inbox: a subscription puts no message on it, publishing to it never blocks the agent's run, and zero subscribers is the normal case (`internal/gateway/events.go`, `gateway.md`). The agent-ID check runs before the subscription, so a mismatched ID is refused without one being registered. The response head arrives before any event exists and the body then ends only when the client disconnects or the queue closes, so a probe takes the head as its answer and sets its own timeout rather than waiting for the body to finish.
- **`POST /agents/{agent-id}/messages` enqueues real work.** The message goes into the agent's inbox — a bounded channel (`DefaultQueueCapacity`, 64 messages, `internal/gateway/queue.go`) drained by serial turns, each taking a bounded batch (`MaxDrainBatch`, §Load-bearing facts) — and the agent runs a turn on it like any other message. A periodic probe therefore competes with the agent's work instead of observing it: it spends turns and model calls, it takes drain slots from real senders, and once the inbox is full the enqueue blocks the caller until a turn frees a slot, so the probe's own latency becomes a function of the agent's backlog. The route has no probe mode: a post the handler accepts is a message, and no content marks it as anything else.

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

Registry: **ECR, `ap-northeast-2`, account `486152169996`.** Five repositories, and the requirements below bind these five and no others:

- `garam/gagent-agent`, `garam/gagent-workspace`, `garam/gagent-tools` — production, holding what a release publishes. One repository per image.
- `garam/gagent-agent-dev`, `garam/gagent-workspace-dev` — bring-up. A release path to the production repositories now exists, and is described at the end of this section; what these two hold from here was left open when that was decided (#758), and this document does not guess at it.

`garam/gagent-tools` has no bring-up counterpart. `garamsh/infra#219` asked for the repository and for the push-role scope that reaches it, and both are in place: the repository exists and the push role's grant names it. Read from the account 2026-09-05T20:47+09:00; nothing here re-checks it.

A `gagent`-prefixed repository in that namespace that is not on this list is not gagent's: `garam/gagent-operator-dev` is the `gagent-operator` project's.

**What gagent requires of those repositories, and what it does not** (ADR 0020). Stated as requirements rather than as settings, because nothing here can read that account and a description of it goes stale with nobody able to notice:

- **Create every one of them `IMMUTABLE`, and never make any of them mutable.** The setting belongs to the repository rather than to a tag and applies retroactively, and ECR records nothing about the policy a tag was pushed under, so a repository that is ever mutable leaves every tag it holds a label rather than an identifier — the ones pushed while it was immutable included.
- **Push no moving tag.** A tag names one commit and never moves; there is no `dev` tag and no `latest`, in the bring-up two as much as in the production three. garam's bring-up repositories for its other applications take the opposite choice, and the difference is deliberate.
- **Neither a lifecycle policy nor image scanning is required.** gagent depends on neither, and this section described both as settings the account holds until ADR 0020 was taken. Expiry is the wrong behaviour for the production three, which hold released versions, and nothing in the bring-up two is a released version for it to act on. Either control, run for that account's own hygiene, is its to decide — not this project's to require, and not this document's to describe.

**A consumer references an image by digest and sets `imagePullPolicy: Always`** (ADR 0020). The digest, because a tag's stability rests on a repository setting a consumer cannot check from outside. `Always`, because an image a reference names can be deleted from the repository holding it, so the failure to be loud about is a reference that stops resolving — which the default `IfNotPresent` turns into a per-node stale-cache fault that presents as the node's.

**The tools image is referenced the same way and mounted rather than run.** `gagent-operator` names it in `--agent-tools-image`, carries it on the agent Pod as an `image` volume pulled on every start, mounts it read-only at `/opt/gagent/tools`, and sets `GAGENT_TOOLS_DIR` to that path (their ADR 0019, 2026-09-04). So the reference a consumer pins is a digest exactly as for the other two, and the cluster owes the kubelet's `ImageVolume` feature — a cluster where it is off accepts the field and mounts nothing. What that digest fixes about the tree it names, and what it leaves to the operator's pin set, is `security.md` §Boundary posture on tool inputs and outputs.

**What the tools image carries.** Its whole filesystem is the tools directory: `<name>/<name>` beside `<name>/manifest.json` at the root, and nothing else, because the mount places the image root at the mount path and every other top-level entry would appear as an entry of that directory. Every file is owned by uid 0 and is readable and traversable by everyone, writable by none, with the execute bit only on the tool executables. **What makes those executables runnable by the agent's uid 65532 is the mode and not the ownership**, and that is load-bearing rather than incidental: a Pod admitted under the `restricted` Pod Security Standard holds no capabilities at all, so it has no `CAP_DAC_OVERRIDE` to read past an owner-only mode — while a container runtime's default capability set does, which makes an owner-only tree look correct outside a cluster and fail inside one.

**Tag scheme**: a `v`-prefixed git tag pushed to GitHub triggers the release, which pushes the image tagged without the `v` — `v0.0.1` in git becoming `0.0.1` in ECR.

**Base images are pinned by digest.** Every `FROM` in `build/` that names an image names `tag@sha256:...`, which is what `docs/convention/ci.md` §Verify a pinned dependency before adopting it requires of a container image; the tag stays for readability and the digest is what resolves. One `FROM` names no image: the tools image's final stage is `scratch`, the absence of a base rather than a base to pin, with nothing pulled and nothing for a digest to fix. Two builds of one commit therefore pull the same base bytes. What moves those digests is `.github/dependabot.yml`, and it covers less than the whole job — see §Open questions.

**What the local build produces.** `make build-images` tags each image with the same revision it labels it with — `gagent-agent:<commit>`, `gagent-workspace:<commit>` and `gagent-tools:<commit>`, never `:latest` — so an image cannot be tagged one commit and labelled another (#693). Where that revision is the git default, the build compares what docker reads against it first — the build contexts, the three Dockerfiles, and the two ignore files that decide what each context holds — and refuses, naming the paths, where they differ; a build that passes its own `REVISION=` is stating its own claim and is not compared (#696, #698). The `docker` job in CI passes one on every run, so the comparison is a contributor's guard rather than a pipeline's. It also builds with buildx's default attestations off, so each image is one manifest rather than an index over one: an attestation is carried as a manifest of its own, and a digest naming the index that holds both is not a digest naming the image (ADR 0020). Whether the default would have applied depends on the host's image store, so the release reads the pushed object back rather than trusting the build.

**The release workflow.** `.github/workflows/release.yml` is the release path to the production three. It is not what put the images they hold there: every one of those was pushed by hand, and the workflow's own push has never run — the record is below. It triggers on a pushed tag and on nothing else — no branch, no pull request — refuses a tag that is not `vX.Y.Z` before it reaches AWS, builds all three images through `make build-images`, and only then pushes them. Every one is built before any is pushed, because the three are a set that has to agree (ADR 0017) and a version whose later image failed after an earlier one was pushed cannot be repaired under an immutable name, only abandoned. It then reads each pushed reference back from the registry and reports the digest, so what a consumer pins is what the registry holds rather than what a local rebuild would produce.

It reaches the registry as `arn:aws:iam::486152169996:role/garam-dev-gagent-ecr-push`, assumed by OIDC. That role's trust policy admits one subject — `repo:garamsh@307152666/gagent@1311139677:ref:refs/tags/v*` — and its grant reaches the production three and no others, so no branch, no pull request and no other repository obtains a credential for them. The account records no use of that role, so nothing has assumed it yet and the push step below it has never run against the registry. Read from the account 2026-09-05T20:47+09:00; nothing here re-checks it.

**What a role that publishes here needs.** Seven actions:

- `ecr:BatchCheckLayerAvailability`, `ecr:InitiateLayerUpload`, `ecr:UploadLayerPart` and `ecr:CompleteLayerUpload`, on each repository it publishes to — the layer upload.
- `ecr:BatchGetImage`, on the same repositories. It is documented as a pull action rather than a push one, and buildx calls it before writing a manifest, so a set assembled from a documented push list is short by exactly this one.
- `ecr:PutImage`, on the same repositories, and `ecr:GetAuthorizationToken`, which takes no resource and is granted on `*`.

**A missing `BatchGetImage` fails late, and no review of the policy catches it.** The push is denied at `PutImage`, after every layer has uploaded, and a reviewer reading that policy against a documented push list finds nothing missing. Only an execution separates the two halves of an unexercised role, because a denial and a refused trust are indistinguishable until one of them happens.

That denial is the garam PM's, from their first publish on 2026-09-01, established from that push's CloudTrail record rather than from documentation and relayed here; this repository has not reproduced it. The action list above and its order are read here from CloudTrail's record of the tools image's push, 2026-09-05T17:15:35–37+09:00, where `BatchGetImage` falls between the last `CompleteLayerUpload` and `PutImage`. That push carried an administrator's credential, so it shows which calls a push makes and not that each one is necessary. The surface moves and nothing here watches it, so confirm the set against a real push's CloudTrail record before writing a policy from this list.

**Every reversible step comes before the irreversible one.** A git tag can be deleted and re-pushed; a tag in an `IMMUTABLE` repository cannot, so the push is the only step of a release that consumes a permanent name. Everything that can fail belongs before it — the version guard, the build of all three images, the role assumption, the registry login — and a failure in any of them costs a re-run and nothing else. The read-back is the exception and cannot be otherwise, because there is nothing to read before the push: a failure there says the registry does not hold what the run meant to publish, never that nothing was published.

**What the production three hold, and how it got there.** Every image in them was pushed by hand, with a person's own credentials rather than through the workflow: the `0.1.0` pair on the owner's instruction, the tools image with the owner's approval. Read from the registry 2026-09-05T20:47+09:00, each revision from that image's own `org.opencontainers.image.revision` label; nothing here re-checks any of it.

| Repository | Tag | Revision | Pushed | Digest |
|---|---|---|---|---|
| `garam/gagent-agent` | `0.1.0` | `b61451e` | 2026-09-03T19:54:52+09:00 | `sha256:8e8508a086a9707913c417a922e5c1da0fdb0c3267f7f3903d1a857668dce1a5` |
| `garam/gagent-workspace` | `0.1.0` | `b61451e` | 2026-09-03T19:54:54+09:00 | `sha256:ce3256dcd5ac46ceaa9d5e445da69998728b123f86bca92b4001185cfa0e6a7e` |
| `garam/gagent-tools` | `4631159e234b` | `4631159` | 2026-09-05T17:15:37+09:00 | `sha256:b2a281b2d884ec4e00ed734ed71b439b3ffad38d1961c16d31971231bb1672cd` |

Each is a single manifest and not an index over one, so each of those digests names the image (ADR 0020). No git tag names any of them, and this repository holds no tag at all, so the revision column is the only record here of which commit a published version was built from.

**Why by hand is reported and not established here.** The reason given for the `0.1.0` pair is that GitHub Actions was blocked account-wide on billing, so no workflow could run; the tools image took the same route two days later. Nothing in this repository confirms that, at the time of either push or since. The last run it holds is [33709320205](https://github.com/garamsh/gagent/actions/runs/33709320205), a push to `main` on 2026-09-03T02:53Z that completed successfully, and `.github/workflows/ci.yml` triggers only on `main`, so the absence of runs after it is what that trigger already predicts and is evidence of nothing. What is not in question is whether the release workflow was tried and failed: no tag has ever existed, so nothing has triggered it.

**No `v0.1.0` tag is to be pushed.** Both repositories holding `0.1.0` are `IMMUTABLE`, so the workflow would build all three images, assume the role and be refused at the first `PutImage` — the registry declining a second push of a name, which is the protection working, in a run that publishes nothing and reports on nothing. The next release takes the next version. That is an operational call and it is revisable: the tag can still be created later if someone wants the run, at the cost of a failed one.

**Nothing creates the tag.** The workflow reads a tag that was pushed by hand and creates none, and no other automation in this repository creates one either. `make build-images` builds all three images locally and pushes nothing, and the `docker` job in `.github/workflows/ci.yml` builds them as a smoke check and pushes nothing.

**When the three images disagree, the system accepts them.** Nothing refuses and nothing warns. No revision reaches a running process: `REVISION` becomes the image tag and the `org.opencontainers.image.revision` label and is read by nothing the binary runs, and there is no handshake in either direction — the agent does not ask the workspace what it was built from, and the tool registry does not ask the tree it loads. Agreement is a property of how the three are published rather than something checked at run time: one `make build-images` invocation builds all three from one commit, and one release run pushes all three under one version, so a consumer pinning the three digests that run reported holds three images from one commit.

Two startup refusals sit next to this one and neither is it (ADR 0009 invariants 2 and 5). One depends on the operator's configuration: where a pin set is configured, the tool registry refuses a manifest whose hash is not in it, and an agent configured without one loads what the tree holds. The other depends on no configuration at all: the registry refuses to start without `message_send`. Neither reaches further than the manifests — not the executables beside them, and not which revision the agent image reading them was built from.

**Where the missing check would go, if it is wanted.** The tools image would record the revision it was built from at its own root, where the registry's directory walk skips it because it is not a directory, and the registry would compare that against a revision the agent binary carries. The binary carries none today, so it is a two-part change; whether a mismatch should refuse or warn is a decision nobody has taken, and nothing here implements either.

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
- **Whether the dev cluster can pull what a release publishes — closed on both halves.** The credential half: `garam-dev-ecr-pull` in this account is assumed by web-identity federation against a self-hosted OIDC issuer — the on-premises equivalent of IRSA — in sessions of at most an hour, so the twelve-hour authorization-token expiry this question rested on is met by that path rather than by someone remembering. The mechanism is `garamsh/infra`'s and not this repository's. **The reach half is answered by an execution rather than by a policy.** That role's inline pull policy, which is its whole grant, names all three of the repositories §Images and tags calls production, and CloudTrail records fetches through the role against `garam/gagent-agent` (first 2026-09-04T05:29+09:00) and `garam/gagent-tools` (first 2026-09-05T18:43+09:00) — so an image a release publishes is one the cluster's node credential provider does fetch and not merely one it is permitted to fetch. It records none against `garam/gagent-workspace`: a repository nothing has pulled through that role, rather than one the grant leaves out. Read 2026-09-05T20:47+09:00 from the role, its trust policy, its inline pull policy and CloudTrail. Nothing in this repository re-checks any of it, so re-read it before relying on either half — what this bullet asserts about that account has gone stale twice since 2026-08-31, each time with nothing failing to signal it.
- **Agent-to-sidecar authentication.** The workspace's gRPC listener ships pod-local and unauthenticated, which is what forces the loopback-only rule above (`security.md`, `environment.md`).
- **Whether the gateway should offer a durable accept.** What a restart drops is not open: the queue is held in memory, so a message accepted but not yet drained is lost (§Shutdown and the grace period, `gateway.md`). What is open is whether the 202 should promise more than "queued in this process", and what an accept that survives a restart would cost (#492).
