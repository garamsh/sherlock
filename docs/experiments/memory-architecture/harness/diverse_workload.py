"""Diverse memory workload. 35 sessions across 8 cognitive categories.

Each session is a real human memory with a cognitive flavor. The goal is
to test whether memory architectures can handle the variety of what a
real human (or agent) actually remembers.

Categories:
    P  Person (identity, role, who-said-what)
    E  Event (specific dated occurrence)
    R  Routine (recurring habit or pattern)
    O  Opinion (preference, belief, evaluation)
    Pr Procedure (how-to, recipe, fix)
    Sp Spatial (place, location, environment)
    Em Emotion (state, mood, reaction, significance)
    Re Relationship (people, dynamics, history)

Each session is 10 turns. Total: 350 real items + 5000 noise.
"""

from __future__ import annotations
from dataclasses import dataclass
from typing import Any


@dataclass
class Query:
    id: str
    text: str
    expected_keywords: list[str]
    category: str
    recency: bool = False
    noise: bool = False
    expect_turn_id: str = ""


@dataclass
class Session:
    id: str
    category: str
    turns: list[tuple[str, str, dict[str, Any]]]
    queries: list[Query]


SESSIONS: list[Session] = [
    # ───── Person (identity, role, who-said-what) ─────
    Session(
        id="P-team-roster",
        category="P",
        turns=[
            ("user", "My manager is Otto.", {"salience": 0.8, "kind": "identity"}),
            ("user", "Otto's been here six years.", {"salience": 0.5, "kind": "fact"}),
            ("user", "Maria joined last month as a junior engineer.", {"salience": 0.7, "kind": "fact"}),
            ("user", "Maria is on the platform team, not metrics.", {"salience": 0.6, "kind": "fact"}),
            ("user", "Otto is the one who wrote the original deploy pipeline.", {"salience": 0.7, "kind": "fact"}),
            ("assistant", "Noted. Otto is your manager and the deploy-pipeline author.", {"salience": 0.0}),
            ("user", "Maria is super fast at Python.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto is more into Go.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Maria and I sit in the same row.", {"salience": 0.4, "kind": "fact"}),
            ("user", "Otto is in the back-right corner.", {"salience": 0.4, "kind": "fact"}),
        ],
        queries=[
            Query("p-mgr", "Who is my manager?", ["Otto"], "P"),
            Query("p-junior", "Who is the junior engineer on my team?", ["Maria"], "P"),
            Query("p-maria-team", "What team is Maria on?", ["platform"], "P"),
        ],
    ),
    Session(
        id="P-who-said-what",
        category="P",
        turns=[
            ("user", "Otto said the deploy pipeline was 'too clever by half.'", {"salience": 0.5, "kind": "quote"}),
            ("user", "Maria said she preferred pytest over unittest.", {"salience": 0.5, "kind": "quote"}),
            ("user", "I told Maria that the test coverage was 70%.", {"salience": 0.5, "kind": "quote"}),
            ("user", "Otto suggested we use feature flags.", {"salience": 0.5, "kind": "quote"}),
            ("user", "Maria said feature flags were overkill for the prototype.", {"salience": 0.5, "kind": "quote"}),
            ("user", "I disagreed with Maria there.", {"salience": 0.4, "kind": "social"}),
            ("user", "Otto's been pushing for the feature flags; Maria's been skeptical.", {"salience": 0.6, "kind": "social"}),
            ("user", "Maria promised to write the migration plan by Friday.", {"salience": 0.6, "kind": "commitment"}),
            ("user", "Otto said he'd review it on Monday.", {"salience": 0.6, "kind": "commitment"}),
            ("user", "I told them both my deadline is Wednesday.", {"salience": 0.6, "kind": "commitment"}),
        ],
        queries=[
            Query("p-quote-otto", "Who said the deploy pipeline was too clever?", ["Otto"], "P"),
            Query("p-quote-pytest", "Who prefers pytest over unittest?", ["Maria"], "P"),
            Query("p-promise", "Who promised to write the migration plan by Friday?", ["Maria"], "P"),
        ],
    ),
    # ───── Event (specific dated occurrence) ─────
    Session(
        id="E-launch-day",
        category="E",
        turns=[
            ("user", "On March 5, 2024, we shipped the launch.", {"salience": 0.9, "kind": "event"}),
            ("user", "There was a 2-hour outage that day.", {"salience": 0.7, "kind": "event"}),
            ("user", "Otto was on call.", {"salience": 0.6, "kind": "event"}),
            ("user", "I stayed late until 11pm.", {"salience": 0.5, "kind": "event"}),
            ("user", "The launch was the audit log service.", {"salience": 0.7, "kind": "event"}),
            ("user", "We had cake in the office.", {"salience": 0.3, "kind": "event"}),
            ("user", "Maria baked the cake.", {"salience": 0.4, "kind": "event"}),
            ("user", "The outage was a database connection leak.", {"salience": 0.7, "kind": "event-cause"}),
            ("user", "We fixed it by adding a connection pool size limit.", {"salience": 0.7, "kind": "fix"}),
            ("user", "Postmortem was published on March 7.", {"salience": 0.6, "kind": "event"}),
        ],
        queries=[
            Query("e-when-launch", "When did we ship the launch?", ["March 5"], "E"),
            Query("e-outage", "What was the outage cause?", ["connection", "leak"], "E"),
        ],
    ),
    Session(
        id="E-run",
        category="E",
        turns=[
            ("user", "I ran the Bay to Breakers in 2023.", {"salience": 0.7, "kind": "event"}),
            ("user", "I finished in 2 hours 14 minutes.", {"salience": 0.6, "kind": "event"}),
            ("user", "Otto came in 2:30.", {"salience": 0.5, "kind": "event"}),
            ("user", "We had breakfast at Brenda's after.", {"salience": 0.4, "kind": "event"}),
            ("user", "Brenda is Otto's wife.", {"salience": 0.6, "kind": "identity"}),
            ("user", "I hurt my knee at mile 8.", {"salience": 0.5, "kind": "event-personal"}),
            ("user", "Maria crewed for me at the aid station.", {"salience": 0.5, "kind": "event"}),
            ("user", "The weather was clear.", {"salience": 0.3, "kind": "event"}),
            ("user", "I wore my old red shoes.", {"salience": 0.3, "kind": "event-personal"}),
            ("user", "I want to run again next year.", {"salience": 0.5, "kind": "intent"}),
        ],
        queries=[
            Query("e-run-time", "What was my Bay to Breakers time?", ["2:14", "2 hours 14"], "E"),
            Query("e-run-injury", "What did I hurt at the race?", ["knee"], "E"),
        ],
    ),
    # ───── Routine (recurring habit or pattern) ─────
    Session(
        id="R-morning",
        category="R",
        turns=[
            ("user", "I wake up at 7am on weekdays.", {"salience": 0.6, "kind": "routine"}),
            ("user", "I check email first.", {"salience": 0.4, "kind": "routine"}),
            ("user", "Then I make coffee.", {"salience": 0.4, "kind": "routine"}),
            ("user", "I bike to work three days a week.", {"salience": 0.6, "kind": "routine"}),
            ("user", "I take the train the other two.", {"salience": 0.5, "kind": "routine"}),
            ("user", "I do my best thinking in the morning.", {"salience": 0.5, "kind": "self-knowledge"}),
            ("user", "I have a coffee at 3pm.", {"salience": 0.4, "kind": "routine"}),
            ("user", "I leave work at 6.", {"salience": 0.5, "kind": "routine"}),
            ("user", "I run in the evenings on weekdays.", {"salience": 0.5, "kind": "routine"}),
            ("user", "I sleep early.", {"salience": 0.4, "kind": "routine"}),
        ],
        queries=[
            Query("r-wake", "What time do I wake up?", ["7am", "7"], "R"),
            Query("r-bike", "When do I bike to work?", ["three", "week"], "R"),
        ],
    ),
    Session(
        id="R-weekend",
        category="R",
        turns=[
            ("user", "I sleep in on Saturdays.", {"salience": 0.5, "kind": "routine"}),
            ("user", "I do a long run on Sundays.", {"salience": 0.6, "kind": "routine"}),
            ("user", "I visit the farmer's market on Saturdays.", {"salience": 0.5, "kind": "routine"}),
            ("user", "I cook on Sunday evenings.", {"salience": 0.5, "kind": "routine"}),
            ("user", "I call my parents on Sundays.", {"salience": 0.5, "kind": "routine"}),
            ("user", "I read fiction on Saturday afternoons.", {"salience": 0.4, "kind": "routine"}),
            ("user", "I do meal prep on Saturdays.", {"salience": 0.4, "kind": "routine"}),
            ("user", "I watch a movie on Friday nights.", {"salience": 0.4, "kind": "routine"}),
            ("user", "I clean the apartment on Sundays.", {"salience": 0.4, "kind": "routine"}),
            ("user", "I usually meal prep with Otto and Maria.", {"salience": 0.5, "kind": "routine"}),
        ],
        queries=[
            Query("r-sunday", "What do I do on Sundays?", ["long run", "cook"], "R"),
        ],
    ),
    # ───── Opinion (preference, belief, evaluation) ─────
    Session(
        id="O-tech",
        category="O",
        turns=[
            ("user", "I prefer Python over Go for new services.", {"salience": 0.7, "kind": "opinion"}),
            ("user", "I think Rust is best for hot paths.", {"salience": 0.6, "kind": "opinion"}),
            ("user", "I think JavaScript is fine for small sites.", {"salience": 0.4, "kind": "opinion"}),
            ("user", "I dislike Java.", {"salience": 0.6, "kind": "opinion"}),
            ("user", "I think Clojure is overrated.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I believe most code reviews should be async.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I think pair programming is overrated for routine work.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I believe in writing tests first.", {"salience": 0.6, "kind": "opinion"}),
            ("user", "I think Slack is necessary but evil.", {"salience": 0.4, "kind": "opinion"}),
            ("user", "I believe most meetings are unnecessary.", {"salience": 0.5, "kind": "opinion"}),
        ],
        queries=[
            Query("o-python", "What language do I prefer for new services?", ["Python"], "O"),
            Query("o-rust", "Where do I think Rust is best?", ["hot paths"], "O"),
        ],
    ),
    Session(
        id="O-evolved",
        category="O",
        turns=[
            ("user", "I used to think pair programming was essential.", {"salience": 0.5, "kind": "opinion-historical"}),
            ("user", "Now I think it's only useful for unfamiliar code.", {"salience": 0.7, "kind": "opinion-correction"}),
            ("user", "I used to like JavaScript.", {"salience": 0.4, "kind": "opinion-historical"}),
            ("user", "Now I think TypeScript is fine.", {"salience": 0.5, "kind": "opinion-correction"}),
            ("user", "I used to prefer Vim.", {"salience": 0.4, "kind": "opinion-historical"}),
            ("user", "Now I use VS Code.", {"salience": 0.5, "kind": "opinion-correction"}),
            ("user", "I liked Express a few years ago.", {"salience": 0.4, "kind": "opinion-historical"}),
            ("user", "Now I prefer FastAPI.", {"salience": 0.5, "kind": "opinion-correction"}),
            ("user", "I loved PHP in 2010.", {"salience": 0.3, "kind": "opinion-historical"}),
            ("user", "Now I would never go back.", {"salience": 0.5, "kind": "opinion-correction"}),
        ],
        queries=[
            Query("o-pair-now", "Where do I think pair programming is useful?", ["unfamiliar code"], "O"),
            Query("o-editor", "What editor do I use now?", ["VS Code"], "O"),
        ],
    ),
    # ───── Procedure (how-to, recipe, fix) ─────
    Session(
        id="Pr-deploy",
        category="Pr",
        turns=[
            ("user", "To deploy a service: bump the version in Makefile.", {"salience": 0.7, "kind": "procedure"}),
            ("user", "Then run make deploy.", {"salience": 0.7, "kind": "procedure"}),
            ("user", "Then run the smoke test.", {"salience": 0.7, "kind": "procedure"}),
            ("user", "Then merge the PR.", {"salience": 0.6, "kind": "procedure"}),
            ("user", "Then tag the release.", {"salience": 0.6, "kind": "procedure"}),
            ("user", "If the smoke test fails, roll back via git revert.", {"salience": 0.6, "kind": "procedure"}),
            ("user", "Then write the postmortem.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "If the rollback also fails, escalate to Otto.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Ops on-call rotation is weekly.", {"salience": 0.4, "kind": "fact"}),
            ("user", "Paging protocol uses PagerDuty.", {"salience": 0.4, "kind": "fact"}),
        ],
        queries=[
            Query("pr-deploy-step", "What's the first step to deploy a service?", ["bump", "version"], "Pr"),
            Query("pr-failure", "What do I do if the smoke test fails?", ["roll back", "revert"], "Pr"),
        ],
    ),
    Session(
        id="Pr-fix",
        category="Pr",
        turns=[
            ("user", "If the metrics service is slow, check the database connection pool size.", {"salience": 0.7, "kind": "procedure"}),
            ("user", "If the queries are slow, look at the slow query log.", {"salience": 0.6, "kind": "procedure"}),
            ("user", "If the slow query log is clean, check the database CPU.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "If the database CPU is fine, check the application logs.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "If the application logs are clean, restart the service.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "If it still fails, escalate to Maria.", {"salience": 0.4, "kind": "procedure"}),
            ("user", "Monitor with Datadog for spikes.", {"salience": 0.4, "kind": "fact"}),
            ("user", "We use Postgres 14.", {"salience": 0.4, "kind": "fact"}),
            ("user", "Connection pool default is 20.", {"salience": 0.4, "kind": "fact"}),
            ("user", "I increased it to 50 last month.", {"salience": 0.5, "kind": "fact"}),
        ],
        queries=[
            Query("pr-slow-metrics", "What's the first thing to check if the metrics service is slow?", ["connection pool", "pool"], "Pr"),
        ],
    ),
    # ───── Spatial (place, location, environment) ─────
    Session(
        id="Sp-office",
        category="Sp",
        turns=[
            ("user", "Otto sits at the back-right corner.", {"salience": 0.6, "kind": "spatial"}),
            ("user", "Maria sits two desks away from me, by the window.", {"salience": 0.5, "kind": "spatial"}),
            ("user", "The coffee machine is in the kitchen, second door on the right.", {"salience": 0.5, "kind": "spatial"}),
            ("user", "The whiteboards are by the elevator.", {"salience": 0.4, "kind": "spatial"}),
            ("user", "The meeting room is named Aurora.", {"salience": 0.5, "kind": "spatial"}),
            ("user", "Conference room Polaris is on the second floor.", {"salience": 0.4, "kind": "spatial"}),
            ("user", "The standing desks are near the south windows.", {"salience": 0.3, "kind": "spatial"}),
            ("user", "The printer is in the supply closet.", {"salience": 0.3, "kind": "spatial"}),
            ("user", "I sit at the back-left, facing the window.", {"salience": 0.5, "kind": "spatial"}),
            ("user", "Otto's desk has a Plant on it.", {"salience": 0.3, "kind": "spatial"}),
        ],
        queries=[
            Query("sp-otto", "Where does Otto sit?", ["back-right"], "Sp"),
            Query("sp-meeting", "Where is the meeting room named Aurora?", ["Aurora", "meeting"], "Sp"),
        ],
    ),
    # ───── Emotion (state, mood, reaction, significance) ─────
    Session(
        id="Em-feel",
        category="Em",
        turns=[
            ("user", "I felt great after the launch.", {"salience": 0.6, "kind": "emotion"}),
            ("user", "I was burned out in Q3.", {"salience": 0.7, "kind": "emotion"}),
            ("user", "I took a week off in October.", {"salience": 0.6, "kind": "event"}),
            ("user", "I came back refreshed.", {"salience": 0.5, "kind": "emotion"}),
            ("user", "The outage stressed me out.", {"salience": 0.5, "kind": "emotion"}),
            ("user", "Otto noticed the burnout.", {"salience": 0.5, "kind": "social"}),
            ("user", "I felt appreciated after the postmortem.", {"salience": 0.5, "kind": "emotion"}),
            ("user", "I was anxious about the promotion.", {"salience": 0.6, "kind": "emotion"}),
            ("user", "I got the promotion in December.", {"salience": 0.7, "kind": "event"}),
            ("user", "I felt relieved.", {"salience": 0.5, "kind": "emotion"}),
        ],
        queries=[
            Query("em-burnout", "When did I feel burned out?", ["Q3", "burnout"], "Em"),
            Query("em-promotion", "What happened in December?", ["promotion"], "Em"),
        ],
    ),
    # ───── Relationship (people, dynamics, history) ─────
    Session(
        id="Re-otto",
        category="Re",
        turns=[
            ("user", "Otto and I have known each other for six years.", {"salience": 0.7, "kind": "relationship"}),
            ("user", "We worked together at the previous company.", {"salience": 0.6, "kind": "relationship"}),
            ("user", "We both joined the current company the same year.", {"salience": 0.5, "kind": "relationship"}),
            ("user", "Otto has been my manager for three years.", {"salience": 0.7, "kind": "relationship"}),
            ("user", "We had a rough patch in 2022; I almost quit.", {"salience": 0.5, "kind": "relationship"}),
            ("user", "Otto and I rebuilt trust after that.", {"salience": 0.6, "kind": "relationship"}),
            ("user", "I trust Otto's judgment on architecture.", {"salience": 0.7, "kind": "relationship"}),
            ("user", "Otto's been a great mentor.", {"salience": 0.7, "kind": "relationship"}),
            ("user", "We have lunch together on Fridays.", {"salience": 0.4, "kind": "relationship"}),
            ("user", "Otto is the godfather of my dog.", {"salience": 0.4, "kind": "relationship"}),
        ],
        queries=[
            Query("re-otto", "How long have I known Otto?", ["six years"], "Re"),
            Query("re-trust", "What happened in 2022 with Otto?", ["rough", "quit"], "Re"),
        ],
    ),
    Session(
        id="Re-maria",
        category="Re",
        turns=[
            ("user", "Maria and I worked together at the previous startup.", {"salience": 0.6, "kind": "relationship"}),
            ("user", "Maria joined the current company a year after me.", {"salience": 0.5, "kind": "relationship"}),
            ("user", "I mentored Maria when she joined.", {"salience": 0.6, "kind": "relationship"}),
            ("user", "Maria and I have a great working relationship.", {"salience": 0.6, "kind": "relationship"}),
            ("user", "We argue about testing sometimes.", {"salience": 0.4, "kind": "relationship"}),
            ("user", "Maria is more into TDD than I am.", {"salience": 0.4, "kind": "social"}),
            ("user", "We compromise: TDD for new code, test-after for bug fixes.", {"salience": 0.5, "kind": "relationship"}),
            ("user", "Maria's been a great sounding board for architectural decisions.", {"salience": 0.6, "kind": "relationship"}),
            ("user", "We had a fight last quarter about deployment strategy.", {"salience": 0.4, "kind": "relationship"}),
            ("user", "We resolved it by writing a postmortem together.", {"salience": 0.5, "kind": "relationship"}),
        ],
        queries=[
            Query("re-maria-tenure", "How long has Maria been at the company compared to me?", ["a year after", "year"], "Re"),
            Query("re-arg", "What did Maria and I argue about?", ["deployment", "strategy"], "Re"),
        ],
    ),
    # ───── Mixed cognitive (combinations) ─────
    Session(
        id="Otto-birthday",
        category="E",
        turns=[
            ("user", "Otto's birthday was last week.", {"salience": 0.6, "kind": "event"}),
            ("user", "We had cake at the office.", {"salience": 0.4, "kind": "event"}),
            ("user", "Maria baked the cake.", {"salience": 0.4, "kind": "event"}),
            ("user", "I gave Otto a book on debugging.", {"salience": 0.5, "kind": "event"}),
            ("user", "Otto collects debugging books.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto said the book was the best one he's read.", {"salience": 0.5, "kind": "quote"}),
            ("user", "Otto has been my manager for three years.", {"salience": 0.7, "kind": "identity"}),
            ("user", "We had a rough patch in 2022.", {"salience": 0.5, "kind": "relationship"}),
            ("user", "Otto's wife is Brenda.", {"salience": 0.6, "kind": "identity"}),
            ("user", "Brenda and Otto have been married for ten years.", {"salience": 0.5, "kind": "fact"}),
        ],
        queries=[
            Query("otto-bd-gift", "What did I give Otto for his birthday?", ["book", "debugging"], "E"),
        ],
    ),
    Session(
        id="Pr-debug-flow",
        category="Pr",
        turns=[
            ("user", "When a service is slow: check the CPU first.", {"salience": 0.7, "kind": "procedure"}),
            ("user", "Then check the memory.", {"salience": 0.6, "kind": "procedure"}),
            ("user", "Then check the network.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Then check the database.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Then check the application logs.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "If nothing is obvious, do a binary search by disabling features.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Once you find the cause, write a one-line diagnosis.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Then fix the underlying issue.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Then write a regression test.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Then deploy.", {"salience": 0.5, "kind": "procedure"}),
        ],
        queries=[
            Query("pr-debug-first", "What's the first thing to check when a service is slow?", ["CPU"], "Pr"),
        ],
    ),
    Session(
        id="E-mom-visit",
        category="E",
        turns=[
            ("user", "My mom visited last weekend.", {"salience": 0.7, "kind": "event"}),
            ("user", "We went to the art museum.", {"salience": 0.5, "kind": "event"}),
            ("user", "She loved the Monet exhibition.", {"salience": 0.5, "kind": "event"}),
            ("user", "We had lunch at the cafe in the museum.", {"salience": 0.4, "kind": "event"}),
            ("user", "Mom is 67.", {"salience": 0.5, "kind": "identity"}),
            ("user", "Mom lives in Portland.", {"salience": 0.5, "kind": "spatial-relative"}),
            ("user", "Mom calls me every Sunday.", {"salience": 0.5, "kind": "relationship"}),
            ("user", "Mom is a retired teacher.", {"salience": 0.4, "kind": "identity"}),
            ("user", "Mom taught English for 35 years.", {"salience": 0.4, "kind": "fact"}),
            ("user", "I miss her already.", {"salience": 0.5, "kind": "emotion"}),
        ],
        queries=[
            Query("e-mom-age", "How old is my mom?", ["67"], "E"),
            Query("e-mom-loc", "Where does my mom live?", ["Portland"], "E"),
        ],
    ),
    Session(
        id="O-food",
        category="O",
        turns=[
            ("user", "I prefer coffee over tea.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I don't like spicy food.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I think sushi is overrated.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I love pizza.", {"salience": 0.6, "kind": "opinion"}),
            ("user", "I think breakfast is the most important meal.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I prefer savory over sweet.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I dislike cilantro.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I love spicy food despite my earlier statement.", {"salience": 0.6, "kind": "opinion-correction"}),
            ("user", "I think Mexican is the best cuisine.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I prefer Italian over Mexican.", {"salience": 0.5, "kind": "opinion"}),
        ],
        queries=[
            Query("o-spice", "Do I like spicy food?", ["spicy", "love"], "O"),
            Query("o-italian", "What cuisine do I prefer?", ["Italian"], "O"),
        ],
    ),
    Session(
        id="Sp-cafe",
        category="Sp",
        turns=[
            ("user", "The Blue Bottle on Mission is my go-to.", {"salience": 0.6, "kind": "spatial"}),
            ("user", "I usually get the New Orleans iced coffee.", {"salience": 0.5, "kind": "routine"}),
            ("user", "The Sightglass on 7th is for meetings.", {"salience": 0.5, "kind": "spatial"}),
            ("user", "I don't go to the Starbucks on Market.", {"salience": 0.4, "kind": "opinion"}),
            ("user", "I prefer the local roaster over chains.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "Philz on 4th is my fallback.", {"salience": 0.4, "kind": "spatial"}),
            ("user", "I avoid the Starbucks on Folsom.", {"salience": 0.4, "kind": "opinion"}),
            ("user", "I do not like the Starbucks on Howard.", {"salience": 0.4, "kind": "opinion"}),
            ("user", "I prefer independent coffee shops.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I take meetings at Verve on Mission.", {"salience": 0.4, "kind": "spatial"}),
        ],
        queries=[
            Query("sp-go-to", "Where is my go-to coffee shop?", ["Blue Bottle", "Mission"], "Sp"),
        ],
    ),
    Session(
        id="Re-best-friend",
        category="Re",
        turns=[
            ("user", "My best friend is Sam.", {"salience": 0.7, "kind": "identity"}),
            ("user", "Sam and I have known each other since college.", {"salience": 0.6, "kind": "relationship"}),
            ("user", "Sam lives in Brooklyn now.", {"salience": 0.5, "kind": "spatial"}),
            ("user", "Sam is a designer.", {"salience": 0.5, "kind": "identity"}),
            ("user", "Sam has a dog named Pixel.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "We don't talk as often as we used to.", {"salience": 0.4, "kind": "relationship"}),
            ("user", "I miss our video calls.", {"salience": 0.5, "kind": "emotion"}),
            ("user", "Sam is non-binary.", {"salience": 0.5, "kind": "identity"}),
            ("user", "Sam uses they/them pronouns.", {"salience": 0.5, "kind": "person-trait"}),
            ("user", "We try to visit each other once a year.", {"salience": 0.4, "kind": "relationship"}),
        ],
        queries=[
            Query("re-bff", "Who is my best friend?", ["Sam"], "Re"),
            Query("re-bff-pronouns", "What pronouns does Sam use?", ["they", "them"], "Re"),
        ],
    ),
    Session(
        id="Pr-cook",
        category="Pr",
        turns=[
            ("user", "To make pasta: boil water.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Add salt to the water.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Add the pasta when boiling.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Cook for 8 minutes for al dente.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Reserve a cup of pasta water.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Drain and toss with sauce.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Add pasta water if too dry.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Finish with parmesan and pepper.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "I usually use marinara or aglio e olio.", {"salience": 0.4, "kind": "opinion"}),
            ("user", "Pair with a light red wine.", {"salience": 0.4, "kind": "opinion"}),
        ],
        queries=[
            Query("pr-pasta", "How long do I cook pasta for al dente?", ["8 minutes", "8"], "Pr"),
        ],
    ),
    Session(
        id="Em-team",
        category="Em",
        turns=[
            ("user", "I was frustrated with the codebase when I joined.", {"salience": 0.5, "kind": "emotion"}),
            ("user", "Now I love it.", {"salience": 0.6, "kind": "emotion"}),
            ("user", "I felt intimidated by the senior engineers at first.", {"salience": 0.5, "kind": "emotion"}),
            ("user", "Now they feel like peers.", {"salience": 0.5, "kind": "emotion"}),
            ("user", "I felt proud when I shipped my first feature.", {"salience": 0.6, "kind": "emotion"}),
            ("user", "I felt embarrassed when I broke production once.", {"salience": 0.5, "kind": "emotion"}),
            ("user", "I buried myself in postmortems for a month after that.", {"salience": 0.5, "kind": "emotion"}),
            ("user", "I felt vindicated when we hit our SLA target.", {"salience": 0.5, "kind": "emotion"}),
            ("user", "I feel grateful for Otto's mentorship.", {"salience": 0.6, "kind": "emotion"}),
            ("user", "I feel like I belong here now.", {"salience": 0.6, "kind": "emotion"}),
        ],
        queries=[
            Query("em-belong", "How do I feel about the team now?", ["belong"], "Em"),
        ],
    ),
    Session(
        id="P-names",
        category="P",
        turns=[
            ("user", "I'm Lisa.", {"salience": 0.8, "kind": "identity"}),
            ("user", "My partner is Jordan.", {"salience": 0.7, "kind": "identity"}),
            ("user", "Jordan is an architect.", {"salience": 0.5, "kind": "identity"}),
            ("user", "Jordan has a cat named Pixel.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Jordan prefers cooking to eating out.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "My sister is Anna.", {"salience": 0.7, "kind": "identity"}),
            ("user", "Anna lives in Seattle.", {"salience": 0.5, "kind": "spatial"}),
            ("user", "Anna is two years younger.", {"salience": 0.4, "kind": "fact"}),
            ("user", "Anna is a therapist.", {"salience": 0.5, "kind": "identity"}),
            ("user", "I talk to Anna weekly.", {"salience": 0.4, "kind": "relationship"}),
        ],
        queries=[
            Query("p-partner", "Who is my partner?", ["Jordan"], "P"),
            Query("p-sister", "Who is my sister?", ["Anna"], "P"),
        ],
    ),
    Session(
        id="O-tech-historical",
        category="O",
        turns=[
            ("user", "I used to think microservices were the answer to everything.", {"salience": 0.5, "kind": "opinion-historical"}),
            ("user", "Now I think monoliths are underrated.", {"salience": 0.6, "kind": "opinion-correction"}),
            ("user", "I used to think TDD was essential.", {"salience": 0.5, "kind": "opinion-historical"}),
            ("user", "Now I think test-after is fine for bug fixes.", {"salience": 0.6, "kind": "opinion-correction"}),
            ("user", "I used to like NoSQL.", {"salience": 0.4, "kind": "opinion-historical"}),
            ("user", "Now I prefer Postgres for most things.", {"salience": 0.5, "kind": "opinion-correction"}),
            ("user", "I used to think AWS was best.", {"salience": 0.4, "kind": "opinion-historical"}),
            ("user", "Now I prefer GCP for most things.", {"salience": 0.5, "kind": "opinion-correction"}),
            ("user", "I used to think Slack was great.", {"salience": 0.4, "kind": "opinion-historical"}),
            ("user", "Now I think it's just OK.", {"salience": 0.5, "kind": "opinion-correction"}),
        ],
        queries=[
            Query("o-monolith", "What do I think about monoliths now?", ["underrated", "monolith"], "O"),
        ],
    ),
    Session(
        id="E-team-offsite",
        category="E",
        turns=[
            ("user", "The team offsite was in Lake Tahoe.", {"salience": 0.6, "kind": "event"}),
            ("user", "It was in September.", {"salience": 0.5, "kind": "event"}),
            ("user", "We stayed at the Hyatt.", {"salience": 0.4, "kind": "event"}),
            ("user", "I led the strategy session.", {"salience": 0.5, "kind": "event"}),
            ("user", "Otto gave a great talk on observability.", {"salience": 0.5, "kind": "event"}),
            ("user", "Maria organized the logistics.", {"salience": 0.4, "kind": "event"}),
            ("user", "We did a hike on day 2.", {"salience": 0.4, "kind": "event"}),
            ("user", "I played poker with Otto and Sam.", {"salience": 0.4, "kind": "event"}),
            ("user", "I lost.", {"salience": 0.3, "kind": "event"}),
            ("user", "It was a great bonding experience.", {"salience": 0.5, "kind": "emotion"}),
        ],
        queries=[
            Query("e-offsite-loc", "Where was the team offsite?", ["Lake Tahoe", "Tahoe"], "E"),
        ],
    ),
    Session(
        id="R-product",
        category="R",
        turns=[
            ("user", "I check production metrics every morning at 9am.", {"salience": 0.6, "kind": "routine"}),
            ("user", "I look at the dashboard first.", {"salience": 0.5, "kind": "routine"}),
            ("user", "Then I check Slack for any alerts.", {"salience": 0.5, "kind": "routine"}),
            ("user", "Then I review the PR queue.", {"salience": 0.5, "kind": "routine"}),
            ("user", "Then I have a 1:1 with Otto.", {"salience": 0.5, "kind": "routine"}),
            ("user", "I do code reviews in the afternoon.", {"salience": 0.5, "kind": "routine"}),
            ("user", "I have lunch at noon.", {"salience": 0.4, "kind": "routine"}),
            ("user", "I work from home on Wednesdays.", {"salience": 0.4, "kind": "routine"}),
            ("user", "I walk at 5pm.", {"salience": 0.4, "kind": "routine"}),
            ("user", "I read technical blogs in the evening.", {"salience": 0.4, "kind": "routine"}),
        ],
        queries=[
            Query("r-morning", "What do I check every morning?", ["metrics", "dashboard"], "R"),
        ],
    ),
    Session(
        id="Sp-around",
        category="Sp",
        turns=[
            ("user", "I live in the Mission District.", {"salience": 0.6, "kind": "spatial"}),
            ("user", "My apartment is on 24th and Mission.", {"salience": 0.5, "kind": "spatial"}),
            ("user", "I have a south-facing window.", {"salience": 0.4, "kind": "spatial"}),
            ("user", "I bike to work along the Mission bike lane.", {"salience": 0.4, "kind": "spatial"}),
            ("user", "Dolores Park is my favorite weekend spot.", {"salience": 0.5, "kind": "spatial"}),
            ("user", "I avoid the 16th and Mission BART station.", {"salience": 0.4, "kind": "spatial"}),
            ("user", "Tartine is my favorite bakery.", {"salience": 0.4, "kind": "spatial"}),
            ("user", "I love the murals in the Mission.", {"salience": 0.4, "kind": "opinion"}),
            ("user", "I live in a rent-controlled apartment.", {"salience": 0.5, "kind": "fact"}),
            ("user", "My landlord is great.", {"salience": 0.4, "kind": "relationship"}),
        ],
        queries=[
            Query("sp-neighborhood", "What neighborhood do I live in?", ["Mission"], "Sp"),
        ],
    ),
    Session(
        id="Em-loss",
        category="Em",
        turns=[
            ("user", "My grandfather died last year.", {"salience": 0.8, "kind": "emotion"}),
            ("user", "I was with him when he passed.", {"salience": 0.7, "kind": "event"}),
            ("user", "I felt at peace.", {"salience": 0.6, "kind": "emotion"}),
            ("user", "He taught me to play chess.", {"salience": 0.5, "kind": "relationship"}),
            ("user", "I miss him.", {"salience": 0.6, "kind": "emotion"}),
            ("user", "I keep his chess set on my desk.", {"salience": 0.5, "kind": "spatial"}),
            ("user", "I think about him when I play.", {"salience": 0.5, "kind": "emotion"}),
            ("user", "He was 89.", {"salience": 0.5, "kind": "fact"}),
            ("user", "He was a teacher.", {"salience": 0.5, "kind": "identity"}),
            ("user", "I hope to be like him.", {"salience": 0.6, "kind": "emotion"}),
        ],
        queries=[
            Query("em-grandpa", "What did my grandfather teach me?", ["chess"], "Em"),
        ],
    ),
    Session(
        id="Pr-fix-debug",
        category="Pr",
        turns=[
            ("user", "To debug a flaky test: run it 100 times.", {"salience": 0.6, "kind": "procedure"}),
            ("user", "If it fails, look at the failure first.", {"salience": 0.6, "kind": "procedure"}),
            ("user", "Check the test setup.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Check for shared state.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Check for time dependencies.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Use mocked clocks if needed.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Run the test in isolation.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Run with a debugger.", {"salience": 0.4, "kind": "procedure"}),
            ("user", "Check for race conditions.", {"salience": 0.4, "kind": "procedure"}),
            ("user", "Add a regression test once fixed.", {"salience": 0.5, "kind": "procedure"}),
        ],
        queries=[
            Query("pr-flaky", "How do I debug a flaky test?", ["100 times", "isolation"], "Pr"),
        ],
    ),
    Session(
        id="O-music",
        category="O",
        turns=[
            ("user", "I prefer jazz over classical.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I think hip-hop is the most innovative genre.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I dislike most pop music.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I love instrumental music when working.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I think lyrics are distracting.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I prefer vinyl over streaming.", {"salience": 0.4, "kind": "opinion"}),
            ("user", "I like ambient music for coding.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I dislike country music.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I think Radiohead is the best band ever.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I enjoy live music more than recorded.", {"salience": 0.5, "kind": "opinion"}),
        ],
        queries=[
            Query("o-coding", "What music do I prefer when coding?", ["ambient"], "O"),
        ],
    ),
    Session(
        id="P-friends",
        category="P",
        turns=[
            ("user", "Sam is my best friend.", {"salience": 0.7, "kind": "identity"}),
            ("user", "Anna is my sister.", {"salience": 0.7, "kind": "identity"}),
            ("user", "Otto is my manager.", {"salience": 0.7, "kind": "identity"}),
            ("user", "Maria is my mentee.", {"salience": 0.6, "kind": "identity"}),
            ("user", "Brenda is Otto's wife.", {"salience": 0.6, "kind": "identity"}),
            ("user", "Tom is my college roommate.", {"salience": 0.5, "kind": "identity"}),
            ("user", "Lin is my former coworker.", {"salience": 0.5, "kind": "identity"}),
            ("user", "Jess is my book club friend.", {"salience": 0.5, "kind": "identity"}),
            ("user", "Mom is my mom.", {"salience": 0.7, "kind": "identity"}),
            ("user", "Dad passed away when I was 20.", {"salience": 0.7, "kind": "identity"}),
        ],
        queries=[
            Query("p-circle", "Who is in my close circle?", ["Sam", "Anna", "Otto"], "P"),
        ],
    ),
    Session(
        id="Em-hobby",
        category="Em",
        turns=[
            ("user", "I feel calm when I cook.", {"salience": 0.5, "kind": "emotion"}),
            ("user", "I feel proud when I run a long distance.", {"salience": 0.5, "kind": "emotion"}),
            ("user", "I feel anxious before code reviews.", {"salience": 0.5, "kind": "emotion"}),
            ("user", "I feel excited when I learn something new.", {"salience": 0.6, "kind": "emotion"}),
            ("user", "I feel drained after long meetings.", {"salience": 0.5, "kind": "emotion"}),
            ("user", "I feel grateful for my team.", {"salience": 0.6, "kind": "emotion"}),
            ("user", "I feel lonely when I work from home too long.", {"salience": 0.5, "kind": "emotion"}),
            ("user", "I feel energized by good design.", {"salience": 0.6, "kind": "emotion"}),
            ("user", "I feel frustrated when meetings run over.", {"salience": 0.5, "kind": "emotion"}),
            ("user", "I feel content when I read at night.", {"salience": 0.5, "kind": "emotion"}),
        ],
        queries=[
            Query("em-cooking", "When do I feel calm?", ["cook"], "Em"),
        ],
    ),
    Session(
        id="R-network",
        category="Re",
        turns=[
            ("user", "I have a strong network in the Bay Area tech scene.", {"salience": 0.5, "kind": "fact"}),
            ("user", "Sam is my oldest friend.", {"salience": 0.5, "kind": "relationship"}),
            ("user", "I met Otto at a previous job.", {"salience": 0.5, "kind": "relationship"}),
            ("user", "Maria came through a referral.", {"salience": 0.4, "kind": "fact"}),
            ("user", "Lin introduced me to my current manager.", {"salience": 0.4, "kind": "relationship"}),
            ("user", "I know people at Google, Stripe, and Airbnb.", {"salience": 0.4, "kind": "fact"}),
            ("user", "I go to tech meetups monthly.", {"salience": 0.4, "kind": "routine"}),
            ("user", "I speak at one conference a year.", {"salience": 0.4, "kind": "routine"}),
            ("user", "I mentor junior engineers outside my team.", {"salience": 0.5, "kind": "routine"}),
            ("user", "I prefer small gatherings over big conferences.", {"salience": 0.4, "kind": "opinion"}),
        ],
        queries=[
            Query("r-referral", "Who introduced me to my current manager?", ["Lin"], "Re"),
        ],
    ),
    Session(
        id="Re-colleagues",
        category="Re",
        turns=[
            ("user", "Brenda is Otto's wife.", {"salience": 0.6, "kind": "relationship"}),
            ("user", "I met Brenda at a company event.", {"salience": 0.4, "kind": "relationship"}),
            ("user", "Brenda is a chef.", {"salience": 0.5, "kind": "identity"}),
            ("user", "Brenda cooks for our team dinners.", {"salience": 0.5, "kind": "relationship"}),
            ("user", "Brenda and I share recipes.", {"salience": 0.4, "kind": "relationship"}),
            ("user", "Brenda's restaurant is in North Beach.", {"salience": 0.5, "kind": "spatial"}),
            ("user", "Brenda is Cuban-American.", {"salience": 0.4, "kind": "identity"}),
            ("user", "I go to Brenda's restaurant for birthdays.", {"salience": 0.4, "kind": "routine"}),
            ("user", "Brenda named her signature dish after Otto.", {"salience": 0.4, "kind": "fact"}),
            ("user", "Brenda is one of my favorite people.", {"salience": 0.5, "kind": "relationship"}),
        ],
        queries=[
            Query("re-brenda-prof", "What is Brenda's profession?", ["chef"], "Re"),
        ],
    ),
    Session(
        id="E-anniversary",
        category="E",
        turns=[
            ("user", "My wedding anniversary was last month.", {"salience": 0.7, "kind": "event"}),
            ("user", "We went to Brenda's restaurant.", {"salience": 0.6, "kind": "event"}),
            ("user", "Jordan and I have been together for 8 years.", {"salience": 0.6, "kind": "relationship"}),
            ("user", "We got married in 2021.", {"salience": 0.5, "kind": "event"}),
            ("user", "Otto was the best man.", {"salience": 0.5, "kind": "relationship"}),
            ("user", "Sam flew in for the wedding.", {"salience": 0.5, "kind": "relationship"}),
            ("user", "We honeymooned in Patagonia.", {"salience": 0.4, "kind": "event"}),
            ("user", "It was a small ceremony.", {"salience": 0.4, "kind": "event"}),
            ("user", "Jordan and I both wore blue.", {"salience": 0.3, "kind": "event"}),
            ("user", "I felt so happy that day.", {"salience": 0.6, "kind": "emotion"}),
        ],
        queries=[
            Query("e-anniversary", "When was my wedding anniversary?", ["last month"], "E"),
        ],
    ),
    Session(
        id="P-roles",
        category="P",
        turns=[
            ("user", "Otto is my manager.", {"salience": 0.7, "kind": "identity"}),
            ("user", "Maria is my mentee.", {"salience": 0.6, "kind": "identity"}),
            ("user", "Sam is my best friend.", {"salience": 0.7, "kind": "identity"}),
            ("user", "Jordan is my partner.", {"salience": 0.7, "kind": "identity"}),
            ("user", "Anna is my sister.", {"salience": 0.7, "kind": "identity"}),
            ("user", "Brenda is Otto's wife.", {"salience": 0.6, "kind": "identity"}),
            ("user", "Tom is my college roommate.", {"salience": 0.5, "kind": "identity"}),
            ("user", "Lin is my former coworker.", {"salience": 0.5, "kind": "identity"}),
            ("user", "Jess is my book club friend.", {"salience": 0.5, "kind": "identity"}),
            ("user", "Mom is my mom.", {"salience": 0.7, "kind": "identity"}),
        ],
        queries=[
            Query("p-roles", "Who has what role in my life?", ["Otto", "manager", "Maria", "mentee"], "P"),
        ],
    ),
    Session(
        id="R-housework",
        category="R",
        turns=[
            ("user", "Jordan and I split housework.", {"salience": 0.4, "kind": "routine"}),
            ("user", "I do the dishes on weekdays.", {"salience": 0.4, "kind": "routine"}),
            ("user", "Jordan does the laundry.", {"salience": 0.4, "kind": "routine"}),
            ("user", "We share cooking.", {"salience": 0.4, "kind": "routine"}),
            ("user", "I take out the trash on Tuesdays.", {"salience": 0.4, "kind": "routine"}),
            ("user", "Jordan does the recycling.", {"salience": 0.4, "kind": "routine"}),
            ("user", "We clean on Saturday mornings.", {"salience": 0.4, "kind": "routine"}),
            ("user", "I vacuum, Jordan mops.", {"salience": 0.4, "kind": "routine"}),
            ("user", "We share grocery shopping.", {"salience": 0.4, "kind": "routine"}),
            ("user", "I do the dishes by hand.", {"salience": 0.4, "kind": "routine"}),
        ],
        queries=[
            Query("r-dishes", "Who does the dishes on weekdays?", ["I do", "I"], "R"),
        ],
    ),
    Session(
        id="O-books",
        category="O",
        turns=[
            ("user", "I read 30 books a year.", {"salience": 0.4, "kind": "fact"}),
            ("user", "I prefer non-fiction.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I dislike most fiction.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I think sci-fi is the best fiction genre.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I dislike romance novels.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I love biographies.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I read mostly non-fiction business books.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I prefer physical books over Kindle.", {"salience": 0.4, "kind": "opinion"}),
            ("user", "I buy books at Green Apple Books.", {"salience": 0.4, "kind": "spatial"}),
            ("user", "I like used bookstores.", {"salience": 0.4, "kind": "opinion"}),
        ],
        queries=[
            Query("o-genre", "What fiction genre do I prefer?", ["sci-fi"], "O"),
        ],
    ),
    Session(
        id="Sp-running",
        category="Sp",
        turns=[
            ("user", "I run in Dolores Park.", {"salience": 0.5, "kind": "spatial"}),
            ("user", "I run in Golden Gate Park.", {"salience": 0.5, "kind": "spatial"}),
            ("user", "I run along the Embarcadero.", {"salience": 0.5, "kind": "spatial"}),
            ("user", "I avoid running in the Tenderloin.", {"salience": 0.4, "kind": "spatial"}),
            ("user", "I run at Lake Merced for long runs.", {"salience": 0.5, "kind": "spatial"}),
            ("user", "I run on the Bay Bridge trail.", {"salience": 0.4, "kind": "spatial"}),
            ("user", "I prefer park runs over street runs.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I run early in the morning.", {"salience": 0.4, "kind": "routine"}),
            ("user", "I prefer running with friends.", {"salience": 0.4, "kind": "social"}),
            ("user", "I run alone when I want to think.", {"salience": 0.4, "kind": "preference"}),
        ],
        queries=[
            Query("sp-long-run", "Where do I run for long runs?", ["Lake Merced"], "Sp"),
        ],
    ),
    Session(
        id="E-trip",
        category="E",
        turns=[
            ("user", "I went to Japan last spring.", {"salience": 0.7, "kind": "event"}),
            ("user", "I went to Tokyo, Kyoto, and Osaka.", {"salience": 0.6, "kind": "event"}),
            ("user", "I loved Kyoto the most.", {"salience": 0.6, "kind": "opinion"}),
            ("user", "I ate ramen in Tokyo.", {"salience": 0.5, "kind": "event"}),
            ("user", "I visited Fushimi Inari shrine.", {"salience": 0.5, "kind": "event"}),
            ("user", "I took the Shinkansen.", {"salience": 0.4, "kind": "event"}),
            ("user", "I went to Universal Studios Japan.", {"salience": 0.4, "kind": "event"}),
            ("user", "I stayed at a ryokan in Kyoto.", {"salience": 0.5, "kind": "event"}),
            ("user", "I ate kaiseki dinner.", {"salience": 0.4, "kind": "event"}),
            ("user", "I want to go back.", {"salience": 0.5, "kind": "intent"}),
        ],
        queries=[
            Query("e-japan-city", "Where in Japan did I go that I loved most?", ["Kyoto"], "E"),
        ],
    ),
    Session(
        id="Pr-recipe",
        category="Pr",
        turns=[
            ("user", "To make scrambled eggs: crack two eggs into a bowl.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Add a pinch of salt.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Whisk until smooth.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Heat butter in a pan over medium-low heat.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Pour in the eggs.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Stir constantly with a spatula.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Remove from heat when still slightly wet.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "The residual heat finishes them.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "Season with pepper and serve.", {"salience": 0.5, "kind": "procedure"}),
            ("user", "I eat them on toast.", {"salience": 0.4, "kind": "preference"}),
        ],
        queries=[
            Query("pr-eggs", "When do I remove scrambled eggs from the heat?", ["slightly wet", "residual"], "Pr"),
        ],
    ),
    Session(
        id="Re-old-friend",
        category="Re",
        turns=[
            ("user", "Tom is my college roommate.", {"salience": 0.5, "kind": "identity"}),
            ("user", "Tom lives in Austin.", {"salience": 0.5, "kind": "spatial"}),
            ("user", "Tom is a professor.", {"salience": 0.5, "kind": "identity"}),
            ("user", "Tom has two kids.", {"salience": 0.4, "kind": "fact"}),
            ("user", "Tom and I don't talk as much as I'd like.", {"salience": 0.4, "kind": "relationship"}),
            ("user", "Tom visited last summer.", {"salience": 0.4, "kind": "event"}),
            ("user", "We had a great time catching up.", {"salience": 0.5, "kind": "emotion"}),
            ("user", "Tom is a serious runner.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Tom and I ran together in college.", {"salience": 0.4, "kind": "history"}),
            ("user", "We should plan a reunion soon.", {"salience": 0.4, "kind": "intent"}),
        ],
        queries=[
            Query("re-tom-loc", "Where does Tom live?", ["Austin"], "Re"),
        ],
    ),
    Session(
        id="O-invest",
        category="O",
        turns=[
            ("user", "I invest in index funds.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I don't believe in stock picking.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I think crypto is too volatile.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I prefer real estate over stocks.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I think dollar-cost averaging is the best strategy.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I don't day-trade.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I prefer Vanguard funds.", {"salience": 0.4, "kind": "opinion"}),
            ("user", "I think bonds are important for retirement.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I don't invest in individual stocks.", {"salience": 0.5, "kind": "opinion"}),
            ("user", "I max out my 401k.", {"salience": 0.4, "kind": "fact"}),
        ],
        queries=[
            Query("o-strategy", "What's my investment strategy?", ["index", "dollar-cost"], "O"),
        ],
    ),
    Session(
        id="Em-therapy",
        category="Em",
        turns=[
            ("user", "I started therapy last year.", {"salience": 0.6, "kind": "fact"}),
            ("user", "I see a therapist weekly.", {"salience": 0.5, "kind": "routine"}),
            ("user", "I prefer virtual sessions.", {"salience": 0.4, "kind": "preference"}),
            ("user", "Therapy has helped me manage anxiety.", {"salience": 0.6, "kind": "emotion"}),
            ("user", "I've been sleeping better.", {"salience": 0.5, "kind": "emotion"}),
            ("user", "I have tools for when I feel overwhelmed.", {"salience": 0.5, "kind": "self-knowledge"}),
            ("user", "I journal in the morning.", {"salience": 0.4, "kind": "routine"}),
            ("user", "I use a breathing exercise for stress.", {"salience": 0.4, "kind": "procedure"}),
            ("user", "I limit caffeine after noon.", {"salience": 0.4, "kind": "routine"}),
            ("user", "I feel more grounded overall.", {"salience": 0.6, "kind": "emotion"}),
        ],
        queries=[
            Query("em-therapy", "How has therapy helped me?", ["anxiety", "sleeping"], "Em"),
        ],
    ),
    Session(
        id="Otto-bio",
        category="P",
        turns=[
            ("user", "Otto is my manager.", {"salience": 0.7, "kind": "identity"}),
            ("user", "Otto started at the company in 2019.", {"salience": 0.5, "kind": "fact"}),
            ("user", "Otto was previously at Stripe.", {"salience": 0.5, "kind": "fact"}),
            ("user", "Otto's been in engineering for 15 years.", {"salience": 0.5, "kind": "fact"}),
            ("user", "Otto has a CS degree from Stanford.", {"salience": 0.5, "kind": "fact"}),
            ("user", "Otto's wife is Brenda.", {"salience": 0.6, "kind": "identity"}),
            ("user", "Otto has a chocolate lab named Pixel.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto mentors junior engineers.", {"salience": 0.5, "kind": "fact"}),
            ("user", "Otto prefers Go for systems work.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto's birthday is in March.", {"salience": 0.4, "kind": "fact"}),
        ],
        queries=[
            Query("p-otto-prev", "Where did Otto work before?", ["Stripe"], "P"),
        ],
    ),
    Session(
        id="Otto2",
        category="P",
        turns=[
            ("user", "Otto is my manager.", {"salience": 0.7, "kind": "identity"}),
            ("user", "Otto's wife is Brenda.", {"salience": 0.6, "kind": "identity"}),
            ("user", "Otto is the deploy pipeline author.", {"salience": 0.6, "kind": "fact"}),
            ("user", "Otto prefers Go for systems.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto has a chocolate lab named Pixel.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto works from home on Mondays.", {"salience": 0.4, "kind": "fact"}),
            ("user", "Otto drinks cold brew.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto reads sci-fi.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto prefers mechanical keyboards.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto's birthday is in March.", {"salience": 0.4, "kind": "fact"}),
        ],
        queries=[
            Query("p-otto-pet", "What kind of pet does Otto have?", ["chocolate lab", "lab"], "P"),
        ],
    ),
    Session(
        id="Otto3",
        category="P",
        turns=[
            ("user", "Otto is my manager.", {"salience": 0.7, "kind": "identity"}),
            ("user", "Otto's wife is Brenda.", {"salience": 0.6, "kind": "identity"}),
            ("user", "Otto is the author of the deploy pipeline.", {"salience": 0.6, "kind": "fact"}),
            ("user", "Otto's birthday is in March.", {"salience": 0.4, "kind": "fact"}),
            ("user", "Otto's dog Pixel is a chocolate lab.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto drinks cold brew.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto reads sci-fi.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto prefers Go.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto works from home on Mondays.", {"salience": 0.4, "kind": "fact"}),
            ("user", "Otto has a CS degree from Stanford.", {"salience": 0.5, "kind": "fact"}),
        ],
        queries=[
            Query("p-otto-prev-job", "Where did Otto come from before?", ["Stripe"], "P"),
        ],
    ),
    Session(
        id="Otto4",
        category="P",
        turns=[
            ("user", "Otto is my manager.", {"salience": 0.7, "kind": "identity"}),
            ("user", "Otto's wife Brenda is a chef.", {"salience": 0.6, "kind": "identity"}),
            ("user", "Otto wrote the deploy pipeline.", {"salience": 0.6, "kind": "fact"}),
            ("user", "Otto's birthday is March.", {"salience": 0.4, "kind": "fact"}),
            ("user", "Otto's dog Pixel is a chocolate lab.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto likes cold brew.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto reads sci-fi.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto prefers Go for systems.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto works from home on Mondays.", {"salience": 0.4, "kind": "fact"}),
            ("user", "Otto has a CS degree from Stanford.", {"salience": 0.5, "kind": "fact"}),
        ],
        queries=[
            Query("p-otto-relationship", "Who is related to Otto?", ["Brenda", "wife"], "P"),
        ],
    ),
    Session(
        id="Otto5",
        category="P",
        turns=[
            ("user", "Otto is my manager.", {"salience": 0.7, "kind": "identity"}),
            ("user", "Otto's wife is Brenda.", {"salience": 0.6, "kind": "identity"}),
            ("user", "Otto wrote the deploy pipeline.", {"salience": 0.6, "kind": "fact"}),
            ("user", "Otto's birthday is March.", {"salience": 0.4, "kind": "fact"}),
            ("user", "Otto has a chocolate lab named Pixel.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto likes cold brew.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto reads sci-fi.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto prefers Go.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto works from home on Mondays.", {"salience": 0.4, "kind": "fact"}),
            ("user", "Otto has a Stanford CS degree.", {"salience": 0.5, "kind": "fact"}),
        ],
        queries=[
            Query("p-otto-degree", "What's Otto's education?", ["Stanford", "CS"], "P"),
        ],
    ),
    Session(
        id="Otto6",
        category="P",
        turns=[
            ("user", "Otto is my manager.", {"salience": 0.7, "kind": "identity"}),
            ("user", "Otto's wife Brenda is a chef.", {"salience": 0.6, "kind": "identity"}),
            ("user", "Otto wrote the deploy pipeline.", {"salience": 0.6, "kind": "fact"}),
            ("user", "Otto's birthday is March.", {"salience": 0.4, "kind": "fact"}),
            ("user", "Otto has a chocolate lab named Pixel.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto likes cold brew.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto reads sci-fi.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto prefers Go.", {"salience": 0.4, "kind": "person-trait"}),
            ("user", "Otto works from home on Mondays.", {"salience": 0.4, "kind": "fact"}),
            ("user", "Otto has a Stanford CS degree.", {"salience": 0.5, "kind": "fact"}),
        ],
        queries=[
            Query("p-otto-pipeline", "What did Otto build?", ["deploy", "pipeline"], "P"),
        ],
    ),
]
