# The workspace image (ADR 0017). The agent image is a separate file
# beside this one, and the two stay separate images (#512).
#
# The build context is the repository root — the build stage copies the
# module — and `make build-images` is the entry point that builds both.

FROM golang:1.27@sha256:e432b43af23a9328d56a7c499be0476810aa344acbcf65fc7c455d4ff5a40602 AS build
WORKDIR /src

# Layer caching: resolve dependencies before copying source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# All dependencies are CGO-free (modernc.org/sqlite is pure Go), so the
# binary is fully static.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sherlock ./cmd/sherlock \
    && mkdir -p /out/data

# Workspace image: the agent's data plane — serves the gRPC contract
# (Read/Write/List/Exec) over loopback. Tools live in the agent and
# reach the workspace through gRPC; the workspace hosts no tools and no
# plugin loader (ADR 0017).
#
# Minimal glibc base with a shell (Exec children are `sh -c`
# sub-processes) and the privilege drop made real: a privileged
# workspace chowns its data directory to the configured exec-uid at
# startup and drops Exec children to it (ADR 0008).
FROM debian:13-slim@sha256:a29215f6a35e51e22adffa17f89e9d2ef06214e64a2bad10d765c46aea49f11f AS workspace
ARG REVISION=unknown
LABEL org.opencontainers.image.title="sherlock-workspace" \
      org.opencontainers.image.source="https://github.com/garamsh/sherlock" \
      org.opencontainers.image.revision="${REVISION}"

# The unprivileged UID exec children drop to (ADR 0008). useradd comes
# from the passwd package, which ships setuid helpers, so the hygiene
# strip runs after it (security.md: no setuid/setgid binaries at all —
# the privilege drop is the executor's job, not a suid helper's).
RUN useradd --system --uid 1001 --no-create-home agent \
    && find / -xdev -type f -perm /6000 -exec chmod a-s {} +

COPY --from=build /out/sherlock /sherlock
# Scratch space for the default workspace (data/workspace); mount the
# workspace volume here.
COPY --from=build /out/data /data

# Root with four capabilities (deploy/local/docker-compose.yml):
# SETUID/SETGID for the child credential drop, CHOWN for the workspace
# directory handover, SYS_ADMIN for the netns unshare (ADR 0010). On
# single-account runs the workspace runs as its own UID and the drop is
# a no-op.
USER root
ENTRYPOINT ["/sherlock"]
CMD ["workspace"]
