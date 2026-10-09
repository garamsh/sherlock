# ADR 0010: One execution boundary — agent-influenced effects under the unprivileged UID

> Status: accepted
> Date: 2026-08-02 (revised in place 2026-10-10)

## Context

Two paths carry agent-influenced effects: file writes through the workspace data plane (ADR 0017) and sub-processes spawned by the workspace's executor behind the Exec RPC (ADR 0008). They must share one trust boundary, not drift into per-path privileges.

## Decision

All agent-influenced effects land under the unprivileged UID; only operator-sanctioned tools reach the network.

- **File writes** run under the sidecar's own UID with kernel-enforced ownership (DAC), not filters — the data plane is the action site for files (ADR 0017). Performing a write and owning the result are separate: which account a contract-written file ends up under is ADR 0008's, not this record's.
- **Executor children** carry the UID drop, scrubbed environment, and resource limits of ADR 0008. Network isolation is per-request (`NoNetwork`).
- **`NoNetwork` has one meaning in every supported shape and environment: the child lands in a fresh, empty network namespace of its own, with no interface but its own loopback.** The privileged drop creates it with an unshare under `SYS_ADMIN`; the unprivileged shape (ADR 0008) creates it inside a user namespace with the workspace's uid mapped to itself, which needs no capability and needs a host that permits the clone. That makes two host requirements of the unprivileged shape, under Kubernetes and Docker standalone alike, both infra artifacts and both the deployment's to meet: `user.max_user_namespaces` above `0`, which a Pod's security context does not set and whoever builds the node does; and a seccomp profile admitting `unshare` and `clone` with `CLONE_NEWUSER|CLONE_NEWNET`. `RuntimeDefault` does not admit it (ADR 0008, Context), so under PodSecurity `restricted`, which admits `RuntimeDefault` and `Localhost` profiles only, the second is a `Localhost` profile installed on every node and named by the workspace container's own `seccompProfile` — never the Pod's, which would loosen the agent container as well; the Pod and the agent container stay on `RuntimeDefault`. Under Docker standalone the profile is given to the workspace container alone. Meeting the sysctl alone leaves every `NoNetwork` call refused; an LSM policy can refuse the clone as well. The Pod's network and a NetworkPolicy, or a Docker container's network mode, bound what the whole Pod or container reaches; they are an additional mechanism and never stand in for `NoNetwork`.
- **Where the clone is refused, a `NoNetwork` call is refused, never served.** It reports `agent_uid unavailable`, the same as a privileged workspace without `SYS_ADMIN`. `tools/shell_exec` asks for `NoNetwork` on every call, so on such a host every agent-influenced Exec is refused while the file contract keeps working. The workspace states at startup, in either shape, whether it can serve `NoNetwork`, whatever would refuse it — a missing capability, the sysctl, a seccomp or LSM denial — so a deployment learns this from its log before the first call rather than from that call.
- **Resource caps bind untrusted code only.** Agent-influenced, untrusted children carry the executor's rlimits (CPU, address space, file size, open files, NPROC in the privileged drop). Operator-chosen tool packages run without them: their bound is the deployment's container/pod resource policy (the orchestration layer owns that policy, ADR 0014/ADR 0017), not in-process rlimits. The trust class is explicit in the executor's request, never inferred from the network axis.
- When the platform cannot run a child as the configured account, the executor reports `agent_uid unavailable`; it never silently upgrades. A silent privilege upgrade is worse than an unavailable capability.

## Consequences

- One trust domain for agent-influenced effects: the configured account, kernel DAC, no ambiguity about which account an effect lands under. In the unprivileged shape that domain is the workspace process's own account — an explicit operator declaration, never an implicit fallback — so the boundary encloses the workspace process too: what ADR 0008 states that shape does not separate is the price of reaching PodSecurity `restricted`.
- In the pod topology both sides of this boundary live inside the sidecar: the workspace owns the files and the executor (ADR 0017); the agent executes nothing.
- The Playwright example keeps working: browser automation is an operator-sanctioned egress channel — the operator chooses a tool's reach by choosing the tool.

## Rejected alternatives

- **Network-isolating operator-sanctioned tools too** — breaks the flagship use case (browser automation *is* network egress); the tool is the sanctioned channel, unlike agent-influenced exec.
- **Per-tool `uid` option in the config** — an option nobody needs yet (`simplicity.md` §Rules); it arrives with its first use case.
- **Silent privileged fallback** — a privilege upgrade the operator never sees; the hard-fail contract is the honest one.
- **The Pod's network and a NetworkPolicy, or a Docker container's network mode, as what `NoNetwork` means in the unprivileged shape** (#1049, option B in the consumers' form). The workspace is a same-pod sidecar sharing the agent container's network namespace (ADR 0017; `deploy/local/docker-compose.yml` sets `network_mode: service:agent`). A child left in it reaches the agent's gateway over loopback, where it serves unauthenticated unless `tls:` is set (ADR 0012), and every destination the Pod's egress allows, which has to include the agent's own model, embeddings and key-set endpoints. A NetworkPolicy selects Pods, so it cannot tell the child from the agent container. Serving `NoNetwork` that way without the operator declaring it is the silent upgrade rejected above, and declaring it needs a new setting for a weaker meaning of the same request.
- **A sandboxed runtime class as the execution boundary** (#1049, option C). The boundary would live in a runtime this repository neither ships nor can test, and each environment would need its own. Nothing here stops a deployment adding one beneath either shape.
