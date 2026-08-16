# Memory-frames experiment

What a memory structure does with a decade of turns, measured through a seam that structurally different designs can sit behind, with the design this project ships measured through it as the baseline the rest are read against.

## Contents
- Method
- The seam
- The workload
- The frames
- The arity question
- What a wrong declaration costs
- Whether a write need destroy
- What a read cannot reach
- What a run costs
- Results
- What the numbers say
- What would be chosen today
- What this record does not say
- Limits
- Reproducing

## Method

Every memory experiment in this tree so far has compared variants inside one frame: a store of rows, an embedding-similarity retrieval step, a per-turn model decision, and a score that ranks rows. This one builds the instrument for comparing frames rather than variants, and measures the shipped design through it. It proposes no new design; the designs are separate issues.

Like `memory-retrieval/`, the shipped design is measured as it runs rather than as a reimplementation of it: the store, the hybrid search, the ranking function and the after-turn lifecycle are `internal/memory`'s own, called through `memory.NewService` and `internal/gateway`'s salience constants. Unlike `memory-retrieval/`, it is one implementation behind an interface rather than the thing the harness is wired to, so the next design is added without the harness changing.

Every figure below is produced by `harness/` from the committed results and written into this file between the generated markers. No figure in this record was typed by hand, which is why there is no register of claims here: the project owner has ruled that a record restating result figures should generate them instead. `harness/record_test.go` fails when this file and the committed results disagree.

## The seam

`harness/frame.go` declares it. A turn goes in; what the design would put in the model's context at any later turn comes out; what it holds can be asked for.

The interface returns text rather than rows. A design that keeps one rewritten document has no rows to return, and scoring on rows would settle the comparison before it ran. Nothing in the interface names a query, a limit, a score or a similarity, so a design that performs no retrieval answers by returning what it holds. The bounded-document frame below exists to prove that is true rather than claimed: it keeps no store, no identity and no ranking, and sits behind the same interface as the shipped design.

What a design is told about a turn is the other half of the seam. A turn always carries its text. In one arm it also carries the assertion the generator planted, as a subject, an attribute and a value. That is there because with no model in the run, a design that addresses facts by subject and attribute would otherwise have to parse sentences, and the comparison would become a comparison of parsers. Holding extraction constant removes that variable.

The extracted form says what was asserted and nothing else. It does not say whether an attribute holds one value or several, and it does not say what an assertion replaces. Arity and supersession are the questions being asked; a design handed either has been handed the answer.

One more thing arrives in that arm and it is not extraction. `Declared` is the write operation — whether the value ends what is at its address or joins it — which is where this record's own arity finding puts it: nothing recovers from the triple what the sentence carried, so only a writeback that reads the sentence can state it. Every frame receives it and only the ceiling reads it, which is what makes that arm a ceiling rather than a design. `Fact` still says nothing about arity, so the question the other frames are asked is the question they were asked before.

A question is a turn too, and in the same arm it also arrives extracted: the address it is asking about, never the value. That is the read-side half of the same decision. A design that addresses facts has to turn a question into an address, and with no model in the run it would otherwise parse the question — which would put on the read side exactly the parser comparison that was removed from the write side. Every frame receives it and each decides what to do with it: the shipped frame ignores it and queries with the question's text, because its read path takes a task statement and giving it an address would stop it being the shipped design.

## The workload

`harness/workload.go` generates it from a seed. The span, the session count, the turns per session and the thread count are the run's parameters, reported in the results table below rather than buried as constants: a later comparison cannot be read against a noise volume nobody chose.

Sessions are spread over the years with a jitter inside the gap between them; turns inside a session are minutes apart. Most turns carry no fact at all. Assertions are scattered among them, each in a turn of a session inside its own year.

A **thread** is one subject's value for one attribute over the whole span. Six classes differ only in how a thread behaves:

- `carried` — asserted once in the first year and never mentioned again. It must still be answerable in the last year.
- `replaced` — asserted, replaced, and replaced again. Only the latest value may come back.
- `reverted` — asserted, replaced, and then returned to the value it started with. What must come back is a value the thread already superseded once.
- `parallel` — two values of one attribute, both true, asserted years apart, neither contradicting the other.
- `refined` — one value, and then a narrower statement of the same attribute that does not contradict it. This is the case `memory-creation/`'s best-scoring structure beat with a per-case rule, which is why it is here.
- `fresh` — asserted in the year it is asked. The control that says whether a frame holds what it has just seen.

At the end of every year, every thread answerable that year is asked its attribute's question. The question names the subject and the attribute and never the value. Three things are then read off the answer: **recall**, the share of currently-true values present in the injected context, **stale returned**, whether any value that has stopped being true is present, and **units read**, how much of what the frame holds it touched to produce the answer, counted in the unit its own size is counted in. A checkpoint reads a frame and never writes to it.

Recall is never reported without the stale figure beside it. A frame that answers everything by keeping everything posts the best recall in the table and the worst stale figure, and a reader ranking on recall alone would crown it. Units read counts what a structure touches and not how long that takes: this record measures no latency, and the count is not a proxy for one. What it says is whether answering one question costs a structure a lookup or a pass over everything it holds, which over a decade is the difference between a store that stays answerable and one that does not.

Every planted value is a hyphenated pair of words, and no pair is planted on more than one thread. A value is therefore a token that identifies the assertion carrying it, so crediting is exact by reading — the trap #371 had to add a word boundary for cannot arise. `harness/workload_test.go` checks the uniqueness, checks that no turn carrying no fact mentions a planted value, and checks each class's schedule; `harness/measure_test.go` checks the credit rule against the substring cases it has to reject.

`replaced` and `reverted` state the new value without repeating the old one. A correction that named what it corrects would plant the stale value in the transcript, and the stale figure would then be counting the workload's own words.

`parallel` and `refined` differ in wording and in nothing else: both plant two simultaneously-true values of one attribute years apart. A design that scores well on one and badly on the other is keying on phrasing.

**How the disqualification criterion is read off these numbers.** The project owner's standard is that a design needing code added to handle a particular case is disqualified rather than patched. A per-case rule is not visible in a total, which is how the best-scoring structure of the last round passed. It is visible in a profile: each class is reported at each year, and the two coexistence classes are the same case in different words. A structure whose score comes from a rule written for one case has an uneven profile across the classes, or a gap between `parallel` and `refined`, and the tables below are laid out so that both show.

## The frames

- `shipped` — `internal/memory` as it runs: the SQLite store, hybrid search over embeddings and FTS, the one ranking function, supersession on insert, and the two passes `internal/gateway/queue.go` runs after a completed turn, threshold consolidation and tiered retention. `Recall` is what it puts in the context, bounded by the shipped token budget.
- `document` — no store at all: one bounded document holding the most recent turns' text, rewritten each turn, dropping the oldest to stay inside a limit set to the shipped token budget so both frames put contexts of the same size in front of the model. It performs no retrieval and returns the document whatever the question. It is the floor a structure with a store has to beat to be buying anything.
- `attribute-*` — a fact is held at its address, `(subject, attribute)`. Writing to an address is how a value is replaced, so identity is settled outright and similarity appears nowhere in the write path: the mechanism `memory-retrieval/` refuted is not weakened here, it is absent. The frame keeps no sentence, no vector and no keyword index — an address and a value — and answers a question by going to one place rather than by filling a budget. Four arms differ in how many values one address holds and in nothing else: `attribute-single` keeps one and a write replaces it; `attribute-multi` keeps every value ever written; `attribute-learned` and `attribute-cued` are the two rules §The arity question states; `attribute-declared` is the ceiling below.
- `interval` — a value is written with a start and is never removed. A replacement closes the open span instead of deleting the row, so a value that has stopped being true is still there with the span it was true for. It inherits the arity question and does not answer it — closing is replacing and not closing is coexisting — and it closes, because a frame that never closed would have nothing closed and the separation of what is true now from what was ever true would not exist. It answers as of the moment it is asked.
- `log` — every observation is an event, appended and never changed; the memory is not stored but computed from the log when it is asked for. Its arity decision lives in the projection, which makes it the only structure here where that decision can be taken again: no event is lost, so a different projection over the same events is a different memory with nothing re-observed. `harness/log_test.go` runs one log through both projections and gets both memories, which is what that claim rests on.
- `attribute-declared` — **not a candidate and not a structure.** It is the attribute store with arity supplied rather than decided: the write states whether the value replaces what is at its address or joins it, and the frame takes it. A frame reading that is not answering the arity question, it is assuming it answered. It is here as the ceiling every structure that has to decide for itself is read against, and it belongs in no ranking of designs — a reader scanning the rows for the best one has found the one row that is not competing.

**Why three of these post the same rows.** `attribute-single`, `interval` and `log` take the same arity decision — the newest value at an address is what is true there — in three structurally different stores. They therefore answer identically, class for class and year for year, and the tables below show that. It is the round's result rather than a copy-paste: the store shape does not change the answer, the arity rule does. `harness/measure_test.go` asserts the equality, so it fails if it stops being true. Where they differ is what they hold, what they read to answer, and what they can be asked, and those three columns are where this comparison lives.

**The arms bracket the writeback, and the bracket is not optional.** The shipped store is curation-only: only the writeback block writes to it, and the writeback is a model decision (`docs/architecture/memory.md`). With no model in the run something has to stand in for it, and picking one stand-in would hide a choice that moves every figure. So both frames run in both arms, and the arms bracket the same thing for every design.

In the `oracle` arm a fact-carrying turn arrives already extracted, and the shipped frame writes it as the semantic add `internal/gateway/writeback.go` derives; every other turn is an episodic observation. In the `text` arm no turn carries an extracted fact, so every turn is an episodic observation. Neither frame is told which arm it is in — the arms differ only in what a turn carries, and a frame that reads only the text cannot tell them apart, which `harness/measure_test.go` checks.

**The `oracle` arm is a ceiling, not a measurement of the shipped design.** It hands the writeback a perfect extractor that never misses a fact and never invents one. No real writeback has come close: `memory-cycle/` measured zero voluntary writeback blocks in fifteen of fifteen responses, and #336 is open on it. A reader who takes the `oracle` row for what the shipped design scores is reading the baseline flattered.

## The arity question

Addressing a fact at `(subject, attribute)` settles identity and moves the problem rather than removing it. If an address holds one value, a write replaces, and two values that are true at once have nowhere to live. If it holds many, nothing is ever replaced, and the structure reproduces the baseline's failure by a different route. So the frame has to decide, and the seam withholds the answer.

**No rule was found that is worth believing in.** Four were considered and three were rejected before being built; what follows is what each would have read, and the table §What the addresses look like is where that reading was checked rather than assumed.

- **The interval between the writes to one address.** A window that keeps recent values and retires older ones. The table gives the interval between the last two writes on every class: the two classes whose earlier value must go and the two whose earlier value must stay have intervals whose confidence intervals all overlap. No window separates them. For a timing rule to work, a workload would have to be one in which correcting is prompt and accumulating is slow — where a replacement follows what it replaces sooner than a second simultaneously-true value follows the first — and this one is not.
- **The statement's length**, on the reasoning that an addition has to say what makes it different and so costs words. Tested against the workload's own sentences before it was built: for one attribute the replacement is the longer statement and for another it is the shorter. It would have scored by accident.
- **Similarity between the new statement and the one already at the address.** This puts back exactly what addressing removes, and `memory-retrieval/` measured that it cannot separate a correction from two facts that must both survive — on the shipped embedder the correction family's median sits below the coexisting family's minimum.
- **Arity as a property of the attribute, learned from the attribute's own addresses.** This one is built and measured, as `attribute-learned`, because it is the rule the question invites: an attribute holds several values once any address under it has been written more than one, read off what the frame has observed rather than off a list of attributes written into it. It is one rule and it needs no clause per case.

`attribute-learned` collapses, and the table says why. Every attribute carries threads from every class, so the share of its addresses ever written a second value falls in one narrow band for all twelve of them. The generator assigns classes to subject-attribute pairs by shuffling every pair, so where inside that band an attribute lands is which threads it happened to be given and not a fact about the attribute — and a threshold set inside the band would separate attributes by that and nothing else. At the weakest threshold every attribute turns multi-valued the moment the first replacement lands, and the arm's rows are `attribute-multi`'s at every year and every class. At a majority threshold none of them ever turns, and it is `attribute-single`. The rule does not decide arity; the threshold does, and nothing calibrates the threshold.

**What separates the classes is the wording, and `attribute-cued` is that rule written down.** A value joins the ones already at its address when the sentence carries a phrase that marks an addition, and replaces them otherwise. It scores best of every arm measured here, on both figures at once, and it is disqualified. Not because reading language is illegitimate — the writeback this project ships reads language and every model-driven design will — but because the rule is a list that grows. Nothing in it generates its next entry. The workload's narrowing statements mark an addition with a qualifier instead of a conjunction, the list has no conjunction to find, and they are read as replacements: `refined` halves while `parallel`, which is the same requirement worded with a conjunction, is answered in full. Adding qualifiers to the list would answer `refined` and answer nothing else. That the repair exists and is a clause is what disqualifies the rule, and the two class columns beside each other are where a reader checks it rather than taking it on trust.

That wording is the only thing separating these classes is a fact about the workload, and a fact about conversation. Nothing but the words distinguishes "she leads that project as well" from "she leads that project now", and no structure recovers from the triple what the sentence carried. The conclusion is not that language is a poor signal. It is that a rule enumerating language is a poor structure, and that arity belongs where the language is read — in the write operation — rather than in the store that receives it.

## What a wrong declaration costs

`attribute-declared` answers everything and returns nothing stale, and it was measured with a declaration that is never wrong. In production that declaration comes from a model reading a sentence, and models err. This section measures what the recommendation costs when it is wrong, whether the damage repairs itself, and where a real model sits.

**The two directions are never blended, here or in any table below.** A write that should have joined and arrives as a replacement ends a value that was still true: the information is gone. A write that should have replaced and arrives as a join leaves a value that has stopped being true beside the one that replaced it: the information is wrong but still there. One rate covering both would let a reader average a failure that destroys with one that accumulates, and the tables report each against its own denominator.

The corruption is a property of the run and not of any structure. It is applied where an arm hands a turn over, so no frame carries a clause for it and no frame can tell a corrupted declaration from an honest one — which is what a frame would face from a writeback that is sometimes wrong. Which writes are corrupted comes from each turn's own index, so a rate corrupts the same writes whatever order a run reaches them in.

**Only the declared-operation frame is swept, and only in the oracle arm.** It is the one structure that reads a declaration, so it is the only one a wrong declaration can reach; the text arm carries no declaration for anything to be wrong about.

**What the rate is a share of.** Each direction can only corrupt the writes that declare its operation, and the eligible counts are in the table. They are very different: most writes over a decade replace, and the ones that join are the two coexistence classes' second values alone. The join direction therefore has coarse resolution — a rate below one over its eligible count corrupts nothing, which is why its lowest rows show no errors and no damage rather than a small amount of both.

**A write to an empty address cannot be wrong.** The first assertion at an address is the same store either way it is declared, so its declaration is unfalsifiable. Most assertions over a decade are of that kind, which is why the model's rates below are reported against two denominators: every eligible write, which is what the sweep's rate is a share of, and the writes that land on an address already holding something, which are the ones a declaration has consequences for.

**Healing is measured, not asserted.** After a wrong declaration, the tables report the share of corrupted addresses answering correctly again at the last year and how many checkpoints each carried the damage. `harness/misdeclaration_test.go` asserts the asymmetry those columns show.

## Whether a write need destroy

The two failures are not symmetric and the record already says why: a replacement clears the address, so it clears a wrongly-kept value with it, and nothing re-writes a wrongly-cleared one. That leaves one question, and it is the last one before a structure can be chosen — can a structure be built in which no write destroys, and what does it cost?

Three things are measured, all on the declarations already committed.

**Erring toward the cheaper failure.** A share of the writes that declare a replacement is resolved to a join instead, from taking every declaration as it comes to joining whatever the declaration says. The two ends are structures anyone could build; every point between them is a share of writes chosen by nothing, so the interior is a measuring instrument and not a design. What it is for is the shape of the trade, which is what a chooser needs and what this record did not have.

**A store where replacement closes a span rather than deleting.** `interval-declared` is the interval store taking the declaration instead of deciding for itself, so a wrongly-declared replacement closes a span rather than removing a row and the value is still there. It is run beside `attribute-declared` on the same declarations, and the question is whether keeping the value changes what can be answered.

**Whether the model is in doubt where it is wrong.** A bias needs something to bias on, and the candidate that does not ask the model for a number is asking it more than once and treating the samples disagreeing as the doubt. Every assertion whose declaration has a consequence was asked again, several times, at a temperature that lets the answer vary. Whether disagreement tracks error at all is the figure that decides whether any doubt-reading rule is possible, and it is reported before the rule built on it.

`harness/no_destroy_test.go` asserts each of these: that the interval store holds what a wrong declaration cleared and still cannot answer with it, that the bias's far end is what keeping every value already does, that both declaration-reading stores answer identically, and that a split resolves to a join.

## What a read cannot reach

Every figure before this one reports what a structure *answers*. None reports what it cannot reach, and the two are different questions. The writeback only ever sees what arrived in the context, so a memory no read returns is not merely unread — it is frozen, and it stays whatever it was for as long as it exists. A decade of rows in that state shows up in no recall figure.

