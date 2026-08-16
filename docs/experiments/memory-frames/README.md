# Memory-frames experiment

What a memory structure does with a decade of turns, measured through a seam that structurally different designs can sit behind, with the design this project ships measured through it as the baseline the rest are read against.

## Contents
- Method
- The seam
- The workload
- The frames
- The arity question
- What a run costs
- Results
- What the numbers say
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

At the end of every year, every thread answerable that year is asked its attribute's question. The question names the subject and the attribute and never the value. Two things are then read off the injected context: **recall**, the share of currently-true values present in it, and **stale returned**, whether any value that has stopped being true is present. A checkpoint reads a frame and never writes to it.

Every planted value is a hyphenated pair of words, and no pair is planted on more than one thread. A value is therefore a token that identifies the assertion carrying it, so crediting is exact by reading — the trap #371 had to add a word boundary for cannot arise. `harness/workload_test.go` checks the uniqueness, checks that no turn carrying no fact mentions a planted value, and checks each class's schedule; `harness/measure_test.go` checks the credit rule against the substring cases it has to reject.

`replaced` and `reverted` state the new value without repeating the old one. A correction that named what it corrects would plant the stale value in the transcript, and the stale figure would then be counting the workload's own words.

`parallel` and `refined` differ in wording and in nothing else: both plant two simultaneously-true values of one attribute years apart. A design that scores well on one and badly on the other is keying on phrasing.

**How the disqualification criterion is read off these numbers.** The project owner's standard is that a design needing code added to handle a particular case is disqualified rather than patched. A per-case rule is not visible in a total, which is how the best-scoring structure of the last round passed. It is visible in a profile: each class is reported at each year, and the two coexistence classes are the same case in different words. A structure whose score comes from a rule written for one case has an uneven profile across the classes, or a gap between `parallel` and `refined`, and the tables below are laid out so that both show.

## The frames

- `shipped` — `internal/memory` as it runs: the SQLite store, hybrid search over embeddings and FTS, the one ranking function, supersession on insert, and the two passes `internal/gateway/queue.go` runs after a completed turn, threshold consolidation and tiered retention. `Recall` is what it puts in the context, bounded by the shipped token budget.
- `document` — no store at all: one bounded document holding the most recent turns' text, rewritten each turn, dropping the oldest to stay inside a limit set to the shipped token budget so both frames put contexts of the same size in front of the model. It performs no retrieval and returns the document whatever the question. It is the floor a structure with a store has to beat to be buying anything.
- `attribute-*` — a fact is held at its address, `(subject, attribute)`. Writing to an address is how a value is replaced, so identity is settled outright and similarity appears nowhere in the write path: the mechanism `memory-retrieval/` refuted is not weakened here, it is absent. The frame keeps no sentence, no vector and no keyword index — an address and a value — and answers a question by going to one place rather than by filling a budget. Four arms differ in how many values one address holds and in nothing else: `attribute-single` keeps one and a write replaces it; `attribute-multi` keeps every value ever written; `attribute-learned` and `attribute-cued` are the two rules §The arity question states.

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

## What a run costs

Established before the run, as issue #443 requires.

Neither frame calls a model, and the embedder is the deterministic offline one `cmd/gagent` defaults to, so a run costs nothing and needs no key. It was started without asking on that basis. What the shipped OpenAI embedder costs the read path, and which conclusions may be quoted across the two embedders, is #435's measurement and is not repeated here.

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

