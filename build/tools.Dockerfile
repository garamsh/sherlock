# The tools image (ADR 0017, #767). The agent and workspace images are
# separate files beside this one, and the three stay separate images.
#
# This image is never run. It is mounted: `sherlock-operator` names it in
# `--agent-tools-image`, the kubelet mounts it into the agent Pod as a
# read-only image volume, and the whole of this filesystem appears at the
# mount path with `SHERLOCK_TOOLS_DIR` pointing there. So the image root *is*
# the tools directory `internal/tool`'s registry walks — `<name>/<name>`
# beside `<name>/manifest.json`, and nothing else at the root, because
# every other top-level entry would appear inside that directory.
#
# The build context is the repository root and is narrower than the other
# two images' — `build/tools.Dockerfile.dockerignore` owns it — and
# `make build-images` is the entry point that builds all three.

FROM golang:1.27@sha256:4013ae0f9e7994f8535c58c811f8f863fbed38b72e0d51e6592156f758d66146 AS build
WORKDIR /src

# Layer caching: resolve dependencies before copying source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# The tool set is read out of the tree rather than listed here: every
# `tools/<name>/` holding a manifest is one tool, which is the same rule
# `internal/tool`'s registry applies to the directory this image becomes,
# so the image holds what the repository holds.
#
# Nothing is built into `tools/<name>/<name>`, where `make build-tools`
# writes on a contributor's machine and `.gitignore` keeps the result out
# of the tree: the output goes to /out, so a stale local binary that rode
# into the context cannot be the one that ships.
#
# The count is printed and a zero fails, because `for` over a glob that
# matched nothing writes an image with an empty root and exits 0 exactly
# like a run that built every tool (ci.md §One entry point per task).
# `message_send` is checked by name for a second reason: it is the tool
# the platform requires at startup (ADR 0009 §Decision invariant 5), so an
# image without it reproduces the CrashLoopBackOff this image exists to
# end, one layer further in.
#
# All dependencies are CGO-free, so each tool is fully static and runs on
# a base that carries no libc.
RUN set -eu; \
    built=0; \
    for dir in ./tools/*/; do \
        name=$(basename "$dir"); \
        [ -f "$dir/manifest.json" ] || continue; \
        CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "/out/$name/$name" "$dir"; \
        [ -x "/out/$name/$name" ] || { echo "tools: go build wrote /out/$name/$name without the execute bit; ./tools/$name/ must be a main package" >&2; exit 1; }; \
        cp "$dir/manifest.json" "/out/$name/manifest.json"; \
        built=$((built + 1)); \
        echo "tools: built $name"; \
    done; \
    [ "$built" -gt 0 ] || { echo "tools: no tools/<name>/ holding a manifest.json reached the build context; the image would carry nothing" >&2; exit 1; }; \
    [ -x /out/message_send/message_send ] || { echo "tools: message_send is missing, and the platform refuses to start without it (ADR 0009 invariant 5)" >&2; exit 1; }; \
    echo "tools: $built tools built"
# Read and traversable by every UID, writable by none, and the execute bit
# kept only where go build set one. The agent container runs as uid 65532
# and does not own these files — the mount carries whatever this image
# records — so what makes them executable there is the mode's `other`
# bits and not ownership, and a mode that depended on ownership would fail
# on any operator who runs the agent image under a different UID.
RUN chmod -R a=rX /out

# The tools image: the tree and nothing else. `scratch` rather than a
# base, because every path a base carries — /etc, /var, /tmp — would
# appear as an entry of the agent's tools directory once this filesystem
# is mounted there. It is also why this stage sets no ENTRYPOINT: there is
# nothing here to run, and the image is mounted rather than started.
FROM scratch AS tools
ARG REVISION=unknown
LABEL org.opencontainers.image.title="sherlock-tools" \
      org.opencontainers.image.source="https://github.com/garamsh/sherlock" \
      org.opencontainers.image.revision="${REVISION}"

COPY --from=build /out/ /
