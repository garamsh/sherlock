"""Build the #1153 corpus: fifty task families of three instances each.

A family is one kind of task the person asks for three times, about one
subject. On the first instance the person corrects one aspect of the work:
the task turn, then a second turn that says what to do from now on and asks
for the same task again, so the turn that applies the rule is a turn of that
task. The second and third instances ask the same kind of task again, and
the probe is whether the correction still holds without being restated. Five
families never carry a correction (the control: no arm invents a rule), and
five change the rule again on the third instance (the reversal: the new
instruction must win).

    python3 research/experience-memory/corpus.py

The output is deterministic: running this again writes byte-identical files.
"""

import json
from pathlib import Path

HERE = Path(__file__).resolve().parent
REPLICATION = HERE.parent / "mid-memory-rag" / "evidence" / "replication"

# The rolling window every arm runs with, and the messages of unrelated
# conversation that separate two instances of a family. The correction has
# left the window by the time the next instance runs, which is what the
# study measures; the distance is recorded in the corpus and stated in the
# preregistration.
WINDOW = 32
DISTANCE_MESSAGES = 40
FILLER_CHATS = 5
FILLER_BLOCKS = 2

# The subject each family is about. One per family, so that two families
# never ask about the same thing.
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
PEOPLE = ["Mara", "Tomas", "Ines", "Kofi", "Anya", "Ravi", "Lena", "Oscar"]

# The marker every workspace file carries, so a check can tell that the
# agent read the file rather than guessed.
FILE_MARKER = "kiwi-4821"

# The corrections the study gives, and the check that reads each one back.
# Every rule states what must hold in the reply, the files, or the tool
# calls, so the scorer never judges prose.
RULES = {
    "status_prefix": {
        "aspect": "output format",
        "statement": "Good, but from now on always begin your reply with 'STATUS:'.",
        "check": {"kind": "prefix", "value": "STATUS:"},
    },
    "tl_dr_prefix": {
        "aspect": "output format",
        "statement": "Keep doing it that way from now on: start every reply with a line beginning 'TL;DR:'.",
        "check": {"kind": "prefix", "value": "TL;DR:"},
    },
    "juniper_suffix": {
        "aspect": "output format",
        "statement": "Do it like this from now on: sign every reply with '— Juniper'.",
        "check": {"kind": "suffix", "value": "— Juniper"},
    },
    "lowercase": {
        "aspect": "output format",
        "statement": "That is right, but from now on write every reply in lowercase only.",
        "check": {"kind": "all_lowercase"},
    },
    "uppercase": {
        "aspect": "output format",
        "statement": "That is right, but from now on write every reply in uppercase only.",
        "check": {"kind": "all_uppercase"},
    },
    "roman_list": {
        "aspect": "output format",
        "statement": "Use this numbering from now on: number every list you write with Roman numerals.",
        "check": {"kind": "roman_list"},
    },
    "no_bullets": {
        "aspect": "output format",
        "statement": "Not like that any more — from now on answer in plain prose, with no bullet lists.",
        "check": {"kind": "no_bullets"},
    },
    "tab_indent": {
        "aspect": "output format",
        "statement": "Change one thing and keep it: from now on indent code with tabs, never with spaces.",
        "check": {"kind": "tab_indent"},
    },
    "space_indent": {
        "aspect": "output format",
        "statement": "Change one thing and keep it: from now on indent code with spaces, never with tabs.",
        "check": {"kind": "space_indent"},
    },
    "kebab_files": {
        "aspect": "naming rule",
        "statement": "Rename things that way from now on: name every file you save in kebab-case.",
        "check": {"kind": "kebab_files"},
    },
    "snake_files": {
        "aspect": "naming rule",
        "statement": "Use this naming from now on: name every file you save in snake_case.",
        "check": {"kind": "snake_files"},
    },
    "caveat_first": {
        "aspect": "order of steps",
        "statement": "Order it this way from now on: put the caveat before the recommendation.",
        "check": {"kind": "order_words", "first": "caveat", "second": "recommend"},
    },
    "recommend_first": {
        "aspect": "order of steps",
        "statement": "Order it the other way from now on: put the recommendation before the caveat.",
        "check": {"kind": "order_words", "first": "recommend", "second": "caveat"},
    },
    "list_before_read": {
        "aspect": "tool choice",
        "statement": "Work like that from now on: check the file list before you read a file.",
        "check": {"kind": "all_of", "of": [
            {"kind": "tool_before", "first": "file_list", "second": "file_read"},
            {"kind": "contains", "value": FILE_MARKER},
        ]},
    },
    "read_before_list": {
        "aspect": "tool choice",
        "statement": "Work like that from now on: read the file before you list the directory.",
        "check": {"kind": "all_of", "of": [
            {"kind": "tool_before", "first": "file_read", "second": "file_list"},
            {"kind": "contains", "value": FILE_MARKER},
        ]},
    },
}