| frame | arm | year | asked | recall | carried | replaced | reverted | parallel | refined | fresh | stale returned | context chars |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| `shipped` | `oracle` | 1 | 160 | 0.919 ±0.042 | 0.925 ±0.083 | 0.950 ±0.068 | 0.900 ±0.094 | n/a | n/a | 0.900 ±0.094 | n/a | 2172 ±2 |
| `shipped` | `oracle` | 2 | 240 | 0.946 ±0.029 | 0.900 ±0.094 | 0.950 ±0.068 | 0.875 ±0.104 | 1.000 ±0.000 | 0.975 ±0.049 | 0.975 ±0.049 | n/a | 2169 ±2 |
| `shipped` | `oracle` | 3 | 240 | 0.921 ±0.034 | 0.900 ±0.094 | 0.925 ±0.083 | 0.750 ±0.136 | 0.975 ±0.049 | 0.975 ±0.049 | 1.000 ±0.000 | 0.863 ±0.076 | 2159 ±3 |
| `shipped` | `oracle` | 4 | 240 | 0.908 ±0.037 | 0.900 ±0.094 | 0.900 ±0.094 | 0.750 ±0.136 | 0.975 ±0.049 | 0.950 ±0.068 | 0.975 ±0.049 | 0.850 ±0.079 | 2162 ±3 |
| `shipped` | `oracle` | 5 | 240 | 0.912 ±0.036 | 0.900 ±0.094 | 0.900 ±0.094 | 0.750 ±0.136 | 0.975 ±0.049 | 0.950 ±0.068 | 1.000 ±0.000 | 0.838 ±0.081 | 2169 ±3 |
| `shipped` | `oracle` | 6 | 240 | 0.892 ±0.037 | 0.900 ±0.094 | 0.900 ±0.094 | 0.750 ±0.136 | 0.950 ±0.059 | 0.850 ±0.080 | 1.000 ±0.000 | 0.838 ±0.081 | 2161 ±2 |
| `shipped` | `oracle` | 7 | 240 | 0.927 ±0.029 | 0.900 ±0.094 | 0.875 ±0.104 | 1.000 ±0.000 | 0.938 ±0.063 | 0.850 ±0.080 | 1.000 ±0.000 | 0.838 ±0.081 | 2157 ±2 |
| `shipped` | `oracle` | 8 | 240 | 0.927 ±0.029 | 0.900 ±0.094 | 0.875 ±0.104 | 1.000 ±0.000 | 0.938 ±0.063 | 0.850 ±0.080 | 1.000 ±0.000 | 0.838 ±0.081 | 2161 ±3 |
| `shipped` | `oracle` | 9 | 240 | 0.925 ±0.030 | 0.900 ±0.094 | 0.875 ±0.104 | 1.000 ±0.000 | 0.925 ±0.066 | 0.850 ±0.080 | 1.000 ±0.000 | 0.838 ±0.081 | 2159 ±3 |
| `shipped` | `oracle` | 10 | 240 | 0.923 ±0.030 | 0.900 ±0.094 | 0.875 ±0.104 | 1.000 ±0.000 | 0.925 ±0.066 | 0.838 ±0.081 | 1.000 ±0.000 | 0.838 ±0.081 | 2160 ±2 |
| `document` | `oracle` | 1 | 160 | 0.075 ±0.041 | 0.000 ±0.000 | 0.100 ±0.094 | 0.125 ±0.104 | n/a | n/a | 0.075 ±0.083 | n/a | 2031 ±0 |
| `document` | `oracle` | 2 | 240 | 0.037 ±0.024 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.100 ±0.094 | 0.050 ±0.068 | 0.075 ±0.083 | n/a | 2008 ±0 |
| `document` | `oracle` | 3 | 240 | 0.037 ±0.024 | 0.000 ±0.000 | 0.100 ±0.094 | 0.025 ±0.049 | 0.000 ±0.000 | 0.000 ±0.000 | 0.100 ±0.094 | 0.000 ±0.000 | 2037 ±0 |
| `document` | `oracle` | 4 | 240 | 0.008 ±0.012 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.050 ±0.068 | 0.000 ±0.000 | 2042 ±0 |
| `document` | `oracle` | 5 | 240 | 0.025 ±0.020 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.150 ±0.112 | 0.000 ±0.000 | 2045 ±0 |
| `document` | `oracle` | 6 | 240 | 0.023 ±0.016 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.075 ±0.056 | 0.013 ±0.024 | 0.050 ±0.068 | 0.000 ±0.000 | 2029 ±0 |
| `document` | `oracle` | 7 | 240 | 0.042 ±0.025 | 0.000 ±0.000 | 0.125 ±0.104 | 0.075 ±0.083 | 0.000 ±0.000 | 0.000 ±0.000 | 0.050 ±0.068 | 0.000 ±0.000 | 2011 ±0 |
| `document` | `oracle` | 8 | 240 | 0.021 ±0.018 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.125 ±0.104 | 0.000 ±0.000 | 2018 ±0 |
| `document` | `oracle` | 9 | 240 | 0.004 ±0.008 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.025 ±0.049 | 0.000 ±0.000 | 2017 ±0 |
| `document` | `oracle` | 10 | 240 | 0.008 ±0.012 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.050 ±0.068 | 0.000 ±0.000 | 2047 ±0 |
| `attribute-single` | `oracle` | 1 | 160 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a | 26 ±0 |
| `attribute-single` | `oracle` | 2 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | 25 ±0 |
| `attribute-single` | `oracle` | 3 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 |
| `attribute-single` | `oracle` | 4 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 |
| `attribute-single` | `oracle` | 5 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 |
| `attribute-single` | `oracle` | 6 | 240 | 0.833 ±0.030 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 |
| `attribute-single` | `oracle` | 7 | 240 | 0.833 ±0.030 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 25 ±0 |
| `attribute-single` | `oracle` | 8 | 240 | 0.833 ±0.030 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 25 ±0 |
| `attribute-single` | `oracle` | 9 | 240 | 0.833 ±0.030 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 25 ±0 |
| `attribute-single` | `oracle` | 10 | 240 | 0.833 ±0.030 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 25 ±0 |
| `attribute-multi` | `oracle` | 1 | 160 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a | 26 ±0 |
| `attribute-multi` | `oracle` | 2 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | 25 ±0 |
| `attribute-multi` | `oracle` | 3 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 34 ±2 |
| `attribute-multi` | `oracle` | 4 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 34 ±2 |
| `attribute-multi` | `oracle` | 5 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 34 ±2 |
| `attribute-multi` | `oracle` | 6 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 43 ±2 |
| `attribute-multi` | `oracle` | 7 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 47 ±2 |
| `attribute-multi` | `oracle` | 8 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 47 ±2 |
| `attribute-multi` | `oracle` | 9 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 47 ±2 |
| `attribute-multi` | `oracle` | 10 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 48 ±2 |
| `attribute-learned` | `oracle` | 1 | 160 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a | 26 ±0 |
| `attribute-learned` | `oracle` | 2 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | 25 ±0 |
| `attribute-learned` | `oracle` | 3 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 34 ±2 |
| `attribute-learned` | `oracle` | 4 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 34 ±2 |
| `attribute-learned` | `oracle` | 5 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 34 ±2 |
| `attribute-learned` | `oracle` | 6 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 43 ±2 |
| `attribute-learned` | `oracle` | 7 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 47 ±2 |
| `attribute-learned` | `oracle` | 8 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 47 ±2 |
| `attribute-learned` | `oracle` | 9 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 47 ±2 |
| `attribute-learned` | `oracle` | 10 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 48 ±2 |
| `attribute-cued` | `oracle` | 1 | 160 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | n/a | 1.000 ±0.000 | n/a | 26 ±0 |
| `attribute-cued` | `oracle` | 2 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | n/a | 25 ±0 |
| `attribute-cued` | `oracle` | 3 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 |
| `attribute-cued` | `oracle` | 4 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 |
| `attribute-cued` | `oracle` | 5 | 240 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 26 ±0 |
| `attribute-cued` | `oracle` | 6 | 240 | 0.917 ±0.024 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 30 ±1 |
| `attribute-cued` | `oracle` | 7 | 240 | 0.917 ±0.024 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 30 ±1 |
| `attribute-cued` | `oracle` | 8 | 240 | 0.917 ±0.024 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 30 ±1 |
| `attribute-cued` | `oracle` | 9 | 240 | 0.917 ±0.024 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 30 ±1 |
| `attribute-cued` | `oracle` | 10 | 240 | 0.917 ±0.024 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 1.000 ±0.000 | 0.500 ±0.000 | 1.000 ±0.000 | 0.000 ±0.000 | 30 ±1 |
| `shipped` | `text` | 1 | 160 | 0.056 ±0.036 | 0.000 ±0.000 | 0.075 ±0.083 | 0.075 ±0.083 | n/a | n/a | 0.075 ±0.083 | n/a | 1304 ±0 |
| `shipped` | `text` | 2 | 240 | 0.017 ±0.016 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.025 ±0.049 | 0.025 ±0.049 | 0.050 ±0.068 | n/a | 610 ±0 |
| `shipped` | `text` | 3 | 240 | 0.025 ±0.020 | 0.000 ±0.000 | 0.050 ±0.068 | 0.025 ±0.049 | 0.000 ±0.000 | 0.000 ±0.000 | 0.075 ±0.083 | 0.000 ±0.000 | 1263 ±0 |
| `shipped` | `text` | 4 | 240 | 0.004 ±0.008 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.025 ±0.049 | 0.000 ±0.000 | 1183 ±0 |
| `shipped` | `text` | 5 | 240 | 0.021 ±0.018 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.125 ±0.104 | 0.000 ±0.000 | 1216 ±0 |
| `shipped` | `text` | 6 | 240 | 0.013 ±0.013 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.013 ±0.024 | 0.013 ±0.024 | 0.050 ±0.068 | 0.000 ±0.000 | 572 ±0 |
| `shipped` | `text` | 7 | 240 | 0.013 ±0.014 | 0.000 ±0.000 | 0.050 ±0.068 | 0.025 ±0.049 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 539 ±0 |
| `shipped` | `text` | 8 | 240 | 0.004 ±0.008 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.025 ±0.049 | 0.000 ±0.000 | 500 ±0 |
| `shipped` | `text` | 9 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 430 ±0 |
| `shipped` | `text` | 10 | 240 | 0.008 ±0.012 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.050 ±0.068 | 0.000 ±0.000 | 977 ±0 |
| `document` | `text` | 1 | 160 | 0.075 ±0.041 | 0.000 ±0.000 | 0.100 ±0.094 | 0.125 ±0.104 | n/a | n/a | 0.075 ±0.083 | n/a | 2031 ±0 |
| `document` | `text` | 2 | 240 | 0.037 ±0.024 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.100 ±0.094 | 0.050 ±0.068 | 0.075 ±0.083 | n/a | 2008 ±0 |
| `document` | `text` | 3 | 240 | 0.037 ±0.024 | 0.000 ±0.000 | 0.100 ±0.094 | 0.025 ±0.049 | 0.000 ±0.000 | 0.000 ±0.000 | 0.100 ±0.094 | 0.000 ±0.000 | 2037 ±0 |
| `document` | `text` | 4 | 240 | 0.008 ±0.012 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.050 ±0.068 | 0.000 ±0.000 | 2042 ±0 |
| `document` | `text` | 5 | 240 | 0.025 ±0.020 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.150 ±0.112 | 0.000 ±0.000 | 2045 ±0 |
| `document` | `text` | 6 | 240 | 0.023 ±0.016 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.075 ±0.056 | 0.013 ±0.024 | 0.050 ±0.068 | 0.000 ±0.000 | 2029 ±0 |
| `document` | `text` | 7 | 240 | 0.042 ±0.025 | 0.000 ±0.000 | 0.125 ±0.104 | 0.075 ±0.083 | 0.000 ±0.000 | 0.000 ±0.000 | 0.050 ±0.068 | 0.000 ±0.000 | 2011 ±0 |
| `document` | `text` | 8 | 240 | 0.021 ±0.018 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.125 ±0.104 | 0.000 ±0.000 | 2018 ±0 |
| `document` | `text` | 9 | 240 | 0.004 ±0.008 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.025 ±0.049 | 0.000 ±0.000 | 2017 ±0 |
| `document` | `text` | 10 | 240 | 0.008 ±0.012 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.050 ±0.068 | 0.000 ±0.000 | 2047 ±0 |
| `attribute-single` | `text` | 1 | 160 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | n/a | 0.000 ±0.000 | n/a | 0 ±0 |
| `attribute-single` | `text` | 2 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | 0 ±0 |
| `attribute-single` | `text` | 3 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-single` | `text` | 4 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-single` | `text` | 5 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-single` | `text` | 6 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-single` | `text` | 7 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-single` | `text` | 8 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-single` | `text` | 9 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-single` | `text` | 10 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-multi` | `text` | 1 | 160 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | n/a | 0.000 ±0.000 | n/a | 0 ±0 |
| `attribute-multi` | `text` | 2 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | 0 ±0 |
| `attribute-multi` | `text` | 3 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-multi` | `text` | 4 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-multi` | `text` | 5 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-multi` | `text` | 6 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-multi` | `text` | 7 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-multi` | `text` | 8 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-multi` | `text` | 9 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-multi` | `text` | 10 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-learned` | `text` | 1 | 160 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | n/a | 0.000 ±0.000 | n/a | 0 ±0 |
| `attribute-learned` | `text` | 2 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | 0 ±0 |
| `attribute-learned` | `text` | 3 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-learned` | `text` | 4 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-learned` | `text` | 5 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-learned` | `text` | 6 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-learned` | `text` | 7 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-learned` | `text` | 8 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-learned` | `text` | 9 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-learned` | `text` | 10 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-cued` | `text` | 1 | 160 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | n/a | 0.000 ±0.000 | n/a | 0 ±0 |
| `attribute-cued` | `text` | 2 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | n/a | 0 ±0 |
| `attribute-cued` | `text` | 3 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-cued` | `text` | 4 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-cued` | `text` | 5 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-cued` | `text` | 6 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-cued` | `text` | 7 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-cued` | `text` | 8 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-cued` | `text` | 9 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |
| `attribute-cued` | `text` | 10 | 240 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0.000 ±0.000 | 0 ±0 |

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

