"""Count MiniMax-M2 tokens in sherlock's committed corpora, with no model call.

The tokenizer is MiniMax-M2's published byte-level BPE, read from
tokenizer.json at the Hugging Face revision below, and applied here in plain
Python because the `tokenizers` package is not installed on the machine that
ran it. Download it first:

    curl -sL -o <scratch>/tokenizer.json \\
      https://huggingface.co/MiniMaxAI/MiniMax-M2/resolve/757303d492a50514c312788b5247a4f696a4c6a3/tokenizer.json
    python3 docs/research/memory-paradigms/tokens.py <scratch>/tokenizer.json [<encoding corpus.json>]

The #890 corpus is on its study branch, not on dev; pass it as the second
argument (`git show 46ca289:docs/research/memory-encoding/validation/corpus.json`).

It writes tokens.json beside this file:
- `validation`: the tokenizer checked against MiniMax's own counts. For every
  successful batch-4 synthesis call in #874's shared ledger, the request
  differs from the next only in its user message, so prompt_tokens minus this
  count of that message must be one constant if the count is exact.
- `corpora`: per corpus, the messages a full-context arm would hold, as
  content tokens and as tokens in M2's chat template (chat_template.jinja at
  the same revision: `]~b]<role>\\n<content>[e~[\\n` per message), and the
  `window` a full-context arm holds under HISTORY_BUDGET: how many of the
  newest messages fit, whether the whole stream does, and how many E and K
  guards, whose evidence lies in the base, keep it all in view.
"""

import json
import re
import sys
import unicodedata
from pathlib import Path

HERE = Path(__file__).resolve().parent
RESEARCH = HERE.parent
REVISION = "757303d492a50514c312788b5247a4f696a4c6a3"
# History a full-context arm may restore: M2's 196,608 positions
# (config.json), less about 4,600 for instructions, tools and recall (S's
# live prompts in #885 averaged 4,443 tokens with a 32-message window) and
# 12,000 for the turn's tool results and output. README §W. Full-context ceiling.
HISTORY_BUDGET = 180_000

# tokenizer.json's Split pattern, with each \p{..} class left as a named slot
# that char_classes fills from the characters actually present.
PATTERN = (
    r"[^\r\n{L}{N}]?[{UPPER}]*[{LOWER}]+(?i:'s|'t|'re|'ve|'m|'ll|'d)?"
    r"|[^\r\n{L}{N}]?[{UPPER}]+[{LOWER}]*(?i:'s|'t|'re|'ve|'m|'ll|'d)?"
    r"|[{N}]{{1,3}}"
    r"| ?[^\s{L}{N}]+[\r\n/]*"
    r"|\s*[\r\n]+"
    r"|\s+(?!\S)"
    r"|\s+"
)


def char_classes(chars):
    """The pattern's Unicode classes, restricted to the characters given.

    Python's re has no \\p{..}; a class listing every present character of the
    category matches exactly what \\p{..} would on text drawn from them.
    """
    groups = {"L": [], "N": [], "UPPER": [], "LOWER": []}
    for c in sorted(chars):
        cat = unicodedata.category(c)
        if cat[0] == "L":
            groups["L"].append(c)
        if cat[0] == "N":
            groups["N"].append(c)
        if cat in ("Lu", "Lt", "Lm", "Lo") or cat[0] == "M":
            groups["UPPER"].append(c)
        if cat in ("Ll", "Lm", "Lo") or cat[0] == "M":
            groups["LOWER"].append(c)
    return {k: "".join(re.escape(c) for c in v) for k, v in groups.items()}


def bytes_to_unicode():
    """ByteLevel's byte-to-character table (GPT-2's)."""
    bs = list(range(ord("!"), ord("~") + 1)) + list(range(ord("¡"), ord("¬") + 1)) + list(range(ord("®"), ord("ÿ") + 1))
    cs = bs[:]
    n = 0
    for b in range(256):
        if b not in bs:
            bs.append(b)
            cs.append(256 + n)
            n += 1
    return dict(zip(bs, map(chr, cs)))


class Tokenizer:
    def __init__(self, path, texts):
        spec = json.loads(Path(path).read_text())
        model = spec["model"]
        if model["type"] != "BPE" or spec["normalizer"] != {"type": "NFC"}:
            raise SystemExit("unexpected tokenizer.json: not NFC byte-level BPE")
        self.vocab = model["vocab"]
        self.ranks = {tuple(m.split(" ")): i for i, m in enumerate(model["merges"])}
        chars = set()
        for t in texts:
            chars.update(unicodedata.normalize("NFC", t))
        self.split = re.compile(PATTERN.format(**char_classes(chars)))
        self.byte_map = bytes_to_unicode()
        self.cache = {}

    def bpe(self, piece):
        if piece in self.cache:
            return self.cache[piece]
        word = [self.byte_map[b] for b in piece.encode("utf-8")]
        while len(word) > 1:
            pairs = [(self.ranks.get((a, b), 1 << 30), i) for i, (a, b) in enumerate(zip(word, word[1:]))]
            rank, i = min(pairs)
            if rank == 1 << 30:
                break
            first, second = word[i], word[i + 1]
            merged, j = [], 0
            while j < len(word):
                if j < len(word) - 1 and word[j] == first and word[j + 1] == second:
                    merged.append(first + second)
                    j += 2
                else:
                    merged.append(word[j])
                    j += 1
            word = merged
        for token in word:
            if token not in self.vocab:
                raise SystemExit(f"token {token!r} not in vocab")
        self.cache[piece] = len(word)
        return len(word)

    def count(self, text):
        text = unicodedata.normalize("NFC", text)
        pieces = self.split.findall(text)
        if "".join(pieces) != text:
            raise SystemExit("pre-tokenizer dropped characters")
        return sum(self.bpe(p) for p in pieces)


