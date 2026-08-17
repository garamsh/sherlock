# ADR 0019: A question becomes an address before the store is read, and a read that finds nothing there widens to the subject

> Status: accepted
> Date: 2026-08-17

## Contents
- Context
- Decision
- What is not measured
- Consequences
- Rejected alternatives

## Context

`docs/architecture/memory.md` §Current state says the agent recalls nothing. The store holds an address and a value and answers every class in full when handed one; the writeback states an address, a value and an operation in a shape the model API enforces. What is missing is the half that turns a question into an address: asked in words, an addressed store answers nothing at all, so the finding has to happen in front of it (ADR 0018 §What ships is three parts).

ADR 0018 named that step and left it unbuilt. It also recorded what nobody has measured about it — the one resolution figure was taken resolving into the topic address space, and "resolution into a question-shaped address space is reasoned about and measured nowhere" (§What is not measured).

Two things the experiment record settles bound what this decision may claim, and both are limits rather than results.

The record had to write its own read step, "because the platform ships none — #336 step 3 is where one lands", and it deliberately gave that step no more than the write had: the write prompt's own two address sentences with "statement" read as "question", no example, no subject list, no attribute list. Its reason is the one this decision inherits — "A read prompt carrying more than the write prompt could place a question the write could not, and the figure would then describe the read prompt" (`docs/experiments/memory-frames/` §Limits).

And the prose lever is close to exhausted against that workload. Two revisions were measured; the second scored `0.394` against the first's `0.380`, "well inside both intervals", and the record reads further gain as "guessing which interrogative a probe author chose, and that is fitting the workload rather than fixing the prose" (§What the shipped prose produces). A better prompt is not what makes the agent recall.

## Decision

**A question becomes an address before the store is read, in the writeback's own vocabulary. The step resolves a subject and, where it can, the question; the store is read at that address, and where the address holds nothing the read widens to the subject.**

### The step's target is agreement with the writeback, not correctness

There is no true address for a question. There is only the address the writeback chose for the fact that answers it, and a read reaches that fact exactly when it computes the same address. Every property below follows from that, including which part of the design is allowed to be clever: none.

So the read prompt is the write prompt's address sentences read as a question, and carries nothing the write does not carry. This is not symmetry for its own sake. A read step that addresses better than the write addresses lands on addresses the write never wrote, and the record states the same thing from the measurement side (§Limits).

### The address has two halves and they are not equally reliable

Measured on the shipped write prose against a question-shaped read step, both halves of the pair run together (§What the shipped prose produces):

- `subject match`, write and read agree on the subject: `0.999 ±0.002`
- `question match`, write and read agree on the question: `0.394 ±0.033`
- `reachable`, the paired probe landed on the same address: `0.394 ±0.033`

The subject half is agreed on almost always; the question half fewer than half the time; and `reachable` tracks the question half, because the subject half almost never fails. That asymmetry is the most useful measured fact available here, and this decision is built on it rather than on an attempt to close it.

`reachable` is not this step's accuracy and this record does not use it as one. It is a joint property of two prose halves measured together; it mixes a write that asks the wrong question with a probe set whose own authors disagree about the question word, a share the record declines to estimate "because separating it would need a probe set written to one rule and this one was not"; and it is a floor rather than a ceiling, because "A looser rule would recover some of the gap and would be a constant nothing measured, so none was used — but it means `reachable` is a floor for any store that normalises more than this one does" (§Limits).

### Where a read must err, it errs toward the wider address

ADR 0018 states the write's direction: a wrong join heals and a wrong replace does not. The read has its own asymmetry and it is not the same one.

A read writes nothing, so no read damages the store and every wrong read is repaired by the next question. What a read can lose is this turn's answer, and it can lose it two ways. Naming a question the write did not use leaves the store with nothing to return, and the turn proceeds as though memory were empty — the loss is silent, and the model cannot tell an unaddressed question from an unpopulated store. Declining to narrow and reading the subject instead returns every address under it, which contains the answer whenever the subject is right, and costs context the model discards.

One failure hides the answer and reports nothing; the other shows the answer with company. So the step errs toward the wider address.