# The kinds of task the person asks for, each with the three instances of
# one family and the rules that can be corrected in it.
TASKS = {
    "list": {
        "rules": ["status_prefix", "tl_dr_prefix", "juniper_suffix", "lowercase", "uppercase", "roman_list", "no_bullets"],
        "texts": [
            "List three practical tips for {topic}.",
            "Give me three tips for {topic}.",
            "What are three things worth knowing about {topic}?",
        ],
    },
    "code": {
        "rules": ["status_prefix", "tl_dr_prefix", "juniper_suffix", "lowercase", "tab_indent", "space_indent"],
        "texts": [
            "Write a short Python function that keeps track of {topic}.",
            "Show me a small Python function that summarizes {topic}.",
            "Write a Python function that ranks {topic} by priority.",
        ],
    },
    "file": {
        "rules": ["status_prefix", "tl_dr_prefix", "juniper_suffix", "lowercase", "uppercase", "kebab_files", "snake_files"],
        "texts": [
            "Save a short note about {topic} in my workspace.",
            "Write a note about {topic} into a file in my workspace.",
            "Put a short note about {topic} in a file for me.",
        ],
    },
    "files": {
        "rules": ["status_prefix", "tl_dr_prefix", "juniper_suffix", "list_before_read", "read_before_list"],
        "texts": [
            "What is in my workspace, and what does {file} say?",
            "List what is in my workspace and read {file} for me.",
            "Show me the files in my workspace and the contents of {file}.",
        ],
    },
    "advice": {
        "rules": ["status_prefix", "tl_dr_prefix", "juniper_suffix", "lowercase", "uppercase", "roman_list", "no_bullets", "caveat_first", "recommend_first"],
        "texts": [
            "Recommend a plan for {topic} and mention any caveat.",
            "What do you recommend for {topic}, and what is the caveat?",
            "Suggest a plan for {topic}, noting the caveat.",
        ],
    },
    "note": {
        "rules": ["status_prefix", "tl_dr_prefix", "juniper_suffix", "lowercase", "uppercase", "no_bullets"],
        "texts": [
            "Write a short note to {person} about {topic}.",
            "Draft a brief message to {person} about {topic}.",
            "Write a few lines to {person} about {topic}.",
        ],
    },
}

# The forty primary families, in order: the kind of task and the rule the
# person corrects in it.
PRIMARY = [
    ("list", "status_prefix"), ("list", "juniper_suffix"), ("list", "lowercase"),
    ("list", "uppercase"), ("list", "roman_list"), ("list", "no_bullets"),
    ("list", "tl_dr_prefix"),
    ("code", "status_prefix"), ("code", "juniper_suffix"), ("code", "lowercase"),
    ("code", "tab_indent"), ("code", "space_indent"), ("code", "tl_dr_prefix"),
    ("file", "status_prefix"), ("file", "juniper_suffix"), ("file", "lowercase"),
    ("file", "uppercase"), ("file", "kebab_files"), ("file", "snake_files"),
    ("file", "tl_dr_prefix"),
    ("files", "status_prefix"), ("files", "juniper_suffix"), ("files", "list_before_read"),
    ("files", "read_before_list"), ("files", "tl_dr_prefix"),
    ("advice", "status_prefix"), ("advice", "juniper_suffix"), ("advice", "lowercase"),
    ("advice", "uppercase"), ("advice", "roman_list"), ("advice", "no_bullets"),
    ("advice", "caveat_first"), ("advice", "recommend_first"), ("advice", "tl_dr_prefix"),
    ("note", "status_prefix"), ("note", "juniper_suffix"), ("note", "lowercase"),
    ("note", "uppercase"), ("note", "no_bullets"), ("note", "tl_dr_prefix"),
]

# The control families: no correction is ever given, so the probe is that
# no arm invents one. The forbidden markers are rules the corpus states and
# an ordinary answer does not carry.
CONTROL = ["list", "code", "file", "files", "advice"]
FORBIDDEN = [
    {"kind": "prefix", "value": "STATUS:"},
    {"kind": "prefix", "value": "TL;DR:"},
    {"kind": "suffix", "value": "— Juniper"},
]

