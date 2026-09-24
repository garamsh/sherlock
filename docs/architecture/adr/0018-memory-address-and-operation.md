# ADR 0018: A retrieved memory carries a path to its source

> Status: accepted
> Date: 2026-08-17 (revised in place 2026-09-24)

## Context

The former addressed store could answer an exact address but did not retain the original message. Correction and paraphrase probes exposed that finding the right address was fragile (`docs/experiments/recall-specificity/`). Issue #823 requires multiple searchable memories and a route back to the source.

## Decision

Each searchable mid memory links to one short summary, which links to one visible user or assistant source message. Retrieval presents bounded candidates with their linked short IDs. The reserved inspection call reads a short summary or source ID; exact wording is answered from a source message, not from a clipped search candidate. Newer candidates are offered before older candidates, while both remain inspectable so a correction is not mistaken for erasure of history.

## Consequences

- Retrieval of a related candidate is not itself evidence that an answer is supported. The agent must use source inspection when a candidate lacks detail or exact wording is requested.
- The source archive preserves corrections and their prior versions. Correctness of a model's choice between them needs live-model evaluation; deterministic tests check the ordering and scripted answer separately.

## Rejected alternatives

- A source-free fact index would repeat the old inability to drill into original wording.
- Deleting an older statement on correction would erase historical evidence an exact-wording question may ask for.
