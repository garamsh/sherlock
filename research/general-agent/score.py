"""Score the #1164 runs: per-kind results, controls, guardrails, cost and the verdict.

    python3 research/general-agent/score.py <run dir> [corpus file]

A run directory holds one results.jsonl per arm under S/ and H/, each line
one family's episode, and the proxy's ledger under ledger/calls.jsonl. The
rule the verdict applies is the preregistration's.
"""

import ast
import json
import math
import posixpath
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
ARMS = ["S", "H"]
KINDS = ["code", "writing", "tools", "continuation"]
ALPHA = 0.05

# The guardrail margins, in probes: H may hold at most this many fewer
# control or reversal probes than S.
GUARD_MARGIN = {"control": 3, "reversal": 2}

# H's cost per completed task may exceed S's by at most this share.
COST_MARGIN = 0.20

# Seconds one module's cases may run before the run counts as failed.
CASE_TIMEOUT = 10


# ---- what a turn produced -------------------------------------------------

def clean(path):
    """A workspace path as the harness stores it: relative, cleaned."""
    return posixpath.normpath("/" + (path or "")).lstrip("/")


def calls(turn, name=None):
    """The tool calls a turn made that succeeded, in order, with their input."""
    out = []
    for t in turn.get("tools") or []:
        if t.get("failed"):
            continue
        if name is not None and t.get("name") != name:
            continue
        data = t.get("input")
        if isinstance(data, str):
            try:
                data = json.loads(data)
            except ValueError:
                data = {}
        out.append((t.get("name", ""), data if isinstance(data, dict) else {}))
    return out


def writes(turn):
    """The files a turn wrote, in order, as (path, content)."""
    return [(clean(d.get("path")), d.get("content") or "") for _, d in calls(turn, "file_write")
            if d.get("path")]


def written(turn, basename):
    """The last file the turn wrote with the given base name, as (path,
    content), or None."""
    found = None
    for path, content in writes(turn):
        if posixpath.basename(path) == basename:
            found = (path, content)
    return found


def said(turn):
    """What the person reads: the messages the agent sent, or its reply when
    it sent none."""
    sent = [s for s in turn.get("sent") or [] if s]
    if sent:
        return "\n".join(sent)
    return turn.get("reply") or ""


def normalize(text):
    return text.replace("—", "-").replace("–", "-").replace("’", "'").lower()


BULLET = re.compile(r"^\s*[-*•]\s+\S")


def bullets(text):
    return [ln.strip() for ln in text.splitlines() if BULLET.match(ln)]


def bare(line):
    """A line without the emphasis markers Markdown puts around it."""
    return line.strip().strip("*_").strip()


# ---- the code tasks -------------------------------------------------------

RUNNER = r'''
import importlib.util, json, sys
spec = importlib.util.spec_from_file_location("module_under_test", sys.argv[1])
results = []
try:
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    function = getattr(module, sys.argv[2])
except BaseException as exc:
    print(json.dumps({"error": type(exc).__name__}))
    sys.exit(0)
for args, expected in json.loads(sys.stdin.read()):
    try:
        got = function(*args)
    except BaseException as exc:
        results.append({"raised": type(exc).__name__,
                        "mro": [c.__name__ for c in type(exc).__mro__]})
        continue
    try:
        results.append({"value": json.loads(json.dumps(got))})
    except (TypeError, ValueError):
        results.append({"value": repr(got)})
print(json.dumps({"results": results}))
'''


def sandbox_argv(workdir):
    """The command that runs Python inside a sandbox: read-only system, no
    network, no home directory, and only workdir writable."""
    bwrap = shutil.which("bwrap")
    if bwrap is None:
        raise RuntimeError("bwrap is required to run the code tasks' cases")
    return [bwrap, "--ro-bind", "/", "/", "--tmpfs", "/tmp", "--tmpfs", "/home",
            "--bind", str(workdir), "/tmp/w", "--chdir", "/tmp/w", "--unshare-all",
            "--die-with-parent", "--new-session", "--dev", "/dev", "--proc", "/proc",
            sys.executable, "-I"]


def same(got, expected):
    if isinstance(expected, float) or isinstance(got, float):
        return isinstance(got, (int, float)) and not isinstance(got, bool) and \
            isinstance(expected, (int, float)) and math.isclose(got, expected, rel_tol=1e-9)
    return got == expected