# The reversal families: the rule the person gives on the first instance,
# and the rule that replaces it on the third.
REVERSAL = [
    ("list", "status_prefix", "tl_dr_prefix"),
    ("note", "lowercase", "uppercase"),
    ("file", "kebab_files", "snake_files"),
    ("code", "tab_indent", "space_indent"),
    ("advice", "caveat_first", "recommend_first"),
]


def filler_blocks():
    """The unrelated conversation that separates two instances of a family.

    It comes from the replication corpus's filler chats, so the study's
    filler is the same recorded conversation the earlier studies used. Each
    block is a list of user and assistant pairs, seeded as completed turns.
    """
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
    """A file name for a family's subject."""
    return "-".join(domain.split()) + "-notes.txt"


def instance_text(task, index, family_index, domain):
    """The person's request for one instance of one family."""
    spec = TASKS[task]
    return spec["texts"][index].format(topic=domain, file=slug(domain),
                                        person=PEOPLE[family_index % len(PEOPLE)])


def live(content, correction=False, probe=None, instance=None):
    """One live turn: what the person sends and what the turn is probed for."""
    return {"kind": "live", "content": content, "correction": correction, "probe": probe, "instance": instance}


def block(index):
    """A block of unrelated conversation, seeded as completed turns."""
    return {"kind": "block", "index": index, "content": "", "correction": False, "probe": None, "instance": None}


def family(index, kind, task, rule_id, new_rule_id):
    """One family: three instances, the correction on the first, and the
    check that reads each instance back."""
    rule = RULES[rule_id] if rule_id else None
    new_rule = RULES[new_rule_id] if new_rule_id else None
    domain = DOMAINS[index % len(DOMAINS)]
    texts = [instance_text(task, i, index, domain) for i in range(3)]
    steps = []

    if kind == "control":
        # No rule is ever given, so every instance is a probe for an
        # invented one.
        for i, text in enumerate(texts):
            steps.append(live(text, probe={"check": {"kind": "not", "of": {"kind": "any_of", "of": FORBIDDEN}}}, instance=i + 1))
    else:
        steps.append(live(texts[0], instance=1))
        steps.append(live(f"{rule['statement']} {texts[0]}", correction=True, instance=1))
        steps.append(block(0))
        steps.append(live(texts[1], probe={"check": rule["check"]}, instance=2))
        steps.append(block(1))
        if kind == "reversal":
            steps.append(live(f"{new_rule['statement']} {texts[2]}", correction=True,
                              probe={"check": {"kind": "all_of", "of": [new_rule["check"], {"kind": "not", "of": rule["check"]}]}},
                              instance=3))
        else:
            steps.append(live(texts[2], probe={"check": rule["check"]}, instance=3))

    files = {slug(domain): f"{FILE_MARKER}\nNotes on {domain}.\n"} if task == "files" else {}

    return {
        "id": f"F{index + 1:02d}",
        "family": kind,
        "task": task,
        "domain": domain,
        "rule": rule_id,
        "new_rule": new_rule_id,
        "files": files,
        "steps": steps,
    }


def build():
    families = []
    index = 0
    for task, rule_id in PRIMARY:
        families.append(family(index, "primary", task, rule_id, None))
        index += 1
    for task in CONTROL:
        families.append(family(index, "control", task, None, None))
        index += 1
    for task, rule_id, new_rule_id in REVERSAL:
        families.append(family(index, "reversal", task, rule_id, new_rule_id))
        index += 1
    return {
        "window": WINDOW,
        "distance_messages": DISTANCE_MESSAGES,
        "seed": 1153,
        "blocks": filler_blocks(),
        "families": families,
    }


def main():
    corpus = build()
    out = HERE / "corpus.json"
    out.write_text(json.dumps(corpus, indent=1, ensure_ascii=False) + "\n")
    kinds = {}
    probes = 0
    for f in corpus["families"]:
        kinds[f["family"]] = kinds.get(f["family"], 0) + 1
        probes += sum(1 for s in f["steps"] if s.get("probe"))
    print(json.dumps({"families": len(corpus["families"]), "kinds": kinds, "probes": probes,
                      "window": WINDOW, "distance": DISTANCE_MESSAGES}))


if __name__ == "__main__":
    main()
