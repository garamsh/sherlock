# The runtime fixture's model meter (main.go in this directory). The build
# context is this directory: main.go uses the standard library only and is
# built as a single file, so neither the module nor the repository-root
# allow-list (.dockerignore) is involved.
#
# Both bases are the ones build/agent.Dockerfile pins, by the same digests.

FROM golang:1.27@sha256:ec47ea3725fea0cc88f6de94919aef19f40f12fc1ec0e2d1b3426dbf15ecca04 AS build
WORKDIR /src
COPY main.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/meter main.go

# distroless static carries the CA certificates the HTTPS upstream needs.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/meter /meter
USER nonroot
ENTRYPOINT ["/meter"]