**The widening fires on an empty address rather than on a confidence.** A step that widened when it doubted its own question would need a threshold on that doubt, and nothing calibrates such a threshold — that is the shape ADR 0018 removed from this path rather than retuned. An empty address is not a judgement: the store reports it already, for one more indexed read and no model call. The order is therefore fixed and threshold-free — resolve, read the address, and read the subject only when the address held nothing.

The three outcomes the step can produce — a full address, a subject alone, nothing placed — are the shape the record's own read step already took, which answers with the subject and an empty attribute for a question about a person in general and leaves the address empty for a question it cannot place (`docs/experiments/memory-frames/harness/resolve.go`). That is precedent for the shape and for nothing else: that step resolved into the topic space.

What the widening costs is measured on the decade workload (§What a question about a whole person returned): a subject spans `7.50 ±0.41` addresses, returns `223.9 ±13.8` characters, reads `8.5 ±0.5` units, and sits at `0.109 ±0.007` of the read budget with `1.000 ±0.000` of the answer inside it. Against an address read's `34 ±2` characters and `1.3 ±0.1` units (§The structures compared), widening is an order of magnitude more context and still a tenth of the budget on this workload.

**That widening recovers the distance between `0.394` and `0.999` is an expectation of this design and not a result.** No arm in the record falls back; nothing here has measured a read step that does. The two figures the expectation rests on are measured and the recovery is not, and no run in this repository can settle it — the split inside `reachable` is unquantified, and the key the record measured with has been revoked.

### A read that places nothing says so, separately from a store that holds nothing

ADR 0006 requires that an empty recall be stated explicitly. This record splits that emptiness in two, because they are facts about different parts: the step placed no address, or the address it placed held nothing. The recall step distinguishes them in what it injects, and the turn's recall event carries which.

The reason is this decision's own weakness. Its accuracy is unmeasured, the measurement that would settle it cannot be run here, and the running system is therefore the only place that evidence will come from. An event stream that collapses "memory was not consulted" into "memory was empty" teaches an operator nothing about whether the read step works, and it is the one instrument a later measurement would start from. The record's own scoring keeps the same two apart for the same reason: "A question placed at the wrong address is answered from a place that holds something else, which is a different harm from one it could not place at all" (`docs/experiments/memory-frames/harness/resolve.go`).

### Ordering is deferred, and the condition for revisiting it is named

ADR 0018 §What is not measured says a question spanning many addresses has no ordering, and that where an ordering would have to live is the read step. That says where it belongs, not that it belongs in the first version, and this record leaves it out for three reasons.

Nothing has measured one, and the same limit says specifying and measuring one is not in the record. An ordering is a ranking with constants, and the hybrid score's uncalibrated constants (#411) left this path with the ranking ADR 0018 removed; putting one back in the first version restores the shape the programme disqualified, with no more to calibrate it than there was then. The record refuses the same move for itself — "any ordering over the addresses under a subject is a mechanism those stores do not have, and building one to publish a number is what this programme refuses" (§Limits). And the overflow an ordering would answer does not bite on the evidence available: the subject read, the widest thing this design produces, sits at a tenth of the budget with the answer inside it.

So the store's behaviour stands unchanged: where a subject spans more than fits, more than fits comes back. The condition for revisiting is a subject read observed in production to exceed the budget, and the recall event above is what would show it.

## What is not measured

These are limits of the decision, recorded here rather than left to be discovered.

- **Nothing in this repository has measured a resolution step into a question-shaped address space.** The one scored resolution figure was taken resolving into the topic space, `(subject, attribute)` (ADR 0018 §What is not measured). Every statement this record makes about how often the step will reach a fact is a design expectation, not evidence.
- **That figure was also handed a closed vocabulary, and a question-shaped space has none.** The scored run gave the model the twelve attribute names and states why: "what is measured is whether a model puts a question at the right address among names it was handed, not whether it can invent a key vocabulary" (`docs/experiments/memory-frames/harness/resolve.go`). A question address is not drawn from a list, so the part that run held constant is the part this design has to do.
- **The widening rule is reasoned and not measured.** No arm falls back from an empty address to its subject, so what the rule recovers, and what it costs in context over a real question mix, are both unmeasured.
- **A wrong question that lands on a populated address does not widen.** The rule fires on emptiness, so a question resolved onto some other populated address under the right subject returns values that are true and do not answer it. Nothing measures how often a wrong question is also a populated one.
- **The read prompt is new prose and nothing has measured it.** The record's read prompt was written for the record and is not the platform's (§Limits). What the platform ships is a third piece of prose after the write's two revisions and inherits none of their figures.
- **`reachable` is not a forecast of this design.** It is joint, it mixes in a workload residue the record does not estimate, and it is a floor for any store that normalises more than exact match (§Limits).
- **The cost figures are one workload's one shape.** The subject read's `7.50` addresses and `0.109` of the budget are the decade workload's, whose questions all name their subject and usually the attribute's own noun (ADR 0018 §What is not measured).
- **What the added call costs in latency is not measured**, and neither is what it does to a turn that drains eight messages rather than one.