**Related is read off the workload and never off similarity.** The workload knows which values are true, which thread each sits on and which subject it belongs to. A relation defined by a retrieval function would measure that function rather than the structure, which is the substitution this programme exists to avoid. Two relations are reported: the values live on the thread the question is about, which is recall under another name, and the values live anywhere on that subject, which is what a turn could have corrected had the context carried it.

**The frozen share is what a structure holds that no context has ever carried**, counted in planted values so the same number means the same thing for a store of rows and a store of addresses. It is reported per year so it can be seen accumulating, and beside it is how much of it a later checkpoint still needs — which is what forgetting the never-reached would cost.

**Whether a write can reach what a read cannot** is constructed rather than argued, in `harness/coupling_test.go`: a fact is written, never once read back, and then corrected, with enough other facts in the same words that the row a lookup must find is not alone. What each structure does with that correction is the measurement.

**Capacity is measured rather than bounded.** The trajectory is reported for every year rather than three of them, and what it extrapolates to is computed from it, so "nothing approaches a limit" is a finding rather than an assumption.

## What a run costs

Established before the run, as issue #443 requires.

No frame calls a model. Every structure measured here decides what it decides from what it was given, and only the shipped frame touches an embedder at all — the deterministic offline one `cmd/gagent` defaults to. The frames, the workload and the error sweep are free, and the model-call count in the first table below is the check on that rather than an assurance.

**Two things in this record were paid for**, and both are in a table of its own after every free figure: asking a model to declare the operation for each of the workload's assertions, and asking it again, several times over, for the assertions whose declaration has a consequence. Each is one call per question and nothing else, on the cheapest model that answers in one word without billing reasoning as output. Each refuses to start past its own call bound and to finish past its own cost bound, all four stated in that table, because a bound discovered afterwards is not a bound. Both answer sets are committed and the second measurement re-used the first rather than re-buying it. Its answers are committed, so scoring them costs nothing and re-running the measurement does not re-buy them: only `-declare` reaches the provider, and the key reaches the process from the environment and nothing else. What the shipped OpenAI embedder costs the read path, and which conclusions may be quoted across the two embedders, is #435's measurement and is not repeated here.

The figure that matters for the designs coming next is in the cost table below, and it is in tokens rather than money on purpose: a price list expires and a token count does not. A design that rewrites a bounded document with a model on every turn makes one call per turn for the whole span, and the table states what one run of one such design spends in prompt and output tokens. Multiply by the price of whatever model it uses at the time, and by the number of designs and seeds being compared.

## Results

<!-- generated by the harness; do not edit between the markers -->

### The run

| parameter | value |
|---|---|
| seed | 7 |
| embedder | hashing |
| first day | 2016-01-01T09:00:00Z |
| years | 10 |
| sessions per year | 36 |
| turns per session | 16 |
| fact threads per class | 40 |
| sessions | 360 |
| turns | 5760 |
| turns carrying a fact | 840 |
| turns carrying none | 4920 |
| fact threads | 600 |
| questions asked | 2320 |
| turn-stream digest | 466624f9ea463291930522af5b619bcf5fa2b07f44dbde8fb3e6c24837316eae |
| supersession similarity | 0.6 |
| retention keep salience | 0.6 |
| retention drop salience | 0.3 |
| retention window hours | 168 |
| consolidation threshold | 100 |
| salience of an episodic add | 0.5 |
| salience of a semantic add | 0.7 |
| recall token budget | 512 |
| document limit, bytes | 2048 |
| active index capacity | 10000 |

### What each frame answered, by year

| frame | arm | year | asked | recall | carried | replaced | reverted | parallel | refined | fresh | stale returned | context chars | units read |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| `shipped` | `oracle` | 1 | 160 | 0.919 ±0.042 | 0.925 ±0.083 | 0.950 ±0.068 | 0.900 ±0.094 | n/a | n/a | 0.900 ±0.094 | n/a | 2172 ±2 | 168.0 ±0.0 |
| `shipped` | `oracle` | 2 | 240 | 0.946 ±0.029 | 0.900 ±0.094 | 0.950 ±0.068 | 0.875 ±0.104 | 1.000 ±0.000 | 0.975 ±0.049 | 0.975 ±0.049 | n/a | 2169 ±2 | 272.0 ±0.0 |
| `shipped` | `oracle` | 3 | 240 | 0.921 ±0.034 | 0.900 ±0.094 | 0.925 ±0.083 | 0.750 ±0.136 | 0.975 ±0.049 | 0.975 ±0.049 | 1.000 ±0.000 | 0.863 ±0.076 | 2159 ±3 | 387.0 ±0.0 |
| `shipped` | `oracle` | 4 | 240 | 0.908 ±0.037 | 0.900 ±0.094 | 0.900 ±0.094 | 0.750 ±0.136 | 0.975 ±0.049 | 0.950 ±0.068 | 0.975 ±0.049 | 0.850 ±0.079 | 2162 ±3 | 425.0 ±0.0 |
| `shipped` | `oracle` | 5 | 240 | 0.912 ±0.036 | 0.900 ±0.094 | 0.900 ±0.094 | 0.750 ±0.136 | 0.975 ±0.049 | 0.950 ±0.068 | 1.000 ±0.000 | 0.838 ±0.081 | 2169 ±3 | 461.0 ±0.0 |
| `shipped` | `oracle` | 6 | 240 | 0.892 ±0.037 | 0.900 ±0.094 | 0.900 ±0.094 | 0.750 ±0.136 | 0.950 ±0.059 | 0.850 ±0.080 | 1.000 ±0.000 | 0.838 ±0.081 | 2161 ±2 | 552.0 ±0.0 |
| `shipped` | `oracle` | 7 | 240 | 0.927 ±0.029 | 0.900 ±0.094 | 0.875 ±0.104 | 1.000 ±0.000 | 0.938 ±0.063 | 0.850 ±0.080 | 1.000 ±0.000 | 0.838 ±0.081 | 2157 ±2 | 658.0 ±0.0 |
| `shipped` | `oracle` | 8 | 240 | 0.927 ±0.029 | 0.900 ±0.094 | 0.875 ±0.104 | 1.000 ±0.000 | 0.938 ±0.063 | 0.850 ±0.080 | 1.000 ±0.000 | 0.838 ±0.081 | 2161 ±3 | 698.0 ±0.0 |
| `shipped` | `oracle` | 9 | 240 | 0.925 ±0.030 | 0.900 ±0.094 | 0.875 ±0.104 | 1.000 ±0.000 | 0.925 ±0.066 | 0.850 ±0.080 | 1.000 ±0.000 | 0.838 ±0.081 | 2159 ±3 | 735.0 ±0.0 |
| `shipped` | `oracle` | 10 | 240 | 0.923 ±0.030 | 0.900 ±0.094 | 0.875 ±0.104 | 1.000 ±0.000 | 0.925 ±0.066 | 0.838 ±0.081 | 1.000 ±0.000 | 0.838 ±0.081 | 2160 ±2 | 783.0 ±0.0 |
| `document` | `oracle` | 1 | 160 | 0.075 ±0.041 | 0.000 ±0.000 | 0.100 ±0.094 | 0.125 ±0.104 | n/a | n/a | 0.075 ±0.083 | n/a | 2031 ±0 | 1.0 ±0.0 |
| `document` | `oracle` | 2 | 240 | 0.037 ±0.024 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.100 ±0.094 | 0.050 ±0.068 | 0.075 ±0.083 | n/a | 2008 ±0 | 1.0 ±0.0 |
| `document` | `oracle` | 3 | 240 | 0.037 ±0.024 | 0.000 ±0.000 | 0.100 ±0.094 | 0.025 ±0.049 | 0.000 ±0.000 | 0.000 ±0.000 | 0.100 ±0.094 | 0.000 ±0.000 | 2037 ±0 | 1.0 ±0.0 |
| `document` | `oracle` | 4 | 240 | 0.008 ±0.012 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.050 ±0.068 | 0.000 ±0.000 | 2042 ±0 | 1.0 ±0.0 |
| `document` | `oracle` | 5 | 240 | 0.025 ±0.020 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.150 ±0.112 | 0.000 ±0.000 | 2045 ±0 | 1.0 ±0.0 |
| `document` | `oracle` | 6 | 240 | 0.023 ±0.016 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.075 ±0.056 | 0.013 ±0.024 | 0.050 ±0.068 | 0.000 ±0.000 | 2029 ±0 | 1.0 ±0.0 |
| `document` | `oracle` | 7 | 240 | 0.042 ±0.025 | 0.000 ±0.000 | 0.125 ±0.104 | 0.075 ±0.083 | 0.000 ±0.000 | 0.000 ±0.000 | 0.050 ±0.068 | 0.000 ±0.000 | 2011 ±0 | 1.0 ±0.0 |
| `document` | `oracle` | 8 | 240 | 0.021 ±0.018 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.125 ±0.104 | 0.000 ±0.000 | 2018 ±0 | 1.0 ±0.0 |
| `document` | `oracle` | 9 | 240 | 0.004 ±0.008 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.025 ±0.049 | 0.000 ±0.000 | 2017 ±0 | 1.0 ±0.0 |
| `document` | `oracle` | 10 | 240 | 0.008 ±0.012 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.050 ±0.068 | 0.000 ±0.000 | 2047 ±0 | 1.0 ±0.0 |
| `attribute-single` | `oracle` | 1 | 160 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a | 26 ±0 | 1.0 ±0.0 |
| `attribute-single` | `oracle` | 2 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | 25 ±0 | 1.0 ±0.0 |
| `attribute-single` | `oracle` | 3 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 | 1.0 ±0.0 |
| `attribute-single` | `oracle` | 4 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 | 1.0 ±0.0 |
| `attribute-single` | `oracle` | 5 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 | 1.0 ±0.0 |
| `attribute-single` | `oracle` | 6 | 240 | 0.833 ±0.030 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 | 1.0 ±0.0 |
| `attribute-single` | `oracle` | 7 | 240 | 0.833 ±0.030 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 25 ±0 | 1.0 ±0.0 |
| `attribute-single` | `oracle` | 8 | 240 | 0.833 ±0.030 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 25 ±0 | 1.0 ±0.0 |
| `attribute-single` | `oracle` | 9 | 240 | 0.833 ±0.030 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 25 ±0 | 1.0 ±0.0 |
| `attribute-single` | `oracle` | 10 | 240 | 0.833 ±0.030 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 25 ±0 | 1.0 ±0.0 |
| `attribute-multi` | `oracle` | 1 | 160 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a | 26 ±0 | 1.0 ±0.0 |
| `attribute-multi` | `oracle` | 2 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | 25 ±0 | 1.0 ±0.0 |
| `attribute-multi` | `oracle` | 3 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 34 ±2 | 1.3 ±0.1 |
| `attribute-multi` | `oracle` | 4 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 34 ±2 | 1.3 ±0.1 |
| `attribute-multi` | `oracle` | 5 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 34 ±2 | 1.3 ±0.1 |
| `attribute-multi` | `oracle` | 6 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 43 ±2 | 1.7 ±0.1 |
| `attribute-multi` | `oracle` | 7 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 47 ±2 | 1.8 ±0.1 |
| `attribute-multi` | `oracle` | 8 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 47 ±2 | 1.8 ±0.1 |
| `attribute-multi` | `oracle` | 9 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 47 ±2 | 1.8 ±0.1 |
| `attribute-multi` | `oracle` | 10 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 48 ±2 | 1.8 ±0.1 |
| `attribute-learned` | `oracle` | 1 | 160 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a | 26 ±0 | 161.0 ±0.0 |
| `attribute-learned` | `oracle` | 2 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | 25 ±0 | 281.0 ±0.0 |
| `attribute-learned` | `oracle` | 3 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 34 ±2 | 321.3 ±0.1 |
| `attribute-learned` | `oracle` | 4 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 34 ±2 | 361.3 ±0.1 |
| `attribute-learned` | `oracle` | 5 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 34 ±2 | 401.3 ±0.1 |
| `attribute-learned` | `oracle` | 6 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 43 ±2 | 441.7 ±0.1 |
| `attribute-learned` | `oracle` | 7 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 47 ±2 | 481.8 ±0.1 |
| `attribute-learned` | `oracle` | 8 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 47 ±2 | 521.8 ±0.1 |
| `attribute-learned` | `oracle` | 9 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 47 ±2 | 561.8 ±0.1 |
| `attribute-learned` | `oracle` | 10 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 48 ±2 | 601.8 ±0.1 |
| `attribute-cued` | `oracle` | 1 | 160 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a | 26 ±0 | 1.0 ±0.0 |
| `attribute-cued` | `oracle` | 2 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | 25 ±0 | 1.0 ±0.0 |
| `attribute-cued` | `oracle` | 3 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 | 1.0 ±0.0 |
| `attribute-cued` | `oracle` | 4 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 | 1.0 ±0.0 |
| `attribute-cued` | `oracle` | 5 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 | 1.0 ±0.0 |
| `attribute-cued` | `oracle` | 6 | 240 | 0.917 ±0.024 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 30 ±1 | 1.2 ±0.0 |
| `attribute-cued` | `oracle` | 7 | 240 | 0.917 ±0.024 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 30 ±1 | 1.2 ±0.0 |
| `attribute-cued` | `oracle` | 8 | 240 | 0.917 ±0.024 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 30 ±1 | 1.2 ±0.0 |
| `attribute-cued` | `oracle` | 9 | 240 | 0.917 ±0.024 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 30 ±1 | 1.2 ±0.0 |
| `attribute-cued` | `oracle` | 10 | 240 | 0.917 ±0.024 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 30 ±1 | 1.2 ±0.0 |
| `attribute-declared` | `oracle` | 1 | 160 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a | 26 ±0 | 1.0 ±0.0 |
| `attribute-declared` | `oracle` | 2 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | 25 ±0 | 1.0 ±0.0 |
| `attribute-declared` | `oracle` | 3 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 | 1.0 ±0.0 |
| `attribute-declared` | `oracle` | 4 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 | 1.0 ±0.0 |
| `attribute-declared` | `oracle` | 5 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 | 1.0 ±0.0 |
| `attribute-declared` | `oracle` | 6 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 34 ±2 | 1.3 ±0.1 |
| `attribute-declared` | `oracle` | 7 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 34 ±2 | 1.3 ±0.1 |
| `attribute-declared` | `oracle` | 8 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 34 ±2 | 1.3 ±0.1 |
| `attribute-declared` | `oracle` | 9 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 34 ±2 | 1.3 ±0.1 |
| `attribute-declared` | `oracle` | 10 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 34 ±2 | 1.3 ±0.1 |
| `interval` | `oracle` | 1 | 160 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a | 26 ±0 | 1.0 ±0.0 |
| `interval` | `oracle` | 2 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | 25 ±0 | 1.0 ±0.0 |
| `interval` | `oracle` | 3 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 | 1.3 ±0.1 |
| `interval` | `oracle` | 4 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 | 1.3 ±0.1 |
| `interval` | `oracle` | 5 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 | 1.3 ±0.1 |
| `interval` | `oracle` | 6 | 240 | 0.833 ±0.030 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 | 1.7 ±0.1 |
| `interval` | `oracle` | 7 | 240 | 0.833 ±0.030 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 25 ±0 | 2.0 ±0.1 |
| `interval` | `oracle` | 8 | 240 | 0.833 ±0.030 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 25 ±0 | 2.0 ±0.1 |
| `interval` | `oracle` | 9 | 240 | 0.833 ±0.030 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 25 ±0 | 2.0 ±0.1 |
| `interval` | `oracle` | 10 | 240 | 0.833 ±0.030 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 25 ±0 | 2.0 ±0.1 |
| `interval-declared` | `oracle` | 1 | 160 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a | 26 ±0 | 1.0 ±0.0 |
| `interval-declared` | `oracle` | 2 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | 25 ±0 | 1.0 ±0.0 |
| `interval-declared` | `oracle` | 3 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 | 1.3 ±0.1 |
| `interval-declared` | `oracle` | 4 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 | 1.3 ±0.1 |
| `interval-declared` | `oracle` | 5 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 | 1.3 ±0.1 |
| `interval-declared` | `oracle` | 6 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 34 ±2 | 1.7 ±0.1 |
| `interval-declared` | `oracle` | 7 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 34 ±2 | 2.0 ±0.1 |
| `interval-declared` | `oracle` | 8 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 34 ±2 | 2.0 ±0.1 |
| `interval-declared` | `oracle` | 9 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 34 ±2 | 2.0 ±0.1 |
| `interval-declared` | `oracle` | 10 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 34 ±2 | 2.0 ±0.1 |
| `log` | `oracle` | 1 | 160 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a | 26 ±0 | 160.0 ±0.0 |
| `log` | `oracle` | 2 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | 25 ±0 | 280.0 ±0.0 |
| `log` | `oracle` | 3 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 | 400.0 ±0.0 |
| `log` | `oracle` | 4 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 | 440.0 ±0.0 |
| `log` | `oracle` | 5 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 | 480.0 ±0.0 |
| `log` | `oracle` | 6 | 240 | 0.833 ±0.030 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 | 600.0 ±0.0 |
| `log` | `oracle` | 7 | 240 | 0.833 ±0.030 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 25 ±0 | 720.0 ±0.0 |
| `log` | `oracle` | 8 | 240 | 0.833 ±0.030 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 25 ±0 | 760.0 ±0.0 |
| `log` | `oracle` | 9 | 240 | 0.833 ±0.030 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 25 ±0 | 800.0 ±0.0 |
| `log` | `oracle` | 10 | 240 | 0.833 ±0.030 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 25 ±0 | 840.0 ±0.0 |
| `shipped` | `text` | 1 | 160 | 0.056 ±0.036 | 0.000 ±0.000 | 0.075 ±0.083 | 0.075 ±0.083 | n/a | n/a | 0.075 ±0.083 | n/a | 1304 ±0 | 30.0 ±0.0 |
| `shipped` | `text` | 2 | 240 | 0.017 ±0.016 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.025 ±0.049 | 0.025 ±0.049 | 0.050 ±0.068 | n/a | 610 ±0 | 13.0 ±0.0 |
| `shipped` | `text` | 3 | 240 | 0.025 ±0.020 | 0.000 ±0.000 | 0.050 ±0.068 | 0.025 ±0.049 | 0.000 ±0.000 | 0.000 ±0.000 | 0.075 ±0.083 | 0.000 ±0.000 | 1263 ±0 | 29.0 ±0.0 |
| `shipped` | `text` | 4 | 240 | 0.004 ±0.008 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.025 ±0.049 | 0.000 ±0.000 | 1183 ±0 | 26.0 ±0.0 |
| `shipped` | `text` | 5 | 240 | 0.021 ±0.018 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.125 ±0.104 | 0.000 ±0.000 | 1216 ±0 | 28.0 ±0.0 |
| `shipped` | `text` | 6 | 240 | 0.013 ±0.013 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.013 ±0.024 | 0.013 ±0.024 | 0.050 ±0.068 | 0.000 ±0.000 | 572 ±0 | 13.0 ±0.0 |
| `shipped` | `text` | 7 | 240 | 0.013 ±0.014 | 0.000 ±0.000 | 0.050 ±0.068 | 0.025 ±0.049 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 539 ±0 | 12.0 ±0.0 |
| `shipped` | `text` | 8 | 240 | 0.004 ±0.008 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.025 ±0.049 | 0.000 ±0.000 | 500 ±0 | 11.0 ±0.0 |
| `shipped` | `text` | 9 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 430 ±0 | 9.0 ±0.0 |
| `shipped` | `text` | 10 | 240 | 0.008 ±0.012 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.050 ±0.068 | 0.000 ±0.000 | 977 ±0 | 22.0 ±0.0 |
| `document` | `text` | 1 | 160 | 0.075 ±0.041 | 0.000 ±0.000 | 0.100 ±0.094 | 0.125 ±0.104 | n/a | n/a | 0.075 ±0.083 | n/a | 2031 ±0 | 1.0 ±0.0 |
| `document` | `text` | 2 | 240 | 0.037 ±0.024 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.100 ±0.094 | 0.050 ±0.068 | 0.075 ±0.083 | n/a | 2008 ±0 | 1.0 ±0.0 |
| `document` | `text` | 3 | 240 | 0.037 ±0.024 | 0.000 ±0.000 | 0.100 ±0.094 | 0.025 ±0.049 | 0.000 ±0.000 | 0.000 ±0.000 | 0.100 ±0.094 | 0.000 ±0.000 | 2037 ±0 | 1.0 ±0.0 |
| `document` | `text` | 4 | 240 | 0.008 ±0.012 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.050 ±0.068 | 0.000 ±0.000 | 2042 ±0 | 1.0 ±0.0 |
| `document` | `text` | 5 | 240 | 0.025 ±0.020 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.150 ±0.112 | 0.000 ±0.000 | 2045 ±0 | 1.0 ±0.0 |
| `document` | `text` | 6 | 240 | 0.023 ±0.016 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.075 ±0.056 | 0.013 ±0.024 | 0.050 ±0.068 | 0.000 ±0.000 | 2029 ±0 | 1.0 ±0.0 |
| `document` | `text` | 7 | 240 | 0.042 ±0.025 | 0.000 ±0.000 | 0.125 ±0.104 | 0.075 ±0.083 | 0.000 ±0.000 | 0.000 ±0.000 | 0.050 ±0.068 | 0.000 ±0.000 | 2011 ±0 | 1.0 ±0.0 |
| `document` | `text` | 8 | 240 | 0.021 ±0.018 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.125 ±0.104 | 0.000 ±0.000 | 2018 ±0 | 1.0 ±0.0 |
| `document` | `text` | 9 | 240 | 0.004 ±0.008 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.025 ±0.049 | 0.000 ±0.000 | 2017 ±0 | 1.0 ±0.0 |
| `document` | `text` | 10 | 240 | 0.008 ±0.012 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.050 ±0.068 | 0.000 ±0.000 | 2047 ±0 | 1.0 ±0.0 |
| `attribute-single` | `text` | 1 | 160 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | n/a | 0.000 ±0.000 | n/a | 0 ±0 | 0.0 ±0.0 |
| `attribute-single` | `text` | 2 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | 0 ±0 | 0.0 ±0.0 |
| `attribute-single` | `text` | 3 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-single` | `text` | 4 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-single` | `text` | 5 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-single` | `text` | 6 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-single` | `text` | 7 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-single` | `text` | 8 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-single` | `text` | 9 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-single` | `text` | 10 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-multi` | `text` | 1 | 160 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | n/a | 0.000 ±0.000 | n/a | 0 ±0 | 0.0 ±0.0 |
| `attribute-multi` | `text` | 2 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | 0 ±0 | 0.0 ±0.0 |
| `attribute-multi` | `text` | 3 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-multi` | `text` | 4 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-multi` | `text` | 5 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-multi` | `text` | 6 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-multi` | `text` | 7 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-multi` | `text` | 8 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-multi` | `text` | 9 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-multi` | `text` | 10 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-learned` | `text` | 1 | 160 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | n/a | 0.000 ±0.000 | n/a | 0 ±0 | 0.0 ±0.0 |
| `attribute-learned` | `text` | 2 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | 0 ±0 | 0.0 ±0.0 |
| `attribute-learned` | `text` | 3 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-learned` | `text` | 4 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-learned` | `text` | 5 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-learned` | `text` | 6 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-learned` | `text` | 7 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-learned` | `text` | 8 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-learned` | `text` | 9 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-learned` | `text` | 10 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-cued` | `text` | 1 | 160 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | n/a | 0.000 ±0.000 | n/a | 0 ±0 | 0.0 ±0.0 |
| `attribute-cued` | `text` | 2 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | 0 ±0 | 0.0 ±0.0 |
| `attribute-cued` | `text` | 3 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-cued` | `text` | 4 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-cued` | `text` | 5 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-cued` | `text` | 6 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-cued` | `text` | 7 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-cued` | `text` | 8 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-cued` | `text` | 9 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-cued` | `text` | 10 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-declared` | `text` | 1 | 160 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | n/a | 0.000 ±0.000 | n/a | 0 ±0 | 0.0 ±0.0 |
| `attribute-declared` | `text` | 2 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | 0 ±0 | 0.0 ±0.0 |
| `attribute-declared` | `text` | 3 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-declared` | `text` | 4 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-declared` | `text` | 5 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-declared` | `text` | 6 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-declared` | `text` | 7 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-declared` | `text` | 8 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-declared` | `text` | 9 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `attribute-declared` | `text` | 10 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `interval` | `text` | 1 | 160 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | n/a | 0.000 ±0.000 | n/a | 0 ±0 | 0.0 ±0.0 |
| `interval` | `text` | 2 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | 0 ±0 | 0.0 ±0.0 |
| `interval` | `text` | 3 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `interval` | `text` | 4 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `interval` | `text` | 5 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `interval` | `text` | 6 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `interval` | `text` | 7 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `interval` | `text` | 8 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `interval` | `text` | 9 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `interval` | `text` | 10 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `interval-declared` | `text` | 1 | 160 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | n/a | 0.000 ±0.000 | n/a | 0 ±0 | 0.0 ±0.0 |
| `interval-declared` | `text` | 2 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | 0 ±0 | 0.0 ±0.0 |
| `interval-declared` | `text` | 3 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `interval-declared` | `text` | 4 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `interval-declared` | `text` | 5 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `interval-declared` | `text` | 6 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `interval-declared` | `text` | 7 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `interval-declared` | `text` | 8 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `interval-declared` | `text` | 9 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `interval-declared` | `text` | 10 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `log` | `text` | 1 | 160 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | n/a | 0.000 ±0.000 | n/a | 0 ±0 | 0.0 ±0.0 |
| `log` | `text` | 2 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | 0 ±0 | 0.0 ±0.0 |
| `log` | `text` | 3 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `log` | `text` | 4 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `log` | `text` | 5 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `log` | `text` | 6 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `log` | `text` | 7 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `log` | `text` | 8 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `log` | `text` | 9 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |
| `log` | `text` | 10 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 | 0.0 ±0.0 |

