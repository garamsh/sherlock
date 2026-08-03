# ADR 0021: The identity is the agent ID — agent-addressed API

> Status: accepted
> Date: 2026-08-03

## Context

The platform's identity concept was named env-id. The surrounding system — the console and the orchestration layer — uses agent ID for the same concept: one agent per environment, and the agent owns its environment (its body, ADR 0002). Two names for one concept breeds confusion exactly where the systems meet: the HTTP paths, the monitoring stream, and the console message payload.

## Decision

The agent's identity is the agent ID. The HTTP API is agent-addressed — `/agents/{agent-id}/messages` and `/agents/{agent-id}/events` — and outbound message payloads carry `agent_id`. "Environment" remains the name of the agent's body (the directory and its trust domains); only the identifier's name changes. One agent per environment, as always.

## Consequences

- One name at every boundary: console, orchestrator, API, and logs all say agent ID.
- `internal/environment` keeps owning the body's layout; the rename touches identifiers, paths, and docs, not the model.
- `env_id` disappears from the external contract; `data/envs/<id>/` stays the on-disk layout (an internal detail).

## Rejected alternatives

- **Keep env-id externally, alias agent-id at the console** — two names for one concept permanently; every future integration inherits the translation.
- **Rename the package and on-disk layout too** — a large mechanical diff for zero behavioral gain; the body's name is accurate where it lives.
