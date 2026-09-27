"""Remove each rule #906's screen fixes, run the tests, restore it, and
record that the tests failed (evidence/mutation.txt).

    python3 research/memory-encoding/profiles/mutation.py
"""

import os
import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
TEST = ["python3", "-m", "unittest", "screen_test"]

# (text in screen.py, replacement, what the mutation removes)
MUTATIONS = [
    ("        return min(found)[2]\n", "        return max(found)[2]\n", "the owner named earliest"),
    ("            found.append((p, -len(k), k))\n", "            found.append((p, len(k), k))\n", "the longer name winning a tie"),
    ("        if len(k) >= MIN_NAME and k not in PARTICIPANTS and k not in out:\n", "        if len(k) >= MIN_NAME and k not in out:\n", "excluding participants"),
    ('    return name[4:] if name.startswith("the ") else name\n', "    return name\n", "dropping a leading 'the'"),
    ("    u = USER_WORDS.search(fact[\"text\"])\n", "    u = None\n", "the user as a named candidate"),
    ('    return USER if fact["kind"] == "user" else None\n', "    return None\n", "the user fallback for an unnamed user fact"),
    ('    return USER if fact["kind"] == "user" else None\n', "    return USER\n", "dropping an unowned other fact"),
    ('        text = re.sub(r"(?<![A-Za-z0-9])" + re.escape(k)', '        text = text if True else re.sub(r"(?<![A-Za-z0-9])" + re.escape(k)', "removing entity names from the key"),
    ('    text = USER_WORDS.sub(" ", text)\n', "", "removing user words from the key"),
    ('        if re.search(r"\\d", w) or (i > 0 and w[0].isupper()):\n', '        if re.search(r"\\d", w):\n', "removing capitalized values from the key"),
    ("        if w not in STOP and w not in BOOKKEEPING:\n", "        if w not in STOP:\n", "removing bookkeeping words from the key"),
    ("    return bool(a) and bool(b) and len(a & b) / min(len(a), len(b)) >= OVERLAP\n",
     "    return bool(a) and bool(b) and len(a & b) / max(len(a), len(b)) >= OVERLAP\n", "the overlap over the smaller key"),
    ("    return bool(a) and bool(b) and len(a & b)", "    return len(a | b) >= 0 and len(a & b) >= 0 and (len(a & b)", "an empty key never overlapping"),
    ("    for f in reversed(facts):\n", "    for f in facts:\n", "keeping the newest facts"),
    ("            if overwrite:\n", "            if False:\n", "the overwrite"),
    ("            if k in cards and k not in owners:\n", "            if k not in owners:\n", "attaching only owners with a card"),
    ("    for it in items:\n        for k in entity_names(", "    for it in [{\"first\": a, \"last\": b} for a, b in entities]:\n        for k in entity_names(", "attaching through the delivered mids only"),
    ("    if items and USER in cards and USER not in owners:\n", "    if USER in cards and USER not in owners:\n", "the user's card only on a delivery"),
    ("    if items and USER in cards and USER not in owners:\n", "    if False:\n", "the user's card"),
    ('        row["reach"] = any(states(t, probe["answer"]) for t in texts)\n', '        row["reach"] = any(states(it["text"], probe["answer"]) for it in items)\n', "reach from the attached cards"),
    ('        row["stale"] = any(states(t, probe["obsolete"]) and not states(t, probe["key"]) for t in facts)\n',
     '        row["stale"] = row["superseded"]\n', "the stale fact not also stating the current value"),
    ('        row["confusion"] = norm(probe["entity"]) in owners and row["owner_card_states"]\n',
     '        row["confusion"] = row["near_miss_attached"] if "near_miss_attached" in row else any(states(t, probe["near_miss"]) for t in texts)\n',
     "confusion only on the queried entity's own card"),
    ('        row["confusion"] = norm(probe["entity"]) in owners and row["owner_card_states"]\n',
     '        row["confusion"] = row["owner_card_states"]\n', "confusion only when the card is attached"),
    ('    if any(old["text"] in first and', '    if any(old["text"] in corr and', "the KU control reading the first statement"),
    ("    admitted = chain_reach >= CHAIN_MARGIN or up_reach >= UP_MARGIN\n", "    admitted = chain_reach > CHAIN_MARGIN or up_reach >= UP_MARGIN\n", "the chain margin"),
    ("    admitted = chain_reach >= CHAIN_MARGIN or up_reach >= UP_MARGIN\n", "    admitted = chain_reach >= CHAIN_MARGIN or up_reach > UP_MARGIN\n", "the UP margin"),
    ("    admitted = chain_reach >= CHAIN_MARGIN or up_reach >= UP_MARGIN\n", "    admitted = chain_reach >= CHAIN_MARGIN and up_reach >= UP_MARGIN\n", "either family admitting"),
    ('"paid_phase_may_be_proposed": admitted and audit_passed}', '"paid_phase_may_be_proposed": admitted}', "the audit voiding a pass"),
    ('"passed": len(rows) == AUDIT_N and correct >= AUDIT_PASS}', '"passed": correct >= AUDIT_PASS - 1}', "the audit's pass mark"),
]


def run():
    env = dict(os.environ, PYTHONDONTWRITEBYTECODE="1")
    return subprocess.run(TEST, cwd=HERE, capture_output=True, text=True, env=env).returncode


def main():
    if run() != 0:
        raise SystemExit("the unmutated tests do not pass")
    file = HERE / "screen.py"
    original = file.read_text()
    lines = []
    try:
        for old, new, what in MUTATIONS:
            if original.count(old) != 1:
                raise SystemExit(f"screen.py: the text to mutate for {what!r} occurs {original.count(old)} times")
            file.write_text(original.replace(old, new))
            lines.append(("FAILS  " if run() != 0 else "PASSES ") + "screen.py: " + what)
    finally:
        file.write_text(original)
    lines.append("control: every unmutated test passes" if run() == 0 else "control: the unmutated tests FAIL")
    (HERE / "evidence").mkdir(exist_ok=True)
    (HERE / "evidence" / "mutation.txt").write_text("\n".join(lines) + "\n")
    print("\n".join(lines))
    if any(line.startswith("PASSES") for line in lines):
        raise SystemExit("a mutation survived")


if __name__ == "__main__":
    main()
