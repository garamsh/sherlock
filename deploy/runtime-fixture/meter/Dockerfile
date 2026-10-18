# The runtime fixture's model meter (main.go in this directory). The build
# context is this directory: main.go uses the standard library only and is
# built as a single file, so neither the module nor the repository-root
# allow-list (.dockerignore) is involved.
#
# Both bases are the ones build/agent.Dockerfile pins, by the same digests.

FROM golang:1.27@sha256:820153096fda4de6ac1b7194d12e416b7dd8f4e9a33d17e0345b111b945162d8 AS build
WORKDIR /src
COPY main.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/meter main.go

# distroless static carries the CA certificates the HTTPS upstream needs.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/meter /meter
USER nonroot
ENTRYPOINT ["/meter"]
