# Continuous Integration

## Contents
- Core principle
- Local-first: Makefile + lefthook
- Verify an action before adopting
- Workflow authoring
- Standard recipes
- Security baseline
- Anti-patterns

## Core principle

Local commands and CI commands must be the same commands. Prefer existing actions over inline `run:` blocks when one exists. **Never pick an action version from memory or training data** — verify the current stable version and exact usage via `webfetch` first.

## Local-first: Makefile + lefthook

Make CI commands work locally, and run them locally before pushing.

### Makefile — essentials only

Expose the commands CI will run. Keep each target minimal:

```makefile
.PHONY: lint format test build ci

lint:
	<lint-cmd>

format:
	<format-cmd>

test:
	<test-cmd>

build:
	<build-cmd>

ci: lint format test build
```

Replace `<lint-cmd>`, etc. with the project's actual commands (`npm run lint`, `ruff check`, `go test ./...` — pick whatever the project uses). If a target grows beyond ~3 lines, extract the logic to `scripts/<name>.sh` and have the target call it.

Pattern: local `make lint` and CI `make lint` invoke the same command. No drift between local and CI.

### lefthook — pre-commit / pre-push hooks

Use [lefthook](https://github.com/evilmartians/lefthook) to gate commits and pushes locally.

```yaml
# lefthook.yml
pre-commit:
  commands:
    lint:
      run: make lint
    format:
      run: make format
      stage_fixed: true

pre-push:
  commands:
    test:
      run: make test
```

Hooks give fast feedback before code leaves the machine. CI becomes a second line of defense, not the only one.

## Workflow authoring

For each CI step:

1. **Check for an existing action** on the GitHub Marketplace before writing inline `run:` blocks.
2. **Verify it** per "Verify an action before adopting" below — version, inputs, usage.
3. **Use it as documented**, or write inline `run:` if no action exists.

## Verify an action before adopting

Before pinning any action, `webfetch` its repo to determine:

1. **Current stable version** — don't pick from memory or training data. The version the agent remembers may be deprecated, archived, or replaced.
2. **Exact inputs the version accepts** — inputs shift between releases. Verify against the version's own `action.yml` or README.
3. **Exact usage pattern** — follow what the action's docs show. Don't paraphrase inputs or rearrange the documented pattern.

Pre-trained recollection is a starting point, not source of truth. An agent that picks a version from memory silently uses a stale or nonexistent release.

For non-trivial third-party actions, also check the GitHub Advisory Database (`github.com/advisories?query=type%3Areviewed+ecosystem%3Aactions`) for known vulnerabilities in the candidate version.

## Standard recipes

- **Setup a language toolchain.** Use the official setup action for the project's language. Inputs and version come from the action's docs — verify before pinning.
- **Build, test, lint, format.** Run the project's own commands via `make` targets. No parallel sets of commands.
- **Upload artifacts.** Use the artifact action.
- **Deploy.** Use OIDC for cloud deploys — no long-lived keys.

## Security baseline

- **Minimal permissions**: default `permissions: read-all` at the workflow top; grant `write` per-job as needed.
- **No plain-text secrets**: store tokens, keys, and credentials in repo or environment secrets. Use OIDC for cloud deploys.
- **Avoid script injection**: pass attacker-controlled input via `env:`, not as a literal interpolated into `run:`.
- **Mask derived values**: `::add-mask::VALUE` for values derived from secrets that may appear in logs.

## Anti-patterns

- Inline `run:` blocks for what an action already does.
- CI commands that diverge from `make` targets — local and CI must match.
- A self-rolled CI runner that uses different commands than local.
- `make` targets containing long shell logic — extract to `scripts/`.
- Pinning to mutable branches (`@main`). Use a SHA or tag.
- Reusing a workflow without auditing its contents — treat it like any other dependency.