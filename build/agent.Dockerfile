# The agent image (ADR 0017, ADR 0021). The workspace image is a separate
# file beside this one; the two stay separate images (#512).
#
# The build context is the repository root — the build stage copies the
# module — and `make build-images` is the entry point that builds both.
# First-party tools are baked into this image at /opt/sherlock/tools/
# (ADR 0021); no separate sherlock/tools image is published.

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

# First-party tool binaries (ADR 0021). Each `tools/<name>/` holding a
# manifest becomes `/out/tools/<name>/<name>` (the executable) and
# `/out/tools/<name>/manifest.json` (the contract). The build fails
# closed if no tool is present or if the required `message_send` is
# missing (ADR 0009 invariant 5). All CGO-free, statically linked; the
# agent image's distroless base has no libc, no shell, no Go runtime.
RUN set -eu; \
    built=0; \
    for dir in ./tools/*/; do \
        name=$(basename "$dir"); \
        [ -f "$dir/manifest.json" ] || continue; \
        install -d "/out/tools/$name"; \
        CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "/out/tools/$name/$name" "$dir"; \
        [ -x "/out/tools/$name/$name" ] || { echo "tools: go build wrote /out/tools/$name/$name without the execute bit; ./tools/$name/ must be a main package" >&2; exit 1; }; \
        cp "$dir/manifest.json" "/out/tools/$name/manifest.json"; \
        built=$((built + 1)); \
        echo "tools: built $name"; \
    done; \
    [ "$built" -gt 0 ] || { echo "tools: no tools/<name>/ holding a manifest.json reached the build context; the agent image would carry nothing" >&2; exit 1; }; \
    [ -x /out/tools/message_send/message_send ] || { echo "tools: message_send is missing, and the platform refuses to start without it (ADR 0009 invariant 5)" >&2; exit 1; }; \
    echo "tools: $built tools built"

# Permissions: every tool directory 0755, every executable 0755, every
# manifest 0644. Mode bits, not ownership, decide who can run the tool —
# the agent image runs as uid 65532 with no DAC_OVERRIDE capability
# under the restricted Pod Security Standard, and an owner-only tree
# would refuse the agent the read of its own tool
# (`docs/architecture/deployment.md` §`What the tools image carries`).
RUN find /out/tools -type d -exec chmod 0755 {} + \
    && find /out/tools -type f -name manifest.json -exec chmod 0644 {} + \
    && find /out/tools -type f ! -name manifest.json -exec chmod 0755 {} +

# Agent image: the agent executes nothing — the workspace owns process
# exec (ADR 0017) — so the agent runs no untrusted code and distroless
# static stays correct. Serves the gateway, the rolling session, memory,
# the mouth, and the tool client, which reads the operator's tool
# manifests and spawns each tool's executable per call. Tool executables
# ship in this image at /opt/sherlock/tools/<name>/<name> (ADR 0021).
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS agent
ARG REVISION=unknown
LABEL org.opencontainers.image.title="sherlock-agent" \
      org.opencontainers.image.source="https://github.com/garamsh/sherlock" \
      org.opencontainers.image.revision="${REVISION}"

COPY --from=build /out/sherlock /sherlock
# Writable data directory for the nonroot user (uid 65532); mount the
# memory volume here.
COPY --from=build --chown=65532:65532 /out/data /data
# First-party tools, baked in (ADR 0021). Owned by the agent's uid so
# the kernel permission check at exec time is the agent's own uid owning
# the file; mode bits are the cross-uid fallback. A ConfigMap or image
# volume mounted at /opt/sherlock/tools overrides individual entries.
COPY --from=build --chown=65532:65532 /out/tools /opt/sherlock/tools

USER nonroot
ENV SHERLOCK_TOOLS_DIR=/opt/sherlock/tools
# Relative config paths resolve here: `tools-dir`'s default `./tools` is
# /home/nonroot/tools. The base sets the same value; stating it means a
# base digest bump cannot move it.
WORKDIR /home/nonroot
# Config comes from SHERLOCK_* env vars or a mounted config file
# (`--config <path>`); no config is baked in.
ENTRYPOINT ["/sherlock"]
CMD ["agent"]