### What each frame held

| frame | arm | year | units | content KB | stored KB |
|---|---|---|---|---|---|
| `shipped` | `oracle` | 1 | 168 | 6.5 | 856.0 |
| `shipped` | `oracle` | 5 | 461 | 18.2 | 4060.0 |
| `shipped` | `oracle` | 10 | 783 | 31.6 | 8112.0 |
| `document` | `oracle` | 1 | 1 | 2.0 | 2.0 |
| `document` | `oracle` | 5 | 1 | 2.0 | 2.0 |
| `document` | `oracle` | 10 | 1 | 2.0 | 2.0 |
| `attribute-single` | `oracle` | 1 | 160 | 4.0 | 4.0 |
| `attribute-single` | `oracle` | 5 | 400 | 10.0 | 10.0 |
| `attribute-single` | `oracle` | 10 | 600 | 14.9 | 14.9 |
| `attribute-multi` | `oracle` | 1 | 160 | 4.0 | 4.0 |
| `attribute-multi` | `oracle` | 5 | 480 | 12.0 | 12.0 |
| `attribute-multi` | `oracle` | 10 | 800 | 19.9 | 19.9 |
| `attribute-learned` | `oracle` | 1 | 160 | 4.0 | 4.0 |
| `attribute-learned` | `oracle` | 5 | 480 | 12.0 | 12.0 |
| `attribute-learned` | `oracle` | 10 | 800 | 19.9 | 19.9 |
| `attribute-cued` | `oracle` | 1 | 160 | 4.0 | 4.0 |
| `attribute-cued` | `oracle` | 5 | 400 | 10.0 | 10.0 |
| `attribute-cued` | `oracle` | 10 | 640 | 15.9 | 15.9 |
| `attribute-declared` | `oracle` | 1 | 160 | 4.0 | 4.0 |
| `attribute-declared` | `oracle` | 5 | 400 | 10.0 | 10.0 |
| `attribute-declared` | `oracle` | 10 | 680 | 16.9 | 16.9 |
| `interval` | `oracle` | 1 | 160 | 4.0 | 4.0 |
| `interval` | `oracle` | 5 | 480 | 12.0 | 12.0 |
| `interval` | `oracle` | 10 | 840 | 20.9 | 20.9 |
| `interval-declared` | `oracle` | 1 | 160 | 4.0 | 4.0 |
| `interval-declared` | `oracle` | 5 | 480 | 12.0 | 12.0 |
| `interval-declared` | `oracle` | 10 | 840 | 20.9 | 20.9 |
| `log` | `oracle` | 1 | 160 | 4.0 | 4.0 |
| `log` | `oracle` | 5 | 480 | 12.0 | 12.0 |
| `log` | `oracle` | 10 | 840 | 20.9 | 20.9 |
| `shipped` | `text` | 1 | 30 | 1.2 | 812.0 |
| `shipped` | `text` | 5 | 28 | 1.2 | 3940.0 |
| `shipped` | `text` | 10 | 22 | 0.9 | 7844.0 |
| `document` | `text` | 1 | 1 | 2.0 | 2.0 |
| `document` | `text` | 5 | 1 | 2.0 | 2.0 |
| `document` | `text` | 10 | 1 | 2.0 | 2.0 |
| `attribute-single` | `text` | 1 | 0 | 0.0 | 0.0 |
| `attribute-single` | `text` | 5 | 0 | 0.0 | 0.0 |
| `attribute-single` | `text` | 10 | 0 | 0.0 | 0.0 |
| `attribute-multi` | `text` | 1 | 0 | 0.0 | 0.0 |
| `attribute-multi` | `text` | 5 | 0 | 0.0 | 0.0 |
| `attribute-multi` | `text` | 10 | 0 | 0.0 | 0.0 |
| `attribute-learned` | `text` | 1 | 0 | 0.0 | 0.0 |
| `attribute-learned` | `text` | 5 | 0 | 0.0 | 0.0 |
| `attribute-learned` | `text` | 10 | 0 | 0.0 | 0.0 |
| `attribute-cued` | `text` | 1 | 0 | 0.0 | 0.0 |
| `attribute-cued` | `text` | 5 | 0 | 0.0 | 0.0 |
| `attribute-cued` | `text` | 10 | 0 | 0.0 | 0.0 |
| `attribute-declared` | `text` | 1 | 0 | 0.0 | 0.0 |
| `attribute-declared` | `text` | 5 | 0 | 0.0 | 0.0 |
| `attribute-declared` | `text` | 10 | 0 | 0.0 | 0.0 |
| `interval` | `text` | 1 | 0 | 0.0 | 0.0 |
| `interval` | `text` | 5 | 0 | 0.0 | 0.0 |
| `interval` | `text` | 10 | 0 | 0.0 | 0.0 |
| `interval-declared` | `text` | 1 | 0 | 0.0 | 0.0 |
| `interval-declared` | `text` | 5 | 0 | 0.0 | 0.0 |
| `interval-declared` | `text` | 10 | 0 | 0.0 | 0.0 |
| `log` | `text` | 1 | 0 | 0.0 | 0.0 |
| `log` | `text` | 5 | 0 | 0.0 | 0.0 |
| `log` | `text` | 10 | 0 | 0.0 | 0.0 |

### What the addresses look like

| class | addresses | values per address | years between the last two writes |
|---|---|---|---|
| `carried` | 40 | 1.000 ±0.000 | n/a |
| `replaced` | 40 | 3.000 ±0.000 | 4.05 ±0.14 |
| `reverted` | 40 | 2.000 ±0.000 | 4.01 ±0.13 |
| `parallel` | 40 | 2.000 ±0.000 | 4.04 ±0.14 |
| `refined` | 40 | 2.000 ±0.000 | 4.10 ±0.12 |
| `fresh` | 400 | 1.000 ±0.000 | n/a |

| attribute | addresses | written more than one value | share |
|---|---|---|---|
| `project` | 44 | 12 | 0.273 |
| `clinic` | 56 | 15 | 0.268 |
| `line` | 53 | 10 | 0.189 |
| `accounts` | 46 | 14 | 0.304 |
| `hall` | 48 | 17 | 0.354 |
| `supplier` | 52 | 15 | 0.288 |
| `cottage` | 48 | 8 | 0.167 |
| `bank` | 59 | 18 | 0.305 |
| `course` | 49 | 12 | 0.245 |
| `team` | 52 | 14 | 0.269 |
| `bakery` | 47 | 11 | 0.234 |
| `backups` | 46 | 14 | 0.304 |

### What a read reached, and what it never did

