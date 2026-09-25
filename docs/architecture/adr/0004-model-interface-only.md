# ADR 0004: Model access is consumer-owned interfaces, and the wire protocol is configuration

> Status: accepted
> Date: 2026-07-25 (revised in place 2026-09-26)

## Context

The agent's reasoning loop needs a chat model with tool-calling semantics. The project has to be buildable before a vendor is picked and runnable without reaching a network. Endpoints serving comparable models differ in wire protocol — where the system prompt sits, how a tool call and its result are encoded, whether a token ceiling is required — so which endpoint to call and which protocol it speaks are two questions rather than one.

Memory recall ranks mid episodes by embedding similarity (ADR 0019), so it also needs an embedding model. Embeddings are served by hosted vendors and by local servers alike over the OpenAI-compatible `/embeddings` protocol.

## Decision

Chat model access goes through one narrow Go interface owned by the consumer — multi-turn chat with tool-call requests and results, consumed by the agent loop and by mid-episode synthesis. Embedding goes through a second one, text to vector, consumed by the memory store for each episode and each recall.

Which protocol implements that interface is configuration, not code. The provider setting names the wire protocol and the base URL picks the endpoint speaking it, so a vendor arriving on a protocol that already ships needs neither a new implementation nor a new value. Three protocols ship: a scripted mock that drives the whole tool-call loop offline, the OpenAI chat-completions protocol, and Anthropic's Messages protocol. A value naming none of them fails at startup, as does a missing API key, rather than mid-turn.

The embedder has one implementation, the OpenAI-compatible embeddings protocol, configured in its own section beside the model's: base URL, model, and the name of the variable holding the key. A local server needs no key, so an empty key name sends none. Under the mock chat provider the embedder is an offline stand-in and the section is not read; under any other provider a missing base URL or model fails at startup.

## Consequences

- The full agent pipeline is testable offline and in CI. The mock stands in for a model's availability, never for its behavior, so what it establishes is the loop contract; it cannot be asked to evaluate a real model.
- Provider choice is a configuration line and blocks no architecture work. A new protocol is one more implementation behind the same interface; a new vendor on a shipped protocol is a base URL.
- What one protocol places differently from another is the implementation's to absorb — not the interface's to widen, and not the operator's to configure.
- A live deployment needs an embeddings endpoint beside its chat endpoint. The embedder speaks only the OpenAI-compatible protocol, so an agent on the Messages protocol still names an OpenAI-compatible embeddings endpoint.
- The interface is provider-shaped rather than a lowest common denominator, so a capability one protocol offers and another has no vocabulary for stays out of reach until the interface gains it.
