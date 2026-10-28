# ADR 0030: Every model request of a turn carries one turn identifier

> Status: accepted
> Date: 2026-10-28

Drafted for #1166, split out of #1161 under the owner direction recorded on #1162. Facts about Sherlock hold at `dev` `1c6d4708392bb9ed6ada23f4ff48ddf2f0f05e8c`. Facts about garam hold at garamsh/garam `25f2b1fabb055c373cd46e84fd40639ee542ac04`, file `docs/architecture/ai-gateway.md`, and at garamsh/garam#1704, comment 6466427076.

## Context

- **garam's AI gateway chooses a model per turn, not per call.** It keeps later calls carrying the same turn identifier on the model the first one chose, because switching models inside a turn discards the prompt cache. A call carrying none is routed by itself (`ai-gateway.md` §Current decisions, "Routing", and §Rationale).
- **Who mints the identifier, and the header that carries it, were left to Sherlock** (`ai-gateway.md` §Open questions, "Where the turn identifier comes from"). Sherlock proposed `X-Turn-Id`, minted by the runtime as one opaque value per turn. garam's PM confirmed both on garamsh/garam#1704 (comment 6466427076): the gateway treats the value as opaque and records it per call.
- **Only the session knows where a turn starts and ends.** `internal/agent/service.go` `Run` is one turn, and the drained message IDs already ride on its context to every tool the turn calls (`tool.WithInReplyTo`).
- **The gateway Sherlock exposes is platform-neutral** (ADR 0007), and so is what Sherlock sends a platform.

## Decision

Each turn mints one identifier, and every model request of that turn sends it in the header `X-Turn-Id`.

- **The session mints the identifier when a turn starts**: 128 random bits as 32 lowercase hex characters, opaque to every reader. It is carried on the turn's context.
- **Both protocol clients send it as `X-Turn-Id`** on every request made under that context. That covers the reasoning loop's calls, the retry after a 401 (ADR 0029), and the mid-episode synthesis that runs inside the turn's persistence step. A request made outside a turn carries no header.
- **The header name carries no platform name**, because Sherlock's side of the wire stays platform-neutral (ADR 0007).
- **The identifier is sent wherever `model.base-url` points**, under either protocol. It identifies a turn and nothing else: it carries no content, no agent identity and no credential.

## Rejected alternatives

- **Letting the gateway or the platform mint the turn identifier.** Only the session knows where a turn starts and ends. An identifier minted elsewhere would have to be passed in through the gateway's ingress, which is written for no platform.
- **Sending the identifier only when `model.base-url` is garam's gateway.** It would make the clients recognise one platform's URL, against ADR 0007, to save one header an endpoint can ignore.
- **Reusing an existing identifier** — a drained message ID or the monitoring stream's turn counter. A turn can drain several messages or none, and the counter restarts with the process, so neither names one turn uniquely.

## Consequences

- garam's gateway can keep each of Sherlock's turns on one model, which `ai-gateway.md` §Current decisions, "Routing", makes conditional on the identifier.
- Every model request of a turn carries `X-Turn-Id`, whatever the endpoint. An endpoint that records headers learns how many requests each turn made, and nothing more.
- **Responsibility documents paired with this record** (`docs/architecture/README.md` §Rules, "Decisions land in pairs"): `agent.md` (the turn identifier in model access), `security.md` (what the header sends to the model endpoint).

## Signs the decision was wrong

- garam asks the identifier to carry more than an opaque value, such as the agent or the organization. That would mean a turn identifier is a platform concept after all, and ADR 0007's neutrality needs restating for it.
- A model call that belongs to a turn reaches the gateway without the header — work started on a context that is not the turn's — and the gateway routes it apart. That would mean the context is the wrong carrier and the identifier belongs in the request the session builds.
- A second platform asks for a different header name for the same purpose, which would mean the name belongs in configuration.
