"""The exploratory re-read (preregistration.md §Exploratory re-read).

    python3 research/memory-encoding/attributed/reread.py   # evidence/reread.json

Applies the matcher to every N and NC reply in the live records of #885,
#888 and #896 (audit.py EARLIER) and counts, per arm record, the replies
acceptable under the strict rule, the owner's rule (reading A) and reading
B, with those attributed and asserted. It informs and decides nothing.
"""

import json
from collections import defaultdict
from pathlib import Path

import audit
import matcher

HERE = Path(__file__).resolve().parent
READINGS = ("strict", "owner", "reading_b", "attributed", "asserted")


def reread(rows):
    out = defaultdict(lambda: defaultdict(lambda: {"n": 0, **{k: 0 for k in READINGS}}))
    for r in rows:
        study, arm, setting, _ = r["key"].split("/")
        e = out[f"{study}/{arm}/{setting}"][r["guard"]["family"]]
        e["n"] += 1
        j = matcher.judge(r["reply"], r["guard"])
        for k in READINGS:
            e[k] += j[k]
    return {k: dict(v) for k, v in sorted(out.items())}


def main():
    out = {"exploratory": True, "records": reread(audit.absent_replies(audit.EARLIER))}
    (HERE / "evidence" / "reread.json").write_text(json.dumps(out, indent=1) + "\n")
    for record, fams in out["records"].items():
        print(record, {f: {k: v for k, v in e.items()} for f, e in fams.items()})


if __name__ == "__main__":
    main()
