# Platform images (ADR 0018): one build stage, two targets.
#   docker build --target agent   — the agent's self (no untrusted code)
#   docker build --target sidecar — the agent's computer (shells, tools)

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
# (ADR 0018). Serves the gateway, the rolling session, memory, and the
# mouth; discovers its computer tools from the pod's sidecar.
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

# Sidecar image: the agent's computer — runs shells and tool executables,
# so distroless is wrong for it (no /bin/sh, no tool runtimes). Minimal
# glibc base with the privilege drop made real: start the container with
# CAP_SETUID, CAP_SETGID, CAP_CHOWN, and CAP_SYS_ADMIN (the netns
# unshare) and nothing else — see deploy/ (ADR 0008, ADR 0010).
FROM debian:12-slim AS sidecar
ARG REVISION=unknown
LABEL org.opencontainers.image.title="gagent-sidecar" \
      org.opencontainers.image.source="https://github.com/garamsh/gagent" \
      org.opencontainers.image.revision="${REVISION}"

# The unprivileged UID agent-influenced execution drops to (ADR 0008).
# useradd comes from the passwd package, which ships setuid helpers, so
# the hygiene strip runs after it (security.md: no setuid/setgid binaries
# at all — the privilege drop is the executor's job, not a suid helper's).
RUN useradd --system --uid 1001 --no-create-home agent \
    && find / -xdev -type f -perm /6000 -exec chmod a-s {} +

COPY --from=build /out/gagent /gagent
# Scratch space for the default workspace (data/workspace); mount the
# workspace volume here.
COPY --from=build /out/data /data

# Root with the four capabilities above: the executor drops every child
# to the agent UID itself (UID 1001, created as `agent` above).
USER root
ENTRYPOINT ["/gagent"]
CMD ["sidecar"]
