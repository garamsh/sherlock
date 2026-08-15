"""Turn stream and probes for the memory-creation comparison."""
from __future__ import annotations
import random

# 16 subjects x 20 categories; the unprobed surplus is what makes the store large.
PERSONS = ["Lisa", "Otto", "Maria", "Sam", "Nina", "Raj", "Yuki", "Chen",
           "Ida", "Bruno", "Alma", "Kofi", "Vera", "Milo", "Sena", "Tariq"]

# (category, probe question template, [values], initial templates, correction templates)
CATEGORIES = [
    ("drink", "What does {p} drink in the morning?",
     ["coffee", "tea", "matcha", "espresso", "chai"],
     ["{p} drinks {v} every morning.", "{p}'s morning ritual is {v}."],
     ["{p} has switched to {v} lately.", "These days {p} goes for {v} instead."]),
    ("editor", "Which code editor does {p} use?",
     ["VS Code", "Neovim", "Emacs", "Zed", "IntelliJ"],
     ["{p} writes code in {v}.", "{p}'s editor is {v}."],
     ["{p} moved over to {v} recently.", "{p} now edits everything in {v}."]),
    ("language", "Which programming language does {p} prefer for new work?",
     ["Python", "Go", "Rust", "TypeScript", "Elixir"],
     ["{p} prefers {v} for new services.", "For new work {p} reaches for {v}."],
     ["{p} defaults to {v} on anything new now.", "{p} has since standardized on {v}."]),
    ("commute", "How does {p} get to work?",
     ["bike", "train", "bus", "car", "scooter"],
     ["{p} commutes by {v}.", "{p} takes the {v} to the office."],
     ["{p} gets in by {v} these days.", "{p} switched the commute to {v}."]),
    ("database", "Which database does {p}'s team run in production?",
     ["Postgres", "MySQL", "CockroachDB", "SQLite", "MongoDB"],
     ["{p}'s team runs {v} in production.", "Production for {p} is {v}."],
     ["{p}'s team migrated production to {v}.", "{p} moved the prod database to {v}."]),
    ("pet", "What pet does {p} have?",
     ["a dog", "a cat", "a rabbit", "a parrot", "a turtle"],
     ["{p} has {v} at home.", "{p} keeps {v}."],
     ["{p} adopted {v} recently.", "{p} now has {v} instead."]),
    # Values avoid the PERSONS names, which would let unrelated memories match.
    ("manager", "Who does {p} report to?",
     ["Diane", "Felix", "Priya", "Karin", "Tomas"],
     ["{p} reports to {v}.", "{p}'s manager is {v}."],
     ["{p} was moved under {v}.", "{p} now reports into {v}."]),
    ("tracing", "What does {p}'s team use for tracing?",
     ["OpenTelemetry", "Datadog", "Jaeger", "Honeycomb", "Zipkin"],
     ["{p}'s team traces with {v}.", "Tracing for {p} runs on {v}."],
     ["{p}'s team moved tracing to {v}.", "{p} replaced the tracing stack with {v}."]),
    ("indent", "How does {p} indent code?",
     ["tabs", "two spaces", "four spaces", "eight spaces", "mixed indentation"],
     ["{p} indents with {v}.", "{p} formats code using {v}."],
     ["{p} has switched to {v}.", "{p} reformatted everything to {v}."]),
    ("cloud", "Which cloud provider does {p} deploy on?",
     ["AWS", "GCP", "Azure", "Fly.io", "Hetzner"],
     ["{p} deploys on {v}.", "{p}'s infrastructure sits on {v}."],
     ["{p} migrated the deployment to {v}.", "{p} runs everything on {v} now."]),
    ("os", "Which operating system does {p} work on?",
     ["Ubuntu", "macOS", "Fedora", "Arch", "Windows"],
     ["{p} works on {v}.", "{p}'s machine runs {v}."],
     ["{p} reinstalled onto {v}.", "{p} switched the laptop to {v}."]),
    ("gym", "What exercise does {p} do?",
     ["swimming", "climbing", "running", "yoga", "rowing"],
     ["{p} does {v} after work.", "{p} keeps fit with {v}."],
     ["{p} took up {v} instead.", "{p} dropped that and does {v} now."]),
    ("music", "What music does {p} listen to while working?",
     ["jazz", "ambient", "techno", "classical", "lo-fi"],
     ["{p} listens to {v} while working.", "{p} codes to {v}."],
     ["{p} moved on to {v}.", "{p} plays {v} these days instead."]),
    ("framework", "Which web framework does {p} build with?",
     ["Django", "Next.js", "Rails", "Phoenix", "SvelteKit"],
     ["{p} builds with {v}.", "{p}'s web stack is {v}."],
     ["{p} rewrote the frontend in {v}.", "{p} adopted {v} for the web tier."]),
    ("testing", "What testing approach does {p} follow?",
     ["table-driven tests", "property-based tests", "snapshot tests",
      "end-to-end tests", "mutation testing"],
     ["{p} writes {v}.", "{p}'s test suite is built on {v}."],
     ["{p} converted the suite to {v}.", "{p} now relies on {v}."]),
    ("ci", "Which CI system does {p} use?",
     ["GitHub Actions", "Buildkite", "CircleCI", "GitLab CI", "Jenkins"],
     ["{p} runs CI on {v}.", "{p}'s pipelines live in {v}."],
     ["{p} moved CI to {v}.", "{p} rebuilt the pipelines on {v}."]),
    ("terminal", "Which terminal does {p} use?",
     ["Alacritty", "iTerm2", "Kitty", "WezTerm", "GNOME Terminal"],
     ["{p} uses {v} as a terminal.", "{p}'s terminal is {v}."],
     ["{p} replaced the terminal with {v}.", "{p} runs {v} now."]),
    ("shell", "Which shell does {p} use?",
     ["bash", "zsh", "fish", "nushell", "elvish"],
     ["{p} uses {v}.", "{p}'s shell is {v}."],
     ["{p} switched the shell to {v}.", "{p} moved to {v}."]),
    ("notes", "Where does {p} keep notes?",
     ["Obsidian", "Notion", "plain text files", "Logseq", "Apple Notes"],
     ["{p} keeps notes in {v}.", "{p} writes notes in {v}."],
     ["{p} migrated the notes to {v}.", "{p} keeps everything in {v} now."]),
    ("timezone", "Which timezone does {p} work from?",
     ["KST", "CET", "PST", "JST", "GMT"],
     ["{p} works from {v}.", "{p} is based in {v}."],
     ["{p} relocated and now works from {v}.", "{p}'s hours moved to {v}."]),
]

