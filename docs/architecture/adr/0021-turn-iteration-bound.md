# ADR 0021: Bound the turn rather than successful calls to each tool

> Status: accepted
> Date: 2026-09-24

## Context

The first workspace CLI trial for #840 cloned and inspected the target but exhausted the six-call shell allowance before selecting a change. A tool-name limit treats useful repository discovery like a stalled loop and makes the available work depend on how the same operations are divided into tools. The owner approved removing that gate while retaining the whole-turn ceiling and identical-failure detection; the [study](../../research/repository-cli/README.md) preserves the failed trial rather than treating the policy change as proof of success.

## Decision

The reasoning loop has no per-tool call allowance or replacement counter. Its default whole-turn ceiling remains 16 model iterations, and a call already failed twice with the same tool name, input and error receives a synthetic refusal for the rest of that turn.

## Consequences

- More than six successful calls to one tool may execute, including repeated successful inputs. The terminal response must still fit within the turn ceiling.
- An iteration can request multiple calls, so the iteration ceiling is not a total tool-effect, wall-time or monetary budget. Tool/workspace deadlines and resource limits remain separate bounds; paid studies enforce their own external accounting ceilings.
- The identical-failure record resets each turn. Different inputs or changing errors are not the same failure; removing a tool-name allowance does not widen workspace credentials or network grants.
- A new bounded comparison measures the policy change. It neither overwrites the earlier failure nor authorizes continuing an exhausted task without a new instruction.