def run_cases(source, function, cases):
    """Whether the module source passes every case, run in the sandbox."""
    with tempfile.TemporaryDirectory() as tmp:
        work = Path(tmp)
        (work / "module_under_test.py").write_text(source)
        (work / "runner.py").write_text(RUNNER)
        argv = sandbox_argv(work) + ["runner.py", "module_under_test.py", function]
        try:
            proc = subprocess.run(argv, input=json.dumps([[c[0], c[1]] for c in cases]),
                                  capture_output=True, text=True, timeout=CASE_TIMEOUT)
        except subprocess.TimeoutExpired:
            return False
    try:
        out = json.loads(proc.stdout.strip().splitlines()[-1])
    except (ValueError, IndexError):
        return False
    if "results" not in out or len(out["results"]) != len(cases):
        return False
    for (_, expected), result in zip(cases, out["results"]):
        if isinstance(expected, dict) and "raises" in expected:
            if expected["raises"] not in result.get("mro", []):
                return False
        elif "value" not in result or not same(result["value"], expected):
            return False
    return True


def module_file(turn, task):
    return written(turn, task["module"] + ".py")


def functions(source):
    try:
        tree = ast.parse(source)
    except SyntaxError:
        return None
    return [n for n in ast.walk(tree) if isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef))]


def annotated(fn):
    args = fn.args
    params = args.posonlyargs + args.args + args.kwonlyargs
    params += [a for a in (args.vararg, args.kwarg) if a is not None]
    for i, a in enumerate(params):
        if i == 0 and a.arg in ("self", "cls"):
            continue
        if a.annotation is None:
            return False
    return fn.returns is not None


# ---- the continuation tasks -----------------------------------------------

def heading_index(lines, name):
    pattern = re.compile(r"^#{1,4}\s.*\b" + re.escape(name) + r"\b")
    for i, ln in enumerate(lines):
        if pattern.match(ln.strip()):
            return i
    return -1


def has_part(content, name):
    return heading_index(content.splitlines(), name) >= 0


def expected_part(task, before):
    """The index of the part this turn should write: the one the task names,
    or else the first part of the plan the guide did not hold before."""
    if "part" in task:
        return task["part"]
    for i, name in enumerate(task["parts"]):
        if not has_part(before, name):
            return i
    return len(task["parts"])


def section(content, name):
    """The heading line of a part and the lines under it, up to the next
    heading; None when the part has no heading."""
    lines = content.splitlines()
    start = heading_index(lines, name)
    if start < 0:
        return None
    end = len(lines)
    for j in range(start + 1, len(lines)):
        if re.match(r"^#{1,4}\s", lines[j].strip()):
            end = j
            break
    return lines[start], [ln for ln in lines[start + 1:end] if ln.strip()]


def guide(turn, task):
    return written(turn, posixpath.basename(task["path"]))


# ---- checks ---------------------------------------------------------------