def go_json(value):
    """encoding/json's Marshal of a struct of strings: <, > and & escaped."""
    s = json.dumps(value, ensure_ascii=False, separators=(",", ":"))
    return s.replace("<", "\\u003c").replace(">", "\\u003e").replace("&", "\\u0026").replace("\u2028", "\\u2028").replace("\u2029", "\\u2029")


def load(path):
    return json.loads(Path(path).read_text())


def main():
    if len(sys.argv) not in (2, 3):
        raise SystemExit(__doc__)
    base = load(RESEARCH / "mid-memory-rag/evidence/replication/corpus.json")
    corpora = {
        "#874 chained-recall": RESEARCH / "chained-recall/corpus.json",
        "#885 confirm": RESEARCH / "chained-recall/confirm/corpus.json",
        "#888 readtime": RESEARCH / "chained-recall/readtime/corpus.json",
    }
    if len(sys.argv) == 3:
        corpora["#890 encoding validation (46ca289)"] = Path(sys.argv[2])
    loaded = {name: load(p) for name, p in corpora.items()}
    streams = {name: c["stream"] for name, c in loaded.items()}

    # #874's shared batch-4 synthesis: episode b4-NNNN is stream batch NNNN.
    stream874 = streams["#874 chained-recall"]
    ledger = [json.loads(line) for line in (RESEARCH / "chained-recall/evidence/ledger/calls.jsonl").open()]
    calls = [c for c in ledger if c.get("phase") == "synth-b4" and c.get("arm") == "shared" and c.get("status") == 200]
    requests = {}
    for c in calls:
        i = int(c["episode"].split("-")[1]) - 1
        batch = stream874[4 * i : 4 * i + 4]
        requests[c["request_sha256"]] = (c["prompt_tokens"], go_json({"updates": [m["role"] + ": " + m["content"] for m in batch]}))

    texts = [m["content"] for m in base] + [m["content"] for s in streams.values() for m in s]
    texts += [r[1] for r in requests.values()] + ["user\n", "ai\n", "\n"]
    tok = Tokenizer(sys.argv[1], texts)

    residuals = {}
    for prompt_tokens, message in requests.values():
        r = prompt_tokens - tok.count(message)
        residuals[r] = residuals.get(r, 0) + 1
    validation = {
        "requests": len(requests),
        "prompt_tokens_minus_counted_user_message": dict(sorted(residuals.items(), key=lambda kv: -kv[1])),
    }

    role_tokens = {"user": tok.count("user\n"), "assistant": tok.count("ai\n")}
    close = tok.count("\n")

    def templated(m):
        return tok.count(m["content"]) + 1 + role_tokens[m["role"]] + 1 + close

    def measure(msgs):
        chars = sum(len(m["content"]) for m in msgs)
        content = sum(tok.count(m["content"]) for m in msgs)
        return {"messages": len(msgs), "chars": chars, "content_tokens": content, "templated_tokens": sum(templated(m) for m in msgs), "chars_per_token": round(chars / content, 3)}

    def window(msgs, guards):
        """The newest messages whose templated tokens fit HISTORY_BUDGET, and
        which guards with evidence in the base keep all of it in view."""
        used, first = 0, len(msgs)
        while first > 0 and used + templated(msgs[first - 1]) <= HISTORY_BUDGET:
            first -= 1
            used += templated(msgs[first])
        oldest = msgs[first]["id"] if first < len(msgs) else None
        in_view = {}
        for g in guards:
            evidence = g.get("evidence") or []
            if evidence and max(evidence) <= len(base):
                kept, total = in_view.get(g["family"], (0, 0))
                in_view[g["family"]] = (kept + (min(evidence) >= oldest), total + 1)
        return {"history_budget": HISTORY_BUDGET, "messages": len(msgs) - first, "templated_tokens": used, "oldest_id": oldest,
                "whole_stream_in_view": oldest is not None and oldest <= len(base) + 1,
                "base_evidence_guards_in_view": {f: f"{k} of {n}" for f, (k, n) in in_view.items()}}

    out = {
        "tokenizer": f"MiniMaxAI/MiniMax-M2@{REVISION} tokenizer.json",
        "validation": validation,
        "base, #856 replication": measure(base),
        "corpora": {name: {"stream": measure(s), "base and stream": measure(base + s), "window": window(base + s, loaded[name]["guards"])} for name, s in streams.items()},
    }
    (HERE / "tokens.json").write_text(json.dumps(out, indent=2) + "\n")
    print(json.dumps(out, indent=2))


if __name__ == "__main__":
    main()
