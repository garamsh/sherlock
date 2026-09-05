# Recall-specificity experiment

What the running agent recalls when the question says less than the fact it is looking for, measured against the built binary and the store its own turns wrote.

## Contents
- The bound, stated before the run
- Method
- The curve
- Where the loss is
- What the read step placed
- What the run cost
- What this record does not say
- Limits
- Reproducing

## The bound, stated before the run

The ceilings below are constants in `tests/recall_specificity_test.go`, read by a metering proxy that stands between the agent process and the endpoint. It refuses the call that would pass any of them **before that call leaves the machine**, so the bound is a refusal and not an observation. Four ceilings rather than one: two of them need no price, so they still hold if the price is wrong.

| ceiling | value | why that value |
|---|---|---|
| model | `MiniMax-M2` at `https://api.minimax.io/v1`, key from `MINIMAX_KEY` | the endpoint this project runs on (`README.md` §MiniMax, on `openai-compatible`) |
| calls | 150 | 40 turns at the 3 model calls a turn makes when nothing goes wrong is 120; the margin covers a turn that takes an extra act iteration |
| input tokens | 1,200,000 | reported by the endpoint, not estimated |
| output tokens | 300,000 | the same, and the ceiling a reasoning model's `reasoning_content` is charged against |
| USD | 0.29 | at $0.30 per million input and $1.20 per million output, MiniMax's published `MiniMax-M2` rates on the run date |

A refused call fails the turn and the run stops. Nothing retries: a call pattern that does not work is reported rather than iterated against the provider.

The granularity is one call. A ceiling already reached refuses the next request, and a request's cost is not known until it answers, so the run can overshoot by at most whatever the last admitted call cost.

## Method

Every previous round in this tree measured a port of the design. This one drives the two shipped binaries: `gagent workspace` serving the gRPC data plane, and `gagent agent` serving its gateway, both built by `go build` inside the run (`tests/e2e_binary_test.go`'s `buildGagentBinary`, `startWorkspaceBinary`, `startAgentBinary`, `emptyConfig`). Nothing about the loop, the tool registry, the prompt or the memory service is reimplemented. What the run supplies is the facts, the questions, and the meter in front of the endpoint.

**Every turn is its own agent process, against one memory file.** The agent's session is rolling, so a probe asked in the process that planted the fact could answer out of its own history rather than out of the store — which is the whole confound. A fresh process per turn removes it: the only thing a probe turn shares with a plant turn is `memory.db`.

**Plant.** Ten facts, one per turn, each a plain declarative about a different person. The turn stores what it stores: the writeback is the single write path (ADR 0011) and nothing here writes to the store directly.

**Snapshot.** After the tenth plant, the memory file is copied under each probe. A probe turn also ends in a writeback, so without the copy the third probe would read a store the first two had written to.

**Probe.** The same ten facts, asked back at three levels of specificity, one question per turn:

| level | what the question gives | example |
|---|---|---|
| L1 | the subject and the attribute | "Where does Sarah live now?" |
| L2 | the subject; the attribute implied | "Has anything changed for Sarah?" |
| L3 | neither, both implied | "Didn't someone move to a new city recently?" |

L1 is the control **inside this run**, and it is not comparable to `memory-frames`' `reachable` 0.394: that figure was a different model, a different prompt and a store this tree no longer has. L1 is here so L2 and L3 have a baseline in which exactly one thing moved.

L2 is one template with the name substituted, so between L1 and L2 the only change is that the question stops naming what it asks about. L3 names the change instead of the person, and each fact's change is unique among the ten, so an L3 question has exactly one answer in the store — the store is never the reason it cannot be answered.

**Score**, per level, from the monitoring stream (ADR 0007) and the store, never from the model's own account of itself:

- **placed** — the `recall` event's `recall_outcome` is anything but `unplaced`. That is `ReadAddress.Placed()` as the running loop reported it: the read step named a subject. ADR 0019's other three outcomes — `addressed`, `widened`, `empty` — all placed one.
- **planted fact injected** — the memory the plant phase wrote is among the `recalled_ids` the turn injected. Its ID comes from reading the store after the plant phase through the shipped service, not from matching text on the stream.
- **answer carries the value** — the turn's reply, which is the `memory_write` call's `reply` field and the only thing a person reads, contains the planted value on word boundaries and without case. The word boundary is #371's rule: without it `fish` is credited to "allergic to shellfish".

The ten values are distinct and none contains another, so no probe can be credited with another probe's answer.

## The curve

<!-- filled from results/summary.md -->

## Where the loss is

<!-- filled from results/probes_minimax-m2.json -->

## What the read step placed

<!-- filled from results/calls_minimax-m2.json -->

## What the run cost

<!-- filled from results/calls_minimax-m2.json -->

## What this record does not say

<!-- written after the run -->

## Limits

<!-- written after the run -->

## Reproducing

The run is `TestRecallFromImpliedInput` in `tests/`. It skips unless `GAGENT_PAID_MODEL_TESTS=1` and `MINIMAX_KEY` are both set, so `make ci` never buys anything:

```
GAGENT_PAID_MODEL_TESTS=1 go test ./tests/ -run TestRecallFromImpliedInput -timeout 90m
```

It rewrites every file under `results/`. The committed ones are what the run below produced; a second run against the same endpoint produces its own, and the figures in this record are pinned to the first.

Two checks in the same package cost nothing and do run in `make ci`:

- `TestRecallFromImpliedInput_AgainstAScriptedEndpoint` drives the whole procedure — process per turn, snapshot, three levels, scoring, the result files — against an endpoint answering from a script, so what the paid run depends on is exercised before it is paid for.
- `TestSpendMeter_RefusesPastEachCeiling` is the evidence that the bound is a bound: each of the four ceilings refuses the call that would pass it, each paired with the control that admits the same calls under a wider ceiling.
