# ADR 0016: The identity is the agent ID — agent-addressed API

> Status: accepted
> Date: 2026-08-03

## Context

The platform's identity concept was named env-id. The surrounding system — the console and the orchestration layer — uses agent ID for the same concept: one agent per environment. Two names for one concept breeds confusion exactly where the systems meet: the HTTP paths, the monitoring stream, and the console message payload.

The ID also has to be the one the launcher issued. Until #796 the flag defaulted to `default`, and a launcher that passed no ID produced an agent answering on `/agents/default/` and stamping `agent_id: "default"` on its messages while the launcher held a distinct GRN for it — a plausible value that was wrong for every deployment.

## Decision

The agent's identity is the agent ID. The HTTP API is agent-addressed — `/agents/{agent-id}/messages` and `/agents/{agent-id}/events` — and outbound message payloads carry `agent_id`. "Environment" remains the name of what the agent owns — in the pod topology (ADR 0017) the sidecar and its workspace. One agent per environment, as always.

The ID is what the launcher passes as `--agent-id`, and nothing else:

- **Required, with no default.** An absent or empty `--agent-id` refuses startup with `--agent-id is required`.
- **Opaque and verbatim.** Sherlock does not parse, normalise or escape it. A GRN such as `grn:garam:default:agent:144460a71649d1a1` is the path segment the routes answer on and the value tools receive as `SHERLOCK_AGENT_ID`.
- **The assignment epoch is a launcher label.** `--assignment-epoch` is a flag only, opaque and optional, with empty meaning unknown. The agent holds it beside the ID, logs it, and publishes it to the tools it spawns as `SHERLOCK_ASSIGNMENT_EPOCH`.
- **The generation is a runtime start nonce.** Every serving start mints 128 random bits from `crypto/rand` as 32 lowercase hex characters, after the memory store's writer lock is held and before the gateway serves, and never persists them. Tools spawned after the mint receive it as `SHERLOCK_GENERATION`. A one-shot `--message` run serves nothing and mints none, so its tools receive it empty. A launcher cannot pass one.
- Sherlock never compares or fences on either.

## Consequences

- One name at every boundary: console, orchestrator, API, and logs all say agent ID.
- The rename touched identifiers, paths, and docs, not the model.
- `env_id` disappeared from the external contract.
- A launcher must name the agent. An agent that ran on the implicit `default` stops starting until its arguments carry `--agent-id`; one that passed `--agent-id default` explicitly is unchanged.
- Moving an agent from `default` to its GRN keeps its memory: `memory.db` is not keyed by agent ID. Callers move to the new route, and messages already in the outbox still carry `default`.

## Rejected alternatives

- **Keep env-id externally, alias agent-id at the console** — two names for one concept permanently; every future integration inherits the translation.
- **Rename the package and on-disk layout too** — a large mechanical diff for zero behavioral gain; the name is accurate where it lives.
- **Keep a default ID** — a default addresses something and is wrong for every deployment, so a launcher that omits the ID gets a silent wrong identity rather than a refusal (#796).
- **Take the generation from a launcher flag** — the launcher re-uses its arguments verbatim across container restarts, so a flag would label two process starts alike where the generation must tell them apart.
- **Derive the ID from the gateway certificate** — would make the ID and the issued credential unable to diverge, but ties identity to the mTLS material ADR 0012 leaves optional.
