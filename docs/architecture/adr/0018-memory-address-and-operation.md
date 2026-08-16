# ADR 0018: A fact is held at the address of the question it answers, and the write states the operation

> Status: accepted
> Date: 2026-08-17

## Contents
- Context
- Decision
- What is not measured
- Consequences
- Rejected alternatives

## Context

The shipped memory holds sentences with an embedding and a keyword index over them, and infers on insert whether a new sentence replaces an existing one from the similarity between the two. That the mechanism is contradicted by measurement has been on the record since #334 was filed, and the lifecycle (ADR 0006) carried it anyway, because a decision is not withdrawn by an objection alone and nothing had yet said what replaces it.

`docs/experiments/memory-frames/` is the record that answers that. It compares seventeen ways of holding a memory over a decade of turns behind one seam — the shipped design, ten structures invented here, and five ported from published papers — measuring what each stores, what it answers class by class, what it returns that has stopped being true, and what answering costs it. Its §The structures compared is the comparison on one page and its §What would be chosen today is where the programme stands. That programme is now closed, and this record is the decision taken on its evidence. Every figure below is the record's; none is restated from an issue.

Two results frame the rest. What a structure *stores* separates the table further than how it decides: every structure holding what a turn asserted answers most of what it is asked, every structure holding the turn answers a small fraction of it, and the gap between those groups is wider than any gap inside either (§What the comparison shows). And recall is never read without the stale figure beside it, nor stale without the class columns beside it — a structure that destroys nearly everything returns nothing stale, and one that keeps everything answers nearly everything.

## Decision

**A fact is held at the address of the question it answers. The write states whether a value replaces what is there or joins it. Where a writeback must err, it errs toward join.**

### The address settles identity, and similarity leaves the write path

A fact is held at an address, and writing to that address is how a value is replaced. Every addressed arm answers `replaced` and `reverted` in full at every year of the decade, where the shipped store returns a superseded value on most revised threads from the first replacement onward — `stale returned` `0.838` at the last year (§The structures compared). That is not a better inference than identity from similarity; it is no inference, so there is nothing left to be wrong.

Similarity is not weakened here, it is absent. `docs/experiments/memory-retrieval/` measured that no threshold separates a correction from two facts that must both survive: on the shipped embedder the correction family's median sits below the coexisting family's minimum. `mem0` is the published design that takes the same declaration from the same model and differs from the decision above in one thing — it finds the memory the operation applies to by similarity instead of computing where it is. It posts recall `0.948` with stale `0.900`, and removing its similarity gate collapses recall to `0.188` (§The structures compared, §`mem0`). Those two arms bracket a model call the record's seam withholds, and the design sits between them at a point nothing measured can locate.

### The address names the question, not the topic

The address is the subject and the question a fact answers — with whatever restriction the sentence puts on that question — rather than the subject and a topic noun. A question-shaped address recovers the narrowing coexistence a topic address loses: `refined` goes `0.500` to `1.000` under the oracle (§The structures compared). It recovers it by not having to decide — a narrowing statement asks its own question, so it gets its own address and no write has to choose between the two values.

It does not recover the conjunctive case. A second value added by a conjunction answers the question the first one answered, so the two share an address whatever the address is made of, and the question address alone halves `parallel` exactly where a topic address with a cue rule halves `refined` (§Whether the address carries the arity).

### The write states the operation

This is the part the evidence turns on, and the reasoning is the programme's own asymmetry rather than a recall comparison.

Under a real extractor — a model reading the messages, which is where a buildable design sits — a question address **with no declaration** posts `stale returned` of `0.000` on all three conversation shapes and scores `parallel` at `0.417`–`0.458`. With the declaration, `parallel` is `0.583`–`0.708`, and stale is `0.000` on the plain and subordinated shapes and `0.125` on the packed one (§What harder language costs). The recall difference between the two sits inside the stated intervals; the class column and the stale column are what separate them.

A stale figure of zero beside a halved coexistence class is not a clean result. It is the store *replacing* where it should have joined — the destroying direction. On the decade run, where the record measures healing directly, that direction heals at `0.000` and carries its damage for the rest of the span; the accumulating direction the declaration moves it to heals at `0.933` and carries its damage for no checkpoints (§What a wrong declaration costs; the shape study measures no healing, so the two runs are read for different things and no figure of one is compared with a figure of the other). So the declaration is not bought for recall. It is bought because it moves the residual error from the direction nothing repairs to the direction the next correct write repairs by itself.

What the declaration is asked for narrows with the address. A statement that narrows its question gets its own address, so the writeback states an operation only where a sentence adds a value that leaves the question alone — not on every assertion (§Whether the address carries the arity). The address and the declaration recover different classes and compose; they are not alternatives.

### Where a writeback must err, it errs toward join

A wrong join is repaired by the next correct replacement at that address; a wrong replacement is repaired by nothing. The asymmetry is measured, it is structural, and stating it costs no code.

It is a direction, not a policy of joining declared replacements. Which declarations to doubt, and what to do about them, is not settled by the record and is not settled here — the two policies it measured are under §Rejected alternatives, and which failure is cheaper is a property of what the agent is for rather than of anything measured.

### Forgetting is decided by use

Least-recently-read keeps `carried` at `1.000` where an age-ordered rule loses all of it — `0.000` — at the same bound (§What decides the eviction). This is the one place the arity answer does not generalise: the store can decide what to forget from what it has watched happen, where it could not decide arity from what it holds. What a use-ordered rule saves it pays for out of the newest facts, which have not been read yet.

Nothing evicts at the capacities a decade reaches. Every structure that holds facts adds tens of units a year against an active-index ceiling twelve decades off or further (§What a read cannot reach). The eviction rule is chosen for what it does when a bound bites, not because one is expected.

