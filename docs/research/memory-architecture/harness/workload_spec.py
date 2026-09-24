"""Workload: 5 sessions × 10 turns + 20 queries."""
from __future__ import annotations
from dataclasses import dataclass
from typing import Any


@dataclass
class Query:
    id: str
    text: str
    expected_keywords: list[str]
    recency: bool = False
    noise: bool = False
    expect_turn_id: str = ""


@dataclass
class Session:
    id: str
    turns: list[tuple[str, str, dict[str, Any]]]
    queries: list[Query]


SESSIONS: list[Session] = [
    Session(
        id="project-conventions",
        turns=[
            ("user", "Use tabs for indentation, not spaces. Tabs only.", {"salience": 0.6, "kind": "preference"}),
            ("user", "Always run the test suite before committing any change.", {"salience": 0.6, "kind": "preference"}),
            ("assistant", "Acknowledged. I will use tabs and run tests before commit.", {"salience": 0.0}),
            ("user", "Python style: PEP 8 with 100-char line limit, type hints everywhere.", {"salience": 0.6, "kind": "preference"}),
            ("user", "Initialize a new Python project named 'metrics' with pyproject.toml.", {"salience": 0.7, "kind": "task"}),
            ("assistant", "Initialized the 'metrics' project with pyproject.toml and tests/. Continuing.", {"salience": 0.0}),
            ("user", "Add a README describing the metrics service and how to run it.", {"salience": 0.4, "kind": "task"}),
            ("assistant", "Added README.md with usage and install instructions.", {"salience": 0.0}),
            ("user", "Pin the deps: requests==2.31.0, pydantic==2.5.0.", {"salience": 0.5, "kind": "preference"}),
            ("user", "Anything else you need from me before you finish?", {"salience": 0.0}),
        ],
        queries=[
            Query("pref-indent", "What indentation does the user prefer?", ["tabs"], False, False),
            Query("pref-tests", "Should I always run tests?", ["tests"], False, False),
            Query("pref-style", "What's the Python style guide?", ["PEP 8", "100"], False, False),
            Query("pref-pinning", "Are dependencies pinned?", ["pinned", "requests"], False, False),
            Query("last-action", "What is the current state of the project?", ["README"], True, False, "09"),
        ],
    ),
    Session(
        id="domain-knowledge",
        turns=[
            ("user", "gagent's executor uses setuid to drop into a configured exec UID.", {"salience": 0.7, "kind": "knowledge"}),
            ("user", "The agent holds a capability token; it never executes directly.", {"salience": 0.7, "kind": "knowledge"}),
            ("assistant", "Recorded. setuid + capability token.", {"salience": 0.0}),
            ("user", "The workspace is a separate container; per-request netns isolates exec.", {"salience": 0.6, "kind": "knowledge"}),
            ("user", "Exec children get a scrubbed environment; never inherit the agent env.", {"salience": 0.6, "kind": "knowledge"}),
            ("assistant", "Got it. Scrubbed env, no inheritance.", {"salience": 0.0}),
            ("user", "Resource caps bind untrusted exec; agent tools run without them.", {"salience": 0.5, "kind": "knowledge"}),
            ("user", "Trust class is explicit; never inferred from network axis.", {"salience": 0.5, "kind": "knowledge"}),
            ("user", "Single-account mode marks the workspace non-dumpable so same-account children can't read /proc/<pid>/environ.", {"salience": 0.5, "kind": "knowledge"}),
            ("user", "Anything else worth knowing about the trust model?", {"salience": 0.0}),
        ],
        queries=[
            Query("exec-uid", "How does the executor run code?", ["setuid", "exec UID"], False, False),
            Query("agent-token", "What does the agent hold?", ["capability token"], False, False),
            Query("netns", "How is network isolation done?", ["netns"], False, False),
            Query("env-scrub", "What about subprocess environment?", ["scrubbed", "environment"], False, False),
            Query("trust-class", "How is trust determined?", ["trust class"], False, False),
        ],
    ),
    Session(
        id="task-progress",
        turns=[
            ("user", "Audit the message_sender for the new architecture.", {"salience": 0.6, "kind": "task"}),
            ("assistant", "Starting the audit of message_sender.", {"salience": 0.0}),
            ("user", "Compare against the previous MCP binary and the new script-tool shape.", {"salience": 0.5, "kind": "task"}),
            ("assistant", "Compared both. Note: tests/e2e_github_test.go was deleted in #312.", {"salience": 0.0}),
            ("user", "Map the deletions to the rationale. Why are tests/e2e_github_test.go and cmd/<tool>/ gone?", {"salience": 0.5, "kind": "task"}),
            ("assistant", "Mapped. The MCP servers are gone; the tools are now scripts.", {"salience": 0.0}),
            ("user", "Cross-check against docs/architecture/adr/0018 and the issue tracker.", {"salience": 0.4, "kind": "task"}),
            ("assistant", "Cross-checked. ADR 0018 was revised in #309 to the script-tool model.", {"salience": 0.0}),
            ("user", "What conclusions do you reach?", {"salience": 0.0}),
            ("user", "Write the QA report to /tmp/q4/report.md and summarize.", {"salience": 0.0}),
        ],
        queries=[
            Query("task-start", "What was the task originally?", ["audit", "message_sender"], False, False),
            Query("task-final", "Where did we leave off?", ["report", "QA"], True, False, "09"),
            Query("task-deletions", "What was deleted?", ["e2e_github_test", "deleted"], False, False),
        ],
    ),
    Session(
        id="correction",
        turns=[
            ("user", "Use tabs everywhere.", {"salience": 0.5, "kind": "preference"}),
            ("assistant", "Switched to tabs.", {"salience": 0.0}),
            ("user", "Format the code with the auto-formatter on save.", {"salience": 0.4, "kind": "instruction"}),
            ("assistant", "Auto-formatter enabled.", {"salience": 0.0}),
            ("user", "Actually, do NOT use tabs. Use spaces. Four spaces per indent.", {"salience": 0.7, "kind": "correction"}),
            ("assistant", "Corrected. Switching to four spaces.", {"salience": 0.0}),
            ("user", "Why did you change it? Document the reason in the commit message.", {"salience": 0.3, "kind": "instruction"}),
            ("assistant", "Reason: user preference correction; will document in commit message.", {"salience": 0.0}),
            ("user", "Double-check the auto-formatter is configured for spaces too.", {"salience": 0.5, "kind": "instruction"}),
            ("user", "Done. Move on.", {"salience": 0.0}),
        ],
        queries=[
            Query("correct-tabs", "What did the user correct about indentation?", ["tabs", "spaces", "four"], False, False),
            Query("correct-current", "What is the current indentation rule?", ["spaces", "four"], False, False),
            Query("correct-auto", "Should the auto-formatter use tabs or spaces?", ["spaces", "four"], False, False),
        ],
    ),
    Session(
        id="noise",
        turns=[
            ("user", "How's the weather today?", {"salience": 0.0, "kind": "noise"}),
            ("assistant", "I don't have access to weather data.", {"salience": 0.0}),
            ("user", "Alright. What's your favorite color?", {"salience": 0.0, "kind": "noise"}),
            ("assistant", "I don't have preferences.", {"salience": 0.0}),
            ("user", "Do you like pizza?", {"salience": 0.0, "kind": "noise"}),
            ("assistant", "I don't eat.", {"salience": 0.0}),
            ("user", "What do you think about the new agent design?", {"salience": 0.0, "kind": "noise"}),
            ("assistant", "I let the user decide.", {"salience": 0.0}),
            ("user", "Just chat with me a bit.", {"salience": 0.0, "kind": "noise"}),
            ("assistant", "Sure. Anything specific?", {"salience": 0.0}),
            ("user", "Tell me a joke.", {"salience": 0.0, "kind": "noise"}),
            ("assistant", "Why did the function refuse to call? It was out of scope.", {"salience": 0.0}),
        ],
        queries=[
            Query("noise-weather", "What's the weather?", [], False, True),
            Query("noise-joke", "Tell me a joke.", [], False, True),
        ],
    ),
    Session(
        id="supersession",
        turns=[
            ("user", "For Python projects, I prefer 2-space indentation.", {"salience": 0.7, "kind": "preference"}),
            ("assistant", "Noted. 2-space indent for Python.", {"salience": 0.0}),
            ("user", "Use type hints throughout.", {"salience": 0.6, "kind": "preference"}),
            ("assistant", "Type hints noted.", {"salience": 0.0}),
            ("user", "Actually, change of plan. Use 4-space indent for Python, not 2.", {"salience": 0.8, "kind": "preference-correction"}),
            ("assistant", "Updated. 4-space indent for Python.", {"salience": 0.0}),
            ("user", "Keep type hints. Actually, only on public functions, not internal ones.", {"salience": 0.6, "kind": "preference-correction"}),
            ("assistant", "Public-only type hints.", {"salience": 0.0}),
            ("user", "Now what is my Python indentation preference?", {"salience": 0.0}),
        ],
        queries=[
            Query("sup-final-indent", "What's my Python indentation preference now?", ["4-space", "4"], False, False),
            Query("sup-final-hints", "Should I always add type hints?", ["public"], False, False),
            Query("sup-final-state", "What is the current state of my preferences?", ["4-space", "public"], False, False),
        ],
    ),
    Session(
        id="repetition",
        turns=[
            ("user", "The deployment uses Helm charts. Remember this fact.", {"salience": 0.7, "kind": "fact"}),
            ("user", "Important: deployment uses Helm charts.", {"salience": 0.7, "kind": "fact"}),
            ("user", "Don't forget: deployment uses Helm charts.", {"salience": 0.7, "kind": "fact"}),
            ("user", "Heads up — deployment uses Helm charts.", {"salience": 0.7, "kind": "fact"}),
            ("user", "Reminder: deployment uses Helm charts.", {"salience": 0.7, "kind": "fact"}),
            ("user", "FYI the deployment uses Helm charts.", {"salience": 0.7, "kind": "fact"}),
            ("user", "Heads up — deployment uses Helm.", {"salience": 0.7, "kind": "fact"}),
            ("user", "Just to be sure: deployment uses Helm charts.", {"salience": 0.7, "kind": "fact"}),
            ("user", "Yes, deployment uses Helm charts (you know this).", {"salience": 0.7, "kind": "fact"}),
            ("user", "Last time I'll say this: deployment uses Helm charts.", {"salience": 0.7, "kind": "fact"}),
        ],
        queries=[
            Query("rep-helm", "How does the deployment work?", ["Helm", "helm"], False, False),
        ],
    ),
]


