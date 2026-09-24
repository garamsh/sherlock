# ADR 0013: Operators grant egress without a dedicated proxy

> Status: accepted
> Date: 2026-08-03

## Context

The earlier tool-only-egress policy prevented ordinary repository CLI work. On 2026-09-24 the owner selected direct `git`/`gh` in the isolated workspace and accepted network and repository-token access by workspace code (#840).

## Decision

- Installed agent-side tools retain their own egress and inherit the agent environment; the model endpoint remains operator-configured.
- Workspace egress is disabled by default. Its operator can grant network access to all executed code through `exec-network`; model arguments cannot enable it. An RPC can request a narrower isolated execution.
- Once enabled, CLI commands choose destinations subject to deployment network policy. External authority comes from actual credential scope and forge protections. Repository/branch task instructions are not an egress boundary.
- No dedicated broker/proxy, GitHub-specific command rewriter, or secret-reference service is introduced. Workspace credentials arrive directly in its environment, and `exec-env` selects which names reach children (ADR 0003).

## Consequences

- Workspace code can reach external services and expose its intentionally granted credentials. The operator accepts and scopes that authority.
- Agent secrets and memory/configuration remain outside the workspace container. Network policy and gateway authentication must account for a network-enabled workspace.
- A domain tool remains useful where its contract adds value; ordinary repository work uses CLI tools rather than a second API publication workflow.