| frame | arm | year | thread reach | subject reach | held | reached | frozen | frozen share | frozen still needed |
|---|---|---|---|---|---|---|---|---|---|
| `shipped` | `oracle` | 1 | 0.919 ±0.042 | 0.919 ±0.028 | 147 | 147 | 0 | 0.000 | 0 |
| `shipped` | `oracle` | 2 | 0.946 ±0.029 | 0.941 ±0.017 | 263 | 263 | 0 | 0.000 | 0 |
| `shipped` | `oracle` | 3 | 0.921 ±0.034 | 0.881 ±0.024 | 364 | 364 | 0 | 0.000 | 0 |
| `shipped` | `oracle` | 4 | 0.908 ±0.037 | 0.880 ±0.024 | 400 | 400 | 0 | 0.000 | 0 |
| `shipped` | `oracle` | 5 | 0.912 ±0.036 | 0.878 ±0.023 | 438 | 438 | 0 | 0.000 | 0 |
| `shipped` | `oracle` | 6 | 0.892 ±0.037 | 0.750 ±0.031 | 543 | 543 | 0 | 0.000 | 0 |
| `shipped` | `oracle` | 7 | 0.927 ±0.029 | 0.576 ±0.042 | 619 | 619 | 0 | 0.000 | 0 |
| `shipped` | `oracle` | 8 | 0.927 ±0.029 | 0.560 ±0.044 | 658 | 658 | 0 | 0.000 | 0 |
| `shipped` | `oracle` | 9 | 0.925 ±0.030 | 0.518 ±0.044 | 696 | 696 | 0 | 0.000 | 0 |
| `shipped` | `oracle` | 10 | 0.923 ±0.030 | 0.507 ±0.045 | 733 | 733 | 0 | 0.000 | 0 |
| `document` | `oracle` | 1 | 0.075 ±0.041 | 0.075 ±0.032 | 12 | 12 | 0 | 0.000 | 0 |
| `document` | `oracle` | 2 | 0.037 ±0.024 | 0.030 ±0.010 | 9 | 9 | 0 | 0.000 | 0 |
| `document` | `oracle` | 3 | 0.037 ±0.024 | 0.030 ±0.009 | 9 | 9 | 0 | 0.000 | 0 |
| `document` | `oracle` | 4 | 0.008 ±0.012 | 0.007 ±0.004 | 2 | 2 | 0 | 0.000 | 0 |
| `document` | `oracle` | 5 | 0.025 ±0.020 | 0.017 ±0.007 | 6 | 6 | 0 | 0.000 | 0 |
| `document` | `oracle` | 6 | 0.023 ±0.016 | 0.017 ±0.006 | 9 | 9 | 0 | 0.000 | 0 |
| `document` | `oracle` | 7 | 0.042 ±0.025 | 0.024 ±0.008 | 10 | 10 | 0 | 0.000 | 0 |
| `document` | `oracle` | 8 | 0.021 ±0.018 | 0.009 ±0.006 | 5 | 5 | 0 | 0.000 | 0 |
| `document` | `oracle` | 9 | 0.004 ±0.008 | 0.002 ±0.002 | 1 | 1 | 0 | 0.000 | 0 |
| `document` | `oracle` | 10 | 0.008 ±0.012 | 0.003 ±0.002 | 2 | 2 | 0 | 0.000 | 0 |
| `attribute-single` | `oracle` | 1 | 1.000 ±0.000 | 0.450 ±0.037 | 160 | 160 | 0 | 0.000 | 0 |
| `attribute-single` | `oracle` | 2 | 1.000 ±0.000 | 0.269 ±0.021 | 280 | 280 | 0 | 0.000 | 0 |
| `attribute-single` | `oracle` | 3 | 1.000 ±0.000 | 0.246 ±0.018 | 320 | 320 | 0 | 0.000 | 0 |
| `attribute-single` | `oracle` | 4 | 1.000 ±0.000 | 0.212 ±0.011 | 360 | 360 | 0 | 0.000 | 0 |
| `attribute-single` | `oracle` | 5 | 1.000 ±0.000 | 0.194 ±0.010 | 400 | 400 | 0 | 0.000 | 0 |
| `attribute-single` | `oracle` | 6 | 0.833 ±0.030 | 0.149 ±0.008 | 440 | 440 | 0 | 0.000 | 0 |
| `attribute-single` | `oracle` | 7 | 0.833 ±0.030 | 0.139 ±0.007 | 480 | 480 | 0 | 0.000 | 0 |
| `attribute-single` | `oracle` | 8 | 0.833 ±0.030 | 0.131 ±0.006 | 520 | 520 | 0 | 0.000 | 0 |
| `attribute-single` | `oracle` | 9 | 0.833 ±0.030 | 0.117 ±0.004 | 560 | 560 | 0 | 0.000 | 0 |
| `attribute-single` | `oracle` | 10 | 0.833 ±0.030 | 0.113 ±0.004 | 600 | 600 | 0 | 0.000 | 0 |
| `attribute-multi` | `oracle` | 1 | 1.000 ±0.000 | 0.450 ±0.037 | 160 | 160 | 0 | 0.000 | 0 |
| `attribute-multi` | `oracle` | 2 | 1.000 ±0.000 | 0.269 ±0.021 | 280 | 280 | 0 | 0.000 | 0 |
| `attribute-multi` | `oracle` | 3 | 1.000 ±0.000 | 0.246 ±0.018 | 400 | 400 | 0 | 0.000 | 0 |
| `attribute-multi` | `oracle` | 4 | 1.000 ±0.000 | 0.212 ±0.011 | 440 | 440 | 0 | 0.000 | 0 |
| `attribute-multi` | `oracle` | 5 | 1.000 ±0.000 | 0.194 ±0.010 | 480 | 480 | 0 | 0.000 | 0 |
| `attribute-multi` | `oracle` | 6 | 1.000 ±0.000 | 0.192 ±0.011 | 600 | 600 | 0 | 0.000 | 0 |
| `attribute-multi` | `oracle` | 7 | 1.000 ±0.000 | 0.179 ±0.010 | 680 | 680 | 0 | 0.000 | 0 |
| `attribute-multi` | `oracle` | 8 | 1.000 ±0.000 | 0.169 ±0.009 | 720 | 720 | 0 | 0.000 | 0 |
| `attribute-multi` | `oracle` | 9 | 1.000 ±0.000 | 0.153 ±0.008 | 760 | 760 | 0 | 0.000 | 0 |
| `attribute-multi` | `oracle` | 10 | 1.000 ±0.000 | 0.148 ±0.008 | 800 | 800 | 0 | 0.000 | 0 |
| `attribute-learned` | `oracle` | 1 | 1.000 ±0.000 | 0.450 ±0.037 | 160 | 160 | 0 | 0.000 | 0 |
| `attribute-learned` | `oracle` | 2 | 1.000 ±0.000 | 0.269 ±0.021 | 280 | 280 | 0 | 0.000 | 0 |
| `attribute-learned` | `oracle` | 3 | 1.000 ±0.000 | 0.246 ±0.018 | 400 | 400 | 0 | 0.000 | 0 |
| `attribute-learned` | `oracle` | 4 | 1.000 ±0.000 | 0.212 ±0.011 | 440 | 440 | 0 | 0.000 | 0 |
| `attribute-learned` | `oracle` | 5 | 1.000 ±0.000 | 0.194 ±0.010 | 480 | 480 | 0 | 0.000 | 0 |
| `attribute-learned` | `oracle` | 6 | 1.000 ±0.000 | 0.192 ±0.011 | 600 | 600 | 0 | 0.000 | 0 |
| `attribute-learned` | `oracle` | 7 | 1.000 ±0.000 | 0.179 ±0.010 | 680 | 680 | 0 | 0.000 | 0 |
| `attribute-learned` | `oracle` | 8 | 1.000 ±0.000 | 0.169 ±0.009 | 720 | 720 | 0 | 0.000 | 0 |
| `attribute-learned` | `oracle` | 9 | 1.000 ±0.000 | 0.153 ±0.008 | 760 | 760 | 0 | 0.000 | 0 |
| `attribute-learned` | `oracle` | 10 | 1.000 ±0.000 | 0.148 ±0.008 | 800 | 800 | 0 | 0.000 | 0 |
| `attribute-cued` | `oracle` | 1 | 1.000 ±0.000 | 0.450 ±0.037 | 160 | 160 | 0 | 0.000 | 0 |
| `attribute-cued` | `oracle` | 2 | 1.000 ±0.000 | 0.269 ±0.021 | 280 | 280 | 0 | 0.000 | 0 |
| `attribute-cued` | `oracle` | 3 | 1.000 ±0.000 | 0.246 ±0.018 | 320 | 320 | 0 | 0.000 | 0 |
| `attribute-cued` | `oracle` | 4 | 1.000 ±0.000 | 0.212 ±0.011 | 360 | 360 | 0 | 0.000 | 0 |
| `attribute-cued` | `oracle` | 5 | 1.000 ±0.000 | 0.194 ±0.010 | 400 | 400 | 0 | 0.000 | 0 |
| `attribute-cued` | `oracle` | 6 | 0.917 ±0.024 | 0.171 ±0.010 | 480 | 480 | 0 | 0.000 | 0 |
| `attribute-cued` | `oracle` | 7 | 0.917 ±0.024 | 0.159 ±0.009 | 520 | 520 | 0 | 0.000 | 0 |
| `attribute-cued` | `oracle` | 8 | 0.917 ±0.024 | 0.150 ±0.008 | 560 | 560 | 0 | 0.000 | 0 |
| `attribute-cued` | `oracle` | 9 | 0.917 ±0.024 | 0.135 ±0.007 | 600 | 600 | 0 | 0.000 | 0 |
| `attribute-cued` | `oracle` | 10 | 0.917 ±0.024 | 0.131 ±0.007 | 640 | 640 | 0 | 0.000 | 0 |
| `attribute-declared` | `oracle` | 1 | 1.000 ±0.000 | 0.450 ±0.037 | 160 | 160 | 0 | 0.000 | 0 |
| `attribute-declared` | `oracle` | 2 | 1.000 ±0.000 | 0.269 ±0.021 | 280 | 280 | 0 | 0.000 | 0 |
| `attribute-declared` | `oracle` | 3 | 1.000 ±0.000 | 0.246 ±0.018 | 320 | 320 | 0 | 0.000 | 0 |
| `attribute-declared` | `oracle` | 4 | 1.000 ±0.000 | 0.212 ±0.011 | 360 | 360 | 0 | 0.000 | 0 |
| `attribute-declared` | `oracle` | 5 | 1.000 ±0.000 | 0.194 ±0.010 | 400 | 400 | 0 | 0.000 | 0 |
| `attribute-declared` | `oracle` | 6 | 1.000 ±0.000 | 0.192 ±0.011 | 520 | 520 | 0 | 0.000 | 0 |
| `attribute-declared` | `oracle` | 7 | 1.000 ±0.000 | 0.179 ±0.010 | 560 | 560 | 0 | 0.000 | 0 |
| `attribute-declared` | `oracle` | 8 | 1.000 ±0.000 | 0.169 ±0.009 | 600 | 600 | 0 | 0.000 | 0 |
| `attribute-declared` | `oracle` | 9 | 1.000 ±0.000 | 0.153 ±0.008 | 640 | 640 | 0 | 0.000 | 0 |
| `attribute-declared` | `oracle` | 10 | 1.000 ±0.000 | 0.148 ±0.008 | 680 | 680 | 0 | 0.000 | 0 |
| `interval` | `oracle` | 1 | 1.000 ±0.000 | 0.450 ±0.037 | 160 | 160 | 0 | 0.000 | 0 |
| `interval` | `oracle` | 2 | 1.000 ±0.000 | 0.269 ±0.021 | 280 | 280 | 0 | 0.000 | 0 |
| `interval` | `oracle` | 3 | 1.000 ±0.000 | 0.246 ±0.018 | 400 | 400 | 0 | 0.000 | 0 |
| `interval` | `oracle` | 4 | 1.000 ±0.000 | 0.212 ±0.011 | 440 | 440 | 0 | 0.000 | 0 |
| `interval` | `oracle` | 5 | 1.000 ±0.000 | 0.194 ±0.010 | 480 | 480 | 0 | 0.000 | 0 |
| `interval` | `oracle` | 6 | 0.833 ±0.030 | 0.149 ±0.008 | 600 | 600 | 0 | 0.000 | 0 |
| `interval` | `oracle` | 7 | 0.833 ±0.030 | 0.139 ±0.007 | 680 | 680 | 0 | 0.000 | 0 |
| `interval` | `oracle` | 8 | 0.833 ±0.030 | 0.131 ±0.006 | 720 | 720 | 0 | 0.000 | 0 |
| `interval` | `oracle` | 9 | 0.833 ±0.030 | 0.117 ±0.004 | 760 | 760 | 0 | 0.000 | 0 |
| `interval` | `oracle` | 10 | 0.833 ±0.030 | 0.113 ±0.004 | 800 | 800 | 0 | 0.000 | 0 |
| `interval-declared` | `oracle` | 1 | 1.000 ±0.000 | 0.450 ±0.037 | 160 | 160 | 0 | 0.000 | 0 |
| `interval-declared` | `oracle` | 2 | 1.000 ±0.000 | 0.269 ±0.021 | 280 | 280 | 0 | 0.000 | 0 |
| `interval-declared` | `oracle` | 3 | 1.000 ±0.000 | 0.246 ±0.018 | 400 | 400 | 0 | 0.000 | 0 |
| `interval-declared` | `oracle` | 4 | 1.000 ±0.000 | 0.212 ±0.011 | 440 | 440 | 0 | 0.000 | 0 |
| `interval-declared` | `oracle` | 5 | 1.000 ±0.000 | 0.194 ±0.010 | 480 | 480 | 0 | 0.000 | 0 |
| `interval-declared` | `oracle` | 6 | 1.000 ±0.000 | 0.192 ±0.011 | 600 | 600 | 0 | 0.000 | 0 |
| `interval-declared` | `oracle` | 7 | 1.000 ±0.000 | 0.179 ±0.010 | 680 | 680 | 0 | 0.000 | 0 |
| `interval-declared` | `oracle` | 8 | 1.000 ±0.000 | 0.169 ±0.009 | 720 | 720 | 0 | 0.000 | 0 |
| `interval-declared` | `oracle` | 9 | 1.000 ±0.000 | 0.153 ±0.008 | 760 | 760 | 0 | 0.000 | 0 |
| `interval-declared` | `oracle` | 10 | 1.000 ±0.000 | 0.148 ±0.008 | 800 | 800 | 0 | 0.000 | 0 |
| `log` | `oracle` | 1 | 1.000 ±0.000 | 0.450 ±0.037 | 160 | 160 | 0 | 0.000 | 0 |
| `log` | `oracle` | 2 | 1.000 ±0.000 | 0.269 ±0.021 | 280 | 280 | 0 | 0.000 | 0 |
| `log` | `oracle` | 3 | 1.000 ±0.000 | 0.246 ±0.018 | 400 | 400 | 0 | 0.000 | 0 |
| `log` | `oracle` | 4 | 1.000 ±0.000 | 0.212 ±0.011 | 440 | 440 | 0 | 0.000 | 0 |
| `log` | `oracle` | 5 | 1.000 ±0.000 | 0.194 ±0.010 | 480 | 480 | 0 | 0.000 | 0 |
| `log` | `oracle` | 6 | 0.833 ±0.030 | 0.149 ±0.008 | 600 | 600 | 0 | 0.000 | 0 |
| `log` | `oracle` | 7 | 0.833 ±0.030 | 0.139 ±0.007 | 680 | 680 | 0 | 0.000 | 0 |
| `log` | `oracle` | 8 | 0.833 ±0.030 | 0.131 ±0.006 | 720 | 720 | 0 | 0.000 | 0 |
| `log` | `oracle` | 9 | 0.833 ±0.030 | 0.117 ±0.004 | 760 | 760 | 0 | 0.000 | 0 |
| `log` | `oracle` | 10 | 0.833 ±0.030 | 0.113 ±0.004 | 800 | 800 | 0 | 0.000 | 0 |
| `shipped` | `text` | 1 | 0.056 ±0.036 | 0.056 ±0.030 | 9 | 9 | 0 | 0.000 | 0 |
| `shipped` | `text` | 2 | 0.017 ±0.016 | 0.015 ±0.007 | 4 | 4 | 0 | 0.000 | 0 |
| `shipped` | `text` | 3 | 0.025 ±0.020 | 0.020 ±0.008 | 6 | 6 | 0 | 0.000 | 0 |
| `shipped` | `text` | 4 | 0.004 ±0.008 | 0.003 ±0.003 | 1 | 1 | 0 | 0.000 | 0 |
| `shipped` | `text` | 5 | 0.021 ±0.018 | 0.014 ±0.006 | 5 | 5 | 0 | 0.000 | 0 |
| `shipped` | `text` | 6 | 0.013 ±0.013 | 0.009 ±0.005 | 4 | 4 | 0 | 0.000 | 0 |
| `shipped` | `text` | 7 | 0.013 ±0.014 | 0.008 ±0.004 | 3 | 3 | 0 | 0.000 | 0 |
| `shipped` | `text` | 8 | 0.004 ±0.008 | 0.002 ±0.002 | 1 | 1 | 0 | 0.000 | 0 |
| `shipped` | `text` | 9 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `shipped` | `text` | 10 | 0.008 ±0.012 | 0.003 ±0.002 | 2 | 2 | 0 | 0.000 | 0 |
| `document` | `text` | 1 | 0.075 ±0.041 | 0.075 ±0.032 | 12 | 12 | 0 | 0.000 | 0 |
| `document` | `text` | 2 | 0.037 ±0.024 | 0.030 ±0.010 | 9 | 9 | 0 | 0.000 | 0 |
| `document` | `text` | 3 | 0.037 ±0.024 | 0.030 ±0.009 | 9 | 9 | 0 | 0.000 | 0 |
| `document` | `text` | 4 | 0.008 ±0.012 | 0.007 ±0.004 | 2 | 2 | 0 | 0.000 | 0 |
| `document` | `text` | 5 | 0.025 ±0.020 | 0.017 ±0.007 | 6 | 6 | 0 | 0.000 | 0 |
| `document` | `text` | 6 | 0.023 ±0.016 | 0.017 ±0.006 | 9 | 9 | 0 | 0.000 | 0 |
| `document` | `text` | 7 | 0.042 ±0.025 | 0.024 ±0.008 | 10 | 10 | 0 | 0.000 | 0 |
| `document` | `text` | 8 | 0.021 ±0.018 | 0.009 ±0.006 | 5 | 5 | 0 | 0.000 | 0 |
| `document` | `text` | 9 | 0.004 ±0.008 | 0.002 ±0.002 | 1 | 1 | 0 | 0.000 | 0 |
| `document` | `text` | 10 | 0.008 ±0.012 | 0.003 ±0.002 | 2 | 2 | 0 | 0.000 | 0 |
| `attribute-single` | `text` | 1 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-single` | `text` | 2 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-single` | `text` | 3 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-single` | `text` | 4 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-single` | `text` | 5 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-single` | `text` | 6 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-single` | `text` | 7 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-single` | `text` | 8 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-single` | `text` | 9 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-single` | `text` | 10 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-multi` | `text` | 1 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-multi` | `text` | 2 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-multi` | `text` | 3 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-multi` | `text` | 4 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-multi` | `text` | 5 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-multi` | `text` | 6 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-multi` | `text` | 7 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-multi` | `text` | 8 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-multi` | `text` | 9 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-multi` | `text` | 10 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-learned` | `text` | 1 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-learned` | `text` | 2 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-learned` | `text` | 3 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-learned` | `text` | 4 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-learned` | `text` | 5 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-learned` | `text` | 6 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-learned` | `text` | 7 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-learned` | `text` | 8 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-learned` | `text` | 9 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-learned` | `text` | 10 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-cued` | `text` | 1 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-cued` | `text` | 2 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-cued` | `text` | 3 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-cued` | `text` | 4 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-cued` | `text` | 5 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-cued` | `text` | 6 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-cued` | `text` | 7 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-cued` | `text` | 8 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-cued` | `text` | 9 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-cued` | `text` | 10 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-declared` | `text` | 1 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-declared` | `text` | 2 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-declared` | `text` | 3 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-declared` | `text` | 4 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-declared` | `text` | 5 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-declared` | `text` | 6 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-declared` | `text` | 7 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-declared` | `text` | 8 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-declared` | `text` | 9 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `attribute-declared` | `text` | 10 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `interval` | `text` | 1 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `interval` | `text` | 2 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `interval` | `text` | 3 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `interval` | `text` | 4 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `interval` | `text` | 5 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `interval` | `text` | 6 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `interval` | `text` | 7 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `interval` | `text` | 8 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `interval` | `text` | 9 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `interval` | `text` | 10 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `interval-declared` | `text` | 1 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `interval-declared` | `text` | 2 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `interval-declared` | `text` | 3 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `interval-declared` | `text` | 4 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `interval-declared` | `text` | 5 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `interval-declared` | `text` | 6 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `interval-declared` | `text` | 7 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `interval-declared` | `text` | 8 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `interval-declared` | `text` | 9 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `interval-declared` | `text` | 10 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `log` | `text` | 1 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `log` | `text` | 2 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `log` | `text` | 3 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `log` | `text` | 4 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `log` | `text` | 5 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `log` | `text` | 6 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `log` | `text` | 7 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `log` | `text` | 8 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `log` | `text` | 9 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |
| `log` | `text` | 10 | 0.000 ±0.000 | 0.000 ±0.000 | 0 | 0 | 0 | 0.000 | 0 |

