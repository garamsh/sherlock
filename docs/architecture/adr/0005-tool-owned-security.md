# ADR 0005: Tools own security; guard models are layers, not boundaries

> Status: accepted
> Date: 2026-07-25

## Context

Security enforcement needs a home consistent with the component model. Two candidate philosophies: enforcement concentrated in a separate security layer, or enforcement distributed into the tools themselves. A related question is whether model-based inspection (a guard model classifying commands, scanning outputs) can serve as a security boundary.

## Decision

Each tool owns and enforces the security policy of its own domain. No separate security layer exists.

- **Trust-domain invariant**: a tool that executes agent-influenced code runs with no reachable secrets; a tool that holds secrets never executes agent-influenced code and accepts only schema-validated structured input. In the pod topology (ADR 0018) the two domains land on the two containers: execution in the sidecar, secrets in the agent container.
- **External access is tool-routed by principle.** Services are reached through tool calls; raw network egress from executed code exists only through operator-sanctioned tools (ADR 0003, ADR 0014).
- **Guard models are adopted as defense-in-depth**: command risk classification before execution, secret scanning of tool outputs. They catch accidents and provide audit value. They are documented as *not* a security boundary — model judgment is probabilistic and bypassable by obfuscation; boundaries are structural only (file ownership, secret absence, network policy).
- **Light confinement inside the execution domain**: the file tool is rooted at the workspace with path-traversal validation, separating working data from the tool runtime itself. This is convenience, not a trust boundary.

## Consequences

- "Which trust domain does this tool belong to?" is answered at design time, making placement automatic.
- Agent-facing performance is unaffected: hot-path operations execute locally in-domain; boundary crossing is limited to operations that are already network-bound.
- Guard behavior can be tuned without touching boundary mechanisms, and boundary mechanisms need no model involvement.