# Simultaneously-true facts. Their vocabularies do not overlap CATEGORIES.
COEXIST = [
    ("languages", "Which spoken languages does {p} know?",
     ["Korean", "English", "German"],
     "{p} speaks {v}."),
    ("instrument", "Which instruments does {p} play?",
     ["the cello", "the drums"],
     "{p} plays {v}."),
    ("allergy", "What is {p} allergic to?",
     ["peanuts", "shellfish"],
     "{p} is allergic to {v}."),
]

CHATTER = [
    "How was the weekend?", "The build is green again.", "Let's sync tomorrow.",
    "I read a good post about compilers.", "Standup is moved to 10.",
    "The office coffee machine is broken.", "Nice weather today.",
    "Anything blocking you right now?", "That deploy took forever.",
    "I'll be out Friday afternoon.", "The docs need a refresh.",
    "Someone left a laptop in the room.", "Lunch at the usual place?",
    "The staging environment is flaky.", "Long meeting, no decisions.",
]


class Probe:
    __slots__ = ("kind", "subject", "question", "expect_present",
                 "expect_absent", "correction_text")

    def __init__(self, kind, subject, question, expect_present, expect_absent=(),
                 correction_text=None):
        self.kind = kind                      # corrected | control | coexist
        # Values repeat across subjects, so a probe needs the subject too.
        self.subject = subject
        self.question = question
        self.expect_present = list(expect_present)
        self.expect_absent = list(expect_absent)
        # The exact correction turn; matching by value would catch other subjects.
        self.correction_text = correction_text


def build(seed: int = 7, n_corrected: int = 100, n_control: int = 40,
          n_distractor: int = 120, chatter_ratio: float = 0.35):
    """Return (turns, probes).

    turns: list of (text, is_fact). Facts are the statements a memory system
    should consider storing; chatter is noise that should stay out.

    The stream order is: every initial statement and every distractor first,
    then chatter, then every correction. A correction therefore lands with
    hundreds of items already in the store, so no recency window contains the
    fact it contradicts.
    """
    rnd = random.Random(seed)
    pairs = [(p, c) for p in PERSONS for c in CATEGORIES]
    rnd.shuffle(pairs)

    corrected = pairs[:n_corrected]
    control = pairs[n_corrected:n_corrected + n_control]
    distract = pairs[n_corrected + n_control:][:n_distractor]

    initial_turns, correction_turns, probes = [], [], []

    for person, cat in corrected:
        name, question, values, inits, corrs = cat
        old, new = rnd.sample(values, 2)
        initial_turns.append((rnd.choice(inits).format(p=person, v=old), True))
        correction = rnd.choice(corrs).format(p=person, v=new)
        correction_turns.append((correction, True))
        probes.append(Probe("corrected", person, question.format(p=person),
                            [new], [old], correction_text=correction))

    for person, cat in control:
        name, question, values, inits, corrs = cat
        val = rnd.choice(values)
        initial_turns.append((rnd.choice(inits).format(p=person, v=val), True))
        probes.append(Probe("control", person, question.format(p=person), [val]))

    for person, cat in distract:
        name, question, values, inits, corrs = cat
        initial_turns.append(
            (rnd.choice(inits).format(p=person, v=rnd.choice(values)), True))

    for person in PERSONS[:4]:
        for name, question, values, tmpl in COEXIST:
            for v in values:
                initial_turns.append((tmpl.format(p=person, v=v), True))
            probes.append(Probe("coexist", person, question.format(p=person),
                                values))

    rnd.shuffle(initial_turns)
    rnd.shuffle(correction_turns)

    n_chatter = int(len(initial_turns) * chatter_ratio)
    chatter = [(rnd.choice(CHATTER), False) for _ in range(n_chatter)]

    turns = initial_turns + chatter + correction_turns
    return turns, probes
