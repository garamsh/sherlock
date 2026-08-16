# Platform images (ADR 0017): one build stage, two targets.
#   docker build --target agent     — the agent's self (gateway, loop, tools)
#   docker build --target workspace — the agent's data plane (filesystem + exec gRPC)

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

# Agent image: the agent executes nothing — the workspace owns process
# exec (ADR 0017) — so the agent runs no untrusted code and distroless
# static stays correct. Serves the gateway, the rolling session, memory,
# the mouth, and the tool client, which reads the operator's tool
# manifests and spawns each tool's executable per call.
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

# Workspace image: the agent's data plane — serves the gRPC contract
# (Read/Write/List/Exec) over loopback. Tools live in the agent and
# reach the workspace through gRPC; the workspace hosts no tools and no
# plugin loader (ADR 0017).
#
# Minimal glibc base with a shell (Exec children are `sh -c`
# sub-processes) and the privilege drop made real: a privileged
# workspace chowns its data directory to the configured exec-uid at
# startup and drops Exec children to it (ADR 0008).
FROM debian:12-slim AS workspace
ARG REVISION=unknown
LABEL org.opencontainers.image.title="gagent-workspace" \
      org.opencontainers.image.source="https://github.com/garamsh/gagent" \
      org.opencontainers.image.revision="${REVISION}"

# The unprivileged UID exec children drop to (ADR 0008). useradd comes
# from the passwd package, which ships setuid helpers, so the hygiene
# strip runs after it (security.md: no setuid/setgid binaries at all —
# the privilege drop is the executor's job, not a suid helper's).
RUN useradd --system --uid 1001 --no-create-home agent \
    && find / -xdev -type f -perm /6000 -exec chmod a-s {} +

COPY --from=build /out/gagent /gagent
# Scratch space for the default workspace (data/workspace); mount the
# workspace volume here.
COPY --from=build /out/data /data

# Root with four capabilities (deploy/local/docker-compose.yml):
# SETUID/SETGID for the child credential drop, CHOWN for the workspace
# directory handover, SYS_ADMIN for the netns unshare (ADR 0010). On
# single-account runs the workspace runs as its own UID and the drop is
# a no-op.
USER root
ENTRYPOINT ["/gagent"]
CMD ["workspace"]
