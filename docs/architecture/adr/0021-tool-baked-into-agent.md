# ADR 0021: First-party tool executables are baked into the sherlock-agent image; no separate sherlock/tools image is published

> Status: accepted
> Date: 2026-09-25

## Context

sherlock ships six first-party tools from this repository — `file_read`, `file_write`, `file_list`, `shell_exec`, `message_send`, `github` — each as a `tools/<name>/` directory holding a `manifest.json` and a Go executable the platform spawns per call (ADR 0009). They are built from the same commit as the agent binary and were distributed through a third OCI image, `sherlock/tools`, published to ECR and intended to be mounted into the agent Pod as an image volume by `sherlock-operator` (their ADR 0019, 2026-09-04).

That third image was, and is, dead weight.

The sherlock-operator's own documentation records the reason it has never been consumed in any deployment (`garam-agent-operator/docs/architecture/configuration.md:58` and `agent.md:152`, read 2026-09-25): *"no image exists for `--agent-tools-image` to name"*. The operator's `admin@garam-dev` Pod has run without a tool tree since the operator existed; the role that would push it has no scope to do so (`garamsh/infra#124`); and the operator's sherlock-typed path has no operator-customised tool set to distribute. Every consumer would have been sherlock itself, but sherlock never wired the agent to mount it: the local Compose deployment mounts the host's `tools/` directory instead, and the operator's documented intent is the same shape.

The case for separating the tool tree from the agent image rested on three legs:

- **A separate update path.** Tools could change without an agent image rebuild. In sherlock's case, the tool set is first-party and versioned with the agent binary from one commit, under one release tag (`ADR 0020` and the release workflow); there is no operator-customised tool set the operator would want to advance independently.
- **Operator-installed third-party tools.** A platform user can drop a directory under `tools/<name>/` and pin its manifest hash (ADR 0009). The distribution mechanism for that operator's tools is the operator's `tools/` directory on the host they build on, not an image — the agent image's `/opt/sherlock/tools/` is the runtime view, and a `ConfigMap` or emptyDir volume at that path is the runtime override.
- **A cleaner operator contract.** The operator declares which tools an agent gets, separately from the agent image. In sherlock the agent is the self (ADR 0017) and a tool is the agent's own capability; the operator's contract is the manifest pins, not the distribution channel.

What remains after this decision is the same first-party tool set, versioned the same way, executed the same way, with two paths an operator can take to extend it: rebuild `sherlock-agent` from this Dockerfile with extra tool directories copied into the build stage, or mount `/opt/sherlock/tools` at runtime with the additions.

## Decision

**First-party tool executables are baked into the `sherlock-agent` image at `/opt/sherlock/tools/<name>/` at build time. No `sherlock/tools` image is built, pushed, or consumed. `SHERLOCK_TOOLS_DIR` is set to `/opt/sherlock/tools` in the image.**

The agent image's `build/agent.Dockerfile` builds the tool executables in its `golang:1.27` build stage, copies them into `/out/tools/<name>/<name>` and the manifests into `/out/tools/<name>/manifest.json`, sets permissions (directories `0755`, executables `0755`, manifests `0644`), and the agent stage `COPY`s the tree to `/opt/sherlock/tools/` owned by `uid 65532:65532`. The agent image runs as `uid 65532` (distroless `nonroot`); the mode bits are what make the executables runnable by that uid under the `restricted` Pod Security Standard, where no `CAP_DAC_OVERRIDE` reaches past an owner-only tree (`docs/architecture/deployment.md` §`What the tools image carries`, in the new prose under §Images and tags).

The build fails closed at compile time when no tool directory reaches the context (`built == 0`) or when the required `message_send` is missing (ADR 0009 invariant 5).

A consumer that wants a different tool set extends `sherlock-agent` in their own Dockerfile, or mounts `/opt/sherlock/tools` at runtime — a ConfigMap, an emptyDir, or another image's volume — and the mount's contents override the baked-in entries by inode. The agent image carries the default first-party set either way; the runtime path is the operator's.

The local Compose deployment keeps the host-directory mount at `/opt/sherlock/tools` so a contributor's edit to `tools/<name>/<name>` or `manifest.json` is reflected on the next container restart without rebuilding the image. With the mount absent, the image carries the tools baked at build time.

## Consequences

- **One image, one less ECR repository.** The release workflow builds two images; the digest verification loop iterates over the two; `sherlock/agent`, `sherlock/workspace`, `sherlock/agent-dev`, `sherlock/workspace-dev` are the four ECR repositories this project binds (`ADR 0020`, in the new prose). The `sherlock/tools` repository is not deleted by this record — that is `garamsh/infra`'s action and is noted as an open follow-up in `deployment.md`.
- **The operator's `--agent-tools-image` flag is no longer required for sherlock agents.** It is owned by `garam-agent-operator` and stays for that operator's other agent types. The operator's ADR 0019 keeps the flag in their contract; their sherlock-only path may drop it in their own PR.
- **A tool change is an agent image change.** Every edit under `tools/<name>/` rebuilds `sherlock-agent`. The edit is committed next to the rest of the agent binary, which is what every other change under `cmd/`, `internal/`, `configs/` already does. The Makefile's `build-tools` target stays for local tool compilation — contributors run `make build-tools` to update `tools/<name>/<name>` in their worktree without rebuilding the image.
- **The agent image gains ~50 MB.** Six statically-linked Go binaries, with manifests; the agent image's distroless base does not change. The image stays under 100 MB, which is the bound the operator's pod-admission review reads.
- **A customisation path is now strictly simpler.** A sherlock user adding their own tool extends `sherlock-agent` from a `FROM sherlock-agent:<tag>` Dockerfile with a `COPY tools/<name>/ /opt/sherlock/tools/<name>/` step. The result is one image, one tag, one pull. The previous path required a second image and the operator wiring that mounts it, with no real consumer recorded.

## Rejected alternatives

- **Keep `sherlock/tools` for those who want it.** An opt-in third image leaves the build pipeline carrying a dead branch and the ECR account carrying an extra repository, and the operator ADR's own admission that no deployment uses it is the strongest evidence the demand is not there. The customisation paths above let an operator rebuild a different tool tree without this project carrying one.
- **Bake into the workspace image.** The workspace image is the data plane (ADR 0017) and runs root with a shell because Exec children need one; a tool tree there would move tools across the trust boundary from the agent to the sidecar. The agent holds the secrets the tools inherit (ADR 0009); tools belong on the agent's side.
- **Bake into a base image that the agent extends.** A two-stage `FROM` chain (sherlock-tools-base → sherlock-agent) recovers the modularity the third image had at the cost of a base image to publish, version and audit. The customisation paths above carry less weight than the dead weight of a base image, and the operator's `${SHERLOCK_DATA_DIR}:${SHERLOCK_DATA_DIR}` volume mount is already a cleaner point of extension than another image.
- **`sherlock-operator`'s ADR 0019 — a separate image for the tool tree.** Right for the operator's general case (a tool set an operator chooses to override without rebuilding the agent), wrong for sherlock specifically (tools are versioned with the agent). The operator's ADR stays in force; sherlock does not adopt it for sherlock-typed agents, and the operator is free to keep the flag for non-sherlock agents.