def check(spec, turn, task, before):
    """Whether one check holds for one recorded turn. task is the step's
    task, which names the module, output file or guide the rule reads;
    before maps each workspace path to its content before the turn."""
    kind = spec["kind"]
    if kind == "all_of":
        return all(check(s, turn, task, before) for s in spec["of"])
    if kind == "any_of":
        return any(check(s, turn, task, before) for s in spec["of"])
    if kind == "not":
        return not check(spec["of"], turn, task, before)

    # What the person reads.
    if kind == "prefix":
        return normalize(said(turn)).lstrip().startswith(normalize(spec["value"]))
    if kind == "suffix":
        return normalize(said(turn)).rstrip().endswith(normalize(spec["value"]))
    if kind in ("all_lowercase", "all_uppercase"):
        letters = [c for c in said(turn) if c.isalpha()]
        test = str.islower if kind == "all_lowercase" else str.isupper
        return bool(letters) and all(test(c) for c in letters)
    if kind == "bullet_count":
        return len(bullets(said(turn))) == spec["value"]
    if kind == "bullet_ends":
        found = bullets(said(turn))
        return bool(found) and all(b.rstrip().endswith(".") == spec["period"] for b in found)
    if kind == "mentions":
        return spec["value"].lower() in said(turn).lower()

    # Code.
    if kind == "module_tests":
        found = module_file(turn, task)
        return found is not None and run_cases(found[1], task["function"], task["cases"])
    if kind == "first_line":
        found = module_file(turn, task)
        return found is not None and found[1].lstrip().splitlines()[:1] == [spec["value"]]
    if kind == "module_dir":
        found = module_file(turn, task)
        return found is not None and found[0].startswith(spec["dir"] + "/")
    if kind == "test_file":
        found = written(turn, "test_" + task["module"] + ".py")
        return found is not None and re.search(r"(?m)^\s*def test_", found[1]) is not None
    if kind == "test_file_any":
        return any(re.fullmatch(r"test_\w*\.py", posixpath.basename(p)) for p, _ in writes(turn))
    if kind in ("type_hints", "docstrings"):
        found = module_file(turn, task)
        fns = functions(found[1]) if found else None
        if not fns:
            return False
        if kind == "type_hints":
            return all(annotated(fn) for fn in fns)
        return all(ast.get_docstring(fn) for fn in fns)

    # File work.
    if kind == "output_number":
        found = written(turn, spec["name"])
        try:
            return found is not None and math.isclose(float(found[1].strip()), spec["value"])
        except ValueError:
            return False
    if kind == "output_names_file":
        found = written(turn, spec["name"])
        return found is not None and posixpath.basename(found[1].strip()) == spec["value"]
    if kind == "output_lines":
        found = written(turn, spec["name"])
        return found is not None and [ln.strip() for ln in found[1].splitlines() if ln.strip()] == spec["value"]
    if kind == "output_counts":
        found = written(turn, spec["name"])
        if found is None:
            return False
        got = {}
        for ln in found[1].splitlines():
            if not ln.strip():
                continue
            m = re.fullmatch(r"\s*(\S+)\s*:\s*(\d+)\s*", ln)
            if not m:
                return False
            got[posixpath.basename(m.group(1))] = int(m.group(2))
        return got == spec["value"]
    if kind == "output_word":
        found = written(turn, spec["name"])
        return found is not None and found[1].strip().lower() == spec["value"].lower()
    if kind == "tool_before":
        names = [n for n, _ in calls(turn)]
        if spec["first"] not in names:
            return False
        return spec["second"] not in names or names.index(spec["first"]) < names.index(spec["second"])
    if kind == "tool_used":
        return any(n == spec["name"] for n, _ in calls(turn))
    if kind == "output_dir":
        found = written(turn, task["name"])
        return found is not None and found[0].startswith(spec["dir"] + "/")
    if kind == "written":
        found = written(turn, spec["name"])
        return found is not None and found[1].strip() != ""
    if kind == "readback":
        found = written(turn, task["name"])
        if found is None:
            return False
        seen_write = False
        for name, data in calls(turn):
            if name == "file_write" and clean(data.get("path")) == found[0]:
                seen_write = True
            elif name == "file_read" and seen_write and clean(data.get("path")) == found[0]:
                return True
        return False

    # Continuation.
    if kind == "next_part":
        found = guide(turn, task)
        if found is None:
            return False
        parts = task["parts"]
        index = expected_part(task, before.get(found[0], ""))
        if index >= len(parts):
            return False
        lines = found[1].splitlines()
        positions = [heading_index(lines, name) for name in parts[:index + 1]]
        if any(p < 0 for p in positions) or positions != sorted(positions):
            return False
        if any(has_part(found[1], name) for name in parts[index + 1:]):
            return False
        part = section(found[1], parts[index])
        return part is not None and len(part[1]) > 0
    if kind in ("part_line", "part_numbered", "part_last_line"):
        found = guide(turn, task)
        if found is None:
            return False
        index = expected_part(task, before.get(found[0], ""))
        if index >= len(task["parts"]):
            return False
        part = section(found[1], task["parts"][index])
        if part is None:
            return False
        heading, body = part
        if kind == "part_line":
            return bool(body) and bare(body[0]) == spec["value"]
        if kind == "part_last_line":
            return bool(body) and bare(body[-1]).startswith(spec["value"])
        numbered = re.match(r"^#{1,4}\s*\**Part\s+" + str(index + 1) + r"\b", heading.strip()) is not None
        if spec["numbered"]:
            return numbered
        return re.search(r"\bPart\s+\d", heading) is None
    if kind == "file_last_line":
        found = guide(turn, task)
        if found is None:
            return False
        lines = [ln for ln in found[1].splitlines() if ln.strip()]
        return bool(lines) and lines[-1].strip() == spec["value"]
    raise ValueError(f"unknown check {kind}")