### What the run cost

| quantity | value |
|---|---|
| model calls | 0 |
| turns observed, over every frame and arm | 69120 |
| contexts produced, over every frame and arm | 27840 |
| embedder calls | 27680 |
| distinct texts embedded | 6018 |
| tokens those texts estimate to | 61324 |
| a per-turn model design: calls per run | 5760 |
| a per-turn model design: prompt tokens per call | 722 |
| a per-turn model design: output tokens per call | 512 |
| a per-turn model design: prompt tokens per run | 4158720 |
| a per-turn model design: output tokens per run | 2949120 |
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

## What this record does not say

- It recommends no structure. The attribute frame answers one of the questions the programme is asking and leaves the rest open; validity intervals and a derived view are separate issues, and what to build comes after the frames are measured rather than from any one of them.
- It does not measure a model-driven writeback. Both arms stand in for one, at the two ends of a bracket, and the record says which is which.
- It does not say the attribute frame's storage advantage would survive persistence. It is measured as an in-memory structure with no file format, so what it stores is what it holds; a persisted form would add a page layout and an index the shipped store's figure already carries.
- It does not measure retrieval quality as such. That is `memory-retrieval/`'s subject, and the constants that decide it are #411's.
- It says nothing about latency. No run time is measured, and the frames differ so much in what they do per turn that comparing theirs would compare implementations rather than structures.

