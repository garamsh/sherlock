"""Build the #1164 corpus: task families of four kinds of general work.

A family is one kind of task the person asks for three times, in one
episode. Instances are separated by unrelated conversation long enough to
roll the earlier instance out of the window, and every live input starts
from what a restart restores, so each instance is a new session.

- primary: on the first instance the person corrects one aspect of the
  work, in a second turn that states the rule and asks for the same task
  again. Instances 2 and 3 are probes: the task must be done and the rule
  must hold, without the rule being restated.
- control: no correction is ever given. Every instance is a probe: the task
  must be done and no rule the corpus can state may appear.
- reversal: as primary, and instance 3 states a new rule that replaces the
  first. Instance 3 is the probe: the new rule holds and the old one does
  not.

The four kinds are coding in the workspace (scored by tests the scorer
runs), writing to a stated format, multi-step file work with a checkable
end state, and continuing a piece of work across sessions.

    python3 research/general-agent/corpus.py

The output is deterministic: running this again writes a byte-identical
corpus.json.
"""

import json
import random
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPLICATION = HERE.parent / "mid-memory-rag" / "evidence" / "replication"

SEED = 1164

# The rolling window every arm runs with, and the messages of unrelated
# conversation between two instances. Both are #1153's
# (research/experience-memory/corpus.py at
# 4115061f7ac6c3776982dcffd14a41278e702941): the distance exceeds the
# window, so the earlier instance has left it when the next one runs.
WINDOW = 32
DISTANCE_MESSAGES = 40
FILLER_CHATS = 5
FILLER_BLOCKS = 2

KINDS = ["code", "writing", "tools", "continuation"]
LETTERS = {"code": "K", "writing": "W", "tools": "T", "continuation": "C"}
PRIMARY_PER_KIND = 15
CONTROL_PER_KIND = 4
REVERSAL_PER_KIND = 4

# One subject per family, #1153's list.
DOMAINS = [
    "bicycle maintenance", "sourdough baking", "spreadsheet budgeting",
    "houseplant care", "board-game nights", "hiking gear", "coffee brewing",
    "resume writing", "meal prepping", "chess openings", "aquarium care",
    "photo organizing", "garden planting", "public speaking", "pet grooming",
    "camping in the rain", "home insurance", "learning a language",
    "keeping a journal", "training for a race", "fixing a bicycle chain",
    "planning a birthday", "reading more books", "saving money monthly",
    "choosing a laptop", "growing herbs indoors", "running a book club",
    "shipping a small order", "teaching a child to swim", "restoring furniture",
    "buying a first guitar", "staying in touch abroad", "composting at home",
    "planning a wedding", "moving to a new city", "training a puppy",
    "keeping a shared calendar", "reviewing a contract", "making cold brew",
    "restoring a bicycle", "writing a will", "booking a cheap flight",
    "starting a podcast", "repotting houseplants", "keeping bees",
    "running a marathon", "choosing a mortgage", "sharing photos",
    "learning to swim", "staying focused at work",
]

# Part names for the continuation guides. They carry no order of their own,
# so the order a guide's parts come in is known only from what the person
# said when the guide was started.
PART_NAMES = [
    "Kestrel", "Osprey", "Heron", "Wren", "Plover", "Merlin", "Avocet",
    "Bittern", "Curlew", "Dunlin", "Egret", "Fulmar", "Gannet", "Hobby",
    "Ibis", "Jackdaw", "Kittiwake", "Linnet", "Martin", "Nightjar",
]

PEOPLE = ["Mara", "Tomas", "Ines", "Kofi", "Anya", "Ravi", "Lena", "Oscar"]


# ---- rules ------------------------------------------------------------------
#
# A rule is what the person states once and expects kept. Its check reads
# only what the turn produced, so the scorer never judges prose.