### What a decade consumes

| frame | arm | year | units | content KB | stored KB |
|---|---|---|---|---|---|
| `shipped` | `oracle` | 1 | 168 | 6.5 | 856.0 |
| `shipped` | `oracle` | 2 | 272 | 10.4 | 1656.0 |
| `shipped` | `oracle` | 3 | 387 | 15.4 | 2480.0 |
| `shipped` | `oracle` | 4 | 425 | 16.9 | 3288.0 |
| `shipped` | `oracle` | 5 | 461 | 18.2 | 4060.0 |
| `shipped` | `oracle` | 6 | 552 | 22.2 | 4892.0 |
| `shipped` | `oracle` | 7 | 658 | 26.8 | 5700.0 |
| `shipped` | `oracle` | 8 | 698 | 28.4 | 6496.0 |
| `shipped` | `oracle` | 9 | 735 | 29.8 | 7312.0 |
| `shipped` | `oracle` | 10 | 783 | 31.6 | 8112.0 |
| `document` | `oracle` | 1 | 1 | 2.0 | 2.0 |
| `document` | `oracle` | 2 | 1 | 2.0 | 2.0 |
| `document` | `oracle` | 3 | 1 | 2.0 | 2.0 |
| `document` | `oracle` | 4 | 1 | 2.0 | 2.0 |
| `document` | `oracle` | 5 | 1 | 2.0 | 2.0 |
| `document` | `oracle` | 6 | 1 | 2.0 | 2.0 |
| `document` | `oracle` | 7 | 1 | 2.0 | 2.0 |
| `document` | `oracle` | 8 | 1 | 2.0 | 2.0 |
| `document` | `oracle` | 9 | 1 | 2.0 | 2.0 |
| `document` | `oracle` | 10 | 1 | 2.0 | 2.0 |
| `attribute-single` | `oracle` | 1 | 160 | 4.0 | 4.0 |
| `attribute-single` | `oracle` | 2 | 280 | 7.0 | 7.0 |
| `attribute-single` | `oracle` | 3 | 320 | 8.0 | 8.0 |
| `attribute-single` | `oracle` | 4 | 360 | 9.0 | 9.0 |
| `attribute-single` | `oracle` | 5 | 400 | 10.0 | 10.0 |
| `attribute-single` | `oracle` | 6 | 440 | 11.0 | 11.0 |
| `attribute-single` | `oracle` | 7 | 480 | 12.0 | 12.0 |
| `attribute-single` | `oracle` | 8 | 520 | 12.9 | 12.9 |
| `attribute-single` | `oracle` | 9 | 560 | 13.9 | 13.9 |
| `attribute-single` | `oracle` | 10 | 600 | 14.9 | 14.9 |
| `attribute-multi` | `oracle` | 1 | 160 | 4.0 | 4.0 |
| `attribute-multi` | `oracle` | 2 | 280 | 7.0 | 7.0 |
| `attribute-multi` | `oracle` | 3 | 400 | 10.0 | 10.0 |
| `attribute-multi` | `oracle` | 4 | 440 | 11.0 | 11.0 |
| `attribute-multi` | `oracle` | 5 | 480 | 12.0 | 12.0 |
| `attribute-multi` | `oracle` | 6 | 600 | 15.0 | 15.0 |
| `attribute-multi` | `oracle` | 7 | 680 | 16.9 | 16.9 |
| `attribute-multi` | `oracle` | 8 | 720 | 17.9 | 17.9 |
| `attribute-multi` | `oracle` | 9 | 760 | 18.9 | 18.9 |
| `attribute-multi` | `oracle` | 10 | 800 | 19.9 | 19.9 |
| `attribute-learned` | `oracle` | 1 | 160 | 4.0 | 4.0 |
| `attribute-learned` | `oracle` | 2 | 280 | 7.0 | 7.0 |
| `attribute-learned` | `oracle` | 3 | 400 | 10.0 | 10.0 |
| `attribute-learned` | `oracle` | 4 | 440 | 11.0 | 11.0 |
| `attribute-learned` | `oracle` | 5 | 480 | 12.0 | 12.0 |
| `attribute-learned` | `oracle` | 6 | 600 | 15.0 | 15.0 |
| `attribute-learned` | `oracle` | 7 | 680 | 16.9 | 16.9 |
| `attribute-learned` | `oracle` | 8 | 720 | 17.9 | 17.9 |
| `attribute-learned` | `oracle` | 9 | 760 | 18.9 | 18.9 |
| `attribute-learned` | `oracle` | 10 | 800 | 19.9 | 19.9 |
| `attribute-cued` | `oracle` | 1 | 160 | 4.0 | 4.0 |
| `attribute-cued` | `oracle` | 2 | 280 | 7.0 | 7.0 |
| `attribute-cued` | `oracle` | 3 | 320 | 8.0 | 8.0 |
| `attribute-cued` | `oracle` | 4 | 360 | 9.0 | 9.0 |
| `attribute-cued` | `oracle` | 5 | 400 | 10.0 | 10.0 |
| `attribute-cued` | `oracle` | 6 | 480 | 12.0 | 12.0 |
| `attribute-cued` | `oracle` | 7 | 520 | 13.0 | 13.0 |
| `attribute-cued` | `oracle` | 8 | 560 | 13.9 | 13.9 |
| `attribute-cued` | `oracle` | 9 | 600 | 14.9 | 14.9 |
| `attribute-cued` | `oracle` | 10 | 640 | 15.9 | 15.9 |
| `attribute-declared` | `oracle` | 1 | 160 | 4.0 | 4.0 |
| `attribute-declared` | `oracle` | 2 | 280 | 7.0 | 7.0 |
| `attribute-declared` | `oracle` | 3 | 320 | 8.0 | 8.0 |
| `attribute-declared` | `oracle` | 4 | 360 | 9.0 | 9.0 |
| `attribute-declared` | `oracle` | 5 | 400 | 10.0 | 10.0 |
| `attribute-declared` | `oracle` | 6 | 520 | 13.0 | 13.0 |
| `attribute-declared` | `oracle` | 7 | 560 | 14.0 | 14.0 |
| `attribute-declared` | `oracle` | 8 | 600 | 14.9 | 14.9 |
| `attribute-declared` | `oracle` | 9 | 640 | 15.9 | 15.9 |
| `attribute-declared` | `oracle` | 10 | 680 | 16.9 | 16.9 |
| `interval` | `oracle` | 1 | 160 | 4.0 | 4.0 |
| `interval` | `oracle` | 2 | 280 | 7.0 | 7.0 |
| `interval` | `oracle` | 3 | 400 | 10.0 | 10.0 |
| `interval` | `oracle` | 4 | 440 | 11.0 | 11.0 |
| `interval` | `oracle` | 5 | 480 | 12.0 | 12.0 |
| `interval` | `oracle` | 6 | 600 | 15.0 | 15.0 |
| `interval` | `oracle` | 7 | 720 | 17.9 | 17.9 |
| `interval` | `oracle` | 8 | 760 | 18.9 | 18.9 |
| `interval` | `oracle` | 9 | 800 | 19.9 | 19.9 |
| `interval` | `oracle` | 10 | 840 | 20.9 | 20.9 |
| `interval-declared` | `oracle` | 1 | 160 | 4.0 | 4.0 |
| `interval-declared` | `oracle` | 2 | 280 | 7.0 | 7.0 |
| `interval-declared` | `oracle` | 3 | 400 | 10.0 | 10.0 |
| `interval-declared` | `oracle` | 4 | 440 | 11.0 | 11.0 |
| `interval-declared` | `oracle` | 5 | 480 | 12.0 | 12.0 |
| `interval-declared` | `oracle` | 6 | 600 | 15.0 | 15.0 |
| `interval-declared` | `oracle` | 7 | 720 | 17.9 | 17.9 |
| `interval-declared` | `oracle` | 8 | 760 | 18.9 | 18.9 |
| `interval-declared` | `oracle` | 9 | 800 | 19.9 | 19.9 |
| `interval-declared` | `oracle` | 10 | 840 | 20.9 | 20.9 |
| `log` | `oracle` | 1 | 160 | 4.0 | 4.0 |
| `log` | `oracle` | 2 | 280 | 7.0 | 7.0 |
| `log` | `oracle` | 3 | 400 | 10.0 | 10.0 |
| `log` | `oracle` | 4 | 440 | 11.0 | 11.0 |
| `log` | `oracle` | 5 | 480 | 12.0 | 12.0 |
| `log` | `oracle` | 6 | 600 | 15.0 | 15.0 |
| `log` | `oracle` | 7 | 720 | 17.9 | 17.9 |
| `log` | `oracle` | 8 | 760 | 18.9 | 18.9 |
| `log` | `oracle` | 9 | 800 | 19.9 | 19.9 |
| `log` | `oracle` | 10 | 840 | 20.9 | 20.9 |
| `shipped` | `text` | 1 | 30 | 1.2 | 812.0 |
| `shipped` | `text` | 2 | 13 | 0.6 | 1592.0 |
| `shipped` | `text` | 3 | 29 | 1.2 | 2388.0 |
| `shipped` | `text` | 4 | 26 | 1.1 | 3164.0 |
| `shipped` | `text` | 5 | 28 | 1.2 | 3940.0 |
| `shipped` | `text` | 6 | 13 | 0.5 | 4720.0 |
| `shipped` | `text` | 7 | 12 | 0.5 | 5504.0 |
| `shipped` | `text` | 8 | 11 | 0.5 | 6280.0 |
| `shipped` | `text` | 9 | 9 | 0.4 | 7064.0 |
| `shipped` | `text` | 10 | 22 | 0.9 | 7844.0 |
| `document` | `text` | 1 | 1 | 2.0 | 2.0 |
| `document` | `text` | 2 | 1 | 2.0 | 2.0 |
| `document` | `text` | 3 | 1 | 2.0 | 2.0 |
| `document` | `text` | 4 | 1 | 2.0 | 2.0 |
| `document` | `text` | 5 | 1 | 2.0 | 2.0 |
| `document` | `text` | 6 | 1 | 2.0 | 2.0 |
| `document` | `text` | 7 | 1 | 2.0 | 2.0 |
| `document` | `text` | 8 | 1 | 2.0 | 2.0 |
| `document` | `text` | 9 | 1 | 2.0 | 2.0 |
| `document` | `text` | 10 | 1 | 2.0 | 2.0 |
| `attribute-single` | `text` | 1 | 0 | 0.0 | 0.0 |
| `attribute-single` | `text` | 2 | 0 | 0.0 | 0.0 |
| `attribute-single` | `text` | 3 | 0 | 0.0 | 0.0 |
| `attribute-single` | `text` | 4 | 0 | 0.0 | 0.0 |
| `attribute-single` | `text` | 5 | 0 | 0.0 | 0.0 |
| `attribute-single` | `text` | 6 | 0 | 0.0 | 0.0 |
| `attribute-single` | `text` | 7 | 0 | 0.0 | 0.0 |
| `attribute-single` | `text` | 8 | 0 | 0.0 | 0.0 |
| `attribute-single` | `text` | 9 | 0 | 0.0 | 0.0 |
| `attribute-single` | `text` | 10 | 0 | 0.0 | 0.0 |
| `attribute-multi` | `text` | 1 | 0 | 0.0 | 0.0 |
| `attribute-multi` | `text` | 2 | 0 | 0.0 | 0.0 |
| `attribute-multi` | `text` | 3 | 0 | 0.0 | 0.0 |
| `attribute-multi` | `text` | 4 | 0 | 0.0 | 0.0 |
| `attribute-multi` | `text` | 5 | 0 | 0.0 | 0.0 |
| `attribute-multi` | `text` | 6 | 0 | 0.0 | 0.0 |
| `attribute-multi` | `text` | 7 | 0 | 0.0 | 0.0 |
| `attribute-multi` | `text` | 8 | 0 | 0.0 | 0.0 |
| `attribute-multi` | `text` | 9 | 0 | 0.0 | 0.0 |
| `attribute-multi` | `text` | 10 | 0 | 0.0 | 0.0 |
| `attribute-learned` | `text` | 1 | 0 | 0.0 | 0.0 |
| `attribute-learned` | `text` | 2 | 0 | 0.0 | 0.0 |
| `attribute-learned` | `text` | 3 | 0 | 0.0 | 0.0 |
| `attribute-learned` | `text` | 4 | 0 | 0.0 | 0.0 |
| `attribute-learned` | `text` | 5 | 0 | 0.0 | 0.0 |
| `attribute-learned` | `text` | 6 | 0 | 0.0 | 0.0 |
| `attribute-learned` | `text` | 7 | 0 | 0.0 | 0.0 |
| `attribute-learned` | `text` | 8 | 0 | 0.0 | 0.0 |
| `attribute-learned` | `text` | 9 | 0 | 0.0 | 0.0 |
| `attribute-learned` | `text` | 10 | 0 | 0.0 | 0.0 |
| `attribute-cued` | `text` | 1 | 0 | 0.0 | 0.0 |
| `attribute-cued` | `text` | 2 | 0 | 0.0 | 0.0 |
| `attribute-cued` | `text` | 3 | 0 | 0.0 | 0.0 |
| `attribute-cued` | `text` | 4 | 0 | 0.0 | 0.0 |
| `attribute-cued` | `text` | 5 | 0 | 0.0 | 0.0 |
| `attribute-cued` | `text` | 6 | 0 | 0.0 | 0.0 |
| `attribute-cued` | `text` | 7 | 0 | 0.0 | 0.0 |
| `attribute-cued` | `text` | 8 | 0 | 0.0 | 0.0 |
| `attribute-cued` | `text` | 9 | 0 | 0.0 | 0.0 |
| `attribute-cued` | `text` | 10 | 0 | 0.0 | 0.0 |
| `attribute-declared` | `text` | 1 | 0 | 0.0 | 0.0 |
| `attribute-declared` | `text` | 2 | 0 | 0.0 | 0.0 |
| `attribute-declared` | `text` | 3 | 0 | 0.0 | 0.0 |
| `attribute-declared` | `text` | 4 | 0 | 0.0 | 0.0 |
| `attribute-declared` | `text` | 5 | 0 | 0.0 | 0.0 |
| `attribute-declared` | `text` | 6 | 0 | 0.0 | 0.0 |
| `attribute-declared` | `text` | 7 | 0 | 0.0 | 0.0 |
| `attribute-declared` | `text` | 8 | 0 | 0.0 | 0.0 |
| `attribute-declared` | `text` | 9 | 0 | 0.0 | 0.0 |
| `attribute-declared` | `text` | 10 | 0 | 0.0 | 0.0 |
| `interval` | `text` | 1 | 0 | 0.0 | 0.0 |
| `interval` | `text` | 2 | 0 | 0.0 | 0.0 |
| `interval` | `text` | 3 | 0 | 0.0 | 0.0 |
| `interval` | `text` | 4 | 0 | 0.0 | 0.0 |
| `interval` | `text` | 5 | 0 | 0.0 | 0.0 |
| `interval` | `text` | 6 | 0 | 0.0 | 0.0 |
| `interval` | `text` | 7 | 0 | 0.0 | 0.0 |
| `interval` | `text` | 8 | 0 | 0.0 | 0.0 |
| `interval` | `text` | 9 | 0 | 0.0 | 0.0 |
| `interval` | `text` | 10 | 0 | 0.0 | 0.0 |
| `interval-declared` | `text` | 1 | 0 | 0.0 | 0.0 |
| `interval-declared` | `text` | 2 | 0 | 0.0 | 0.0 |
| `interval-declared` | `text` | 3 | 0 | 0.0 | 0.0 |
| `interval-declared` | `text` | 4 | 0 | 0.0 | 0.0 |
| `interval-declared` | `text` | 5 | 0 | 0.0 | 0.0 |
| `interval-declared` | `text` | 6 | 0 | 0.0 | 0.0 |
| `interval-declared` | `text` | 7 | 0 | 0.0 | 0.0 |
| `interval-declared` | `text` | 8 | 0 | 0.0 | 0.0 |
| `interval-declared` | `text` | 9 | 0 | 0.0 | 0.0 |
| `interval-declared` | `text` | 10 | 0 | 0.0 | 0.0 |
| `log` | `text` | 1 | 0 | 0.0 | 0.0 |
| `log` | `text` | 2 | 0 | 0.0 | 0.0 |
| `log` | `text` | 3 | 0 | 0.0 | 0.0 |
| `log` | `text` | 4 | 0 | 0.0 | 0.0 |
| `log` | `text` | 5 | 0 | 0.0 | 0.0 |
| `log` | `text` | 6 | 0 | 0.0 | 0.0 |
| `log` | `text` | 7 | 0 | 0.0 | 0.0 |
| `log` | `text` | 8 | 0 | 0.0 | 0.0 |
| `log` | `text` | 9 | 0 | 0.0 | 0.0 |
| `log` | `text` | 10 | 0 | 0.0 | 0.0 |

