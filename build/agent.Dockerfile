# The agent image (ADR 0017). The workspace image is a separate file
# beside this one, and the two stay separate images (#512).
#
# The build context is the repository root — the build stage copies the
# module — and `make build-images` is the entry point that builds both.

FROM golang:1.27@sha256:65b6f280bf050ec5af12716857e8ea8439d694dbba8f31ceeb7630670071f2bb AS build
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
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS agent
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
