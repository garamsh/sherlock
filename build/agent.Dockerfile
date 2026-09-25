# The agent image (ADR 0017). The workspace image is a separate file
# beside this one, and the two stay separate images (#512).
#
# The build context is the repository root — the build stage copies the
# module — and `make build-images` is the entry point that builds both.

FROM golang:1.27@sha256:4013ae0f9e7994f8535c58c811f8f863fbed38b72e0d51e6592156f758d66146 AS build
WORKDIR /src

# Layer caching: resolve dependencies before copying source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# All dependencies are CGO-free (modernc.org/sqlite is pure Go), so the
# binary is fully static.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sherlock ./cmd/sherlock \
    && mkdir -p /out/data

# Agent image: the agent executes nothing — the workspace owns process
# exec (ADR 0017) — so the agent runs no untrusted code and distroless
# static stays correct. Serves the gateway, the rolling session, memory,
# the mouth, and the tool client, which reads the operator's tool
# manifests and spawns each tool's executable per call.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS agent
ARG REVISION=unknown
LABEL org.opencontainers.image.title="sherlock-agent" \
      org.opencontainers.image.source="https://github.com/garamsh/sherlock" \
      org.opencontainers.image.revision="${REVISION}"

COPY --from=build /out/sherlock /sherlock
# Writable data directory for the nonroot user (uid 65532); mount the
# memory volume here.
COPY --from=build --chown=65532:65532 /out/data /data

USER nonroot
# Relative config paths resolve here: `tools-dir`'s default `./tools` is
# /home/nonroot/tools. The base sets the same value; stating it means a
# base digest bump cannot move it.
WORKDIR /home/nonroot
# Config comes from SHERLOCK_* env vars or a mounted config file
# (`--config <path>`); no config is baked in.
ENTRYPOINT ["/sherlock"]
CMD ["agent"]
