# The workspace image (ADR 0017). The agent image is a separate file
# beside this one, and the two stay separate images (#512).
#
# The build context is the repository root — the build stage copies the
# module — and `make build-images` is the entry point that builds both.

FROM golang:1.26@sha256:0d1d3a794be25f809dd2cb3160d8c73276c4056a9f8242a138e908ddeee7b6b6 AS build
WORKDIR /src

# Layer caching: resolve dependencies before copying source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# All dependencies are CGO-free (modernc.org/sqlite is pure Go), so the
# binary is fully static.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gagent ./cmd/gagent \
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
FROM debian:13-slim@sha256:3a39a0592364683e6bab97937b72cad5a8fa6dcbbee90edb3bb48c7f8e94f258 AS workspace
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
