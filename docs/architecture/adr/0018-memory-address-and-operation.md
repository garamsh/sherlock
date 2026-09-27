# ADR 0018: A retrieved memory carries a path to its source

> Status: accepted
> Date: 2026-08-17 (revised in place 2026-09-24)

## Context

The former addressed store could answer an exact address but did not retain the original message. Correction and paraphrase probes exposed that finding the right address was fragile (`research/recall-specificity/`). Issue #823 requires multiple searchable memories and a route back to the source.

## Decision

Each immutable mid episode records the inclusive interval of original visible messages that produced it. Retrieval carries that source interval and the episode ID. The reserved inspection call reads exact original messages by source ID directly; there is no intermediate short-summary inspection hop.

Recent and relevant episodes are distinct candidate sets, deduplicated by episode ID. Pending source previews are labelled separately. A summary is a retrieval hint, never verified source text. Corrections within a batch are synthesized as corrections; across batches, both episodes and original messages remain inspectable, and the model must compare chronology rather than treating an older assertion as current.

## Consequences

- Retrieval of a related candidate is not itself evidence that an answer is supported. The agent must use source inspection when a candidate lacks detail or exact wording is requested.
- The source archive preserves corrections and their prior versions. Correctness of a model's choice between them needs live-model evaluation; deterministic tests check the ordering and scripted answer separately.

## Rejected alternatives

- A source-free fact index would repeat the old inability to drill into original wording.
- Deleting an older statement on correction would erase historical evidence an exact-wording question may ask for.
