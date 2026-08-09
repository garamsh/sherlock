# Platform images (ADR 0018): one build stage, two targets.
#   docker build --target agent     — the agent's self (gateway, loop, tools, native exec)
#   docker build --target workspace — the agent's data plane (filesystem gRPC)

FROM golang:1.26 AS build
WORKDIR /src

# Layer caching: resolve dependencies before copying source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# All dependencies are CGO-free (modernc.org/sqlite is pure Go), so the
# binary is fully static.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gagent ./cmd/gagent \
    && mkdir -p /out/data

# Agent image: the agent's self — gateway, rolling session, memory,
# mouth, the compile-time tools, and the native exec backend
# (internal/shell, ADR 0008). shell_exec children are `sh -c`
# sub-processes dropped to the exec UID, so the image needs a shell
# and runs privileged enough to setuid (root by default; an
# unprivileged agent runs single-account mode with exec-uid == its own
# UID).
FROM debian:12-slim AS agent
ARG REVISION=unknown
LABEL org.opencontainers.image.title="gagent-agent" \
      org.opencontainers.image.source="https://github.com/garamsh/gagent" \
      org.opencontainers.image.revision="${REVISION}"

# Hygiene strip (security.md: no setuid/setgid binaries at all).
RUN find / -xdev -type f -perm /6000 -exec chmod a-s {} +

COPY --from=build /out/gagent /gagent
# Scratch space for the default data directory (memory, outbox); mount
# the memory volume here.
COPY --from=build /out/data /data

# Config comes from GAGENT_* env vars or a mounted config file
# (`--config <path>`); no config is baked in.
ENTRYPOINT ["/gagent"]
CMD ["agent"]

# Workspace image: the agent's data plane — serves the filesystem-only
# gRPC contract over loopback. Tools live in the agent and reach the
# workspace through gRPC; the workspace hosts no tools, no process
# executor, no plugin loader (ADR 0018).
#
# Minimal glibc base. The workspace spawns no children: its filesystem
# backend owns the data directory directly, so no privilege drop and no
# extra user exist here. When run privileged it chowns the workspace
# directory to the agent's exec UID (ADR 0008) — that needs CAP_CHOWN.
FROM debian:12-slim AS workspace
ARG REVISION=unknown
LABEL org.opencontainers.image.title="gagent-workspace" \
      org.opencontainers.image.source="https://github.com/garamsh/gagent" \
      org.opencontainers.image.revision="${REVISION}"

# Hygiene strip (security.md: no setuid/setgid binaries at all).
RUN find / -xdev -type f -perm /6000 -exec chmod a-s {} +

COPY --from=build /out/gagent /gagent
# Scratch space for the default workspace (data/workspace); mount the
# workspace volume here.
COPY --from=build /out/data /data

# The workspace needs no Linux capabilities beyond CAP_CHOWN for the
# privileged workspace-directory handover — no child credential drop,
# no unshare. Deployments drop everything else
# (deploy/local/docker-compose.yml).
USER root
ENTRYPOINT ["/gagent"]
CMD ["workspace"]
