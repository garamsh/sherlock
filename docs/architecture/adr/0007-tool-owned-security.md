# ADR 0007: Tools own security; guard models are layers, not boundaries

> Status: accepted
> Date: 2026-07-25

## Context

Security enforcement needs a home consistent with the three-component model (Gateway / Agent / Tool). Two candidate philosophies: enforcement concentrated in a separate security layer, or enforcement distributed into the tools themselves. A related question is whether model-based inspection (a guard model classifying commands, scanning outputs) can serve as a security boundary.

## Decision

Each tool owns and enforces the security policy of its own domain. No separate security layer exists.

- **Trust-domain invariant**: a tool that executes agent-supplied code runs with no reachable secrets; a tool that holds secrets never executes agent-supplied code and accepts only schema-validated structured input. Every tool is classified into one of these two domains at design review.
- **External access is tool-routed by principle.** Services are reached through tool calls; raw network egress from the agent UID exists only through the egress tool, which enforces the allowlist and secret-reference bindings (ADR 0004).
- **Guard models are adopted as defense-in-depth**: command risk classification before execution, secret scanning of tool outputs. They catch accidents and provide audit value. They are documented as *not* a security boundary — model judgment is probabilistic and bypassable by obfuscation; boundaries are structural only (file ownership, secret absence, network policy).
- **Light confinement inside the agent domain**: the file tool is rooted at the workspace with path-traversal validation, separating working data from the agent runtime itself. This is convenience, not a trust boundary — both are agent UID territory.

## Consequences

- "Is this expressible as a tool?" (PLAN §6) now has a security corollary: "which trust domain does this tool belong to?" is answered at design time, making placement automatic.
- Agent-facing performance is unaffected: hot-path tools execute locally in-domain; boundary crossing is limited to operations that are already network-bound.
- Guard behavior can be tuned without touching boundary mechanisms, and boundary mechanisms need no model involvement.