## Consequences

- **A turn gains one model call, before the reasoning loop rather than inside it.** ADR 0018 anticipated this as one call per question; a turn drains up to eight messages and renders them as one input (ADR 0015), so concretely it is one call per turn. The act loop already makes one call per iteration up to its own cap (`internal/agent/`), so the addition is one call beside several — and it is unconditional, paid on turns that need no memory. It is on the critical path: nothing is recalled until it returns.
- **The nearest measured cost is the record's resolution run** — `680` calls for `98121` prompt and `6977` completion tokens (§What the paid half cost) — and it is not this design's cost: a different model, a shorter prompt, and the topic address space.
- **`docs/architecture/memory.md`'s account of recall changes when this is implemented, and not before.** This record decides the design; no code changes with it, and the agent still recalls nothing until #336 step 3 is built.
- **The seams this needs already exist, and no new one is introduced.** `agent.Recaller` is the hook and takes the turn's task statement; `agent.Model` is the model surface (ADR 0004); `internal/memory`'s two addressed reads are exactly the two shapes the step resolves to. What is new is that a recall hook implementation in `internal/agent/memory/` needs the model as well as the store, which is a wiring change in the runtime rather than a boundary.
- **The recall event gains a reason.** It carries what was injected; it will have to carry which of the two emptinesses produced an empty recall, which is a field rather than a new event.
- **No new provider configuration.** The step uses the session's own model. A second provider section for a resolver would be the shape #582 deleted when the embedder's went, and nothing here needs a different model from the one the turn already runs on.
- **The store needs nothing.** `At` answers the address and the restrictions under it; `UnderSubject` answers the widening. Both are built and measured.
- **The failure mode ADR 0018 inverted stays inverted.** A misaddressed question still answers nothing rather than something superseded; the widening reduces how often that happens without introducing a way to return something wrong.

## Rejected alternatives

- **A read prompt richer than the write prompt** — examples, the subject list, a question vocabulary. It could place a question the write could not, and the figure would then describe the read prompt rather than the pair (§Limits). There is no ground truth to be more accurate against, so accuracy above the write's is not accuracy.
- **Normalising the question before the store is read.** The record names this as part of what a question-shaped read step needs — "a question address needs the question's own words normalised and its subject named" (§What would be chosen today) — and also says a looser rule is a constant nothing measured (§Limits). Both are true, and this decision takes neither as licence: the first version adopts no normaliser beyond the store's own key, because the widening rule addresses the same gap with no constant to choose. A normaliser stays available and would need its own measurement to justify; that measurement does not exist and cannot be run here.
- **Widening on a confidence rather than on an empty address.** A threshold on the model's own doubt, calibrated by nothing. Emptiness is a fact the store already reports and needs no number.
- **Always reading the subject and never the address.** Recall would be no worse and the design simpler, since the subject half is the reliable one. It is rejected on cost rather than on recall: it pays `223.9` characters and `8.5` units on every question where an address read pays `34` and `1.3`, and it gives up the restriction relation, which is what a question-shaped address bought (ADR 0018 §The address names the question). The record cannot separate the two on recall, and that is stated here rather than hidden — a run comparing them is the first thing a live key should buy.
- **More prose passes on the write.** Two were measured and the second did not separate from the first; the record reads further gain as fitting the workload (§What the shipped prose produces).
- **Similarity over the questions the store holds.** Refuted twice on separate measurements and removed as an identity test rather than weakened (ADR 0018 §Rejected alternatives); the store has no embedder to do it with.
- **An ordering in the first version.** Deferred above, with the condition for revisiting named.
