# ADR 0016: The identity is the agent ID — agent-addressed API

> Status: accepted
> Date: 2026-08-03

## Context

The platform's identity concept was named env-id. The surrounding system — the console and the orchestration layer — uses agent ID for the same concept: one agent per environment. Two names for one concept breeds confusion exactly where the systems meet: the HTTP paths, the monitoring stream, and the console message payload.

## Decision

The agent's identity is the agent ID. The HTTP API is agent-addressed — `/agents/{agent-id}/messages` and `/agents/{agent-id}/events` — and outbound message payloads carry `agent_id`. "Environment" remains the name of what the agent owns — in the pod topology (ADR 0017) the sidecar and its workspace. One agent per environment, as always.

## Consequences

- One name at every boundary: console, orchestrator, API, and logs all say agent ID.
- The rename touched identifiers, paths, and docs, not the model.
- `env_id` disappeared from the external contract.

## Rejected alternatives

- **Keep env-id externally, alias agent-id at the console** — two names for one concept permanently; every future integration inherits the translation.
- **Rename the package and on-disk layout too** — a large mechanical diff for zero behavioral gain; the name is accurate where it lives.
