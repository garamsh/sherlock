"""System-level runner: cycle-aware memory experiment.

Workload: a scripted multi-week conversation with planted facts (preferences,
identity facts, corrections) and checkpoint probes. Each system runs the same
script; at each checkpoint the runner asks the system what the model can see,
and scores fact recall.

The headline metric is context-recall: the fraction of planted facts present
in the model's effective context at the checkpoint. A system whose write path
depends on voluntary model compliance scores what production measured — near
zero — regardless of how good its store is.
"""
from __future__ import annotations
import json
import os
import random
import sys

sys.path.insert(0, os.path.dirname(__file__))

from systems import ALL_SYSTEMS, SimModel


# ---------------------------------------------------------------------------
# The scripted conversation. Each turn: (user_text, planted_facts).
# planted_facts = the facts a correct memory system should retain from this
# turn. The script plants facts, then later probes them at checkpoints.
# ---------------------------------------------------------------------------

SCRIPT = [
    # Week 1 — identity and preferences are planted.
    ("Hi, I'm Lisa. I work on the metrics service.", ["Lisa works on the metrics service"]),
    ("I prefer Python for new services.", ["Lisa prefers Python for new services"]),
    ("Always run tests before committing.", ["Lisa wants tests run before committing"]),
    ("I use VS Code.", ["Lisa uses VS Code"]),
    ("My manager is Otto.", ["Lisa's manager is Otto"]),
    # checkpoint 1
    ("I'm working on the audit log service now.", ["Lisa now works on the audit log service"]),
    ("We use Postgres and Kafka for it.", ["the audit log service uses Postgres and Kafka"]),
    ("I switched to four-space indentation.", ["Lisa switched to four-space indentation"]),
    ("Postgres is at 10.0.0.4.", ["Postgres is at 10.0.0.4"]),
    ("I drink coffee in the mornings.", ["Lisa drinks coffee in the mornings"]),
    # checkpoint 2
    ("Actually, I prefer Go for systems work now.", ["Lisa prefers Go for systems work"]),
    ("Postgres moved to 10.0.0.5.", ["Postgres is now at 10.0.0.5"]),
    ("Maria joined as a junior engineer.", ["Maria joined as a junior engineer"]),
    ("I bike to work three days a week.", ["Lisa bikes to work three days a week"]),
    ("The deploy pipeline uses Helm charts.", ["the deploy pipeline uses Helm charts"]),
    # checkpoint 3
    ("I changed my mind — I drink tea now.", ["Lisa now drinks tea"]),
    ("The audit log service is in production.", ["the audit log service is in production"]),
    ("We migrated to Datadog for traces.", ["the team uses Datadog for traces"]),
    ("I still prefer Go for systems.", ["Lisa still prefers Go for systems"]),
    ("Maria prefers pytest over unittest.", ["Maria prefers pytest over unittest"]),
    # checkpoint 4
]

CHECKPOINTS = [
    # (after turn index, probe id, question, acceptable answer keywords, source fact index range)
    (4,  "cp1", "Who is Lisa's manager?", ["otto"], None),
    (9,  "cp2", "What indentation does Lisa use?", ["four-space"], None),
    (14, "cp3", "Where is Postgres?", ["10.0.0.5"], None),
    (14, "cp3b", "What does Lisa drink?", ["tea"], None),
    (19, "cp4", "What language does Lisa prefer for systems work?", ["go"], None),
    (19, "cp4b", "What does the deploy pipeline use?", ["helm"], None),
]


def run_system(cls, seed: int = 7):
    random.seed(seed)
    model = SimModel()
    sys = cls(model)
    # Checkpoint state: after each checkpoint, capture what the model can see.
    checkpoint_results = {}
    for i, (user_text, planted) in enumerate(SCRIPT):
        sys.turn(user_text, planted)
        for (after_idx, probe_id, question, keywords, _r) in CHECKPOINTS:
            if after_idx == i:
                ctx = sys.checkpoint_context(question)
                # A fact counts as recalled if any keyword appears in any
                # fact string the model can see.
                blob = " ".join(ctx).lower()
                hit = any(kw.lower() in blob for kw in keywords)
                checkpoint_results[probe_id] = hit
    return sys, checkpoint_results


def main():
    os.makedirs("results", exist_ok=True)
    rows = []
    # Multiple seeds to smooth the simulated compliance randomness.
    SEEDS = [7, 42, 1234]
    for cls in ALL_SYSTEMS:
        name = cls.NAME
        agg_hits = 0
        agg_total = 0
        agg_store = 0
        agg_calls = 0.0
        agg_tokens = 0.0
        for seed in SEEDS:
            random.seed(seed)
            sys, results = run_system(cls, seed)
            agg_hits += sum(1 for v in results.values() if v)
            agg_total += len(results)
            agg_store += sys.store.size()
            agg_calls += sys.extra_model_calls
            agg_tokens += sys.injected_tokens
        n = len(SEEDS)
        rows.append({
            "system": name,
            "context_recall": agg_hits / agg_total if agg_total else 0.0,
            "store_items": agg_store / n,
            "extra_calls_per_turn": agg_calls / n / len(SCRIPT),
            "injected_tokens_per_turn": agg_tokens / n / len(SCRIPT),
        })
    headers = ["system", "context-recall", "store items", "extra calls/turn", "injected tokens/turn"]
    lines = ["| " + " | ".join(headers) + " |",
             "|" + "|".join(["-" * len(h) for h in headers]) + "|"]
    for r in rows:
        lines.append("| {} | {:.2f} | {:.0f} | {:.2f} | {:.0f} |".format(
            r["system"], r["context_recall"], r["store_items"],
            r["extra_calls_per_turn"], r["injected_tokens_per_turn"]))
    table = "\n".join(lines)
    with open("results/system_cycle.md", "w") as f:
        f.write("# System-level memory experiment — the full cycle\n\n")
        f.write("Question: which memory *system* — write path, read path, store policy together — keeps facts in the model's context over a multi-week conversation? The previous experiments ranked stores assuming the write happens; QA measured that it does not (0/15 voluntary writeback on gpt-4o).\n\n")
        f.write("Compliance inputs are measured or marked assumptions: voluntary writeback 0% (QA #5-#8), structured output 100% (API-enforced), platform extraction 100% (deterministic), forced narrow call 95% (assumption), on-demand search 30% (assumption).\n\n")
        f.write(table + "\n")
    print(table)
    with open("results/system_cycle_raw.json", "w") as f:
        json.dump(rows, f, indent=2)
    print(f"\nWrote {os.path.abspath('results/system_cycle.md')}")


if __name__ == "__main__":
    main()