RULES = {
    # writing: what the person reads
    "status_prefix": {
        "statement": "Good, but from now on always begin your reply with 'STATUS:'.",
        "check": {"kind": "prefix", "value": "STATUS:"},
    },
    "tldr_prefix": {
        "statement": "Keep doing it that way, and from now on start every reply with a line beginning 'TL;DR:'.",
        "check": {"kind": "prefix", "value": "TL;DR:"},
    },
    "juniper_suffix": {
        "statement": "Do it like this from now on: sign every reply with '— Juniper' as its last line.",
        "check": {"kind": "suffix", "value": "— Juniper"},
    },
    "mara_suffix": {
        "check": {"kind": "suffix", "value": "— Mara"},
    },
    "lowercase": {
        "statement": "That is right, but from now on write every reply in lowercase only.",
        "check": {"kind": "all_lowercase"},
    },
    "uppercase": {
        "check": {"kind": "all_uppercase"},
    },
    "bullet_no_period": {
        "statement": "One thing to keep from now on: never end a bullet point with a full stop.",
        "check": {"kind": "bullet_ends", "period": False},
    },
    "bullet_period": {
        "check": {"kind": "bullet_ends", "period": True},
    },
    # code: the module the instance asks for
    "spdx_mit": {
        "statement": "From now on, start every source file you write with the line '# SPDX-License-Identifier: MIT'.",
        "check": {"kind": "first_line", "value": "# SPDX-License-Identifier: MIT"},
    },
    "spdx_apache": {
        "check": {"kind": "first_line", "value": "# SPDX-License-Identifier: Apache-2.0"},
    },
    "src_dir": {
        "statement": "Keep code organised from now on: put every Python module you write under src/.",
        "check": {"kind": "module_dir", "dir": "src"},
    },
    "lib_dir": {
        "check": {"kind": "module_dir", "dir": "lib"},
    },
    "test_file": {
        "statement": "From now on, whenever you write a module, also write a pytest file test_<module>.py beside it.",
        "check": {"kind": "test_file"},
    },
    "no_test_file": {
        "check": {"kind": "not", "of": {"kind": "test_file_any"}},
    },
    "type_hints": {
        "statement": "From now on, annotate every function's parameters and return type.",
        "check": {"kind": "type_hints"},
    },
    "docstrings": {
        "statement": "From now on, give every function you write a docstring.",
        "check": {"kind": "docstrings"},
    },
    # tools: how the work is done and where its result goes
    "list_first": {
        "statement": "Work like that from now on: list the directory before you read any file.",
        "check": {"kind": "tool_before", "first": "file_list", "second": "file_read"},
    },
    "no_list": {
        "check": {"kind": "not", "of": {"kind": "tool_used", "name": "file_list"}},
    },
    "out_dir": {
        "statement": "From now on, write every result file under out/.",
        "check": {"kind": "output_dir", "dir": "out"},
    },
    "results_dir": {
        "check": {"kind": "output_dir", "dir": "results"},
    },
    "worklog": {
        "statement": "From now on, after each job also write a line saying what you did into worklog.md.",
        "check": {"kind": "written", "name": "worklog.md"},
    },
    "no_worklog": {
        "check": {"kind": "not", "of": {"kind": "written", "name": "worklog.md"}},
    },
    "readback": {
        "statement": "From now on, read every result file back after you write it, to check it.",
        "check": {"kind": "readback"},
    },
    # continuation: the part the instance adds to the guide
    "status_draft": {
        "statement": "From now on, put a line 'Status: draft' directly under each part's heading.",
        "check": {"kind": "part_line", "value": "Status: draft"},
    },
    "status_review": {
        "check": {"kind": "part_line", "value": "Status: review"},
    },
    "numbered": {
        "statement": "From now on, number the part headings, like '## Part 2: <name>'.",
        "check": {"kind": "part_numbered", "numbered": True},
    },
    "unnumbered": {
        "check": {"kind": "part_numbered", "numbered": False},
    },
    "in_short": {
        "statement": "From now on, end each part with a line starting 'In short:'.",
        "check": {"kind": "part_last_line", "value": "In short:"},
    },
    "key_point": {
        "check": {"kind": "part_last_line", "value": "Key point:"},
    },
    "footer": {
        "statement": "From now on, make the guide's last line '_Draft by Sherlock_'.",
        "check": {"kind": "file_last_line", "value": "_Draft by Sherlock_"},
    },
}

