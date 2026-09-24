# ADR 0005: Tools own security; guard models are layers, not boundaries

> Status: accepted
> Date: 2026-07-25

## Context

Security enforcement needs a home consistent with the component model. Two candidate philosophies: enforcement concentrated in a separate security layer, or enforcement distributed into the tools themselves. A related question is whether model-based inspection (a guard model classifying commands, scanning outputs) can serve as a security boundary.

## Decision

Each tool owns and enforces the security policy of its own domain. No separate security layer exists.

- **Trust-domain invariant**: a tool that executes agent-influenced code runs with no reachable secrets; a tool that holds secrets never executes agent-influenced code and accepts only schema-validated structured input. In the pod topology (ADR 0017) the two domains land on the two containers: the secret-free workspace data plane in the sidecar, secrets and the tools that hold them in the agent container.
- **External access is tool-routed by principle.** Services are reached through tool calls; raw network egress from executed code exists only through operator-sanctioned tools (ADR 0003, ADR 0013).
- **Guard models are adopted as defense-in-depth**: command risk classification before execution, secret scanning of tool outputs. They catch accidents and provide audit value. They are documented as *not* a security boundary — model judgment is probabilistic and bypassable by obfuscation; boundaries are structural only (file ownership, secret absence, network policy).
- **Light confinement inside the execution domain**: the file tool is rooted at the workspace with path-traversal validation, separating working data from the tool runtime itself. This is convenience, not a trust boundary.

The GitHub repository workflow is a concrete instance of this policy. The operator supplies an exact repository, a new branch and a validation command in the trusted environment; the tool enforces those choices independently of the model. The tool imports selected files through the workspace contract and validates there with untrusted network-isolated Exec before publishing their captured contents through GitHub's Git database API. Raw ref publication and standalone PR creation are not exposed alongside that path, because either would bypass the validation gate. A Git clone/push inside Exec would require network and credentials in the untrusted domain; executing repository hooks or checks inside the trusted tool would invert the other boundary, so neither is adopted.

## Consequences

- "Which trust domain does this tool belong to?" is answered at design time, making placement automatic.
- Agent-facing performance is unaffected: hot-path operations execute locally in-domain; boundary crossing is limited to operations that are already network-bound.
- Guard behavior can be tuned without touching boundary mechanisms, and boundary mechanisms need no model involvement.