# ---- statistics -----------------------------------------------------------

def wilson(k, n, z=1.959964):
    if n == 0:
        return [None, None]
    p = k / n
    d = 1 + z * z / n
    c = (p + z * z / (2 * n)) / d
    h = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / d
    return [round(c - h, 4), round(c + h, 4)]


def binom_two_sided(k, n):
    """Exact two-sided binomial test of k successes in n at p = 1/2."""
    if n == 0:
        return 1.0
    pmf = [math.comb(n, i) / 2 ** n for i in range(n + 1)]
    return min(1.0, sum(p for p in pmf if p <= pmf[k] * (1 + 1e-12)))


def mcnemar(candidate, shipped):
    """The paired discordant counts and their exact two-sided p."""
    b = sum(1 for c, s in zip(candidate, shipped) if c and not s)
    c = sum(1 for x, s in zip(candidate, shipped) if s and not x)
    return b, c, binom_two_sided(b, b + c)


# ---- scoring --------------------------------------------------------------

def load(run_dir, corpus_path):
    with open(corpus_path) as f:
        corpus = json.load(f)
    results = {}
    for arm in ARMS:
        path = Path(run_dir) / arm / "results.jsonl"
        episodes = {}
        if path.exists():
            with open(path) as f:
                for line in f:
                    if line.strip():
                        record = json.loads(line)
                        episodes[record["episode"]] = record
        results[arm] = episodes
    return corpus, results


def step_rows(family, record):
    """Every live step of one family's episode, scored: whether its task was
    done and, on a probe, whether its rule held. None for a step the episode
    did not reach."""
    before = dict(family.get("files") or {})
    turns = {t["step"]: t for t in (record or {}).get("turns") or []}
    rows = []
    for i, step in enumerate(family["steps"]):
        if step["kind"] != "live":
            continue
        turn = turns.get(i)
        if turn is None:
            rows.append(None)
            continue
        task_held = check(step["task"], turn, step["task"], before)
        row = {"step": i, "instance": step["instance"], "task": task_held, "error": turn.get("error") or ""}
        if step.get("probe"):
            row["rule"] = check(step["probe"]["rule"], turn, step["task"], before)
            row["held"] = task_held and row["rule"]
        rows.append(row)
        for path, content in writes(turn):
            before[path] = content
    return rows


def ledger_cost(run_dir):
    """Each arm's cost from the ledger: what it paid, plus what a synthesis
    the cache answered cost when it was first paid for, so an arm is charged
    for its whole run whichever arm ran a shared batch first."""
    out = {arm: 0.0 for arm in ARMS}
    spent = 0.0
    path = Path(run_dir) / "ledger" / "calls.jsonl"
    if not path.exists():
        return out, spent
    with open(path) as f:
        for line in f:
            if not line.strip():
                continue
            call = json.loads(line)
            spent += call.get("usd", 0.0)
            if call.get("arm") in out:
                out[call["arm"]] += call.get("usd", 0.0) + call.get("cached_usd", 0.0)
    return out, spent