# The rules a primary family can correct, per kind, rotated over the
# kind's families.
PRIMARY_RULES = {
    "code": ["spdx_mit", "src_dir", "test_file", "type_hints", "docstrings"],
    "writing": ["status_prefix", "tldr_prefix", "juniper_suffix", "lowercase", "bullet_no_period"],
    "tools": ["list_first", "out_dir", "worklog", "readback"],
    "continuation": ["status_draft", "numbered", "in_short", "footer"],
}

# The rule given on the first instance of a reversal family, the rule
# instance 3 replaces it with, and how instance 3 says so.
REVERSALS = {
    "code": [
        ("spdx_mit", "spdx_apache", "We relicensed: from now on start every source file with '# SPDX-License-Identifier: Apache-2.0' instead of the MIT line."),
        ("src_dir", "lib_dir", "We moved things: from now on put every Python module under lib/, not src/."),
        ("test_file", "no_test_file", "Stop writing test files from now on; I keep the tests myself."),
    ],
    "writing": [
        ("status_prefix", "tldr_prefix", "Change of rule from now on: start every reply with a line beginning 'TL;DR:' instead of 'STATUS:'."),
        ("lowercase", "uppercase", "Switch it round from now on: write every reply in uppercase only, not lowercase."),
        ("juniper_suffix", "mara_suffix", "Change the sign-off from now on: end every reply with '— Mara' as its last line, not Juniper."),
        ("bullet_no_period", "bullet_period", "Change that from now on: end every bullet point with a full stop after all."),
    ],
    "tools": [
        ("out_dir", "results_dir", "From now on, write result files under results/ instead of out/."),
        ("worklog", "no_worklog", "You can stop keeping worklog.md from now on; don't write to it any more."),
        ("list_first", "no_list", "Change of habit from now on: don't list directories any more, read the files directly."),
    ],
    "continuation": [
        ("status_draft", "status_review", "From now on, the line under each new part's heading reads 'Status: review' instead of draft."),
        ("numbered", "unnumbered", "Drop the part numbers from new headings from now on; just '## <name>'."),
        ("in_short", "key_point", "From now on, end each new part with a line starting 'Key point:' instead of 'In short:'."),
    ],
}

# The markers a control family must not show: rules the corpus can state
# that an ordinary answer does not carry.
FORBIDDEN = {
    "code": ["spdx_mit", "spdx_apache", "src_dir", "lib_dir"],
    "writing": ["status_prefix", "tldr_prefix", "juniper_suffix", "mara_suffix"],
    "tools": ["out_dir", "results_dir", "worklog"],
    "continuation": ["status_draft", "status_review", "footer"],
}


# ---- coding tasks -----------------------------------------------------------
#
# Each function is asked for by a spec precise enough that its cases have
# one right answer. A case is [arguments, expected]; an expected of
# {"raises": name} means the call must raise that exception.

