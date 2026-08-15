"""Runner for the new structures + model-diversity comparison.

Part A: S1-S5 (new structures) over the 1,000-turn real dataset.
Part B: the dedicated-decider structure (C3/C9 shape) with gpt-4o-mini vs
gpt-4o as the decider, on the same dataset — measures the correction
judgment ceiling per decider model.
"""
from __future__ import annotations
import json
import os
import random
import sys
import time

sys.path.insert(0, os.path.dirname(__file__))

from real_api import embed, cosine, API_CALLS, flush_caches
from real_dataset import build
from real_structures import ALL_NEW
from real_runner import RealStore, probe_recall


# ---------------------------------------------------------------------------
# Part A: new structures
# ---------------------------------------------------------------------------

def run_structure(cls, seed=11):
    random.seed(seed)
    turns, probes = build()
    sys_inst = cls()
    for text, topics in turns:
        sys_inst.turn(text, topics)
    correct = stale = 0
    for _, question, new, old in probes:
        blob = " ".join(sys_inst.current_facts()).lower()
        if new.lower() in blob:
            correct += 1
        if old.lower() in blob:
            stale += 1
    return {
        "system": cls.NAME,
        "recall": correct / len(probes),
        "latest_wins_net": (correct - stale) / len(probes),
        "stale_present": stale / len(probes),
        "items": sys_inst.size(),
        "decider_calls": getattr(sys_inst, "calls", 0),
    }


# ---------------------------------------------------------------------------
# Part B: decider model comparison (gpt-4o-mini vs gpt-4o)
# ---------------------------------------------------------------------------

DECIDER_SYS = ("You are a memory decider. Given the user's latest message and "
               "the currently stored memories, decide what to store. Reply with "
               'JSON: {"store": ["<fact>", ...], "retire": ["<substring of an '
               'old memory to retire>", ...]}. Store only durable facts and '
               "corrections. Store nothing for chatter.")


class DeciderSystem:
    NAME = "C3-real"

    def __init__(self, model):
        self.store = RealStore(supersession=True, retention=True)
        self.calls = 0
        self.model = model

    def turn(self, text, topics):
        import urllib.request
        self.store.tick()
        current = [r[1] for r in self.store.rows if not r[5]][-10:]
        key = f"{self.model}\n---\n{text}\n---\n{json.dumps(current)}"
        from real_api import CHAT, _save, CHAT_CACHE
        if key in CHAT:
            out = CHAT[key]
        else:
            req = urllib.request.Request(
                "https://api.openai.com/v1/chat/completions",
                data=json.dumps({
                    "model": self.model,
                    "messages": [{"role": "system", "content": DECIDER_SYS},
                                 {"role": "user", "content": f"Latest message: {text}\nCurrent memories: {json.dumps(current)}"}],
                    "temperature": 0,
                }).encode(),
                headers={"Authorization": f"Bearer {os.environ['OPENAI_API_KEY']}", "Content-Type": "application/json"},
            )
            with urllib.request.urlopen(req, timeout=90) as resp:
                out = json.loads(resp.read())["choices"][0]["message"]["content"]
            CHAT[key] = out
            API_CALLS["chat"] += 1
        self.calls += 1
        try:
            ops = json.loads(out[out.index("{"):out.rindex("}") + 1])
        except (ValueError, json.JSONDecodeError):
            self.store.retain()
            return
        for retire in ops.get("retire", []):
            for r in self.store.rows:
                if not r[5] and retire.lower() in r[1].lower():
                    r[5] = True
        for fact in ops.get("store", []):
            if isinstance(fact, str) and fact.strip():
                self.store.add(fact.strip(), 0.7)
        self.store.retain()


def run_decider(model, seed=11):
    random.seed(seed)
    turns, probes = build()
    sys_inst = DeciderSystem(model)
    for text, topics in turns:
        sys_inst.turn(text, topics)
    correct = stale = 0
    for _, question, new, old in probes:
        r = probe_recall(sys_inst.store, question, new, old)
        if r["new_present"]:
            correct += 1
        if r["old_present"]:
            stale += 1
    return {
        "decider": model,
        "recall": correct / len(probes),
        "latest_wins_net": (correct - stale) / len(probes),
        "stale_present": stale / len(probes),
        "items": sys_inst.store.size(),
        "decider_calls": sys_inst.calls,
    }


def main():
    os.makedirs("results", exist_ok=True)
    print("=== Part A: new structures (real embeddings) ===")
    rows_a = []
    for cls in ALL_NEW:
        t0 = time.perf_counter()
        r = run_structure(cls)
        r["elapsed_s"] = time.perf_counter() - t0
        rows_a.append(r)
        flush_caches()
        print(f"  {r['system']}: recall={r['recall']:.2f} latest={r['latest_wins_net']:.2f} stale={r['stale_present']:.2f} items={r['items']} calls={r['decider_calls']}")
    print("\n=== Part B: decider model comparison ===")
    rows_b = []
    for model in ["gpt-4o-mini", "gpt-4o"]:
        t0 = time.perf_counter()
        r = run_decider(model)
        r["elapsed_s"] = time.perf_counter() - t0
        rows_b.append(r)
        flush_caches()
        print(f"  {model}: recall={r['recall']:.2f} latest={r['latest_wins_net']:.2f} stale={r['stale_present']:.2f} items={r['items']}")
    out = {"new_structures": rows_a, "decider_models": rows_b, "api_calls": API_CALLS}
    with open("results/real_new_structures.json", "w") as f:
        json.dump(out, f, indent=2)
    lines = ["| system | recall | latest-wins | stale | items | calls |", "|---|---|---|---|---|---|"]
    for r in rows_a + rows_b:
        name = r.get("system") or r.get("decider")
        lines.append("| {} | {:.2f} | {:.2f} | {:.2f} | {} | {} |".format(
            name, r["recall"], r["latest_wins_net"], r["stale_present"], r["items"], r["decider_calls"]))
    with open("results/real_new_structures.md", "w") as f:
        f.write("# New structures + decider model comparison (real infrastructure)\n\n")
        f.write("\n".join(lines) + "\n")
    print(f"\nAPI calls total: {API_CALLS}")
    print(f"Wrote {os.path.abspath('results/real_new_structures.md')}")


if __name__ == "__main__":
    main()
