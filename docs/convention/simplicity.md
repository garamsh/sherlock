# Simplicity Conventions

Structural economy in code: how many shapes one concept may have, when a layer earns its place, and what never ships. Scope boundary: `runtime-safety.md` owns types and validation placement, `style.md` owns naming and pattern consistency; this file owns structure — layer count and shape count.

## One concept, one shape

- A domain concept has exactly one struct/type definition. A second struct with the same fields carrying the same data (a "translation DTO") is a defect, not layering.
- A distinct shape is justified only at a real boundary: wire format, storage schema, or an external API. "Clean separation" is not a boundary.
- When two shapes genuinely exist, exactly one named function converts between them, at the boundary — never field-by-field copies spread across call sites.

## Layers earn their place by transforming

- A new layer (package, struct pair, indirection) must transform data or cross a process/machine boundary. A layer that only copies fields is removed.
- Before adding a layer, ask: what breaks if the consumer uses the inner type directly? If the answer is "nothing", the layer does not exist.
- Every capability is a tool behind one contract; no organizing layer is introduced above or beside that contract.
- Before adding a capability, ask first whether it is expressible behind the existing tool contract. A new subsystem is the last resort, not the default.

## No dead generality

- Ship nothing unused: no struct tags nothing applies (e.g. `yaml:` tags on a type nothing YAML-decodes), no options no caller sets, no parameters reserved "for later".
- Generality lands with its first caller. Until then the specific form is the code.

## No guard-stacking

- `runtime-safety.md` owns where validation happens (once, at the boundary). This file owns the consequence: do not add compensating checks, wrappers, or fallback layers around a weak boundary — strengthen the boundary instead.
- A fallback path is justified only when the underlying failure is real and expected (e.g. an optional capability absent at runtime) and is documented as such at the fallback site.