FUNCTIONS = [
    {"module": "slugs", "function": "slugify",
     "spec": "slugify(text) returns text in lowercase with every run of characters that are not a-z or 0-9 replaced by a single '-', and no '-' at either end",
     "cases": [[["Hello, World!"], "hello-world"], [["  Many   spaces "], "many-spaces"],
               [["already-a-slug"], "already-a-slug"], [["Ünïcode & Co"], "n-code-co"], [["!!!"], ""]]},
    {"module": "durations", "function": "parse_duration",
     "spec": "parse_duration(text) takes a string such as '1h30m', '45s' or '2h5s', made of one or more <integer><unit> parts with units h, m and s, and returns the total number of seconds as an int; any other string raises ValueError",
     "cases": [[["1h30m"], 5400], [["45s"], 45], [["2h5s"], 7205], [["10m"], 600],
               [["1x"], {"raises": "ValueError"}], [[""], {"raises": "ValueError"}]]},
    {"module": "roman", "function": "roman_to_int",
     "spec": "roman_to_int(numeral) converts an uppercase Roman numeral string (I, V, X, L, C, D, M, with subtractive pairs such as IV and CM) to an int",
     "cases": [[["III"], 3], [["IV"], 4], [["XIV"], 14], [["MCMXCIV"], 1994], [["XL"], 40]]},
    {"module": "words", "function": "word_counts",
     "spec": "word_counts(text) returns a dict mapping each word, lowercased, to how many times it occurs, where a word is a maximal run of the letters a-z and A-Z",
     "cases": [[["The cat and the hat"], {"the": 2, "cat": 1, "and": 1, "hat": 1}],
               [["Go, go, GO!"], {"go": 3}], [[""], {}]]},
    {"module": "chunks", "function": "chunk",
     "spec": "chunk(items, size) splits the list items into consecutive lists of length size, the last one possibly shorter, and raises ValueError when size is less than 1",
     "cases": [[[[1, 2, 3, 4, 5], 2], [[1, 2], [3, 4], [5]]], [[[1, 2, 3], 3], [[1, 2, 3]]],
               [[[], 2], []], [[[1], 0], {"raises": "ValueError"}]]},
    {"module": "dedupe", "function": "dedupe",
     "spec": "dedupe(items) returns a new list holding the items of the list items with later duplicates removed, keeping the order of first occurrence",
     "cases": [[[[3, 1, 3, 2, 1]], [3, 1, 2]], [[["a", "b", "a"]], ["a", "b"]], [[[]], []]]},
    {"module": "stats", "function": "median",
     "spec": "median(values) returns the median of a non-empty list of numbers as a float (the mean of the two middle values when the count is even), and raises ValueError for an empty list",
     "cases": [[[[3, 1, 2]], 2.0], [[[4, 1, 3, 2]], 2.5], [[[7]], 7.0], [[[]], {"raises": "ValueError"}]]},
    {"module": "rle", "function": "run_length",
     "spec": "run_length(text) encodes each run of a repeated character as the character followed by the run's length, so 'aaab' becomes 'a3b1'",
     "cases": [[["aaab"], "a3b1"], [["abc"], "a1b1c1"], [[""], ""], [["zzzzzzzzzzzz"], "z12"]]},
    {"module": "intervals", "function": "merge_intervals",
     "spec": "merge_intervals(intervals) takes a list of [start, end] pairs and returns the merged, sorted list of [start, end] pairs, where intervals that overlap or touch (end equals the next start) are merged",
     "cases": [[[[[1, 3], [2, 6], [8, 10]]], [[1, 6], [8, 10]]], [[[[5, 7], [1, 5]]], [[1, 7]]],
               [[[]], []], [[[[1, 2], [3, 4]]], [[1, 2], [3, 4]]]]},
    {"module": "names", "function": "initials",
     "spec": "initials(name) returns the uppercase first letter of each space-separated word of name, each followed by a period, with no spaces, so 'ada lovelace' becomes 'A.L.'",
     "cases": [[["ada lovelace"], "A.L."], [["Grace Brewster Hopper"], "G.B.H."], [["  alan   turing "], "A.T."]]},
    {"module": "flatten", "function": "flatten",
     "spec": "flatten(nested) returns a flat list of the non-list items of a list that may contain lists nested to any depth, in left-to-right order",
     "cases": [[[[1, [2, [3, 4]], 5]], [1, 2, 3, 4, 5]], [[[[], [[]]]], []], [[["a", ["b"]]], ["a", "b"]]]},
    {"module": "cases", "function": "to_snake",
     "spec": "to_snake(name) converts a CamelCase or camelCase identifier to snake_case by putting '_' before each uppercase letter that is not the first character and lowercasing everything",
     "cases": [[["HelloWorld"], "hello_world"], [["camelCase"], "camel_case"], [["Simple"], "simple"],
               [["aBC"], "a_b_c"]]},
]


