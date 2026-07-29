# Review Conventions

What makes a PR review valid, and how authors respond. Applies to every review and every response; violations are grounds for rejection.

## Submitting a review

- **Reviews are formal.** Decisions are submitted as approve / request changes / comment via the review mechanism — never as bare comments. Review states gate merges; comments do not — except in single-account setups, below.
- **Every review body carries the evidence table**, approval included:

```
| Check | Result | Evidence |
|---|---|---|
| Scope | pass / fail / unverified | — |
| Commits and branch | … | … |
| Conventions | … | rule file §section — diff file:line |
| Path ownership | … | … |
| Architecture | … | … |
| Documentation | … | … |
| Verification | … | … |
| Depth | … | … |

Decision: approve / request changes / reject
```

- A check you did not verify is marked `unverified` — never guessed.

## Citing violations

- A violation claim cites both sides: `rule file §section` and `diff file:line`.
- If you cannot cite a rule, it is a preference. Preferences never block a merge.

## Blockers and nits

- Tag every requested change **blocker** or **nit**.
- A nit never blocks a merge. A blocker always states the expected fix direction — "wrong" without "do this instead" is noise.

## Single-account setups

When the PM and authors share one GitHub account, GitHub rejects both `approve` and `request changes` on one's own PR. Only `comment` reviews are accepted, so every review is submitted as a `comment` review carrying the evidence table, with the decision stated in the body. In that setup:

- **State the decision explicitly.** End the evidence table with `Decision: approve` / `request changes` / `reject`, as in any other review.
- **The merge is the approval.** For an approval, post the review and merge immediately.
- **A comment review still gates the merge** when its decision is `request changes`. The state cannot gate it, so the PM does: do not merge until every blocker is addressed or rebutted.

## Responding to a review (author)

- Address every blocker, or rebut it by citing a rule or path that supports your approach. Silent ignoring is not allowed.
- Answer nits too: fix, or state briefly why not.
- Push fixes as new commits to the same branch; never open a replacement PR.
