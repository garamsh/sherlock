# ADR 0024: Operator instructions are a separate optional file, placed between the ego and the platform contract

> Status: accepted
> Date: 2026-10-05

## Context

The ego file (`--ego-file`) was the operator's only input to the session instructions. A deployment that needs to tell the agent something about where it runs — how to read and answer a transport's message envelope, for one — had to put that text in the ego, and a non-empty ego replaces `DefaultEgo`. So giving deployment instructions cost the operator the built-in persona, unless they copied it into their own file and kept it current by hand (#949, requested by garamsh/garam-agent-operator#239).

The two texts change for different reasons: the ego says who the agent is, and deployment instructions say what the environment around it expects. One file cannot hold the second without taking on the first.

Alternatives considered:

- **Append deployment text to the ego file** — what the operator does today; it is the displacement this record removes.
- **Append the text after the platform contract** — the contract would no longer be the last of the fixed parts, and an operator's text could read as amending it.
- **A config-file key holding the text inline** — a second delivery mechanism beside the ego's flag-and-file one, for text the platform does not interpret.

## Decision

An optional operator-instructions file, named by `--instructions-file`, is read the way the ego file is: a flag naming a path, read once at startup, with no config-file key or environment form, and a named file that is missing or unreadable failing startup. The session instructions are the ego (or `DefaultEgo` when it is empty), then the operator instructions (omitted when blank), then the platform contract, then the saved note. Sherlock stores and interprets nothing in the file.

## Consequences

- An operator gives deployment instructions without replacing the persona, and the built-in ego keeps evolving with the binary for deployments that do not override it.
- A deployment that names no instructions file gets the instructions it had before this record, byte for byte.
- The platform contract stays the last fixed part, so neither operator file can override it; it is still prose to the model and not an enforced boundary.
- A wrong instructions file is caught at startup and not at the first turn.
- Content and wording are the operator's: Sherlock offers no instruction store, validation, or hot reload, and a change applies on restart.

## Signs the decision was wrong

- Operators put persona text in the instructions file, or deployment text back in the ego, because the split does not match how they divide the two.
- A second kind of operator text arrives and wants its own file, which would mean the parts are being cut by source rather than by purpose and want one ordered list instead.
- Models read operator instructions placed before the contract as overriding it, and the contract's rules lose to them in practice.
