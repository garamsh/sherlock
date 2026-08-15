# Continuous Integration

How this project's checks are named, run, and secured. The rules hold on any toolchain; the toolchain they are met with appears at the end.

## Contents
- Core principle
- One entry point per task
- Run the checks before pushing
- Reuse before writing pipeline steps
- Verify a pinned dependency before adopting it
- Security baseline
- Anti-patterns
- This project's toolchain

## Core principle

Local checks and CI checks are the same checks, invoked by the same names. CI re-runs what a contributor already ran; it does not define a second way to run it.

## One entry point per task

- **One name per task.** Each of lint, format, test, and build has exactly one name that a human and CI both invoke. What implements that name — a task runner, a script, a package manifest entry — is the project's choice; the name is the contract.
- **One name for the whole set.** A single name runs lint, format, test, and build in order, so "run the checks" is one command locally and one step in CI.
- **CI calls the names, not the commands behind them.** A pipeline that spells out the underlying tool invocations has created a second set of commands, and the two drift.
- **Entry points stay thin.** When a step outgrows a one-liner, the answer is a maintained action (§Reuse), not a script. A script is the last resort for what no action does; a step that grew into a script is a signal the entry point needs re-splitting, not that the script needs adding.

## Run the checks before pushing

- Run the project's checks locally before code leaves the machine. CI is the second line of defence, not the only one.
- Automating that locally — a version-control hook, a watch task, an on-save editor command — is the contributor's choice. No tool is mandated, and running the checks by hand satisfies the rule.

## Reuse before writing pipeline steps

For each step in the pipeline:

1. **Search for a maintained component** — a published action, plugin, or reusable job — at its current version, before writing pipeline code.
2. **Verify it** per the next section before pinning.
3. **Use it as its own documentation shows**. If no maintained component covers the step, a checked-in script is the fallback — not a license to hand-roll per-job.

## Verify a pinned dependency before adopting it

Before pinning anything the pipeline pulls in — a published component, container image, or tool version — read its own documentation and determine:

1. **Current stable version** — not the remembered one. The version an agent recalls may be deprecated, archived, or replaced.
2. **Exact inputs that version accepts** — inputs shift between releases. Check them against the version's own documentation.
3. **Exact usage pattern** — follow what the documentation shows. Do not paraphrase inputs or rearrange the documented pattern.

Pre-trained recollection is a starting point, not source of truth. An agent that picks a version from memory silently uses a stale or nonexistent release.

For every third-party dependency, also check the ecosystem's advisory source for known vulnerabilities in the candidate version.

## Security baseline

- **Minimal permissions**: the pipeline's default credential grants read only; grant write on the single job that needs it.
- **No plain-text secrets**: keep tokens, keys, and credentials in the platform's secret store. Prefer short-lived federated credentials (OIDC) over long-lived keys for cloud access.
- **Avoid script injection**: pass attacker-controlled input to a command through the environment, never interpolated into the command text.
- **Mask derived values**: values derived from secrets that can reach the logs are masked with the platform's masking mechanism.

## Anti-patterns

- Hand-written steps for what a maintained component already does.
- Pipeline commands that diverge from the project's entry points — local and CI must match.
- A self-rolled CI runner that uses different commands than local.
- Entry points carrying long shell logic — either reduce to a maintained action (§Reuse) or re-split the entry point into multiple named tasks.
- Pinning to a mutable reference (a branch, `latest`). Pin a tag or a digest.
- Reusing a pipeline component without auditing its contents — treat it like any other dependency.

## This project's toolchain

The rules above hold on any toolchain. This project meets them with Make for the entry points and GitHub Actions for the pipeline.

- **Entry points**: `make lint`, `make format`, `make test`, `make build`, and `make ci` for the whole set. The workflow invokes the targets, never the commands inside them.
- **Before pushing**: `make ci` by hand, or `make hooks` once after cloning to have lefthook run lint and format on commit and test on push.
- **Existing components**: search the GitHub Marketplace before writing an inline `run:` block.
- **Advisories**: for every third-party action, check the GitHub Advisory Database (`github.com/advisories?query=type%3Areviewed+ecosystem%3Aactions`) for the candidate version.
- **Toolchain setup**: use the official setup action for the language; take its version and inputs from its own docs.
- **Artifacts**: use the artifact action.
- **Permissions**: `permissions: read-all` at the workflow top; `write` granted per job.
- **Secrets**: repo or environment secrets, OIDC for cloud deploys, `::add-mask::VALUE` for values derived from secrets.
- **Pinning**: a SHA or a tag, never `@main`.