### What ships is three parts, not one

The store does no finding. Every figure above was produced with the question already turned into an address; asked in words, every addressed structure — the attribute arms, the question arms, the interval store, the log — answers nothing at all, and the units-read column beside it is zero (§Whether a structure can find what it holds). That is not a score to be improved. It is the other face of what addressing bought, which is that identity is settled before the read rather than inferred during it.

So the design is three parts:

- **A writeback** that reads a sentence into an address and, where the sentence adds a value that leaves the question alone, states the operation.
- **A store** that holds an address and a value — no sentence, no vector, no keyword index — and answers by going to one place.
- **A read step** that reads a question into an address, in front of the store. Measured, it placed 679 of the 680 questions it was asked (§Whether a structure can find what it holds).

An ADR describing only the store would read as though a store were a memory. It is not: two of the three parts read language, and the store between them does no finding whatever.

## What is not measured

These are limits of the decision, recorded here rather than left to be discovered.

- **The read step was measured resolving into the topic address space.** `(subject, attribute)` is the space that run was in. Resolution into a question-shaped address space is reasoned about and measured nowhere (`docs/experiments/memory-frames/` §What this record does not say).
- **The resolution figure is a ceiling on the easiest form of the problem.** Every question there names its subject and usually the attribute's own noun. A question that only implies what it asks about is not asked.
- **The policy for a declaration that cannot be trusted is not chosen**, and the record states that its evidence cannot choose it.
- **The question-shaped address is not measured under a capacity bound.** The constrained runs predate that addressing.
- **The plural half of the arity rule never fires.** Every question form in the workload is singular or unmarked, so what is shown is that a singular question predicts a single-valued address and a bare interrogative predicts nothing.
- **The extraction and resolution figures are one cheap model, one prompt, one pass**, on three conversation shapes that are three points rather than a sample of conversation.
- **A question spanning many addresses has no ordering.** An addressed store returns every address under the subject, at any read budget, and where that is more than fits it returns more than fits. Where an ordering would have to live is the read step; specifying and measuring one is not in the record.

## Consequences

- **ADR 0006's supersession on insert is removed.** The mechanism was already contradicted by measurement; the replacement does not need it, and #334 is settled by this record rather than left to a retuning. Similarity disappears from the write path instead of moving to a better threshold.
- **ADR 0011 is unaffected in shape, and this decision rests on it.** The writeback is still the single write path, and it is where the operation is stated. What the writeback carries changes — an address, a value, and an operation where one is needed, instead of a sentence — but not who writes or when.
- **ADR 0002 is unaffected in shape.** One store per agent, one file, behind a store interface stays. Its update semantics — updates supersede rather than overwrite, and the old row archives — are the writeback's `update` and not the similarity-triggered supersession this record removes, so they carry forward.
- **Recall stops being a budget fill.** An addressed store returns the address it was asked for and nothing more; its answer is identical from a fraction of the shipped token budget to four times it. What the store puts in the context is a line or two rather than a filled budget.
- **The failure mode inverts.** A misaddressed question answers nothing where the shipped store answers something superseded. Nothing wrong is returned; something is missing instead, and the direction of error above is what keeps the missing part repairable.
- **The read path gains a model call and the store loses a ranking.** Finding moves in front of the store as one call per question, and the hybrid score's uncalibrated constants (#411) leave the addressed read path with it.
- **The declaration is the part of a writeback most exposed to the sentence.** Of the facts a model both finds and addresses correctly, the share whose operation it also reads correctly falls furthest of anything the extraction measures across the shapes. Reliability of a declaration is a property of the sentence, not only of the model.
- **Nothing in `internal/` changes with this record.** The implementation is #336's sequence and follows this decision.

## Rejected alternatives

- **Identity from similarity** — supersession on insert (ADR 0006) and the same shape in `mem0`. Refuted twice on separate measurements: as a way of deciding *whether* two statements are about one fact (`docs/experiments/memory-retrieval/`) and as a way of finding *which* memory a write is for, where the value at the address a write lands on is no nearer that write than the nearest value somewhere else.
- **Arity learned from the attribute's other addresses** (`attribute-learned`). It is `attribute-multi` row for row, because a rule deciding at read time destroyed nothing at write time, and it reads every address it holds to answer one question. The threshold decides the outcome and nothing calibrates the threshold.
- **Arity from the sentence's addition cues** (`attribute-cued`). The best-scoring rule measured in the record, and disqualified: it enumerates the phrases that mark an addition and nothing in that list generates its next entry. It halves `refined`, the class a question-shaped address recovers.
- **A store of validity spans** (`interval`). It answers exactly what the plain addressed store answers while holding more and reading more; the one question it could answer and an addressed store cannot — what was true in an earlier year — is asked nowhere in the workload, so its extra bytes buy an unmeasured capability.
- **A stream ranked at read time** (Generative Agents, its reflection mechanism, MemoryBank). All three store the turn rather than what it asserted and answer a small fraction of the workload; two of them freeze most of what they hold from the first year, and the third scores the record's floor — a bounded document with no store at all — exactly.
- **Age-ordered eviction.** It takes `carried` first — a fact asserted once and asked a decade later is the oldest thing in the store — and answers it on nothing at all at the hard bound.
- **A policy for a declaration that cannot be trusted.** Two were measured and neither is adopted. A blind bias toward join, swept from taking every declaration to joining all of them, recovers every destroying error and puts staleness at its ceiling; because the destroying errors are few and the correct replacements many, it is overwhelmingly likely to spoil a write that was right. Reading the model's doubt — asking three times and joining where the samples split — beats the blind bias by a wide margin on what it buys per point of staleness, and still costs three model calls where there was one, carries a sample count as a knob, and leaves the coexistence class short of where a correct declaration would put it.