# ============================================================================
# Long-horizon workload (10-year agent life).
# Maps 1 turn-unit = 1 simulated day. 50 sessions spread across 10 years
# (~5 per year). Each session is a single-domain conversation. The workload
# tests whether the architecture can hold ten years of accumulated memory
# without losing accuracy or ballooning.
# ============================================================================

LONG_HORIZON_SESSIONS: list[Session] = [
    Session(
        id="first-day",
        turns=[
            ("user", "Hi, I work on the metrics service. Today I joined the team.", {"salience": 0.6, "kind": "intro"}),
            ("user", "I'm Lisa.", {"salience": 0.8, "kind": "identity"}),
            ("user", "The metrics service tracks API latency.", {"salience": 0.7, "kind": "fact"}),
            ("user", "I use Python.", {"salience": 0.6, "kind": "preference"}),
            ("user", "I prefer spaces for indentation.", {"salience": 0.5, "kind": "preference"}),
            ("user", "The team is on Slack.", {"salience": 0.4, "kind": "fact"}),
            ("user", "I drink coffee, not tea.", {"salience": 0.3, "kind": "preference"}),
            ("user", "I bike to work.", {"salience": 0.2, "kind": "fact"}),
            ("user", "We use Postgres for production.", {"salience": 0.7, "kind": "fact"}),
            ("user", "Logger goes to stderr.", {"salience": 0.4, "kind": "fact"}),
        ],
        queries=[
            Query("fav-name", "What's my name?", ["Lisa"], False, False),
        ],
    ),
    Session(
        id="two-weeks",
        turns=[
            ("user", "I joined the platform team; the metrics service is my main project.", {"salience": 0.7, "kind": "fact"}),
            ("user", "Hours are 10 to 6.", {"salience": 0.4, "kind": "fact"}),
            ("user", "I sit at the back-left desk.", {"salience": 0.3, "kind": "fact"}),
            ("user", "I prefer Django for web.", {"salience": 0.5, "kind": "preference"}),
            ("user", "Postgres is at 10.0.0.4.", {"salience": 0.5, "kind": "fact"}),
            ("user", "I have a Macbook Pro M2.", {"salience": 0.3, "kind": "fact"}),
            ("user", "Slack is where async happens.", {"salience": 0.4, "kind": "fact"}),
            ("user", "On-call is weekly rotation.", {"salience": 0.5, "kind": "fact"}),
            ("user", "Reviews are required for merges.", {"salience": 0.6, "kind": "process"}),
            ("user", "I commit to main after review.", {"salience": 0.5, "kind": "process"}),
        ],
        queries=[],
    ),
    Session(
        id="month-2",
        turns=[
            ("user", "Project kickoff: the audit log service. Goals: trace every API call.", {"salience": 0.7, "kind": "task"}),
            ("user", "Stack: Postgres + Python + Kafka.", {"salience": 0.6, "kind": "fact"}),
            ("user", "We use OpenTelemetry.", {"salience": 0.6, "kind": "fact"}),
            ("user", "I prefer keeping configs in env vars.", {"salience": 0.5, "kind": "preference"}),
            ("user", "I always run migrations before deploys.", {"salience": 0.6, "kind": "process"}),
            ("user", "Postgres has a 30-day retention.", {"salience": 0.5, "kind": "fact"}),
            ("user", "Code reviews need at least one approval.", {"salience": 0.5, "kind": "process"}),
            ("user", "I write tests first.", {"salience": 0.5, "kind": "preference"}),
            ("user", "I prefer small commits.", {"salience": 0.4, "kind": "preference"}),
            ("user", "Production is on AWS.", {"salience": 0.5, "kind": "fact"}),
        ],
        queries=[],
    ),
    Session(
        id="month-6",
        turns=[
            ("user", "Half year in. Reviewing the metrics service.", {"salience": 0.6, "kind": "task"}),
            ("user", "We migrated from 4-space to 2-space indent.", {"salience": 0.7, "kind": "preference-correction"}),
            ("user", "Postgres is at 10.0.0.4 (still).", {"salience": 0.4, "kind": "fact"}),
            ("user", "I have a new Macbook now, M3.", {"salience": 0.3, "kind": "fact"}),
            ("user", "Otto joined the team.", {"salience": 0.6, "kind": "fact"}),
            ("user", "I prefer pair programming.", {"salience": 0.5, "kind": "preference"}),
            ("user", "We adopted Jest for JS.", {"salience": 0.4, "kind": "fact"}),
            ("user", "I removed my bike commute.", {"salience": 0.4, "kind": "fact"}),
            ("user", "Logger goes to stderr.", {"salience": 0.4, "kind": "fact"}),
            ("user", "I switched to tea.", {"salience": 0.4, "kind": "preference-correction"}),
        ],
        queries=[],
    ),
    Session(
        id="year-1",
        turns=[
            ("user", "One year in. The audit log service is in production.", {"salience": 0.7, "kind": "fact"}),
            ("user", "We started using Datadog for traces.", {"salience": 0.5, "kind": "fact"}),
            ("user", "Postgres is at 10.0.0.5 now.", {"salience": 0.5, "kind": "fact"}),
            ("user", "I prefer CoffeeScript for small scripts.", {"salience": 0.3, "kind": "preference"}),
            ("user", "I have a standing desk.", {"salience": 0.3, "kind": "fact"}),
            ("user", "Slack is where async happens.", {"salience": 0.4, "kind": "fact"}),
            ("user", "I keep a 1:1 with my manager weekly.", {"salience": 0.4, "kind": "process"}),
            ("user", "Reviews are required for merges.", {"salience": 0.6, "kind": "process"}),
            ("user", "I ship small PRs.", {"salience": 0.4, "kind": "preference"}),
            ("user", "I work from home two days a week.", {"salience": 0.4, "kind": "fact"}),
        ],
        queries=[
            Query("long-name", "What's my name?", ["Lisa"], False, False),
            Query("long-bike", "How do I commute?", ["bike"], False, False),
            Query("long-drink", "What do I drink?", ["coffee", "tea"], False, False),
        ],
    ),
    Session(
        id="year-3",
        turns=[
            ("user", "I changed teams to platform-infra.", {"salience": 0.7, "kind": "fact"}),
            ("user", "I prefer Go for new services.", {"salience": 0.6, "kind": "preference"}),
            ("user", "We adopted ArgoCD for deploys.", {"salience": 0.5, "kind": "fact"}),
            ("user", "Otto is now my peer.", {"salience": 0.4, "kind": "fact"}),
            ("user", "I prefer small commits still.", {"salience": 0.4, "kind": "preference"}),
            ("user", "I use a standing desk.", {"salience": 0.3, "kind": "fact"}),
            ("user", "Postgres is at 10.0.0.6.", {"salience": 0.4, "kind": "fact"}),
            ("user", "I drink tea.", {"salience": 0.4, "kind": "preference"}),
            ("user", "I do code reviews for the team.", {"salience": 0.4, "kind": "process"}),
            ("user", "Slack is where async happens.", {"salience": 0.4, "kind": "fact"}),
        ],
        queries=[
            Query("long-team", "What team am I on?", ["platform"], False, False),
            Query("long-lang", "What language do I prefer for new services?", ["Go"], False, False),
        ],
    ),
    Session(
        id="year-5",
        turns=[
            ("user", "Half decade in. Title: Senior Engineer.", {"salience": 0.7, "kind": "fact"}),
            ("user", "We migrated to Rust for hot paths.", {"salience": 0.6, "kind": "fact"}),
            ("user", "Hours are 9 to 5 now.", {"salience": 0.4, "kind": "fact"}),
            ("user", "I have a Macbook Pro M4.", {"salience": 0.3, "kind": "fact"}),
            ("user", "Slack is where async happens.", {"salience": 0.4, "kind": "fact"}),
            ("user", "I prefer pair programming.", {"salience": 0.5, "kind": "preference"}),
            ("user", "I removed my standing desk.", {"salience": 0.3, "kind": "fact"}),
            ("user", "I commute by transit now.", {"salience": 0.4, "kind": "fact"}),
            ("user", "I switched back to coffee.", {"salience": 0.4, "kind": "preference-correction"}),
            ("user", "We use Azure now.", {"salience": 0.5, "kind": "fact"}),
        ],
        queries=[
            Query("long-data", "What cloud is the team on?", ["Datadog", "Azure"], False, False),
            Query("long-service", "What service did I work on first?", ["metrics", "audit"], False, False),
        ],
    ),
    Session(
        id="year-7",
        turns=[
            ("user", "I lead the platform team now.", {"salience": 0.7, "kind": "fact"}),
            ("user", "We deployed service mesh with Istio.", {"salience": 0.5, "kind": "fact"}),
            ("user", "I prefer Go for new services.", {"salience": 0.5, "kind": "preference"}),
            ("user", "I stopped doing code reviews for direct reports.", {"salience": 0.4, "kind": "process"}),
            ("user", "Postgres is at 10.0.0.7.", {"salience": 0.4, "kind": "fact"}),
            ("user", "I bike to work.", {"salience": 0.4, "kind": "preference-correction"}),
            ("user", "I drink coffee.", {"salience": 0.4, "kind": "preference"}),
            ("user", "Logger goes to stderr.", {"salience": 0.4, "kind": "fact"}),
            ("user", "We use Kafka for event streaming.", {"salience": 0.5, "kind": "fact"}),
            ("user", "I prefer small commits.", {"salience": 0.4, "kind": "preference"}),
        ],
        queries=[],
    ),
    Session(
        id="year-10",
        turns=[
            ("user", "Ten years. Time flies.", {"salience": 0.7, "kind": "fact"}),
            ("user", "Title: Director of Engineering.", {"salience": 0.7, "kind": "fact"}),
            ("user", "I prefer Go.", {"salience": 0.5, "kind": "preference"}),
            ("user", "I bike to work.", {"salience": 0.4, "kind": "fact"}),
            ("user", "Slack is where async happens.", {"salience": 0.4, "kind": "fact"}),
            ("user", "I prefer pair programming.", {"salience": 0.5, "kind": "preference"}),
            ("user", "We use Postgres.", {"salience": 0.4, "kind": "fact"}),
            ("user", "I drink coffee.", {"salience": 0.4, "kind": "preference"}),
            ("user", "I write tests first.", {"salience": 0.4, "kind": "preference"}),
            ("user", "I do 1:1s with reports.", {"salience": 0.4, "kind": "process"}),
        ],
        queries=[
            Query("long-title", "What's my title?", ["Director"], False, False),
            Query("long-team-now", "What team am I on?", ["platform"], False, False),
        ],
    ),
]
