# Platform images (ADR 0018): one build stage, two targets.
#   docker build --target agent     — the agent's self (no untrusted code)
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

# Agent image: runs no untrusted code, so distroless static stays correct
# (ADR 0018). Serves the gateway, the rolling session, memory, the mouth,
# native process exec, and the tools the agent imports at compile time.
FROM gcr.io/distroless/static-debian12:nonroot AS agent
ARG REVISION=unknown
LABEL org.opencontainers.image.title="gagent-agent" \
      org.opencontainers.image.source="https://github.com/garamsh/gagent" \
      org.opencontainers.image.revision="${REVISION}"

COPY --from=build /out/gagent /gagent
# Writable data directory for the nonroot user (uid 65532); mount the
# memory volume here.
COPY --from=build --chown=65532:65532 /out/data /data

USER nonroot
# Config comes from GAGENT_* env vars or a mounted config file
# (`--config <path>`); no config is baked in.
ENTRYPOINT ["/gagent"]
CMD ["agent"]

# Workspace image: the agent's data plane — serves the filesystem-only
# gRPC contract over loopback. Tools live in the agent and reach the
# workspace through gRPC; the workspace hosts no tools, no process
# executor, no plugin loader (ADR 0018).
#
# Minimal glibc base with the privilege drop made real: a privileged
# workspace chowns its data directory to the configured exec-uid at
# startup. Tools that exec children do so in the agent, with
# capabilities the executor manages; the workspace's job is just the
# data plane.
FROM debian:12-slim AS workspace
ARG REVISION=unknown
LABEL org.opencontainers.image.title="gagent-workspace" \
      org.opencontainers.image.source="https://github.com/garamsh/gagent" \
      org.opencontainers.image.revision="${REVISION}"

# The unprivileged UID workspace ownership drops to (ADR 0008).
# useradd comes from the passwd package, which ships setuid helpers, so
# the hygiene strip runs after it (security.md: no setuid/setgid binaries
# at all — the privilege drop is the executor's job, not a suid helper's).
RUN useradd --system --uid 1001 --no-create-home agent \
    && find / -xdev -type f -perm /6000 -exec chmod a-s {} +

COPY --from=build /out/gagent /gagent
# Scratch space for the default workspace (data/workspace); mount the
# workspace volume here.
COPY --from=build /out/data /data

# Root with the four capabilities above: the workspace chowns its
# directory to the agent UID (UID 1001, created as `agent` above) on
# privileged runs; on single-account runs the workspace runs as its
# own UID and the chown is a no-op.
USER root
ENTRYPOINT ["/gagent"]
CMD ["workspace"]
