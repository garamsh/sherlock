"""Remove each new behaviour in a copy of its commit and show a test fails.

    python3 docs/research/agent-context/mutation.py > docs/research/agent-context/evidence/mutation.txt

Each mutation runs in a fresh `git archive` of the commit that introduced
the behaviour; the working tree is never changed. A mutation passes this
check when the named tests fail with it and pass without it.
"""

import subprocess
import sys
import tempfile
from pathlib import Path

REPO = Path(__file__).resolve().parents[3]
A, B, HARNESS = "443142f", "2d43eec", "6b14e96"

MUTATIONS = [
    (A, "internal/agent/service.go", "if call.Name == MemorySearchTool && s.opts.Conversation != nil {",
     "if false && call.Name == MemorySearchTool && s.opts.Conversation != nil {",
     "./internal/agent/ ./internal/gateway/", "TestSessionRun_SearchRanksWithTheModelsQuery|TestConversation_SearchReturnsTheEpisodeForTheModelsQuery",
     "A: memory_search no longer reaches the store"),
    (A, "internal/agent/service.go", "content += noteSection(note)", "_ = noteSection(note)",
     "./internal/agent/ ./internal/gateway/", "TestSessionRun_NoteLeadsEveryTurnAndIsReplaced|TestConversation_NoteSurvivesCompactionAndRestart",
     "A: the note is not injected into the leading message"),
    (A, "internal/agent/service.go", "if err := s.opts.Conversation.SetNote(ctx, *note); err != nil {",
     "if err := error(nil); err != nil {",
     "./internal/agent/", "TestSessionRun_NoteLeadsEveryTurnAndIsReplaced|TestSessionRun_OvergrownNoteIsHandedBack",
     "A: the terminal call's note is not saved"),
    (A, "internal/agent/writeback.go", "if n := len([]rune(trimmed)); n > MaxNoteLength {", "if n := len([]rune(trimmed)); n < 0 {",
     "./internal/agent/", "TestParseWriteback_NoteTooLong|TestSessionRun_OvergrownNoteIsHandedBack",
     "A: the note bound is not enforced"),
    (B, "internal/agent/service.go", "understood, err := understand(ctx, s.model, note, s.history, task)",
     "understood, err := Understanding{Restatement: task}, error(nil); _ = note",
     "./internal/agent/ ./internal/gateway/", "TestSessionRun_UnderstandingResolvesBeforeRecall|TestConversation_NoteSurvivesCompactionAndRestart",
     "B: the understanding call is skipped"),
    (B, "internal/agent/service.go", "recalled, err := s.recall(ctx, understood.recallText())", "recalled, err := s.recall(ctx, task)",
     "./internal/agent/", "TestSessionRun_UnderstandingResolvesBeforeRecall|TestSessionRun_OvergrownNoteIsRefused",
     "B: recall ranks by the raw input"),
    (B, "internal/agent/service.go", "if err := s.opts.Conversation.SetNote(ctx, *understood.Note); err != nil {",
     "if err := error(nil); err != nil {",
     "./internal/agent/", "TestSessionRun_UnderstandingResolvesBeforeRecall",
     "B: the understanding call's note is not saved"),
    (B, "internal/agent/understand.go", "out = out[len(out)-understandWindow:]", "out = out[:]",
     "./internal/agent/", "TestSessionRun_UnderstandingSeesRecentVisibleConversation",
     "B: the understanding step is not bounded to 16 messages"),
    (HARNESS, "docs/research/agent-context/harness/proxy.go", "if l.spent+l.reserved+usd > l.ceiling {", "if false {",
     "./docs/research/agent-context/harness/", "TestLedger_RefusesPastTheCeilingAndSettlesAtUsage|TestProxy_RetriesARateLimitAndRefusesPastTheCeiling",
     "harness: the ledger does not refuse past the ceiling"),
    (HARNESS, "docs/research/agent-context/harness/proxy.go", "if cached, done := p.cached(base.RequestSHA256); cached != nil {",
     "if cached, done := []byte(nil), func() {}; cached != nil {",
     "./docs/research/agent-context/harness/", "TestProxy_SynthesisIsAnsweredOnceThenFromTheCache",
     "harness: identical syntheses are not cached"),
]


def run_tests(tree, packages, pattern):
    proc = subprocess.run(["go", "test", "-count=1", "-run", pattern] + packages.split(), cwd=tree,
                          capture_output=True, text=True, env={**__import__("os").environ, "GOFLAGS": "-buildvcs=false"})
    failed = sorted({line.split()[2] for line in proc.stdout.splitlines() if line.startswith("--- FAIL")})
    return proc.returncode, failed


def main():
    ok = True
    for commit, path, old, new, packages, pattern, label in MUTATIONS:
        with tempfile.TemporaryDirectory() as tree:
            archive = subprocess.run(["git", "archive", commit], cwd=REPO, capture_output=True, check=True).stdout
            subprocess.run(["tar", "-x", "-C", tree], input=archive, check=True)
            clean_code, _ = run_tests(tree, packages, pattern)
            target = Path(tree) / path
            text = target.read_text()
            if text.count(old) != 1:
                raise SystemExit(f"{label}: expected one occurrence in {path}")
            target.write_text(text.replace(old, new))
            code, failed = run_tests(tree, packages, pattern)
        held = clean_code == 0 and code != 0 and failed
        ok = ok and bool(held)
        print(f"{label}\n  commit {commit}, {path}\n  unmutated: {'pass' if clean_code == 0 else 'FAIL'}; mutated: {'fail' if code else 'pass'}; failing: {', '.join(failed) or 'none'}\n")
    print("every mutation broke a test" if ok else "SOME MUTATION BROKE NO TEST")
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
