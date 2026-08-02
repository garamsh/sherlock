# Simplicity Conventions

Structural economy in code: how many shapes one concept may have, when a layer or abstraction earns its place, and what never ships. Complexity must earn its place. Scope boundary: `runtime-safety.md` owns types and validation placement, `style.md` owns naming and pattern consistency; this file owns structure — shape count, layer count, abstraction count.

## One concept, one shape

- A domain concept has exactly one struct/type definition. A second struct with the same fields carrying the same data (a "translation DTO") is a defect, not layering.
- A distinct shape is justified only at a real boundary: wire format, storage schema, or an external API. "Clean separation" is not a boundary.
- When two shapes genuinely exist, exactly one named function converts between them, at the boundary — never field-by-field copies spread across call sites.

## Layers earn their place by transforming

- A new layer (package, struct pair, indirection) must transform data or cross a process/machine boundary. A layer that only copies fields or forwards arguments is removed.
- Before adding a layer, ask: what breaks if the consumer uses the inner type directly? If the answer is "nothing", the layer does not exist.
- Every capability is a tool behind one contract; no organizing layer is introduced above or beside that contract.
- Before adding a capability, ask first whether it is expressible behind the existing tool contract. A new subsystem is the last resort, not the default.

## No speculative or dead generality

- Generality lands with its first real caller; an abstraction lands when the second real case arrives — never before. No single-use abstractions, no plugin systems, registries, or dispatch tables built ahead of a second user.
- Ship nothing unused: no struct tags nothing applies (e.g. `yaml:` tags on a type nothing YAML-decodes), no options no caller sets, no parameters reserved "for later".

## Consolidate with judgment

- When two features look similar, first verify whether they truly differ. If a unified module is more intuitive and needs no flags and conditionals everywhere, unify. If unification requires branching at every turn, they differ in essence — keep them separate. A wrong abstraction is worse than honest duplication.

## No guard-stacking

- `runtime-safety.md` owns where validation happens (once, at the boundary). This file owns the consequence: do not add compensating checks, wrappers, or fallback layers around a weak boundary — strengthen the boundary instead.
- A fallback path is justified only when the underlying failure is real and expected (e.g. an optional capability absent at runtime) and is documented as such at the fallback site.
- Delete, don't wrap: prefer deleting dead code and dead paths over layering compatibility shims on top of them.

## Clarity is testable

- If a competent engineer cannot understand a unit in a few seconds of reading, restructure it before shipping. Nesting depth, indirection hops, and the number of concepts a reader must hold at once are all reviewable.
