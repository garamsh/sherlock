# ADR 0014: Scope boundaries — authorization lives above the platform, no mid-run intervention

> Status: accepted
> Date: 2026-08-03

## Context

Two questions remained after mTLS landed (ADR 0012): per-caller authorization (ACLs over caller identity) and mid-run steering/interruption. The project owner settled both as scope boundaries rather than deferred features. The agent's computer is its total authority within its permissions (ADR 0017): permissions over what an agent may be used for belong to the layer that owns and directs the agent, not to the platform that hosts it. And the platform's whole execution model is "process the messages that arrive" (ADR 0007): a system with no forced stop or suspend has no place for intervention concepts.

## Decision

- **Authorization lives above the platform.** The gateway authenticates callers with mTLS and that is all; there are no per-caller ACLs in the platform. Granting an agent its capabilities and deciding who may use it for what is the orchestration layer's concern. Capability permissions are enforced at the sidecar, the action site (ADR 0017).
- **No mid-run intervention exists.** The gateway accepts messages and runs them one turn at a time, in FIFO order. The only way to influence an agent is to send it another message, processed after the current turn. There is no forced stop, suspend, or steering — by design, not by deferral.

## Consequences

- Flat authorization is final for the platform, not a phase.
- Monitoring stays the observability surface: subscribers watch turns; they do not control them.

## Rejected alternatives

- **Per-caller ACLs inside the gateway** — duplicates orchestration-layer policy in every platform, and contradicts the ownership model: the computer is the authority, so the platform has nothing finer-grained to grant.
- **Interrupt/steer commands in the message path** — a control channel breaks the FIFO turn model and the exactly-one-terminal-event guarantee of the monitoring stream; a followup message carries the same intent without new machinery.
