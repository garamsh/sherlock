"""Dataset: 100 sessions x 10 turns with planted facts, corrections, probes.

Facts are stated in natural varied phrasings. Corrections rephrase the fact
semantically without lexical overlap, so only a real semantic judgment (real
embedder or real decider model) can link them.

The probe set asks for the current value of each corrected topic.
"""
from __future__ import annotations
import random
from dataclasses import dataclass


@dataclass
class Fact:
    topic: str
    statement: str
    salience: float


# Topic pools: (topic, initial phrasings, correction phrasings, probe, expected_new, expected_old)
TOPICS = [
    ("drink",
     ["I drink coffee every morning", "Coffee is my morning ritual", "I start the day with coffee"],
     ["Actually I've switched to tea these days", "I mostly have tea in the mornings now"],
     "What does Lisa drink in the morning?", "tea", "coffee"),
    ("editor",
     ["I use VS Code for everything", "VS Code is my editor", "I write code in VS Code"],
     ["I've moved to Neovim lately", "These days I edit in Neovim"],
     "What editor does Lisa use?", "neovim", "vs code"),
    ("language",
     ["I prefer Python for new services", "Python is my default for new work", "New services should be Python"],
     ["Lately I reach for Go on anything new", "For new systems I default to Go now"],
     "What language does Lisa prefer for new work?", "go", "python"),
    ("commute",
     ["I bike to work", "I commute by bike", "I ride my bike to the office"],
     ["I take the train now", "These days I ride the train in"],
     "How does Lisa get to work?", "train", "bike"),
    ("database",
     ["We run Postgres for production", "Production is Postgres", "The prod database is Postgres"],
     ["We migrated production to MySQL", "Prod runs on MySQL now"],
     "What database does production use?", "mysql", "postgres"),
    ("indent",
     ["I use tabs for indentation", "Tabs over spaces, always", "Indent with tabs"],
     ["I've switched to four spaces", "I now indent with four spaces"],
     "How does Lisa indent code?", "four spaces", "tabs"),
    ("pet",
     ["My dog is named Pixel", "Pixel is my dog", "I have a dog, Pixel"],
     ["We adopted a cat named Mono", "The new cat is called Mono"],
     "What pet does Lisa have?", "cat", "dog"),
    ("manager",
     ["Otto is my manager", "I report to Otto", "My manager is Otto"],
     ["I now report to Maria", "Maria is my manager these days"],
     "Who is Lisa's manager?", "maria", "otto"),
    ("tracing",
     ["We trace with OpenTelemetry", "Tracing is OpenTelemetry", "We use OTel for traces"],
     ["We moved tracing to Datadog", "Traces live in Datadog now"],
     "What does the team trace with?", "datadog", "opentelemetry"),
    ("reviews",
     ["Reviews are required for merges", "Every merge needs a review", "No merge without review"],
     ["We now allow self-merge for small fixes", "Small fixes can skip review now"],
     "What is the merge policy?", "self-merge", "review"),
]


def build(seed: int = 11, sessions: int = 100, turns_per_session: int = 10):
    """Build (turns, probes). turns: list of (text, [planted fact topics]).
    probes: list of (after_turn_index, question, expected_new, expected_old).
    """
    random.seed(seed)
    turns: list[tuple[str, list[str]]] = []
    probes = []
    # Schedule: for each topic, an initial statement early, a correction later.
    topic_initial_turn = {}
    topic_correction_turn = {}
    t = 0
    for topic, initials, corrections, probe, new, old in TOPICS:
        init_at = t
        for phr in initials:
            turns.append((phr, [topic]))
            t += 1
        topic_initial_turn[topic] = init_at
    # Fill to half the sessions with neutral chatter, then corrections.
    neutral = ["How was your weekend?", "Anything on your plate today?",
               "Let's sync tomorrow.", "The build is green.", "I read a good post."]
    while t < sessions * turns_per_session // 2:
        turns.append((random.choice(neutral), []))
        t += 1
    for topic, initials, corrections, probe, new, old in TOPICS:
        for phr in corrections:
            turns.append((phr, [topic]))
            t += 1
        topic_correction_turn[topic] = t
        probes.append((t, probe, new, old))
    # Fill the rest with chatter.
    while t < sessions * turns_per_session:
        turns.append((random.choice(neutral), []))
        t += 1
    return turns, probes
