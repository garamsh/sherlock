# Harness — AI Agent Project Template

A GitHub template repository for projects developed by AI agents (opencode, Claude Code). Multiple agents work in parallel on separate branches under two roles: a PM agent on `main` that reviews, merges, and supervises — and worker agents that implement tasks following written conventions.

## Usage

1. Create a new repository with **Use this template**.
2. Start a PM agent on `main`. It executes `docs/ai/BOOTSTRAP.md`: selects stack conventions, sets up CI plumbing, writes this README as a project introduction, and opens the bootstrap PR.

This file is replaced during bootstrap; do not edit it to describe your project.

## Structure

- `AGENTS.md` — role matrix and rules every agent follows
- `docs/ai/` — agent-only procedures (bootstrap, PM playbook, worker guide, documentation rules)
- `docs/convention/` — code conventions; stack files are pruned during bootstrap
- `docs/architecture/` — structure overview and ADRs
- `.opencode/agents/`, `.claude/agents/` — tool-specific agent adapters
- `.github/` — PR and issue templates, CODEOWNERS

## GitHub settings not covered by the template

Templates do not carry repository settings. After creation, configure manually:

- **Settings → General → Template repository**: leave unchecked for the new project (that checkbox is for this template repo itself).
- **Branch protection on `main`**: require pull requests, require review before merge, block direct pushes — this mechanically enforces the PM role.
