"""Rank OpenRouter's tool-calling endpoints by price, for #1164's model ladder.

    python3 research/general-agent/models.py

Reads OpenRouter's public model and endpoint lists (no key, no paid call)
and writes evidence/models.json: every eligible endpoint, cheapest first,
with the time it was read. The preregistration's ladder is this file's
first entries.

An endpoint is eligible when its model is a fixed model rather than a
router or a moving alias, is not a free or batch variant, and the endpoint
itself lists `tools` among its supported parameters, is up, and holds at
least MIN_CONTEXT tokens. Price is ranked as INPUT_SHARE input tokens to
one output token, the shape of an agent loop whose every call resends the
conversation.
"""

import json
import sys
import time
import urllib.request
from pathlib import Path

HERE = Path(__file__).resolve().parent
API = "https://openrouter.ai/api/v1"
INPUT_SHARE = 20
MIN_CONTEXT = 32768
# The endpoints of the DEPTH cheapest models by listed price are read. A
# model's listed price is its cheapest endpoint's, so the ranking is
# complete up to the listed price of the first model left unread, which
# the output records.
DEPTH = 40


def get(path):
    with urllib.request.urlopen(API + path, timeout=60) as resp:
        return json.load(resp)["data"]


def blended(prompt, completion):
    """USD per million tokens at INPUT_SHARE input tokens to one output."""
    return (INPUT_SHARE * prompt + completion) * 1e6 / (INPUT_SHARE + 1)


def fixed_model(model_id):
    return not (model_id.startswith("~") or model_id.startswith("openrouter/") or model_id.endswith(":free")
                or model_id.endswith(":batch") or "router" in model_id)


def main():
    read_at = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    models = [m for m in get("/models") if fixed_model(m["id"]) and "tools" in (m.get("supported_parameters") or [])]
    models = [m for m in models if float(m["pricing"]["prompt"]) >= 0 and float(m["pricing"]["completion"]) >= 0]
    models.sort(key=lambda m: blended(float(m["pricing"]["prompt"]), float(m["pricing"]["completion"])))
    endpoints = []
    for m in models[:DEPTH]:
        for e in get(f"/models/{m['id']}/endpoints")["endpoints"]:
            if "tools" not in (e.get("supported_parameters") or []) or e.get("status", 0) != 0:
                continue
            if (e.get("context_length") or 0) < MIN_CONTEXT:
                continue
            prompt, completion = float(e["pricing"]["prompt"]), float(e["pricing"]["completion"])
            endpoints.append({
                "model": m["id"], "provider": e["provider_name"], "tag": e.get("tag"),
                "quantization": e.get("quantization"), "context_length": e.get("context_length"),
                "input_usd_per_m": round(prompt * 1e6, 6), "output_usd_per_m": round(completion * 1e6, 6),
                "blended_usd_per_m": round(blended(prompt, completion), 6),
            })
    endpoints.sort(key=lambda e: (e["blended_usd_per_m"], e["model"], e["provider"]))
    unread = models[DEPTH] if len(models) > DEPTH else None
    complete_below = blended(float(unread["pricing"]["prompt"]), float(unread["pricing"]["completion"])) if unread else None
    out = {"read_at": read_at, "source": API, "input_share": INPUT_SHARE, "min_context": MIN_CONTEXT,
           "models_read": [m["id"] for m in models[:DEPTH]],
           "complete_below_blended_usd_per_m": round(complete_below, 6) if complete_below else None,
           "endpoints": endpoints}
    path = HERE / "evidence" / "models.json"
    path.parent.mkdir(exist_ok=True)
    path.write_text(json.dumps(out, indent=1) + "\n")
    for e in endpoints[:8]:
        print(e["blended_usd_per_m"], e["model"], e["provider"], e["input_usd_per_m"], e["output_usd_per_m"], file=sys.stdout)


if __name__ == "__main__":
    main()