# ---- writing tasks ----------------------------------------------------------

WRITING_TEXTS = [
    "Give me three practical tips for {topic}, as exactly three bullet points.",
    "What are three mistakes people make with {topic}? Answer with exactly three bullet points.",
    "Name three things to prepare before {topic}, as exactly three bullet points.",
]


# ---- file-work tasks --------------------------------------------------------

TOOL_VARIANTS = ["total", "find", "extract", "count", "best"]

ITEMS = ["apples", "bolts", "cables", "drills", "envelopes", "flasks", "gloves", "hinges"]
CODE_WORDS = ["kiwi-4821", "lark-7302", "moss-1956", "newt-6640", "opal-2287", "pike-9014"]
FIRST_NAMES = ["Ada", "Bruno", "Chen", "Dalia", "Emre", "Farah", "Goran", "Hana", "Ivo", "Jun"]


def tool_task(variant, rng, prefix):
    """One multi-step file task: the files it needs, what the person asks,
    and the end state that shows it done. Every path sits under prefix, so
    the three instances of a family never share data."""
    files = {}
    if variant == "total":
        total = 0
        for region in rng.sample(["north", "south", "east", "west", "central"], 3):
            lines = ["item,amount"]
            for item in rng.sample(ITEMS, 3):
                amount = rng.randint(3, 120)
                total += amount
                lines.append(f"{item},{amount}")
            files[f"{prefix}/ledger/{region}.csv"] = "\n".join(lines) + "\n"
        text = (f"The CSV files under {prefix}/ledger/ each have an amount column. "
                f"Add up every amount in all of them and write the total, as a plain number, to a file named total.txt.")
        expect = {"kind": "output_number", "name": "total.txt", "value": total}
    elif variant == "find":
        word = rng.choice(CODE_WORDS)
        names = rng.sample(["alpha", "bravo", "delta", "echo", "golf", "hotel"], 4)
        target = rng.choice(names)
        for n in names:
            body = [f"Notes kept on {n}.", "Nothing urgent here.", "Check again next week."]
            if n == target:
                body.insert(1, f"The reference code is {word}.")
            files[f"{prefix}/notes/{n}.txt"] = "\n".join(body) + "\n"
        text = (f"One of the files under {prefix}/notes/ mentions {word}. "
                f"Find it and write just that file's name to a file named found.txt.")
        expect = {"kind": "output_names_file", "name": "found.txt", "value": f"{target}.txt"}
    elif variant == "extract":
        lines, wanted = [], []
        for i in range(8):
            if rng.random() < 0.5:
                line = f"TODO {rng.choice(['call', 'email', 'fix', 'order', 'pay'])} {rng.choice(ITEMS)} #{i + 1}"
                wanted.append(line)
            else:
                line = f"DONE {rng.choice(['call', 'email', 'fix', 'order', 'pay'])} {rng.choice(ITEMS)} #{i + 1}"
            lines.append(line)
        if not wanted:
            lines[0] = "TODO check " + ITEMS[0] + " #1"
            wanted = [lines[0]]
        files[f"{prefix}/todo.txt"] = "\n".join(lines) + "\n"
        text = (f"Copy every line of {prefix}/todo.txt that starts with 'TODO' into a file named open.txt, "
                f"in the same order, one per line.")
        expect = {"kind": "output_lines", "name": "open.txt", "value": wanted}
    elif variant == "count":
        counts = {}
        for name in sorted(rng.sample(["api", "batch", "cron", "db", "edge", "front"], 3)):
            n = rng.randint(2, 7)
            files[f"{prefix}/logs/{name}.log"] = "\n".join(f"{name} event {i + 1}" for i in range(n)) + "\n"
            counts[f"{name}.log"] = n
        text = (f"For each file under {prefix}/logs/, count its lines. Write one line per file, "
                f"'<file name>: <count>', sorted by file name, to a file named counts.txt.")
        expect = {"kind": "output_counts", "name": "counts.txt", "value": counts}
    elif variant == "best":
        people = rng.sample(FIRST_NAMES, 6)
        scores = rng.sample(range(40, 99), 6)
        for i, team in enumerate(["red", "blue"]):
            rows = ["name,score"] + [f"{people[j]},{scores[j]}" for j in range(i * 3, i * 3 + 3)]
            files[f"{prefix}/scores/{team}.csv"] = "\n".join(rows) + "\n"
        best = people[scores.index(max(scores))]
        text = (f"The CSV files under {prefix}/scores/ list people and scores. "
                f"Write the name of the person with the highest score across all of them to a file named best.txt.")
        expect = {"kind": "output_word", "name": "best.txt", "value": best}
    else:
        raise ValueError(variant)
    return files, text, expect