## Limits

- **One seed.** Every stage is deterministic in it. A second seed would redraw which subjects, attributes and values the threads get, which is the variation the stated intervals already carry.
- **Values are unique tokens.** That is what makes crediting exact, and it is not how every real fact looks: two people can be allergic to the same thing. A workload whose values collided would need a credit rule that reads subject and value together, and this one does not test any structure's behaviour under that.
- **Crediting is by reading the injected text.** It is the only rule that can score a structure with rows and a structure with one document on the same footing. It credits a value present for the wrong reason — a frame that returned everything it ever saw would score perfectly on recall, which is why the stale figure is beside it and why context size is reported.
- **A checkpoint does not write.** Real questions are turns and a design could learn from being asked one. Here it cannot, so nothing scores by echoing the question.
- **What the oracle arm covers narrowed when the question arrived extracted.** A frame handed the address is measured on holding and updating and not on finding. The shipped frame is still measured on all three, because it queries with the question's text, so the two are not measured on the same job and the record does not read their recall figures against each other as though they were. A comparison of the finding half needs a run in which no frame receives the address, and this is not that run.
- **The active index never approaches capacity.** Retention removes the observations before the ceiling is near, so capacity eviction never fires in either arm and this workload says nothing about what it would do.
- **The oracle never errs.** It extracts every fact exactly and nothing else. A partial or noisy extractor is the realistic middle of the bracket and is not measured.
- **One conversation shape.** Turns are short single-clause sentences, sessions are single-day, and a year's assertions are scattered uniformly through it. A workload with long turns, or one where facts arrive in bursts, would move both the token budget and the retention window's effect.
- **The results are not regenerated by `make ci`.** A full run is minutes of CPU, so CI compiles the harness, runs its tests, and checks this record against the committed results; it does not re-measure. A change in `internal/memory` that moves these figures will not fail CI, and re-running is a manual step.

## Reproducing

- `go run ./docs/experiments/memory-frames/harness -seed 7` — the whole measurement and the tables above. Offline, no API key, no cost.
- `go run ./docs/experiments/memory-frames/harness -render` — rewrites the tables above from the committed results without measuring anything.
- `go test ./docs/experiments/memory-frames/harness` — the workload's ground truth, the credit rule, the seam's frames, the interval, the determinism check under two scheduling conditions, and the check that this record matches the committed results.