| frame | arm | units at the last year | units added a year | decades to the active-index capacity |
|---|---|---|---|---|
| `shipped` | `oracle` | 783 | 68.3 | 13 |
| `document` | `oracle` | 1 | 0.0 | n/a |
| `attribute-single` | `oracle` | 600 | 48.9 | 19 |
| `attribute-multi` | `oracle` | 800 | 71.1 | 13 |
| `attribute-learned` | `oracle` | 800 | 71.1 | 13 |
| `attribute-cued` | `oracle` | 640 | 53.3 | 18 |
| `attribute-declared` | `oracle` | 680 | 57.8 | 16 |
| `interval` | `oracle` | 840 | 75.6 | 12 |
| `interval-declared` | `oracle` | 840 | 75.6 | 12 |
| `log` | `oracle` | 840 | 75.6 | 12 |
| `shipped` | `text` | 22 | -0.9 | n/a |
| `document` | `text` | 1 | 0.0 | n/a |
| `attribute-single` | `text` | 0 | 0.0 | n/a |
| `attribute-multi` | `text` | 0 | 0.0 | n/a |
| `attribute-learned` | `text` | 0 | 0.0 | n/a |
| `attribute-cued` | `text` | 0 | 0.0 | n/a |
| `attribute-declared` | `text` | 0 | 0.0 | n/a |
| `interval` | `text` | 0 | 0.0 | n/a |
| `interval-declared` | `text` | 0 | 0.0 | n/a |
| `log` | `text` | 0 | 0.0 | n/a |

### Whether a correction reaches what a read did not

| frame | arm | corrections whose value a read had returned | applied | corrections whose value no read had returned | applied |
|---|---|---|---|---|---|
| `shipped` | `oracle` | 141 | 0.064 ±0.040 | 19 | 1.000 ±0.000 |
| `document` | `oracle` | 14 | 1.000 ±0.000 | 146 | 1.000 ±0.000 |
| `attribute-single` | `oracle` | 160 | 1.000 ±0.000 | 0 | n/a |
| `attribute-multi` | `oracle` | 160 | 0.000 ±0.000 | 0 | n/a |
| `attribute-learned` | `oracle` | 160 | 0.000 ±0.000 | 0 | n/a |
| `attribute-cued` | `oracle` | 160 | 1.000 ±0.000 | 0 | n/a |
| `attribute-declared` | `oracle` | 160 | 1.000 ±0.000 | 0 | n/a |
| `interval` | `oracle` | 160 | 0.000 ±0.000 | 0 | n/a |
| `interval-declared` | `oracle` | 160 | 0.000 ±0.000 | 0 | n/a |
| `log` | `oracle` | 160 | 0.000 ±0.000 | 0 | n/a |
| `shipped` | `text` | 9 | 1.000 ±0.000 | 151 | 1.000 ±0.000 |
| `document` | `text` | 14 | 1.000 ±0.000 | 146 | 1.000 ±0.000 |
| `attribute-single` | `text` | 0 | n/a | 160 | 1.000 ±0.000 |
| `attribute-multi` | `text` | 0 | n/a | 160 | 1.000 ±0.000 |
| `attribute-learned` | `text` | 0 | n/a | 160 | 1.000 ±0.000 |
| `attribute-cued` | `text` | 0 | n/a | 160 | 1.000 ±0.000 |
| `attribute-declared` | `text` | 0 | n/a | 160 | 1.000 ±0.000 |
| `interval` | `text` | 0 | n/a | 160 | 1.000 ±0.000 |
| `interval-declared` | `text` | 0 | n/a | 160 | 1.000 ±0.000 |
| `log` | `text` | 0 | n/a | 160 | 1.000 ±0.000 |

### What a wrong declaration costs, by year

| direction | rate | errors | year | recall | carried | replaced | reverted | parallel | refined | fresh | stale returned |
|---|---|---|---|---|---|---|---|---|---|---|---|
| `join-as-replace` | 0.005 | 0 | 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a |
| `join-as-replace` | 0.005 | 0 | 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a |
| `join-as-replace` | 0.005 | 0 | 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.005 | 0 | 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.005 | 0 | 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.005 | 0 | 6 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.005 | 0 | 7 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.005 | 0 | 8 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.005 | 0 | 9 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.005 | 0 | 10 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.010 | 0 | 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a |
| `join-as-replace` | 0.010 | 0 | 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a |
| `join-as-replace` | 0.010 | 0 | 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.010 | 0 | 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.010 | 0 | 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.010 | 0 | 6 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.010 | 0 | 7 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.010 | 0 | 8 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.010 | 0 | 9 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.010 | 0 | 10 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.020 | 0 | 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a |
| `join-as-replace` | 0.020 | 0 | 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a |
| `join-as-replace` | 0.020 | 0 | 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.020 | 0 | 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.020 | 0 | 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.020 | 0 | 6 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.020 | 0 | 7 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.020 | 0 | 8 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.020 | 0 | 9 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.020 | 0 | 10 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.050 | 1 | 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a |
| `join-as-replace` | 0.050 | 1 | 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a |
| `join-as-replace` | 0.050 | 1 | 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.050 | 1 | 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.050 | 1 | 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.050 | 1 | 6 | 0.998 ±0.004 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.988 ±0.024 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.050 | 1 | 7 | 0.998 ±0.004 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.988 ±0.024 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.050 | 1 | 8 | 0.998 ±0.004 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.988 ±0.024 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.050 | 1 | 9 | 0.998 ±0.004 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.988 ±0.024 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.050 | 1 | 10 | 0.998 ±0.004 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.988 ±0.024 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.100 | 5 | 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a |
| `join-as-replace` | 0.100 | 5 | 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a |
| `join-as-replace` | 0.100 | 5 | 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.100 | 5 | 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.100 | 5 | 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.100 | 5 | 6 | 0.990 ±0.009 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.950 ±0.047 | 0.988 ±0.024 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.100 | 5 | 7 | 0.990 ±0.009 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.950 ±0.047 | 0.988 ±0.024 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.100 | 5 | 8 | 0.990 ±0.009 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.950 ±0.047 | 0.988 ±0.024 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.100 | 5 | 9 | 0.990 ±0.009 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.950 ±0.047 | 0.988 ±0.024 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.100 | 5 | 10 | 0.990 ±0.009 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.950 ±0.047 | 0.988 ±0.024 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.200 | 14 | 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a |
| `join-as-replace` | 0.200 | 14 | 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a |
| `join-as-replace` | 0.200 | 14 | 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.200 | 14 | 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.200 | 14 | 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.200 | 14 | 6 | 0.971 ±0.015 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.900 ±0.063 | 0.925 ±0.056 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.200 | 14 | 7 | 0.971 ±0.015 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.900 ±0.063 | 0.925 ±0.056 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.200 | 14 | 8 | 0.971 ±0.015 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.900 ±0.063 | 0.925 ±0.056 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.200 | 14 | 9 | 0.971 ±0.015 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.900 ±0.063 | 0.925 ±0.056 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.200 | 14 | 10 | 0.971 ±0.015 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.900 ±0.063 | 0.925 ±0.056 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.500 | 36 | 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a |
| `join-as-replace` | 0.500 | 36 | 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a |
| `join-as-replace` | 0.500 | 36 | 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.500 | 36 | 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.500 | 36 | 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.500 | 36 | 6 | 0.925 ±0.023 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.713 ±0.078 | 0.838 ±0.073 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.500 | 36 | 7 | 0.925 ±0.023 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.713 ±0.078 | 0.838 ±0.073 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.500 | 36 | 8 | 0.925 ±0.023 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.713 ±0.078 | 0.838 ±0.073 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.500 | 36 | 9 | 0.925 ±0.023 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.713 ±0.078 | 0.838 ±0.073 | 1.000 ±0.000 | 0.000 ±0.000 |
| `join-as-replace` | 0.500 | 36 | 10 | 0.925 ±0.023 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.713 ±0.078 | 0.838 ±0.073 | 1.000 ±0.000 | 0.000 ±0.000 |
| `replace-as-join` | 0.005 | 4 | 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a |
| `replace-as-join` | 0.005 | 4 | 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a |
| `replace-as-join` | 0.005 | 4 | 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `replace-as-join` | 0.005 | 4 | 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `replace-as-join` | 0.005 | 4 | 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `replace-as-join` | 0.005 | 4 | 6 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `replace-as-join` | 0.005 | 4 | 7 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `replace-as-join` | 0.005 | 4 | 8 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `replace-as-join` | 0.005 | 4 | 9 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `replace-as-join` | 0.005 | 4 | 10 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `replace-as-join` | 0.010 | 8 | 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a |
| `replace-as-join` | 0.010 | 8 | 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a |
| `replace-as-join` | 0.010 | 8 | 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.013 ±0.024 |
| `replace-as-join` | 0.010 | 8 | 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.013 ±0.024 |
| `replace-as-join` | 0.010 | 8 | 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.013 ±0.024 |
| `replace-as-join` | 0.010 | 8 | 6 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.013 ±0.024 |
| `replace-as-join` | 0.010 | 8 | 7 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `replace-as-join` | 0.010 | 8 | 8 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `replace-as-join` | 0.010 | 8 | 9 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `replace-as-join` | 0.010 | 8 | 10 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `replace-as-join` | 0.020 | 13 | 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a |
| `replace-as-join` | 0.020 | 13 | 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a |
| `replace-as-join` | 0.020 | 13 | 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.025 ±0.034 |
| `replace-as-join` | 0.020 | 13 | 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.025 ±0.034 |
| `replace-as-join` | 0.020 | 13 | 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.025 ±0.034 |
| `replace-as-join` | 0.020 | 13 | 6 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.025 ±0.034 |
| `replace-as-join` | 0.020 | 13 | 7 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `replace-as-join` | 0.020 | 13 | 8 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `replace-as-join` | 0.020 | 13 | 9 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `replace-as-join` | 0.020 | 13 | 10 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 |
| `replace-as-join` | 0.050 | 37 | 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a |
| `replace-as-join` | 0.050 | 37 | 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a |
| `replace-as-join` | 0.050 | 37 | 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.050 ±0.048 |
| `replace-as-join` | 0.050 | 37 | 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.050 ±0.048 |
| `replace-as-join` | 0.050 | 37 | 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.050 ±0.048 |
| `replace-as-join` | 0.050 | 37 | 6 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.050 ±0.048 |
| `replace-as-join` | 0.050 | 37 | 7 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.025 ±0.034 |
| `replace-as-join` | 0.050 | 37 | 8 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.025 ±0.034 |
| `replace-as-join` | 0.050 | 37 | 9 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.025 ±0.034 |
| `replace-as-join` | 0.050 | 37 | 10 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.025 ±0.034 |
| `replace-as-join` | 0.100 | 74 | 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a |
| `replace-as-join` | 0.100 | 74 | 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a |
| `replace-as-join` | 0.100 | 74 | 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.113 ±0.070 |
| `replace-as-join` | 0.100 | 74 | 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.113 ±0.070 |
| `replace-as-join` | 0.100 | 74 | 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.113 ±0.070 |
| `replace-as-join` | 0.100 | 74 | 6 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.113 ±0.070 |
| `replace-as-join` | 0.100 | 74 | 7 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.050 ±0.048 |
| `replace-as-join` | 0.100 | 74 | 8 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.050 ±0.048 |
| `replace-as-join` | 0.100 | 74 | 9 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.050 ±0.048 |
| `replace-as-join` | 0.100 | 74 | 10 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.050 ±0.048 |
| `replace-as-join` | 0.200 | 151 | 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a |
| `replace-as-join` | 0.200 | 151 | 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a |
| `replace-as-join` | 0.200 | 151 | 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.225 ±0.092 |
| `replace-as-join` | 0.200 | 151 | 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.225 ±0.092 |
| `replace-as-join` | 0.200 | 151 | 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.225 ±0.092 |
| `replace-as-join` | 0.200 | 151 | 6 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.225 ±0.092 |
| `replace-as-join` | 0.200 | 151 | 7 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.125 ±0.073 |
| `replace-as-join` | 0.200 | 151 | 8 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.125 ±0.073 |
| `replace-as-join` | 0.200 | 151 | 9 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.125 ±0.073 |
| `replace-as-join` | 0.200 | 151 | 10 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.125 ±0.073 |
| `replace-as-join` | 0.500 | 379 | 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a |
| `replace-as-join` | 0.500 | 379 | 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a |
| `replace-as-join` | 0.500 | 379 | 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.588 ±0.109 |
| `replace-as-join` | 0.500 | 379 | 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.588 ±0.109 |
| `replace-as-join` | 0.500 | 379 | 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.588 ±0.109 |
| `replace-as-join` | 0.500 | 379 | 6 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.588 ±0.109 |
| `replace-as-join` | 0.500 | 379 | 7 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.487 ±0.110 |
| `replace-as-join` | 0.500 | 379 | 8 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.487 ±0.110 |
| `replace-as-join` | 0.500 | 379 | 9 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.487 ±0.110 |
| `replace-as-join` | 0.500 | 379 | 10 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.487 ±0.110 |

### Whether a wrong declaration heals

| direction | rate | eligible writes | errors | addresses hit | healed by the last year | years carrying the damage |
|---|---|---|---|---|---|---|
| `join-as-replace` | 0.005 | 80 | 0 | 0 | n/a | n/a |
| `join-as-replace` | 0.010 | 80 | 0 | 0 | n/a | n/a |
| `join-as-replace` | 0.020 | 80 | 0 | 0 | n/a | n/a |
| `join-as-replace` | 0.050 | 80 | 1 | 1 | 0.000 ±0.000 | 5.00 ±0.00 |
| `join-as-replace` | 0.100 | 80 | 5 | 5 | 0.000 ±0.000 | 5.00 ±0.00 |
| `join-as-replace` | 0.200 | 80 | 14 | 14 | 0.000 ±0.000 | 5.00 ±0.00 |
| `join-as-replace` | 0.500 | 80 | 36 | 36 | 0.000 ±0.000 | 5.00 ±0.00 |
| `replace-as-join` | 0.005 | 760 | 4 | 4 | 1.000 ±0.000 | 0.00 ±0.00 |
| `replace-as-join` | 0.010 | 760 | 8 | 8 | 1.000 ±0.000 | 0.50 ±0.98 |
| `replace-as-join` | 0.020 | 760 | 13 | 12 | 1.000 ±0.000 | 0.33 ±0.65 |
| `replace-as-join` | 0.050 | 760 | 37 | 35 | 0.943 ±0.078 | 0.57 ±0.57 |
| `replace-as-join` | 0.100 | 760 | 74 | 70 | 0.943 ±0.055 | 0.63 ±0.41 |
| `replace-as-join` | 0.200 | 760 | 151 | 144 | 0.931 ±0.042 | 0.67 ±0.28 |
| `replace-as-join` | 0.500 | 760 | 379 | 323 | 0.879 ±0.036 | 0.48 ±0.18 |

### What a model declared

| direction | eligible writes | errors | rate | writes with consequence | errors there | rate there |
|---|---|---|---|---|---|---|
| `join-as-replace` | 80 | 23 | 0.287 ±0.100 | 80 | 23 | 0.287 ±0.100 |
| `replace-as-join` | 760 | 630 | 0.829 ±0.027 | 160 | 32 | 0.200 ±0.062 |

| wording | asked | accuracy |
|---|---|---|
| `assert` | 600 | 0.003 ±0.005 |
| `also` | 40 | 1.000 ±0.000 |
| `narrow` | 40 | 0.425 ±0.155 |
| `replace` | 160 | 0.800 ±0.062 |

