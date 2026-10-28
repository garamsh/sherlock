# Does a file-and-skill loop do general work better than the shipped recall

Records issue #1164's comparison of the shipped loop S against H, a loop in the style of Claude Code and Hermes, on coding, writing, file work and continuation. It supersedes #1153. The [preregistration](preregistration.md) fixes the arms, the model ladder, the corpus, the scorer, the decision rule and the controls. Collection has not started: the pull request stops at the PM gate after the pilot.

## Question and method

Does H — bounded memory files the agent edits itself, skills it loads on demand, and older conversation reached only through a search it makes — do general work better than S, for at most 20% more cost per completed task? The probe is whether the task is done and a correction the person gave on the first instance still holds on later instances, once the correction has left the window.

- **Source revision.** S is `90e403add20b670fae1794f2e3e7b02961581710`. H is `e435edbf3928fca3b8c03ba8effb21501b4ce1e1` on `sauce-git/test/general-agent-arm-h`, which never merges. The preregistration §Arms describes H and where it departs from Claude Code and Hermes.
- **Runtime.** Go 1.26.9 for the harness, Python 3.13.5 for the corpus and the scorer, and `bwrap` for the code tasks' sandbox.
- **Corpus.** [`corpus.py`](corpus.py) writes [`corpus.json`](corpus.json): 92 families, 184 probes and 352 live turns per arm (preregistration §Corpus).
- **Harness.** [`harness/`](harness/) is built inside each arm's tree and runs each live input through that arm's `gateway.Factory`. It uses the shipped manifests of `file_list`, `file_read`, `file_write` and `message_send`, served from memory. Every chat call goes through a proxy that pins one OpenRouter model and provider, keeps the ledger, and caches identical syntheses.
- **Scorer.** [`score.py`](score.py) reads the arms' results and the ledger, and writes `scores.json`.

## Reproduction

From a checkout of this branch:

```sh
python3 research/general-agent/corpus.py                                    # byte-identical corpus.json
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s research/general-agent -p '*_test.py'
go test ./research/general-agent/harness/
python3 research/general-agent/models.py                                    # re-reads the ladder; prices move
```

- **The H tree.** `git fetch origin sauce-git/test/general-agent-arm-h`, then `git worktree add <dir> e435edbf3928fca3b8c03ba8effb21501b4ce1e1` and copy `research/general-agent/harness/` and `corpus.json` into it before `go build` or `go test`.
- **The embedder.** It is the container of the preregistration §Embedder, named `tei-1164`, removed with `docker rm -f tei-1164` when done.
- **A paid run** builds `harness` in each tree, starts `harness proxy` with `OPENROUTER_API_KEY` in its environment only, and runs `harness run -arm S` and `-arm H` against it. `score.py <run dir>` scores the result.

## Status

- Corpus, scorer, harness and preregistration are committed. The model is `minimax/minimax-m2` (preregistration Amendment 1).
- The pilot ran on 2026-10-28 and spent USD 0.4836 by the key's usage, or USD 0.4895 by the ledger. Preregistration Amendment 2 records its cost per episode, the tool-calling check, its controls and a projected collection cost of at most USD 7.8.
- Collection has not started. Under the paid-answer reading of the check, M2 answered 71.3% of syntheses with a usable call, short of the 80% required. The PM decides the reading before collection.

## Limits

- **One model**, the cheapest rung that passes the pilot's tool-calling screen. A small model may fail both arms alike, and the exercise control is there to say so.
- **Templated tasks.** Every check is mechanical, so work done in spirit but not to the letter fails. The scorer's near-miss tests pin what each check accepts.
- **Every live input starts from a restart.** The window is 32 messages, and the filler is recorded conversation unrelated to the tasks.
- **H is one design.** Its bounds, its whole-file write and its storage in `memory.db` are choices the preregistration lists, and a loss by H is a loss by this design.