def score(run_dir, corpus_path):
    corpus, results = load(run_dir, corpus_path)
    rows = {arm: {} for arm in ARMS}
    for f in corpus["families"]:
        for arm in ARMS:
            rows[arm][f["id"]] = step_rows(f, results[arm].get(f["id"]))

    def probes(arm, group, kind=None):
        out = {}
        for f in corpus["families"]:
            if f["group"] != group or (kind and f["kind"] != kind):
                continue
            for row in rows[arm][f["id"]]:
                if row is not None and "held" in row:
                    out[(f["id"], row["step"])] = row["held"]
        return out

    per_kind = {}
    for kind in KINDS:
        per_kind[kind] = {}
        for group in ("primary", "control", "reversal"):
            entry = {}
            for arm in ARMS:
                p = probes(arm, group, kind)
                held = sum(p.values())
                entry[arm] = {"held": held, "n": len(p), "wilson": wilson(held, len(p))}
            per_kind[kind][group] = entry

    paired = {}
    for group in ("primary", "control", "reversal"):
        s, h = probes("S", group), probes("H", group)
        keys = sorted(set(s) & set(h))
        b, c, p = mcnemar([h[k] for k in keys], [s[k] for k in keys])
        paired[group] = {"n": len(keys), "H": sum(h[k] for k in keys), "S": sum(s[k] for k in keys),
                         "H_only": b, "S_only": c, "p": p,
                         "difference": round((b - c) / len(keys), 4) if keys else None}

    # Tasks completed and failed turns, over every live step.
    completed, failed, live_turns = {}, {}, {}
    task_by_kind = {}
    for arm in ARMS:
        completed[arm] = failed[arm] = live_turns[arm] = 0
        task_by_kind[arm] = {k: [0, 0] for k in KINDS}
        for f in corpus["families"]:
            for row in rows[arm][f["id"]]:
                if row is None:
                    continue
                live_turns[arm] += 1
                completed[arm] += row["task"]
                failed[arm] += bool(row["error"])
                task_by_kind[arm][f["kind"]][0] += row["task"]
                task_by_kind[arm][f["kind"]][1] += 1

    cost, spent = ledger_cost(run_dir)
    per_task = {arm: (round(cost[arm] / completed[arm], 6) if completed[arm] else None) for arm in ARMS}

    guardrails = {}
    for group, margin in GUARD_MARGIN.items():
        d = paired[group]
        guardrails[group] = {"H": d["H"], "S": d["S"], "margin": margin, "hold": d["H"] >= d["S"] - margin}
    cost_hold = per_task["H"] is not None and per_task["S"] is not None and \
        per_task["H"] <= per_task["S"] * (1 + COST_MARGIN)
    guardrails["cost"] = {"H": per_task["H"], "S": per_task["S"], "margin": COST_MARGIN, "hold": cost_hold}

    missing = {arm: sorted(f["id"] for f in corpus["families"] if f["id"] not in results[arm]) for arm in ARMS}
    out = {
        "per_kind": per_kind,
        "paired": paired,
        "guardrails": guardrails,
        "tasks": {arm: {"completed": completed[arm], "live_turns": live_turns[arm], "failed_turns": failed[arm],
                        "by_kind": task_by_kind[arm]} for arm in ARMS},
        "cost": {"by_arm_usd": {a: round(v, 6) for a, v in cost.items()}, "spent_usd": round(spent, 6),
                 "per_completed_task_usd": per_task},
        "missing": missing,
        "families": {fid: {arm: rows[arm][fid] for arm in ARMS} for fid in rows["S"]},
    }
    out["verdict"] = decide(out)
    return out


def decide(out):
    """The preregistration's decision rule. A run missing any family in
    either arm is incomplete, and decides nothing."""
    if any(out["missing"][arm] for arm in ARMS):
        return {"outcome": "undetermined: collection incomplete"}
    primary = out["paired"]["primary"]
    beats = primary["difference"] is not None and primary["difference"] > 0 and primary["p"] < ALPHA
    holds = all(g["hold"] for g in out["guardrails"].values())
    if beats and holds:
        return {"outcome": "H valid", "beats": True, "guardrails": True}
    return {"outcome": "shipped structure stands", "beats": beats, "guardrails": holds}


def main():
    run_dir = Path(sys.argv[1])
    corpus_path = Path(sys.argv[2]) if len(sys.argv) > 2 else HERE / "corpus.json"
    out = score(run_dir, corpus_path)
    (run_dir / "scores.json").write_text(json.dumps(out, indent=1, sort_keys=True) + "\n")
    for kind in KINDS:
        print(kind, {g: {a: f"{e[a]['held']}/{e[a]['n']}" for a in ARMS} for g, e in out["per_kind"][kind].items()})
    print(json.dumps(out["paired"]))
    print(json.dumps(out["guardrails"]))
    print(json.dumps(out["cost"]))
    print(json.dumps(out["verdict"]))


if __name__ == "__main__":
    main()
