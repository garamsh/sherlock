"""Show that #888's code leaves the settings #885 measured unchanged: S at
b4, L at b4-model and L+A at b4-model-a, re-built and re-recorded offline
over #885's rebuilt stores into a scratch study directory, equal the
committed ../confirm/evidence/offline/ records in every field but timing
(evidence/equivalence.txt). Nothing is written under ../confirm/.

The scratch study directory is <scratch>/research/chained-recall/confirm,
holding copies of ../confirm/corpus.json and
../confirm/arms/links/evidence/entities-b4.jsonl, with
<scratch>/research/mid-memory-rag linked to the repository's, so the runner
finds the replication. Its shared store is harness build over ../confirm.

    go run ./docs/research/chained-recall/arms/shipped build|offline -setting b4 -dir <scratch dir> -work <work> -embed-url <TEI>
    go run ./docs/research/chained-recall/arms/links build|offline -setting b4-model|b4-model-a (likewise)
    python3 docs/research/chained-recall/readtime/equivalence.py <commit> <scratch dir>
"""

import json
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
OFFLINE = "docs/research/chained-recall/confirm/evidence/offline"
SETTINGS = ("S/b4", "L/b4-model", "L/b4-model-a")
FILES = ("built.json", "probes.jsonl", "oracle.jsonl")
TIMING = ("seconds", "build_seconds")


def records(text, name):
    rows = [json.loads(text)] if name.endswith(".json") else [json.loads(line) for line in text.splitlines()]
    for r in rows:
        for key in TIMING:
            r.pop(key, None)
    return rows


def main():
    commit, rerun = sys.argv[1], Path(sys.argv[2])
    root = HERE.parents[3]
    lines = [f"committed records at {commit}, against a scratch re-run with this code"]
    for setting in SETTINGS:
        for name in FILES:
            path = f"{OFFLINE}/{setting}/{name}"
            old = subprocess.run(["git", "show", f"{commit}:{path}"], cwd=root, capture_output=True, text=True, check=True).stdout
            new = (rerun / "evidence" / "offline" / setting / name).read_text()
            if old == new:
                raise SystemExit(f"{path} was not re-recorded: it is byte-identical, timing included")
            a, b = records(old, name), records(new, name)
            differ = sum(x != y for x, y in zip(a, b)) + abs(len(a) - len(b))
            lines.append(f"{'EQUAL' if differ == 0 else 'DIFFERS'}  {setting}/{name}: {len(a)} committed, {len(b)} re-recorded, {differ} differing apart from timing")
    (HERE / "evidence").mkdir(exist_ok=True)
    (HERE / "evidence" / "equivalence.txt").write_text("\n".join(lines) + "\n")
    print("\n".join(lines))


if __name__ == "__main__":
    main()
