# ADR 0005: LLM access behind interfaces, mock for MVP

> Status: accepted
> Date: 2026-07-25

## Context

The agent's reasoning loop needs a chat model with tool-calling semantics, and memory needs embeddings and summarization (PLAN §2.2, §3). No provider has been chosen, and the MVP must run end-to-end without external API dependencies.

## Decision

All LLM access goes through narrow Go interfaces owned by the consumer:

- A model interface (multi-turn chat with tool-call requests/results) consumed by the agent loop.
- An embedder interface consumed by the memory tool (ADR 0003).

MVP ships deterministic local implementations: a scripted/mock model that exercises the full tool-call loop, and a hashing-based embedder. Real providers (chat, embeddings) are added later as new implementations without interface changes.

## Consequences

- The full Gateway → Agent → Tool pipeline, including hybrid memory search and compress, is testable offline and in CI.
- Provider choice is deferred to when real model quality matters, without blocking architecture work.
- The mock model's behavior must be documented in tests so the loop contract stays honest; it is not a stand-in for evaluating real model behavior.
