# ADR 0019: Scope boundaries — authorization lives above the platform, no mid-run intervention

> Status: accepted
> Date: 2026-08-03
> Supersedes: ADR 0017's "per-env ACLs are a future ADR" note.

## Context

Two open questions remained after ADR 0017: per-env authorization (ACLs over caller identity) and mid-run steering/interruption. The project owner settled both as scope boundaries rather than deferred features. An agent's environment is its total authority (ADR 0002): permissions over what an agent may be used for belong to the layer that owns and directs the agent, not to the platform that hosts it. And the platform's whole execution model is "process the messages that arrive" (ADR 0009): a system with no forced stop or suspend has no place for intervention concepts.

## Decision

- **Authorization lives above the platform.** The platform authenticates callers with mTLS and that is all; there are no per-env ACLs in the platform. Granting an agent its capabilities and deciding who may use it for what is the orchestration layer's concern.
- **No mid-run intervention exists.** The gateway accepts messages and runs them one turn at a time per environment, in FIFO order; a queued message is exactly one turn of the rolling session. The only way to influence an environment is to send it another message, processed after the current turn. There is no forced stop, suspend, or steering — by design, not by deferral.

## Consequences

- The `gateway.md` open questions on per-env authorization and mid-run steering close; the security.md note moves from "deferred" to "out of scope, orchestration layer's concern."
- ADR 0017's flat authorization is final for the platform, not a phase.
- Monitoring stays the observability surface: SSE subscribers watch turns; they do not control them.

## Rejected alternatives

- **Per-env ACLs inside the gateway** — duplicates orchestration-layer policy in every platform, and contradicts the body model: the environment is the authority, so the platform has nothing finer-grained to grant.
- **Interrupt/steer commands in the message path** — a control channel breaks the FIFO turn model and the exactly-one-terminal-event guarantee of the monitoring stream; a followup message carries the same intent without new machinery.