| class | asked | accuracy |
|---|---|---|
| `carried` | 40 | 0.000 ±0.000 |
| `replaced` | 120 | 0.550 ±0.089 |
| `reverted` | 120 | 0.517 ±0.090 |
| `parallel` | 80 | 0.512 ±0.110 |
| `refined` | 80 | 0.212 ±0.090 |
| `fresh` | 400 | 0.003 ±0.005 |

### What each frame answered on the model's declarations

| frame | year | recall | carried | replaced | reverted | parallel | refined | fresh | stale returned | units held |
|---|---|---|---|---|---|---|---|---|---|---|
| `attribute-declared` | 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a | 160 |
| `attribute-declared` | 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a |
| `attribute-declared` | 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.188 ±0.086 | n/a |
| `attribute-declared` | 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.188 ±0.086 | n/a |
| `attribute-declared` | 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.188 ±0.086 | 415 |
| `attribute-declared` | 6 | 0.952 ±0.019 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.713 ±0.078 | 1.000 ±0.000 | 0.188 ±0.086 | n/a |
| `attribute-declared` | 7 | 0.952 ±0.019 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.713 ±0.078 | 1.000 ±0.000 | 0.212 ±0.090 | n/a |
| `attribute-declared` | 8 | 0.952 ±0.019 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.713 ±0.078 | 1.000 ±0.000 | 0.212 ±0.090 | n/a |
| `attribute-declared` | 9 | 0.952 ±0.019 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.713 ±0.078 | 1.000 ±0.000 | 0.212 ±0.090 | n/a |
| `attribute-declared` | 10 | 0.952 ±0.019 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.713 ±0.078 | 1.000 ±0.000 | 0.212 ±0.090 | 679 |
| `interval-declared` | 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a | 160 |
| `interval-declared` | 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a |
| `interval-declared` | 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.188 ±0.086 | n/a |
| `interval-declared` | 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.188 ±0.086 | n/a |
| `interval-declared` | 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.188 ±0.086 | 480 |
| `interval-declared` | 6 | 0.952 ±0.019 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.713 ±0.078 | 1.000 ±0.000 | 0.188 ±0.086 | n/a |
| `interval-declared` | 7 | 0.952 ±0.019 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.713 ±0.078 | 1.000 ±0.000 | 0.212 ±0.090 | n/a |
| `interval-declared` | 8 | 0.952 ±0.019 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.713 ±0.078 | 1.000 ±0.000 | 0.212 ±0.090 | n/a |
| `interval-declared` | 9 | 0.952 ±0.019 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.713 ±0.078 | 1.000 ±0.000 | 0.212 ±0.090 | n/a |
| `interval-declared` | 10 | 0.952 ±0.019 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.713 ±0.078 | 1.000 ±0.000 | 0.212 ±0.090 | 840 |

| frame | direction | errors | addresses hit | healed by the last year | years carrying the damage |
|---|---|---|---|---|---|
| `attribute-declared` | `join-as-replace` | 23 | 23 | 0.000 ±0.000 | 5.00 ±0.00 |
| `attribute-declared` | `replace-as-join` | 630 | 598 | 0.933 ±0.020 | 0.00 ±0.00 |
| `interval-declared` | `join-as-replace` | 23 | 23 | 0.000 ±0.000 | 5.00 ±0.00 |
| `interval-declared` | `replace-as-join` | 630 | 598 | 0.933 ±0.020 | 0.00 ±0.00 |

### What erring toward join trades

| share joined | destroying errors recovered | left destroying | year | recall | carried | replaced | reverted | parallel | refined | fresh | stale returned |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 0.00 | 0 | 23 | 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a |
| 0.00 | 0 | 23 | 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a |
| 0.00 | 0 | 23 | 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.188 ±0.086 |
| 0.00 | 0 | 23 | 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.188 ±0.086 |
| 0.00 | 0 | 23 | 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.188 ±0.086 |
| 0.00 | 0 | 23 | 6 | 0.952 ±0.019 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.713 ±0.078 | 1.000 ±0.000 | 0.188 ±0.086 |
| 0.00 | 0 | 23 | 7 | 0.952 ±0.019 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.713 ±0.078 | 1.000 ±0.000 | 0.212 ±0.090 |
| 0.00 | 0 | 23 | 8 | 0.952 ±0.019 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.713 ±0.078 | 1.000 ±0.000 | 0.212 ±0.090 |
| 0.00 | 0 | 23 | 9 | 0.952 ±0.019 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.713 ±0.078 | 1.000 ±0.000 | 0.212 ±0.090 |
| 0.00 | 0 | 23 | 10 | 0.952 ±0.019 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.713 ±0.078 | 1.000 ±0.000 | 0.212 ±0.090 |
| 0.25 | 3 | 20 | 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a |
| 0.25 | 3 | 20 | 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a |
| 0.25 | 3 | 20 | 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.412 ±0.109 |
| 0.25 | 3 | 20 | 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.412 ±0.109 |
| 0.25 | 3 | 20 | 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.412 ±0.109 |
| 0.25 | 3 | 20 | 6 | 0.958 ±0.018 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.750 ±0.078 | 1.000 ±0.000 | 0.412 ±0.109 |
| 0.25 | 3 | 20 | 7 | 0.958 ±0.018 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.750 ±0.078 | 1.000 ±0.000 | 0.425 ±0.109 |
| 0.25 | 3 | 20 | 8 | 0.958 ±0.018 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.750 ±0.078 | 1.000 ±0.000 | 0.425 ±0.109 |
| 0.25 | 3 | 20 | 9 | 0.958 ±0.018 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.750 ±0.078 | 1.000 ±0.000 | 0.425 ±0.109 |
| 0.25 | 3 | 20 | 10 | 0.958 ±0.018 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.750 ±0.078 | 1.000 ±0.000 | 0.425 ±0.109 |
| 0.50 | 8 | 15 | 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a |
| 0.50 | 8 | 15 | 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a |
| 0.50 | 8 | 15 | 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.650 ±0.105 |
| 0.50 | 8 | 15 | 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.650 ±0.105 |
| 0.50 | 8 | 15 | 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.650 ±0.105 |
| 0.50 | 8 | 15 | 6 | 0.969 ±0.015 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.812 ±0.076 | 1.000 ±0.000 | 0.650 ±0.105 |
| 0.50 | 8 | 15 | 7 | 0.969 ±0.015 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.812 ±0.076 | 1.000 ±0.000 | 0.625 ±0.107 |
| 0.50 | 8 | 15 | 8 | 0.969 ±0.015 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.812 ±0.076 | 1.000 ±0.000 | 0.625 ±0.107 |
| 0.50 | 8 | 15 | 9 | 0.969 ±0.015 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.812 ±0.076 | 1.000 ±0.000 | 0.625 ±0.107 |
| 0.50 | 8 | 15 | 10 | 0.969 ±0.015 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.812 ±0.076 | 1.000 ±0.000 | 0.625 ±0.107 |
| 0.75 | 13 | 10 | 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a |
| 0.75 | 13 | 10 | 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a |
| 0.75 | 13 | 10 | 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.825 ±0.084 |
| 0.75 | 13 | 10 | 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.825 ±0.084 |
| 0.75 | 13 | 10 | 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.825 ±0.084 |
| 0.75 | 13 | 10 | 6 | 0.979 ±0.013 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.875 ±0.068 | 1.000 ±0.000 | 0.825 ±0.084 |
| 0.75 | 13 | 10 | 7 | 0.979 ±0.013 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.875 ±0.068 | 1.000 ±0.000 | 0.800 ±0.088 |
| 0.75 | 13 | 10 | 8 | 0.979 ±0.013 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.875 ±0.068 | 1.000 ±0.000 | 0.800 ±0.088 |
| 0.75 | 13 | 10 | 9 | 0.979 ±0.013 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.875 ±0.068 | 1.000 ±0.000 | 0.800 ±0.088 |
| 0.75 | 13 | 10 | 10 | 0.979 ±0.013 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.875 ±0.068 | 1.000 ±0.000 | 0.800 ±0.088 |
| 1.00 | 23 | 0 | 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a |
| 1.00 | 23 | 0 | 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a |
| 1.00 | 23 | 0 | 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 |
| 1.00 | 23 | 0 | 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 |
| 1.00 | 23 | 0 | 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 |
| 1.00 | 23 | 0 | 6 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 |
| 1.00 | 23 | 0 | 7 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 |
| 1.00 | 23 | 0 | 8 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 |
| 1.00 | 23 | 0 | 9 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 |
| 1.00 | 23 | 0 | 10 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 |

| share joined | year | units held | content KB |
|---|---|---|---|
| 0.00 | 1 | 160 | 4.0 |
| 0.00 | 5 | 415 | 10.3 |
| 0.00 | 10 | 679 | 16.8 |
| 0.25 | 1 | 160 | 4.0 |
| 0.25 | 5 | 433 | 10.8 |
| 0.25 | 10 | 704 | 17.5 |
| 0.50 | 1 | 160 | 4.0 |
| 0.50 | 5 | 452 | 11.3 |
| 0.50 | 10 | 731 | 18.1 |
| 0.75 | 1 | 160 | 4.0 |
| 0.75 | 5 | 466 | 11.6 |
| 0.75 | 10 | 764 | 19.0 |
| 1.00 | 1 | 160 | 4.0 |
| 1.00 | 5 | 480 | 12.0 |
| 1.00 | 10 | 800 | 19.9 |

### Whether the model's answer is settled

| quantity | value |
|---|---|
| assertions asked | 240 |
| samples of each | 3 |
| temperature | 1 |
| every sample agreed | 0.850 ±0.045 |
| agreed on the wrong answer | 35 |
| samples split | 36 |
| split, where the settled answer is wrong | 0.352 ±0.129 |
| split, where the settled answer is right | 0.091 ±0.042 |
| joining on a split recovers | 10 |
| joining on a split wrongly joins | 26 |

| wording | asked | every sample agreed |
|---|---|---|
| `also` | 40 | 1.000 ±0.000 |
| `narrow` | 40 | 0.750 ±0.136 |
| `replace` | 160 | 0.838 ±0.057 |

### What the frame answered when a split resolves to join

| year | recall | carried | replaced | reverted | parallel | refined | fresh | stale returned |
|---|---|---|---|---|---|---|---|---|
| 1 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a |
| 2 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a |
| 3 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.263 ±0.097 |
| 4 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.263 ±0.097 |
| 5 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.263 ±0.097 |
| 6 | 0.963 ±0.017 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.775 ±0.078 | 1.000 ±0.000 | 0.263 ±0.097 |
| 7 | 0.963 ±0.017 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.775 ±0.078 | 1.000 ±0.000 | 0.275 ±0.098 |
| 8 | 0.963 ±0.017 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.775 ±0.078 | 1.000 ±0.000 | 0.275 ±0.098 |
| 9 | 0.963 ±0.017 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.775 ±0.078 | 1.000 ±0.000 | 0.275 ±0.098 |
| 10 | 0.963 ±0.017 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.775 ±0.078 | 1.000 ±0.000 | 0.275 ±0.098 |

### What the run cost

| quantity | value |
|---|---|
| model calls | 0 |
| turns observed, over every frame and arm | 115200 |
| contexts produced, over every frame and arm | 46400 |
| embedder calls | 27680 |
| distinct texts embedded | 6018 |
| tokens those texts estimate to | 61324 |
| a per-turn model design: calls per run | 5760 |
| a per-turn model design: prompt tokens per call | 722 |
| a per-turn model design: output tokens per call | 512 |
| a per-turn model design: prompt tokens per run | 4158720 |
| a per-turn model design: output tokens per run | 2949120 |

### What the paid half cost

| quantity | value |
|---|---|
| model | gpt-4.1-nano |
| paid calls, declaring | 840 |
| assertions declared | 840 |
| prompt tokens | 105914 |
| completion tokens | 840 |
| price per million prompt tokens, USD | 0.1 |
| price per million completion tokens, USD | 0.4 |
| price read on | 2026-08-16 |
| cost of declaring, USD | 0.0109 |
| call bound declaring refuses to pass | 1000 |
| cost bound declaring refuses to pass, USD | 0.25 |
| paid calls, resampling | 720 |
| prompt tokens, resampling | 92880 |
| completion tokens, resampling | 720 |
| cost of resampling, USD | 0.0096 |
| call bound resampling refuses to pass | 800 |
| cost bound resampling refuses to pass, USD | 0.10 |
| cost of this record in total, USD | 0.0205 |
<!-- end generated -->

A size is one store read at one moment rather than a sample, so it carries no interval; every other figure above is a mean over the probes of that year with the 95% half-width those probes support.

## What the numbers say

- **The shipped structure holds a fact for the whole span and does not remove the one it replaced.** In the `oracle` arm, `carried` — asserted once in the first year and never mentioned again — is answered as often in the last year as in the first, within its interval, while the stale figure is high from the first replacement year onward and does not fall afterwards. Holding and updating are separate abilities, and this structure has the first without the second.
- **The value it never removed is the value it answers best.** `reverted` is the lowest class of all while the replacement is what is true, and rises to answering every thread in the year each returns to the value it started with — a value the store still held, because nothing had taken it out. `replaced`, where the superseded values have to go, falls across the span instead. A structure that never forgets is punished by a workload that changes its mind and rewarded by one that changes it back.
- **The writeback decides the outcome and the store barely enters it.** The same store, the same search and the same ranking function are orders of magnitude apart between the two arms. A turn arriving without an extracted fact is an episodic observation, its salience tier expires inside the retention window, and a span of years is a great many windows.
- **Even the current year's fact is gone by the year's end without the oracle.** `fresh` is asserted in the year it is asked, and is answered almost every time in the `oracle` arm and almost never in the `text` one. The retention window is measured in days and a checkpoint stands at the end of a year.
- **A structure with no store at all is at or above the shipped structure without its oracle, in every year.** The intervals overlap throughout, so this run does not separate the two. What it does say is that a store its own retention empties is worth no more than one bounded document of the same size.
- **Both coexistence classes decay, and not together.** `parallel` and `refined` require the same thing — two simultaneously-true values of one attribute surviving — and differ only in how the second is worded. `carried` holds level across the span while both of them fall, and `refined` falls further from the year its second value arrives. Their intervals overlap, so the gap between the two wordings is something to widen the workload for rather than a separation this run makes.
- **What a decade costs on disk is not what it holds.** Stored bytes run orders of magnitude above content bytes at every checkpoint in both arms, and in the `text` arm they climb while the active index shrinks. The store never hard-deletes, so every turn ever observed is still paid for long after retention has taken it out of every search.
- **The best-scoring structure measured here is disqualified, and the figure that disqualifies it sits beside the figure that wins.** `attribute-cued` is the only arm that answers every replacement without ever returning a superseded value and answers `parallel` in full as well. It fails one class, `refined`, which asks for exactly what `parallel` asks for in different words. §The arity question states why that is a disqualification rather than a defect to fix.
- **Addressing removes the baseline's failure outright and exposes a different one.** Every attribute arm answers `replaced` and `reverted` in full at every year, where the shipped structure returns a superseded value on most revised threads from the first replacement onward. Identity at an address is not a better inference than identity from similarity; it is no inference, so there is nothing left to be wrong.
- **The two ends fail in opposite directions, each in the year the workload's shape predicts.** `attribute-single` answers everything until the year a second simultaneously-true value is asserted, then holds exactly one of the two on both coexistence classes, and its stale figure never leaves zero. `attribute-multi` never loses a value and returns a superseded one on every revised thread from the first replacement year on. Neither is a failure of addressing. Both are the arity decision, taken at the two ends, showing up where the workload asks about it.
- **`attribute-learned` is `attribute-multi`**, row for row, year for year and class for class, and it pays `attribute-multi`'s storage to get there — a rule that decides at read time cannot have destroyed anything at write time.
- **An addressed structure is inert without an extractor.** In the `text` arm every attribute arm holds nothing and answers nothing at every year: there is nowhere to put a sentence it cannot address, and nowhere to look when a question cannot be addressed either. The shipped structure keeps a floor there and this one has none, which is the sharper form of the same dependency.
- **Holding facts rather than sentences costs orders of magnitude less than the shipped store, and injecting an address costs orders of magnitude less than filling a budget.** At the last year the attribute arms hold a comparable number of things to the shipped active index and store a small fraction of its bytes, because an address and a value are all they keep — no sentence, no vector, no keyword index. What they put in the context is a line or two rather than a filled token budget, and their recall is measured against that.

- **The whole distance between every self-deciding structure and a perfect one is arity.** `attribute-declared` answers every class at every year and returns a superseded value on nothing, because the write states what the store would otherwise have to infer. No arm here is short of it for any other reason: the addressing is the same, the workload is the same, and the gap is the one decision the seam withholds.
- **The store shape does not change the answer; the arity rule does.** `attribute-single`, `interval` and `log` are three different stores taking one arity decision, and their recall and stale figures are identical at every year and every class. Whatever else is chosen between an addressed store, a store of spans and a log with a projection, it is not chosen on what the memory answers.
- **What they can be asked and what they cost differ, and that is the whole of the comparison.** The interval store and the log each keep a row for every assertion and hold more than the plain addressed store, which keeps a row for every value that is still true. The log reads every event it has ever kept to answer one question and the addressed stores read one address, and the gap between those two columns widens every year while the answers stay equal.
- **A structure that reads everything it holds does not survive a decade, whatever its recall says.** The shipped design ranks its whole active index on every recall, and the units it reads to answer one question rise with the store across the span. The log's projection rises the same way, for the same reason: it is derived rather than stored. `attribute-learned` is the third, and this is a cost #453 did not see — its rule reads every address it holds to decide how many values one of them returns, so answering one question touches all of them while every other addressed arm touches one.
- **The interval store buys history and the workload never asks for it.** Every question a checkpoint asks is about the present, so what this structure does that no other arm can — answer what was true in an earlier year — is unmeasured here. Its recall and stale figures are `attribute-single`'s and its extra rows are the price of a capability this workload does not exercise. That is a finding about the instrument: the seam has no way to ask about the past because the workload never asks one.

