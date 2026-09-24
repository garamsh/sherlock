# ADR 0003: Keep agent secrets outside operator-provisioned workspaces

> Status: accepted
> Date: 2026-07-25

## Context

Repository work through ordinary Git and GitHub CLI needs both a native checkout and credentials/network in the execution environment. On 2026-09-24 the owner explicitly accepted that workspace code can read and use the repository credential (#840), replacing the earlier secret-free-workspace requirement. This does not authorize exposing the agent's model keys, memory or configuration.

## Decision

- Preserve the separate agent/workspace containers and storage. Provision repository credentials directly to the isolated workspace, with operator-selected environment names (`exec-env`) and network access (`exec-network`). Never inherit the host or agent environment wholesale.
- All code in the workspace can read those credentials. Their external authority is the credential's real scope and forge policy, not a command filter, prompt, repository-name setting or secret substitution service.
- Agent model keys and memory/configuration volumes remain absent from the workspace. Installing a tool in the agent still trusts it with that agent's environment; this is separate from workspace grants.
- Credentials must not be printed, written into commits, or included in model prompts. This is an operational obligation for granted workspace credentials, not an exfiltration-proof structural claim.

## Consequences

- Ordinary `git` and `gh` commands can inspect, test and publish native workspace repositories; no API substitute or command rewriter is needed.
- Prompt injection or malicious repository code may steal a granted token. The owner accepted this risk; operators must limit its scope and isolate workspace files/processes from host and agent state.
- Default execution remains network-isolated with no inherited credentials until the operator grants them. UID, timeout and resource limits are unchanged.