# ---- the corpus -------------------------------------------------------------

def filler_blocks():
    """The unrelated conversation that separates two instances: #1153's
    blocks, from the replication corpus's recorded filler chats."""
    chats = []
    with open(REPLICATION / "conversations.jsonl") as f:
        for line in f:
            chat = json.loads(line)
            if chat["id"].startswith("filler") and len(chat.get("messages", [])) >= 8:
                chats.append(chat["messages"][:8])
    chats.sort(key=lambda m: m[0]["content"])
    blocks = []
    for i in range(0, len(chats) - FILLER_CHATS + 1, FILLER_CHATS):
        block = []
        for chat in chats[i:i + FILLER_CHATS]:
            for j in range(0, len(chat), 2):
                block.append([{"role": chat[j]["role"], "content": chat[j]["content"]},
                              {"role": chat[j + 1]["role"], "content": chat[j + 1]["content"]}])
        if len(block) * 2 == DISTANCE_MESSAGES:
            blocks.append(block)
        if len(blocks) == FILLER_BLOCKS:
            break
    return blocks


def slug(domain):
    return "-".join(domain.replace("-", " ").split())


def live(content, instance, task, correction=False, probe=None):
    """One live turn: what the person sends, the check that shows its task
    done, and, on a probe, the rule the probe reads."""
    return {"kind": "live", "content": content, "instance": instance, "task": task,
            "correction": correction, "probe": probe}


def block(index):
    return {"kind": "block", "index": index}


def instances(kind, index, rng):
    """The three instances of one family: their texts, task checks and the
    files the workspace starts with."""
    domain = DOMAINS[index % len(DOMAINS)]
    files = {}
    texts, tasks = [], []
    if kind == "code":
        for i in range(3):
            fn = FUNCTIONS[(index + 5 * i) % len(FUNCTIONS)]
            texts.append(f"For my {domain} scripts, write a Python module {fn['module']}.py with a function "
                         f"{fn['spec']}.")
            tasks.append({"kind": "module_tests", "module": fn["module"], "function": fn["function"],
                          "cases": fn["cases"]})
    elif kind == "writing":
        word = max(domain.split(), key=len)
        for i in range(3):
            texts.append(WRITING_TEXTS[i].format(topic=domain))
            tasks.append({"kind": "all_of", "of": [{"kind": "bullet_count", "value": 3},
                                                   {"kind": "mentions", "value": word}]})
    elif kind == "tools":
        for i in range(3):
            variant = TOOL_VARIANTS[(index + i) % len(TOOL_VARIANTS)]
            data, text, expect = tool_task(variant, rng, f"job{i + 1}")
            files.update(data)
            texts.append(text)
            tasks.append(expect)
    elif kind == "continuation":
        parts = rng.sample(PART_NAMES, 4)
        path = f"guide-{slug(domain)}.md"
        texts.append(f"Let's write a guide on {domain} together, in {path}. It has four parts, written in this "
                     f"order: {parts[0]}, {parts[1]}, {parts[2]} and {parts[3]}. Each part is a '## ' heading "
                     f"naming the part, then a short paragraph. Write part {parts[0]} now.")
        texts.append("Let's carry on with the guide we started: write its next part.")
        texts.append("Carry on with the guide again, please: write the next part.")
        for _ in range(3):
            tasks.append({"kind": "next_part", "path": path, "parts": parts})
    else:
        raise ValueError(kind)
    return domain, files, texts, tasks