- **The two error directions fail in opposite figures, and one of them is invisible in recall.** A wrong replacement takes recall down and never touches the stale figure; a wrong join leaves recall at its ceiling at every rate measured, up to and including corrupting half of every eligible write, and shows up only as staleness. A table ranking on recall alone would report the accumulating failure as no failure at all, which is why recall never appears in this record without the stale figure beside it.
- **One of them heals and the other does not, and that is the structural difference.** A wrong join is repaired by the next correct replacement at that address, so nearly all of them are gone by the last year and the ones that are not are addresses the workload never writes to again. A wrong replacement is repaired by nothing: it destroys a value that is still true, and nothing in the workload asserts that value again. Healing is a property of what a structure does next, not of what it holds, and addressing supplies it in one direction only.
- **What repairs the one and not the other is the same mechanism.** A replacement clears the address, so it clears a wrongly-kept value with it; nothing re-writes a wrongly-cleared one. Making a wrong replacement heal would take a record of what was cleared and a rule for putting it back — which is what the interval frame holds and what §The frames says it holds it for, and a rule for when to put a value back is a rule about arity again.
- **A real model does not reach the ceiling, and the gap is one class.** Driven by a model's own declarations rather than by a rate, the frame answers every class at every year until the year the coexistence values arrive, and from then on loses only `refined`. `parallel` — the same requirement worded with a conjunction — stays at its ceiling for the whole decade. Every one of the model's destroying errors landed on the narrowing wording and none landed on the conjunction.
- **The model reproduces the cue list's failure, which says the failure is not the cue list's.** `attribute-cued` is disqualified because it enumerates the words that mark an addition, and it fails on `refined` and passes `parallel`. A model that enumerates nothing fails on the same class and passes the same class. What that says is not that the model is a cue list, but that a conjunction states arity and a qualifier only implies it — so an addition marked by implicature is hard to read rather than merely hard to list, and the disqualification stands against the rule and not against reading language.
- **Most of what a writeback declares cannot be wrong, and a rate that counts it says very little.** The overwhelming majority of assertions over a decade land on an address holding nothing, where replacing and joining are the same store. Counted over every eligible write the model's replacing errors look catastrophic; counted over the writes that land somewhere they are a fifth. The stale figure the frame actually posts follows the second and not the first.
- **The recommendation does not survive intact, and what it becomes is a trade rather than a ruin.** On this model's declarations the structure loses recall in one class permanently and returns a superseded value on about one revised thread in five, against a ceiling of neither. Set beside the arms that decide arity for themselves, it recovers recall that always-replacing gives up and pays for it in staleness that always-replacing does not have — and the staleness heals while the lost recall does not. Neither dominates, so "the write states arity" is a choice about which failure is cheaper rather than a structure that removes both.

- **Nothing recovers a wrongly-cleared value, and the store that keeps it proves the point rather than solving it.** `interval-declared` and `attribute-declared` on the same declarations answer identically at every year and every class, while the interval store holds a quarter again as many units — every value a wrong declaration cleared, sitting in a closed span. A question about the present does not return a closed span, so the information is retained and unreachable. Keeping it is not the same as recovering it, and the gap between those two is where this round ends.
- **Reaching it would take a rule for when to reopen a closed span, which is the arity question again.** The two total policies are the ends already measured: reopen nothing and the answer is the addressed store's; reopen everything and it is what keeping every value scores, staleness at its ceiling. Anything between them decides, after the fact, whether a particular closure was a replacement or a join — which is the decision the whole programme has been trying to source, arriving one step later. The loop closes without an answer, so the destroying error is irreducible on the evidence here and a structure has to be chosen knowing it.
- **Erring toward join blindly is a bad trade, and the exchange rate is the reason.** Joining every declared replacement recovers every destroying error and puts staleness at its ceiling; the curve between is close to linear. The recall it buys is small because the destroying errors are few, and the staleness it pays is large because the correct replacements are many — so a blind bias is overwhelmingly likely to spoil a write that was right. A policy that cannot tell which declarations to doubt should not be biased at all.
- **The model is in doubt about four times more often where it is wrong, and it is not enough.** Asked repeatedly, its answers split far more often on the assertions it gets wrong than on the ones it gets right, so disagreement is a real signal and not noise. But most of its wrong answers are given again every time it is asked: the error is settled, not uncertain. Resolving every split to a join recovers well under half the destroying errors and wrongly joins more correct replacements than it recovers.
- **The one bias that needs no number from the model is measurable, better than blind, and still does not close the gap.** Asking three times and joining where the samples split beats blind biasing by a wide margin on what it buys per point of staleness. It costs three model calls where there was one, carries a sample count as a knob, and leaves the coexistence class short of where a correct declaration would put it. It is reported as what it is rather than recommended.
- **The two stores that read a declaration are separated by what they hold and what they read, and by nothing a reader can ask about.** That is the same result #457 found among three stores under one self-deciding rule, holding now under a declaration that is sometimes wrong. Whatever is chosen, it is not chosen on what the memory answers.

- **A write reaches what a read cannot, in one structure and not the other.** Constructed directly: a fact written, never once read back, then corrected among enough facts in the same words that a lookup does not find it alone. The addressed store applies the correction — the address is computed from the sentence, so the write lands whether or not anything ever returned that address. The store that supersedes what its own lookup surfaces does not. Reading and updating are decoupled in one design and coupled in the other, and that is a larger difference between them than any figure this record carried before.
- **The same store fails to correct even what it can find, which bounds what the coupling explains.** With the row it must correct sitting alone, a correction phrased as a correction still does not land, while a restatement in the same words does: supersession fires on how much the two sentences share, and a correction shares little with what it corrects. So two causes are present and this workload separates them only in the constructed case. The workload-scale column reads backwards for the same reason — on that store a value no read returned is usually a value some unrelated write already archived, so "never reached" and "no longer held" are close to the same event and the column is not evidence about the coupling. The test is the measurement; the column is reported so a reader is not left to find that out.
- **Nothing is frozen, on this workload, in any structure.** Every planted value each structure holds has been carried into some context at least once. For the addressed stores that is unsurprising: the address asked is the address returned, and every address is asked every year. For the shipped store it is not — it holds hundreds of rows and returns tens per turn, but a decade asks thousands of questions, so the budget cycles through everything many times over. The failure mode the criterion names is real and this workload is too small to produce it.
- **What would produce it is a store larger than the reads can cycle through.** The frozen share turns positive when what a structure holds outruns what a decade of contexts can carry, and both of those are in the tables above. Nothing here approaches that, so the number to watch is the ratio rather than this round's zero.
- **The bytes that cannot be reached are the archive, and they are almost all of them.** The frozen share is zero over what a search can reach because the archive is not part of it: the shipped store never hard-deletes, no query reaches an archived row, and at the last year its stored bytes stand at two orders of magnitude above the content its active index holds. Those bytes are held, cannot be read, and cannot be corrected — the case the criterion describes, sitting outside the index rather than inside it.
- **A forgetting rule keyed on use has nothing to forget here.** The rule the frozen share would feed — drop what no read has ever reached — is well defined, needs no threshold and no clause for a class, and on this workload removes nothing, because nothing qualifies. It cannot be shown to be safe or unsafe by a run in which it never fires. The `carried` trap it was written against does not arise for the same reason: a carried fact is injected at every checkpoint that asks it, so a rule keyed on injection never sees it, and only a rule keyed on re-assertion would have eaten it.
- **A structure that answers the question brings back very little else.** The addressed stores carry about an eighth of what is live about the subject they are asked about; the shipped store carries about half, because a budget filled by ranking sweeps in whatever else scores. Under the criterion this round is for, that is the addressed store's one disadvantage — everything about that subject the context did not carry is outside the reach of that turn's writeback. What the coupling result says is that for an addressed store it does not follow that those facts are unfixable, because a later correction does not need to have read them.
- **Nothing approaches the capacity ceiling.** Every structure adds tens of units a year and the active-index capacity is more than a dozen decades away at the fastest rate measured. The question closes: capacity is not what bounds a decade, and it was never the constraint the ceiling was written for.

## What would be chosen today

The programme moves from measuring to deciding after this round, so this is where the record says where it stands. Two of the three questions are answered by the evidence and the third is not.

**The structure is chosen: a fact is held at its address, and arity comes from the write.** Addressing removes the failure the shipped design has — supersession inferred from similarity — outright rather than improving it, and every arm that tried to recover arity from the stored triple failed or reproduced one of the degenerate ends. Among the stores that take a declaration, the plain addressed one answers exactly what the interval store answers while holding less and reading less, so the simpler one is the one to build. The interval store earns its extra bytes only if a question about an earlier year is ever asked, and nothing in this workload asks one.

**The direction of error is chosen: where a writeback must err, it errs toward join.** That is not a preference. A wrong join is repaired by the next correct replacement at that address and carries its damage for no checkpoints; a wrong replacement is repaired by nothing and carries it to the end of the span. The asymmetry is measured, it is structural, and stating it costs no code.

**What this round adds to the structure.** The addressed store's weakness under the read-and-update criterion is that it brings back little beyond the address asked, so a turn's writeback sees little it could correct. That would be decisive if a fact had to be read before it could be fixed — and the constructed case says it does not, for this structure: the write computes its address and lands whether or not any read ever returned it. The same case says the opposite for the store that supersedes by similarity. So the criterion the user named separates these two designs further rather than closing the gap, and the choice already stated stands.

**The policy is not chosen, and the evidence cannot choose it.** Taking a real model's declarations recovers most of the coexistence an always-replacing store gives up, and pays for it with a superseded value on about one revised thread in five. Always replacing returns nothing stale and loses that coexistence permanently. Neither dominates. Which is cheaper depends on whether a fact that is lost costs more than a fact that is wrong, and no measurement in this record answers that — it is a property of what the agent is for, not of the structure. A reader looking for the row with the best numbers should be told that the best-scoring rule measured here is `attribute-cued`, which is disqualified for being a list that grows, and that it sits on the frontier rather than below it. The criterion has to be applied deliberately; it is not visible in the ranking.

What would be built today, then, is the addressed store with the operation stated by the writeback, erring toward join. What is still open is what to do when the declaration is unreliable, and that is a decision to take rather than a measurement to make.

## What this record does not say

- It does not choose the policy for a declaration that cannot be trusted. §What would be chosen today states the structure and the direction of error as settled and says why the evidence cannot settle this third one: taking a model's declarations and always replacing are a genuine trade, and which failure is cheaper is a property of what the agent is for rather than of anything measured here. A reader looking for that decision will not find it in a table.
- It does not measure a writeback that decides what is worth storing. Both arms hold extraction constant, so what a model is measured deciding here is arity and nothing else; whether a model writes anything at all unprompted is `memory-cycle/`'s subject.
- It does not measure a forgetting rule in operation. The rule the frozen share would feed removes nothing here because nothing qualifies, so whether it is safe is untested rather than established — a run in which a rule never fires says nothing about the rule.
- It does not measure a question about an earlier year. The interval store's extra bytes buy the ability to answer one, and the workload asks none, so what that capability is worth is still unmeasured — the same gap #457 reported, unchanged by this round.
- It does not rank the structures. `attribute-declared` is a ceiling rather than a design, several arms are the ends of a range rather than candidates, and the best-scoring rule measured is disqualified — so the order of the rows is not an order of preference.
- It does not say the attribute frame's storage advantage would survive persistence. It is measured as an in-memory structure with no file format, so what it stores is what it holds; a persisted form would add a page layout and an index the shipped store's figure already carries.
- It does not measure retrieval quality as such. That is `memory-retrieval/`'s subject, and the constants that decide it are #411's.
- It says nothing about latency. No run time is measured, and the frames differ so much in what they do per turn that comparing theirs would compare implementations rather than structures.

## Limits

- **One seed.** Every stage is deterministic in it. A second seed would redraw which subjects, attributes and values the threads get, which is the variation the stated intervals already carry.
- **Values are unique tokens.** That is what makes crediting exact, and it is not how every real fact looks: two people can be allergic to the same thing. A workload whose values collided would need a credit rule that reads subject and value together, and this one does not test any structure's behaviour under that.
- **Crediting is by reading the injected text.** It is the only rule that can score a structure with rows and a structure with one document on the same footing. It credits a value present for the wrong reason — a frame that returned everything it ever saw would score perfectly on recall, which is why the stale figure is beside it and why context size is reported.
- **A checkpoint does not write.** Real questions are turns and a design could learn from being asked one. Here it cannot, so nothing scores by echoing the question.
- **The frozen share is zero because the workload is small, not because a structure cannot freeze.** A decade of questions carries more into contexts than any structure here holds, so everything is reached. The number is a property of this workload's ratio of questions to facts, and a store that outgrew that ratio would report differently — which is what the extrapolation beside it is for.
- **The frozen share is counted over what a search can reach.** The shipped store's archive is excluded, because no query returns an archived row: counting it as held would make the figure incomparable with a structure that has no archive. What the archive costs is in the byte columns and is not in the frozen column, and those bytes are the clearest case of the criterion in the record.
- **The subject relation counts what is live, not what would be useful.** A value true anywhere on the probed subject counts as related whether or not a person would think it relevant to that question. It is the widest relation the workload can define without inventing one, and a narrower one would need a judgement the ground truth does not carry.
- **The doubt figures are one model's, three samples deep.** Whether disagreement tracks error was measured at three samples of each consequential assertion. More samples would sharpen it, a different model might be in doubt somewhere else, and neither is measured. What the figure supports is that most of this model's wrong answers are settled; it does not support a claim about models in general.
- **No rule was built on the doubt beyond the simplest one.** Joining where the samples split is the one policy measured, because it needs nothing from the model but another answer. A rule weighing how many samples disagreed, or how far apart they were, would be a threshold on a number, which this round declined to build rather than tested and rejected.
- **One model, one prompt, one day.** The declaration figures describe the cheapest model at the provider this project ships, asked one way, on one afternoon. A larger model, a prompt that showed it examples, or a second attempt at the same call would all move them, and none of that is measured. The provider is not reproducible to the last bit either, which is why the answers are committed rather than re-bought.
- **The paid measurement asks about a sentence and not about a turn.** The model sees the address, what is held there, and the statement — what a writeback with a working recall would see. It does not see the conversation around the statement, and a real writeback would. Whether that context carries the arity a qualifier only implies is the obvious next question and it is not this one.
- **The workload has few writes that join.** Only the two coexistence classes' second values ever do, so the destroying direction's sweep has coarse resolution at the low end and its rows below that resolution show no errors rather than few. A workload with more coexistence would sharpen that curve, and changing it would stop every figure measured against this one from being comparable.
- **The interval store's distinguishing capability is unmeasured.** Every checkpoint asks about the present, so nothing here scores the one question a store of spans can answer and an addressed store cannot. A unit test asks it as of a moment inside a closed span and gets what was true then, which is what the claim rests on; the workload asks no such question and was not widened to, because a workload changed mid-programme stops every figure measured against the earlier one from being comparable. It is a separate issue and it would have to run against every frame from the start.
- **The byte figures understate the stores that keep instants.** Content and stored bytes count the text a frame holds. The interval store keeps two instants bounding every span and the addressed stores keep one beside every value, and neither is in the byte column — so the comparison of bytes between them understates the interval store by exactly the bounds that are the reason to use it. The units column carries that difference and the byte column does not.
- **What the oracle arm covers narrowed when the question arrived extracted.** A frame handed the address is measured on holding and updating and not on finding. The shipped frame is still measured on all three, because it queries with the question's text, so the two are not measured on the same job and the record does not read their recall figures against each other as though they were. A comparison of the finding half needs a run in which no frame receives the address, and this is not that run.
- **The active index never approaches capacity.** Retention removes the observations before the ceiling is near, so capacity eviction never fires in either arm and this workload says nothing about what it would do.
- **The oracle never errs.** It extracts every fact exactly and nothing else. A partial or noisy extractor is the realistic middle of the bracket and is not measured.
- **One conversation shape.** Turns are short single-clause sentences, sessions are single-day, and a year's assertions are scattered uniformly through it. A workload with long turns, or one where facts arrive in bursts, would move both the token budget and the retention window's effect.
- **The results are not regenerated by `make ci`.** A full run is minutes of CPU, so CI compiles the harness, runs its tests, and checks this record against the committed results; it does not re-measure. A change in `internal/memory` that moves these figures will not fail CI, and re-running is a manual step.

## Reproducing

- `go run ./docs/experiments/memory-frames/harness -seed 7` — the whole measurement and the tables above. Offline, no API key, no cost.
- `go run ./docs/experiments/memory-frames/harness -render` — rewrites the tables above from the committed results without measuring anything.
- `go test ./docs/experiments/memory-frames/harness` — the workload's ground truth, the credit rule, the seam's frames, the interval, the determinism check under two scheduling conditions, and the check that this record matches the committed results.
