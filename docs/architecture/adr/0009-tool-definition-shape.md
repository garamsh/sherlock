# ADR 0009: The tool definition shape — a script with a manifest

> Status: accepted
> Date: 2026-08-02 (revised in place 2026-08-13; 2026-08-14)

## Context

Capabilities must join the agent without a platform-owned plugin mechanism: browser automation, a GitHub client, a one-off utility. ADR 0017 settled where tools live: independent units the platform reaches through one binding contract. This record states what a tool is in that shape.

The previous revision made tools independent servers behind a platform protocol (HTTP or stdio JSON-RPC). That shape had two properties the owner rejected: it forced the tool author to write transport code (the protocol itself), and the model-facing JSON Schema was only one of several protocol artefacts the author had to understand. The structural answer is to make the tool author write the *contract* — a JSON Schema — and let the platform own the *transport*. The shape follows opencode's `custom-tools` model: a typed function declaration (zod schema in opencode, JSON Schema in sherlock) plus an executable that implements it.

## Decision

A tool is a **script with a manifest**. The platform specifies the contract shape; the tool author owns everything else.

- **The manifest is the contract.** One JSON file per tool (`manifest.json`) declares: `name`, `description`, `command`, `input_schema` (JSON Schema), `output_schema` (JSON Schema). The directory containing the manifest is the tool's identifier on disk: `tools/<name>/manifest.json` + `tools/<name>/<command>`.
- **The platform fetches the descriptor from the manifest, not from a remote process.** The manifest is local to the operator's tree; the platform reads it at startup. The contract is statically declared, not negotiated.
- **The tool validates its own inputs and conforms its own outputs.** Input validation against `input_schema` and output conformance against `output_schema` are the tool's job; the platform re-validates at the boundary and fails the call when an output does not conform, dropping the output rather than passing it to the model. The tool's descriptor is untrusted input at the boundary.
- **The tool owns its behavior.** External API calls, local compute, workspace calls, error handling — any domain logic — are the tool's choice. Implementation can be any executable: a Go single-file binary, a Python script, a Bash script, anything that produces the contract.
- **The manifest promises nothing about secrets, and has no field that could.** Credentials read from the tool's process environment, which is the agent's: a tool runs inside the agent's environment and inherits it whole, so it can read every credential the agent holds (`security.md` §Tool trust posture). The manifest describes the tool's interface — what it is called, what it accepts, what it returns — and a credential is not part of that description. It names no variable, carries no value, and neither widens nor narrows what the tool can read. **A tool cannot hold a credential the agent does not**: the only mechanisms that would give it one are the platform dispatching a secret, which `security.md` §Rejected alternatives refuses because a materialized secret is a stolen secret (ADR 0003, ADR 0013), and the operator writing a value into the manifest, which would put secret material in a file that is published under free contribution and hashed as the drift pin. A per-tool credential scope is therefore not a thing this contract can express, and an operator who needs one tool to hold a credential needs a second agent.
  - This is settled by removal rather than by wording: the manifest carried an `env` field until #550. It was inert — the runner appended its entries to the child's environment and every manifest filled it with bare names, which are not assignments — and it was removed rather than documented, because a field named after a capability it does not have is the defect #503 was. A manifest still carrying it now fails to parse, which is the strict-fields rule (invariant 1) reporting it rather than an operator discovering it from a tool that reads an empty value.
- **The tool does not implement a transport.** It is a script that exchanges a single JSON request and a single JSON response. The platform owns the lifecycle.

The **call protocol is fixed** — one shape, no exceptions:

- The platform writes the validated input object as one JSON line to the tool's stdin.
- The tool writes the validated output object as one JSON line to stdout (or, on failure, a JSON object containing an `error` field).
- The tool exits with status 0 on success, non-zero on failure.
- The platform reads stderr for the failure case (best-effort, included in the platform's tool-failure event).

The platform's invariants around the tool:

1. The manifest is parseable and its schemas are valid JSON Schema (strict-mode subset). Failure to parse fails startup.
2. The platform hashes every manifest and holds it against the operator's pin set: it refuses to load a tool whose hash drifts from its pin, and refuses one the set does not name. The pin is the operator's record of the manifest they accepted, so the platform reads a pin set and never writes one — a platform-generated pin would vouch for whatever it already loaded, which is the self-registration shape ADR 0017 rejects. An operator who configures no pin set has no drift check, and the platform says so at startup. The tool's self-description is untrusted input at the boundary.
3. The platform validates the input against `input_schema` before invoking the tool. An invalid input never reaches the tool.
4. The platform validates the output against `output_schema` after the tool returns. A non-conforming output is a tool failure: the call fails, the error names the tool, and the output is dropped without reaching the model. The tool stays installed and callable — taking it out of service would be a control the platform cannot recover from, since the required tool is not operator-configurable and the state would not survive a restart.
5. One tool is required — the mouth, `message_send` — and it is validated at startup; a missing required tool fails closed. The set is the platform's and there is no operator key that adds to it: the workspace-coupling tools are capabilities the operator installs or does not (ADR 0017), so their absence is a choice rather than a failure.

## Consequences

- Adding a capability is a directory drop: `tools/<name>/` with a `manifest.json` and an executable. No compilation, no language lock-in, no transport code. An operator who pins accepts the new tool by adding its hash to the pin set, which is the same act as reviewing it; the startup refusal names the hash to add.
- The structural minimum is one manifest format plus a stdio runner (JSON in, JSON out, exit code). No protocol framing, no registration ceremony, no capability negotiation.
- The shape replaces both the previous in-process tools-as-packages model and the protocol-server model; both histories live in git.
- The platform's `internal/tool/` is the manifest parser, the stdio runner, the registry, and the hash pinner — a few hundred lines, no third-party dependency.
- The first-party tool set (file_read, file_write, file_list, shell_exec, message_send, github) ships as scripts under `tools/` with one manifest each; the language is the tool author's choice. The historical Go-binary form (`cmd/<tool>/`) is gone.
- For the operator, granting new egress is dropping a script directory in the tool tree. For the tool author, building a tool is writing a function in their preferred language plus a small JSON contract.
- Permissions policy is not part of the platform. The tool runs with the operator's process privileges; the operator is the gate, by directory placement.