def family(fid, kind, group, index, rule_id, new_rule_id, rng, new_statement=None):
    domain, files, texts, tasks = instances(kind, index, rng)
    steps = []
    if group == "control":
        forbidden = {"kind": "not", "of": {"kind": "any_of", "of": [RULES[r]["check"] for r in FORBIDDEN[kind]]}}
        for i in range(3):
            if i:
                steps.append(block(i - 1))
            steps.append(live(texts[i], i + 1, tasks[i], probe={"rule": forbidden}))
    else:
        rule = RULES[rule_id]
        again, again_task = texts[0], tasks[0]
        if kind == "continuation":
            # The correction turn rewrites the part just written, so its
            # task names that part rather than the next missing one.
            again = "Write that part again, that way."
            again_task = dict(tasks[0], part=0)
        steps.append(live(texts[0], 1, tasks[0]))
        steps.append(live(f"{rule['statement']} {again}", 1, again_task, correction=True))
        steps.append(block(0))
        if group == "primary":
            steps.append(live(texts[1], 2, tasks[1], probe={"rule": rule["check"]}))
            steps.append(block(1))
            steps.append(live(texts[2], 3, tasks[2], probe={"rule": rule["check"]}))
        else:
            new_rule = RULES[new_rule_id]
            steps.append(live(texts[1], 2, tasks[1]))
            steps.append(block(1))
            steps.append(live(f"{new_statement} {texts[2]}", 3, tasks[2], correction=True,
                              probe={"rule": {"kind": "all_of", "of": [new_rule["check"],
                                                                       {"kind": "not", "of": rule["check"]}]}}))
    return {"id": fid, "kind": kind, "group": group, "domain": domain, "rule": rule_id,
            "new_rule": new_rule_id, "files": files, "steps": steps}


def build():
    rng = random.Random(SEED)
    families = []
    index = 0
    for kind in KINDS:
        letter = LETTERS[kind]
        n = 0
        for i in range(PRIMARY_PER_KIND):
            n += 1
            rules = PRIMARY_RULES[kind]
            families.append(family(f"{letter}{n:02d}", kind, "primary", index, rules[i % len(rules)], None, rng))
            index += 1
        for i in range(CONTROL_PER_KIND):
            n += 1
            families.append(family(f"{letter}{n:02d}", kind, "control", index, None, None, rng))
            index += 1
        for i in range(REVERSAL_PER_KIND):
            n += 1
            old, new, statement = REVERSALS[kind][i % len(REVERSALS[kind])]
            families.append(family(f"{letter}{n:02d}", kind, "reversal", index, old, new, rng, statement))
            index += 1
    return {
        "seed": SEED,
        "window": WINDOW,
        "distance_messages": DISTANCE_MESSAGES,
        "blocks": filler_blocks(),
        "families": families,
    }


def main():
    corpus = build()
    (HERE / "corpus.json").write_text(json.dumps(corpus, indent=1, ensure_ascii=False) + "\n")
    groups, probes, live_turns = {}, 0, 0
    for f in corpus["families"]:
        groups[f["group"]] = groups.get(f["group"], 0) + 1
        probes += sum(1 for s in f["steps"] if s.get("probe"))
        live_turns += sum(1 for s in f["steps"] if s["kind"] == "live")
    print(json.dumps({"families": len(corpus["families"]), "groups": groups, "probes": probes,
                      "live_turns": live_turns, "window": WINDOW, "distance": DISTANCE_MESSAGES}))


if __name__ == "__main__":
    main()
