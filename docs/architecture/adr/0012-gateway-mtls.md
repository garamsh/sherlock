# ADR 0012: Gateway authentication via mTLS

> Status: accepted
> Date: 2026-08-02

## Context

An unauthenticated gateway is a remote shell into the environment, so exposure beyond localhost needs auth before anything else. The planned use is an external orchestration layer driving platforms through the call model — `POST /agents/{id}/messages` for delivery and `GET /agents/{id}/events` for state — machine-to-machine traffic with a small fixed peer set and long-lived SSE connections. Orchestration itself (CA operation, certificate issuance, fleet lifecycle) is explicitly outside this project's responsibility.

Bearer tokens were considered and rejected: they put a shared secret into every platform's config (against ADR 0003's secret-custody posture), they carry no per-caller identity, and token expiry interacts badly with long-lived SSE streams.

## Decision

The gateway authenticates callers with mTLS, terminated in the agent container itself (`crypto/tls`) — no separate TLS proxy component.

- A `tls:` config section carries the server certificate/key and the client CA path. When set, the gateway requires and verifies client certificates against that CA.
- When `tls:` is unset, the gateway serves loopback binds only; a non-loopback bind without TLS fails startup. The dev loop on localhost is unchanged.
- Certificate issuance, CA operation, and fleet lifecycle belong to the operator or the orchestration layer, not this project.

## Consequences

- A platform holds only public caller material (the client CA) plus its own server key, which lives outside the execution domain — no shared caller secret enters any config.
- Caller identity (client-certificate CN/SAN) exists at the TLS layer and lands in request logs as trace context. Authorization stays flat: an authenticated caller has full access; scope boundaries are settled in ADR 0014.
- SSE connections authenticate once at handshake; the call model is unchanged for clients.

## Rejected alternatives

- **Bearer token / API key** — shared secret in every platform config, no caller identity, refresh semantics hostile to SSE.
- **TLS termination at a reverse proxy** — adds a component to run and secure; the gateway is a Go process and `crypto/tls` covers the requirement.
- **OIDC/JWT service accounts** — needs an identity provider the project does not have; an external dependency for a small fixed peer set.
