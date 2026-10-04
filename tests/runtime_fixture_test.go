package tests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

// The fixture's identity (#937). The agent ID is a GRN, served verbatim
// (ADR 0016); the epoch is the launcher's label for the activation.
const (
	fixtureAgentID = "grn:garam:default:agent:9370000000000937"
	fixtureEpoch   = "sherlock-937-epoch-1"
	fixtureSenderA = "fixture-sender-a"
	fixtureSenderB = "fixture-sender-b"
	// fixtureLocked is the startup refusal of a second writer (#936).
	fixtureLocked = "another process owns the memory store"
)

// fixtureRuntime is where the fixture's agent runs: the built binary on
// this host, or the pinned images under compose. The acceptance sequence
// is the same against both; only the process handling differs.
type fixtureRuntime interface {
	// start serves the agent on the store under agentDir, with the
	// workspace serving workspaceDir, and returns once the status route
	// answers.
	start(t *testing.T, agentDir, workspaceDir string)
	// stop sends SIGTERM to the agent and returns its exit status once it
	// has exited.
	stop(t *testing.T) int
	// secondWriter starts another agent on the store under agentDir and
	// returns its exit status and output once it exits.
	secondWriter(t *testing.T, agentDir string) (int, string)
	// workspaceFile reads a file the agent's task wrote into the workspace.
	workspaceFile(t *testing.T, workspaceDir, name string) string
	baseURL() string
	client(sender string) *http.Client
	// realModel reports whether turns run on the real model, which is
	// what makes a note and a mid episode part of the sequence.
	realModel() bool
	// spend is the model traffic the run made, or nil where it made none
	// that leaves the host.
	spend() *fixtureSpend
}

// fixtureStatus is the C3 status body (ADR 0022).
type fixtureStatus struct {
	ContractVersion int    `json:"contract_version"`
	AgentID         string `json:"agent_id"`
	AssignmentEpoch string `json:"assignment_epoch"`
	Generation      string `json:"generation"`
	ConfigRevision  string `json:"config_revision"`
	State           string `json:"state"`
}

// snapshotReport is what `snapshot.sh verify` prints.
type snapshotReport struct {
	Integrity   string `json:"integrity"`
	Sources     int    `json:"sources"`
	Completions []struct {
		Sender string `json:"sender"`
		ID     string `json:"id"`
	} `json:"completions"`
	Episodes []struct {
		ID            int64  `json:"id"`
		SourceFirst   int64  `json:"source_first"`
		SourceLast    int64  `json:"source_last"`
		SummarySHA256 string `json:"summary_sha256"`
		Embedded      bool   `json:"embedded"`
	} `json:"episodes"`
	Note struct {
		Present bool   `json:"present"`
		SHA256  string `json:"sha256"`
	} `json:"note"`
	Outbox []fixtureOutboxEntry `json:"outbox"`
}

type fixtureOutboxEntry struct {
	File            string   `json:"file"`
	ID              string   `json:"id"`
	CreatedAt       string   `json:"created_at"`
	AgentID         string   `json:"agent_id"`
	AssignmentEpoch string   `json:"assignment_epoch"`
	Generation      string   `json:"generation"`
	InReplyTo       []string `json:"in_reply_to"`
	ContentSHA256   string   `json:"content_sha256"`
}

// fixtureEvidence is the run's record: identifiers, hashes and outcomes,
// never message or memory content. The real run's copy is what the pull
// request reports.
type fixtureEvidence struct {
	Mode           string                   `json:"mode"`
	AgentID        string                   `json:"agent_id"`
	Epoch          string                   `json:"assignment_epoch"`
	ConfigRevision string                   `json:"config_revision"`
	Statuses       map[string]fixtureStatus `json:"statuses"`
	Completed      map[string]string        `json:"completed"`
	TaskFile       string                   `json:"task_file_sha256"`
	SecondWriter   string                   `json:"second_writer"`
	HeldCopy       string                   `json:"held_copy_refusal"`
	Replay         map[string]string        `json:"replay"`
	SenderScope    map[string]string        `json:"sender_scope"`
	Conflict       int                      `json:"conflict_status"`
	ExitCodes      map[string]int           `json:"exit_codes"`
	Manifest       string                   `json:"snapshot_manifest"`
	Snapshot       snapshotReport           `json:"snapshot"`
	Restored       snapshotReport           `json:"restored"`
	RecalledIDs    []string                 `json:"follow_up_recalled_ids,omitempty"`
	FollowUpNote   bool                     `json:"follow_up_answer_carries_note,omitempty"`
	Spend          *fixtureSpend            `json:"spend,omitempty"`
	Steps          []fixtureStepResult      `json:"steps"`
}

// TestRuntimeFixture_Offline runs the acceptance sequence against the
// built binary with the mock provider: no container, no model, and the
// fixture's own config file under --strict-config. The mock answers one
// turn per process, so the note and the mid episode are left to the real
// run; every other step is asserted here.
func TestRuntimeFixture_Offline(t *testing.T) {
	rt := newOfflineFixture(t)
	runFixture(t, rt, t.TempDir())
}

// runFixture is the acceptance sequence (#937). storage is a fresh
// directory the run owns.
//
// Each step runs as a subtest, so a check that fails ends that step and no
// other. A step runs only when what it depends on happened, and otherwise
// is recorded as not run with the reason. The run fails if any step failed,
// and the evidence records every step's outcome.
func runFixture(t *testing.T, rt fixtureRuntime, storage string) {
	revision := fixtureConfigValue(t, "revision")
	ev := &fixtureEvidence{
		Mode:    map[bool]string{true: "real-model", false: "offline"}[rt.realModel()],
		AgentID: fixtureAgentID, Epoch: fixtureEpoch, ConfigRevision: revision,
		Statuses: map[string]fixtureStatus{}, Completed: map[string]string{},
		Replay: map[string]string{}, SenderScope: map[string]string{},
		ExitCodes: map[string]int{},
	}
	steps := &fixtureSteps{t: t}
	defer func() {
		ev.Steps = steps.results
		ev.Spend = rt.spend()
		writeFixtureEvidence(t, ev)
	}()

	agentDir := filepath.Join(storage, "run", "agent")
	workspaceDir := filepath.Join(storage, "run", "workspace")
	snapshot := filepath.Join(storage, "snapshot")
	restoredDir := filepath.Join(storage, "restored", "agent")
	for _, dir := range []string{agentDir, workspaceDir, filepath.Join(storage, "restored", "workspace")} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	task := fixtureTasks(rt.realModel())
	real := rt.realModel()

	// running is whether an agent process this sequence started is up,
	// which decides whether a drain step has anything to drain.
	running := false
	// started bounds every entry's created_at from below: no entry this
	// run writes can be minted before the run began.
	started := time.Now().UTC()
	var first, second fixtureStatus
	var sidecars map[string]bool

	// C1/C3: the first activation answers with its identity.
	steps.run("first activation", "3: status", "", func(t *testing.T) {
		rt.start(t, agentDir, workspaceDir)
		running = true
		first = fixtureGetStatus(t, rt)
		ev.Statuses["first"] = first
		assertFixtureStatus(t, first, revision)
	})

	// The task's effect, and completion read from the lookup alone.
	steps.run("task m1", "1: task", steps.needs("first activation"), func(t *testing.T) {
		fixtureSend(t, rt, fixtureSenderA, "m1", task.first, http.StatusAccepted)
		ev.Completed["m1"] = fixtureAwaitCompleted(t, rt, fixtureSenderA, "m1")
		written := rt.workspaceFile(t, workspaceDir, task.file)
		require.NotEmpty(t, written, "the task wrote %s into the workspace", task.file)
		ev.TaskFile = sha256Hex(written)
	})
	if real {
		steps.run("task m2", "5: note and episode", steps.needs("first activation"), func(t *testing.T) {
			fixtureSend(t, rt, fixtureSenderA, "m2", task.second, http.StatusAccepted)
			ev.Completed["m2"] = fixtureAwaitCompleted(t, rt, fixtureSenderA, "m2")
		})
	}

	// C4: SIGTERM drains and the process exits cleanly.
	steps.run("drain", "4: drain and restart", steps.unless(running, "no agent was started"), func(t *testing.T) {
		running = false
		ev.ExitCodes["first"] = rt.stop(t)
		require.Equal(t, 0, ev.ExitCodes["first"], "the drained agent exits 0")
	})

	// The supported copy, from the drained, stopped writer. A sidecar the
	// closed store does not have is recorded absent, not invented.
	steps.run("snapshot", "snapshot boundary", steps.needs("drain"), func(t *testing.T) {
		sidecars = storeSidecars(agentDir)
		fixtureSnapshot(t, "copy", filepath.Join(agentDir, "memory.db"), snapshot)
		manifest, err := os.ReadFile(filepath.Join(snapshot, "manifest"))
		require.NoError(t, err)
		ev.Manifest = string(manifest)
		assert.Equal(t, sidecars, storeSidecars(snapshot))
		for suffix, present := range sidecars {
			if !present {
				assert.Contains(t, ev.Manifest, "absent memory.db"+suffix+"\n")
			}
		}
		ev.Snapshot = fixtureVerify(t, snapshot)
		assert.Equal(t, "ok", ev.Snapshot.Integrity)
		wantCompleted := []string{"m1"}
		if real {
			wantCompleted = append(wantCompleted, "m2")
		}
		assert.Equal(t, wantCompleted, completionIDs(ev.Snapshot, fixtureSenderA))
	})

	// C6: every entry the first activation wrote is pending (no consumer
	// collects here), names its origin, and one answers m1.
	steps.run("outbox", "2: outbox", steps.needs("snapshot", "task m1"), func(t *testing.T) {
		require.NotEmpty(t, ev.Snapshot.Outbox)
		repliedM1 := false
		for _, entry := range ev.Snapshot.Outbox {
			assert.Equal(t, entry.ID+".json", entry.File)
			assert.Equal(t, fixtureAgentID, entry.AgentID)
			assert.Equal(t, fixtureEpoch, entry.AssignmentEpoch)
			assert.Equal(t, first.Generation, entry.Generation)
			if len(entry.InReplyTo) == 1 && entry.InReplyTo[0] == "m1" {
				repliedM1 = true
			}
		}
		assert.True(t, repliedM1, "an outbox entry's in_reply_to equals the sent id m1")
	})

	// Every new entry carries created_at: an RFC 3339 instant in UTC,
	// minted inside this run, read as message_send wrote it.
	steps.run("outbox created_at", "2: outbox", steps.needs("outbox"), func(t *testing.T) {
		for _, entry := range ev.Snapshot.Outbox {
			require.NotEmpty(t, entry.CreatedAt, "entry %s carries created_at", entry.ID)
			at, err := time.Parse(time.RFC3339Nano, entry.CreatedAt)
			require.NoError(t, err, "entry %s created_at %q is RFC 3339", entry.ID, entry.CreatedAt)
			assert.True(t, strings.HasSuffix(entry.CreatedAt, "Z"), "entry %s created_at %q is UTC", entry.ID, entry.CreatedAt)
			assert.False(t, at.Before(started.Add(-time.Second)), "entry %s was minted at %s, before the run began at %s", entry.ID, at, started)
			assert.False(t, at.After(time.Now().UTC()), "entry %s was minted at %s, in the future", entry.ID, at)
		}
	})

	steps.run("note and episode", "5: note and episode", steps.needs("snapshot"), func(t *testing.T) {
		if !real {
			// The mock writes an empty note and fills no four-message batch.
			assert.False(t, ev.Snapshot.Note.Present)
			assert.Empty(t, ev.Snapshot.Episodes)
			return
		}
		assert.True(t, ev.Snapshot.Note.Present, "the first activation saved a note")
		require.NotEmpty(t, ev.Snapshot.Episodes, "two completed turns make one mid episode")
		assert.True(t, ev.Snapshot.Episodes[0].Embedded, "the episode is stored with its embedding")
	})

	// Restart on the same storage: same identity, new generation.
	steps.run("restart", "4: drain and restart", steps.needs("drain"), func(t *testing.T) {
		rt.start(t, agentDir, workspaceDir)
		running = true
		second = fixtureGetStatus(t, rt)
		ev.Statuses["restart"] = second
		assertFixtureStatus(t, second, revision)
		assert.NotEqual(t, first.Generation, second.Generation, "a restart mints a new generation")
	})
	restarted := steps.unless(running, "the restart did not start an agent")

	// C2: a second writer on the same store is refused. A copy while the
	// writer holds its lock is refused the same way: whether the writer
	// stopped cannot be established, so nothing may be activated from it.
	steps.run("second writer", "7: second writer", restarted, func(t *testing.T) {
		code, out := rt.secondWriter(t, agentDir)
		ev.SecondWriter = fmt.Sprintf("exit %d: %s", code, lastLine(out, fixtureLocked))
		assert.NotEqual(t, 0, code)
		assert.Contains(t, out, fixtureLocked)
		held := exec.Command(fixtureScript(t), "copy", filepath.Join(agentDir, "memory.db"), filepath.Join(storage, "held-copy"))
		heldOut, err := held.CombinedOutput()
		ev.HeldCopy = strings.TrimSpace(string(heldOut))
		require.Error(t, err, "a copy of a store whose writer holds the lock is refused")
		assert.Contains(t, string(heldOut), "is held: the writer has not stopped")
		assert.NoDirExists(t, filepath.Join(storage, "held-copy"))
	})

	// C5: completion is read from the lookup alone. A replay of m1 is
	// answered from the record and starts no turn; the same ID with other
	// content is a conflict; another sender's lookup of it is unknown.
	steps.run("replay, conflict and sender scope", "6: replay", restarted, func(t *testing.T) {
		ev.Replay["get_after_restart"] = fixtureLookup(t, rt, fixtureSenderA, "m1")
		assert.Equal(t, "completed", ev.Replay["get_after_restart"])
		ev.Replay["post"] = fixtureSend(t, rt, fixtureSenderA, "m1", task.first, http.StatusOK)
		assert.Equal(t, "completed", ev.Replay["post"])
		ev.Conflict = fixtureSendStatus(t, rt, fixtureSenderA, "m1", task.first+" (changed)")
		assert.Equal(t, http.StatusConflict, ev.Conflict)
		ev.SenderScope[fixtureSenderA] = fixtureLookup(t, rt, fixtureSenderA, "m1")
		ev.SenderScope[fixtureSenderB] = fixtureLookup(t, rt, fixtureSenderB, "m1")
		assert.Equal(t, "completed", ev.SenderScope[fixtureSenderA])
		assert.Equal(t, "unknown", ev.SenderScope[fixtureSenderB], "message IDs are scoped to their sender")
	})

	// The note the follow-up asks about has to be the note the fixture set.
	// Read from the store, with no model call, so that a store that altered
	// the input is reported as that and not as a wrong answer.
	if real {
		steps.run("note stored as set", "5: note and episode", steps.needs("snapshot"), func(t *testing.T) {
			require.NoError(t, fixtureNoteGuard(ev.Snapshot, task.note))
		})
	}

	// Read-back: the restored note and the episode, through the follow-up's
	// recall event and, after the drain, its answer.
	if real {
		steps.run("read-back", "5: note and episode", restarted, func(t *testing.T) {
			events := subscribeSSEClient(t, rt.client(fixtureSenderA), rt.baseURL()+"/agents/"+url.PathEscape(fixtureAgentID)+"/events")
			fixtureSend(t, rt, fixtureSenderA, "m3", task.followUp, http.StatusAccepted)
			ev.Completed["m3"] = fixtureAwaitCompleted(t, rt, fixtureSenderA, "m3")
			ev.RecalledIDs = fixtureRecalledIDs(t, events)
			assert.True(t, containsEpisodeID(ev.RecalledIDs), "the follow-up recalled a mid episode: %v", ev.RecalledIDs)
		})
	}

	steps.run("drain after restart", "4: drain and restart", restarted, func(t *testing.T) {
		running = false
		ev.ExitCodes["restart"] = rt.stop(t)
		require.Equal(t, 0, ev.ExitCodes["restart"])
	})

	if real {
		steps.run("follow-up answer", "5: note and episode", steps.needs("note stored as set", "read-back", "drain after restart"), func(t *testing.T) {
			ev.FollowUpNote = fixtureFollowUpCarries(t, agentDir, "m3", task.noteMarker)
			assert.True(t, ev.FollowUpNote, "the follow-up answer carries the note's marker")
		})
	}

	// The replay started no turn: still one completion record for m1 and
	// one entry answering it.
	steps.run("replay started no turn", "6: replay", steps.needs("replay, conflict and sender scope", "drain after restart"), func(t *testing.T) {
		after := filepath.Join(storage, "after-restart")
		fixtureSnapshot(t, "copy", filepath.Join(agentDir, "memory.db"), after)
		afterReport := fixtureVerify(t, after)
		assert.Equal(t, 1, countString(completionIDs(afterReport, fixtureSenderA), "m1"))
		assert.Equal(t, 1, countReplies(afterReport, "m1"))
	})

	// Restore the first snapshot to fresh storage and activate it. It
	// needs the gateway port free, so no agent this sequence started may
	// still be up.
	restore := steps.needs("snapshot")
	if restore == "" {
		restore = steps.unless(!running, "an earlier agent is still up")
	}
	steps.run("restore", "snapshot boundary", restore, func(t *testing.T) {
		fixtureSnapshot(t, "restore", snapshot, filepath.Join(restoredDir, "memory.db"))
		assert.Equal(t, sidecars, storeSidecars(restoredDir), "the restore keeps an absent sidecar absent")
		rt.start(t, restoredDir, filepath.Join(storage, "restored", "workspace"))
		running = true
		third := fixtureGetStatus(t, rt)
		ev.Statuses["restored"] = third
		assertFixtureStatus(t, third, revision)
		assert.NotEqual(t, first.Generation, third.Generation)
		if second.Generation != "" {
			assert.NotEqual(t, second.Generation, third.Generation)
		}
		assert.Equal(t, "completed", fixtureLookup(t, rt, fixtureSenderA, "m1"), "the restored store holds m1's completion")
		if real && ev.Completed["m3"] == "completed" {
			assert.Equal(t, "unknown", fixtureLookup(t, rt, fixtureSenderA, "m3"), "the restored store predates m3")
		}
		running = false
		ev.ExitCodes["restored"] = rt.stop(t)
		require.Equal(t, 0, ev.ExitCodes["restored"])

		// Pending entries keep their payload and origin under the new
		// generation: the restored outbox is the snapshot's, byte for byte.
		restoredCopy := filepath.Join(storage, "restored-copy")
		fixtureSnapshot(t, "copy", filepath.Join(restoredDir, "memory.db"), restoredCopy)
		ev.Restored = fixtureVerify(t, restoredCopy)
		assert.Equal(t, ev.Snapshot.Outbox, ev.Restored.Outbox)
		assert.Equal(t, ev.Snapshot.Completions, ev.Restored.Completions)
		assert.Equal(t, ev.Snapshot.Note, ev.Restored.Note)
		assert.Equal(t, ev.Snapshot.Episodes, ev.Restored.Episodes)
	})
}

// fixtureStepResult is one step's outcome in the evidence.
type fixtureStepResult struct {
	Step string `json:"step"`
	// Item is the acceptance item of #937 the step bears on.
	Item    string `json:"item"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
}

// fixtureSteps runs the sequence's steps as subtests and records each
// outcome: pass, fail, or not-run with the reason.
type fixtureSteps struct {
	t       *testing.T
	results []fixtureStepResult
}

// run runs fn as the subtest step unless blocked names why it cannot run.
func (s *fixtureSteps) run(step, item, blocked string, fn func(t *testing.T)) {
	if blocked != "" {
		s.results = append(s.results, fixtureStepResult{Step: step, Item: item, Outcome: "not-run", Reason: blocked})
		s.t.Logf("step %q not run: %s", step, blocked)
		return
	}
	outcome := "fail"
	if s.t.Run(step, fn) {
		outcome = "pass"
	}
	s.results = append(s.results, fixtureStepResult{Step: step, Item: item, Outcome: outcome})
}

func (s *fixtureSteps) outcome(step string) string {
	for _, r := range s.results {
		if r.Step == step {
			return r.Outcome
		}
	}
	return "not-run"
}

// needs returns why a step depending on these cannot run, or empty when
// every one of them passed.
func (s *fixtureSteps) needs(steps ...string) string {
	for _, step := range steps {
		if got := s.outcome(step); got != "pass" {
			return fmt.Sprintf("needs %q, which is %s", step, got)
		}
	}
	return ""
}

// unless returns reason when cond is false, or empty when it holds.
func (s *fixtureSteps) unless(cond bool, reason string) string {
	if cond {
		return ""
	}
	return reason
}

// fixtureTask is what the sequence asks the agent to do.
type fixtureTask struct {
	first, second, followUp string
	// file is the workspace file the first task writes.
	file string
	// note is the exact note the first task sets, which the fixture checks
	// the store holds before it asks about it.
	note string
	// noteMarker is the marker the first task saves in the note and the
	// follow-up's answer has to carry. It is worded so that the store's
	// credential redaction leaves it alone: a credential word followed by a
	// value is stored as "[redacted credential]" (internal/memory/sqlite).
	noteMarker string
}

// fixtureNote is the note the real-model task sets.
const fixtureNote = "fixture marker amber-heron-937"

func fixtureTasks(real bool) fixtureTask {
	if !real {
		// The mock's canned turn writes question.txt whatever it is asked.
		return fixtureTask{first: e2eQuestion, file: "question.txt"}
	}
	return fixtureTask{
		first: "Use file_write to create fixture-937.txt in the workspace containing the single line " +
			"`sherlock-937 fixture`. Then reply to me with message_send saying the file is written. " +
			"Finally, set your note to exactly: `" + fixtureNote + "`.",
		second: "Read fixture-937.txt back with file_read and reply to me with message_send quoting its content. " +
			"Keep your note unchanged.",
		followUp: "What fixture marker does your note hold, and which file did you write earlier? " +
			"Reply to me with message_send. Keep your note unchanged.",
		file:       "fixture-937.txt",
		note:       fixtureNote,
		noteMarker: "amber-heron-937",
	}
}

// fixtureConfigValue reads a top-level or one-level key out of the
// fixture's agent.yaml, which owns the value.
func fixtureConfigValue(t *testing.T, key string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "deploy", "runtime-fixture", "agent.yaml"))
	require.NoError(t, err)
	section, name, nested := strings.Cut(key, ".")
	if !nested {
		name = section
	}
	inSection := !nested
	for _, line := range strings.Split(string(raw), "\n") {
		if nested && !strings.HasPrefix(line, " ") {
			inSection = strings.TrimSpace(line) == section+":"
			continue
		}
		if !inSection {
			continue
		}
		if m := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(name) + `:\s*"?([^"#]*?)"?\s*$`).FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	t.Fatalf("deploy/runtime-fixture/agent.yaml holds no %s", key)
	return ""
}

func fixtureScript(t *testing.T) string {
	return filepath.Join(moduleRoot(t), "deploy", "runtime-fixture", "snapshot.sh")
}

func fixtureSnapshot(t *testing.T, args ...string) {
	t.Helper()
	out, err := exec.Command(fixtureScript(t), args...).CombinedOutput()
	require.NoError(t, err, "snapshot.sh %v: %s", args, out)
}

func fixtureVerify(t *testing.T, dir string) snapshotReport {
	t.Helper()
	cmd := exec.Command(fixtureScript(t), "verify", dir)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	require.NoError(t, err, "snapshot.sh verify %s: %s", dir, stderr.String())
	var report snapshotReport
	require.NoError(t, json.Unmarshal(out, &report), "verify output: %s", out)
	return report
}

func fixtureURL(rt fixtureRuntime, path string) string {
	return rt.baseURL() + "/agents/" + url.PathEscape(fixtureAgentID) + path
}

func fixtureGetStatus(t *testing.T, rt fixtureRuntime) fixtureStatus {
	t.Helper()
	resp, err := rt.client(fixtureSenderA).Get(fixtureURL(rt, "/status"))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var status fixtureStatus
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&status))
	return status
}

func assertFixtureStatus(t *testing.T, s fixtureStatus, revision string) {
	t.Helper()
	assert.Equal(t, 1, s.ContractVersion)
	assert.Equal(t, fixtureAgentID, s.AgentID)
	assert.Equal(t, fixtureEpoch, s.AssignmentEpoch)
	assert.Regexp(t, `^[0-9a-f]{32}$`, s.Generation)
	assert.Equal(t, revision, s.ConfigRevision)
	assert.Equal(t, "serving", s.State)
}

// fixtureSendStatus posts a message with a sender-chosen ID and returns
// the HTTP status.
func fixtureSendStatus(t *testing.T, rt fixtureRuntime, sender, id, content string) int {
	t.Helper()
	body, err := json.Marshal(map[string]string{"id": id, "content": content})
	require.NoError(t, err)
	resp, err := rt.client(sender).Post(fixtureURL(rt, "/messages"), "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// fixtureSend posts a message, requires the status code, and returns the
// status word the answer carries. It is an acknowledgement, never the
// evidence of completion.
func fixtureSend(t *testing.T, rt fixtureRuntime, sender, id, content string, want int) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"id": id, "content": content})
	require.NoError(t, err)
	resp, err := rt.client(sender).Post(fixtureURL(rt, "/messages"), "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	require.Equal(t, want, resp.StatusCode, "POST %s: %s", id, raw)
	var answer struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(raw, &answer))
	require.Equal(t, id, answer.ID)
	return answer.Status
}

func fixtureLookup(t *testing.T, rt fixtureRuntime, sender, id string) string {
	t.Helper()
	resp, err := rt.client(sender).Get(fixtureURL(rt, "/messages/"+url.PathEscape(id)))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var answer struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&answer))
	require.Equal(t, id, answer.ID)
	return answer.Status
}

// fixtureAwaitCompleted waits until the lookup answers completed, which
// is the condition asserted: a 202 and the event stream are not.
func fixtureAwaitCompleted(t *testing.T, rt fixtureRuntime, sender, id string) string {
	t.Helper()
	timeout := 30 * time.Second
	if rt.realModel() {
		timeout = 15 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	for {
		status := fixtureLookup(t, rt, sender, id)
		if status == "completed" {
			return status
		}
		require.Equal(t, "pending", status, "message %s left pending without completing", id)
		if time.Now().After(deadline) {
			t.Fatalf("message %s not completed within %s", id, timeout)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// fixtureRecalledIDs returns the recall event's IDs from the next turn on
// the stream.
func fixtureRecalledIDs(t *testing.T, events chan map[string]any) []string {
	t.Helper()
	deadline := time.After(time.Minute)
	for {
		select {
		case e, ok := <-events:
			require.True(t, ok, "event stream closed before the recall event")
			if e["kind"] == "recall" {
				return stringsField(e["recalled_ids"])
			}
		case <-deadline:
			t.Fatal("no recall event within a minute of the follow-up's completion")
		}
	}
}

// containsEpisodeID reports whether a recalled ID names a mid episode:
// pending sources are recalled as source:<n>, episodes by their ID.
func containsEpisodeID(ids []string) bool {
	for _, id := range ids {
		if !strings.HasPrefix(id, "source:") {
			return true
		}
	}
	return false
}

// fixtureFollowUpCarries reports whether an outbox entry answering id
// carries token, read from the stopped writer's own outbox.
func fixtureFollowUpCarries(t *testing.T, agentDir, id, token string) bool {
	t.Helper()
	for _, msg := range outboxMessages(t, agentDir) {
		if len(msg.InReplyTo) == 1 && msg.InReplyTo[0] == id && strings.Contains(msg.Content, token) {
			return true
		}
	}
	return false
}

// fixtureNoteGuard fails with "fixture input redacted" unless the store
// holds exactly the note the fixture set. The store redacts what reads as a
// credential before saving a note (internal/memory/sqlite), so a note
// worded like one comes back altered, and an answer quoting it cannot carry
// what the fixture asked for.
func fixtureNoteGuard(stored snapshotReport, set string) error {
	if stored.Note.Present && stored.Note.SHA256 == sha256Hex(set) {
		return nil
	}
	return fmt.Errorf("fixture input redacted: the store holds note sha256 %s, not the note the fixture set (sha256 %s)",
		stored.Note.SHA256, sha256Hex(set))
}

// storeSidecars reports which of memory.db's -wal and -shm exist in dir.
func storeSidecars(dir string) map[string]bool {
	out := map[string]bool{}
	for _, suffix := range []string{"-wal", "-shm"} {
		_, err := os.Stat(filepath.Join(dir, "memory.db"+suffix))
		out[suffix] = err == nil
	}
	return out
}

func completionIDs(r snapshotReport, sender string) []string {
	var ids []string
	for _, c := range r.Completions {
		if c.Sender == sender {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

func countReplies(r snapshotReport, id string) int {
	n := 0
	for _, entry := range r.Outbox {
		for _, reply := range entry.InReplyTo {
			if reply == id {
				n++
			}
		}
	}
	return n
}

func countString(values []string, want string) int {
	n := 0
	for _, v := range values {
		if v == want {
			n++
		}
	}
	return n
}

func lastLine(out, containing string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], containing) {
			return strings.TrimSpace(lines[i])
		}
	}
	return ""
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// writeFixtureEvidence writes the run's record where
// SHERLOCK_FIXTURE_EVIDENCE names, and nowhere when it is unset.
func writeFixtureEvidence(t *testing.T, ev *fixtureEvidence) {
	path := os.Getenv("SHERLOCK_FIXTURE_EVIDENCE")
	if path == "" {
		return
	}
	raw, err := json.MarshalIndent(ev, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(raw, '\n'), 0o600))
	t.Logf("fixture evidence written to %s", path)
}

// offlineFixture runs the built binary on this host with the mock
// provider, over mTLS with two client identities.
type offlineFixture struct {
	binary, config, toolsDir  string
	tlsEnv                    []string
	clients                   map[string]*http.Client
	workspaceAddr, workspaces string
	addr                      string
	cmd                       *exec.Cmd
	exited                    <-chan error
	logPath                   string
	// owner is the test the processes belong to, which outlives the step
	// that starts them: cleanup registered on a step would stop them when
	// the step ends.
	owner *testing.T
}

func newOfflineFixture(t *testing.T) *offlineFixture {
	t.Helper()
	f := &offlineFixture{binary: buildSherlockBinary(t), toolsDir: toolsDirForTest(t), owner: t}

	// The fixture's own config file, at the owner-only mode the loader
	// requires; git keeps it at 0644.
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "deploy", "runtime-fixture", "agent.yaml"))
	require.NoError(t, err)
	f.config = filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(f.config, raw, 0o600))

	tlsDir := t.TempDir()
	ca := newE2ECA(t, "sherlock-937-fixture-ca")
	caPath := filepath.Join(tlsDir, "ca.pem")
	require.NoError(t, os.WriteFile(caPath, ca.pem, 0o600))
	serverCert, serverKey := ca.issue(t, tlsDir, "server", "localhost",
		x509.ExtKeyUsageServerAuth, []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})
	f.tlsEnv = []string{
		"SHERLOCK_TLS_CERT=" + serverCert,
		"SHERLOCK_TLS_KEY=" + serverKey,
		"SHERLOCK_TLS_CLIENT_CA=" + caPath,
	}
	f.clients = map[string]*http.Client{}
	for _, sender := range []string{fixtureSenderA, fixtureSenderB} {
		cert, key := ca.issue(t, tlsDir, sender, sender, x509.ExtKeyUsageClientAuth, nil, nil)
		f.clients[sender] = mtlsClient(t, ca.pem, cert, key)
	}
	return f
}

func (f *offlineFixture) realModel() bool                   { return false }
func (f *offlineFixture) spend() *fixtureSpend              { return nil }
func (f *offlineFixture) baseURL() string                   { return "https://" + f.addr }
func (f *offlineFixture) client(sender string) *http.Client { return f.clients[sender] }

func (f *offlineFixture) workspaceFile(t *testing.T, workspaceDir, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(workspaceDir, name))
	require.NoError(t, err)
	return string(raw)
}

// agentCommand is the agent's command line. The environment is built
// from nothing rather than inherited, so the run carries no credential
// of the host's: the fixture needs none but the model's.
func (f *offlineFixture) agentCommand(agentDir, addr string) *exec.Cmd {
	cmd := exec.Command(f.binary, "agent",
		"--config", f.config, "--strict-config",
		"--agent-id", fixtureAgentID, "--assignment-epoch", fixtureEpoch,
		"--addr", addr)
	cmd.Env = append([]string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + agentDir,
		"SHERLOCK_TOOLS_DIR=" + f.toolsDir,
		"SHERLOCK_MODEL_PROVIDER=mock",
		"SHERLOCK_MEMORY_PATH=" + filepath.Join(agentDir, "memory.db"),
		"SHERLOCK_WORKSPACE_ADDR=" + f.workspaceAddr,
	}, f.tlsEnv...)
	return cmd
}

func (f *offlineFixture) start(t *testing.T, agentDir, workspaceDir string) {
	t.Helper()
	if f.workspaces != workspaceDir {
		_, f.workspaceAddr, _ = startWorkspaceBinary(f.owner, f.binary, workspaceDir, emptyConfig(f.owner, "workspace.yaml"))
		f.workspaces = workspaceDir
	}
	f.addr = freeLoopbackAddr(t)
	f.logPath = filepath.Join(f.owner.TempDir(), "agent.log")
	logFile, err := os.Create(f.logPath)
	require.NoError(t, err)
	f.owner.Cleanup(func() { logFile.Close() })
	cmd := f.agentCommand(agentDir, f.addr)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	require.NoError(t, cmd.Start())
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	f.owner.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			<-exited
		}
	})
	f.cmd, f.exited = cmd, exited

	deadline := time.Now().Add(15 * time.Second)
	for {
		select {
		case err := <-exited:
			logs, _ := os.ReadFile(f.logPath)
			t.Fatalf("agent exited before serving: %v\nlogs:\n%s", err, logs)
		default:
		}
		resp, err := f.client(fixtureSenderA).Get(fixtureURL(f, "/status"))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			logs, _ := os.ReadFile(f.logPath)
			t.Fatalf("agent did not answer its status route within 15s\nlogs:\n%s", logs)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (f *offlineFixture) stop(t *testing.T) int {
	t.Helper()
	require.NoError(t, f.cmd.Process.Signal(syscall.SIGTERM))
	select {
	case err := <-f.exited:
		return exitCode(err)
	case <-time.After(30 * time.Second):
		t.Fatal("agent did not exit within 30s of SIGTERM")
		return -1
	}
}

func (f *offlineFixture) secondWriter(t *testing.T, agentDir string) (int, string) {
	t.Helper()
	cmd := f.agentCommand(agentDir, freeLoopbackAddr(t))
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	require.NoError(t, cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return exitCode(err), out.String()
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatalf("a second writer on the same store was still running after 15s:\n%s", out.String())
		return -1, ""
	}
}

func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listen.Close()
	return listen.Addr().String()
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

// TestFixtureNoteGuard_ReportsRedactedInput sets each wording through the
// real store, which redacts a credential-shaped note before saving it, then
// copies and verifies the store as the fixture does. The wording run-3 used
// trips the guard; the fixture's current note passes it, which is the
// control showing the guard accepts a note the store keeps.
func TestFixtureNoteGuard_ReportsRedactedInput(t *testing.T) {
	stored := func(t *testing.T, note string) snapshotReport {
		t.Helper()
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "agent"), 0o755))
		store, err := sqlite.OpenConversation(filepath.Join(dir, "agent", "memory.db"), tiedEmbedder{})
		require.NoError(t, err)
		require.NoError(t, store.SetNote(context.Background(), note))
		require.NoError(t, store.Close())
		snapshot := filepath.Join(dir, "snapshot")
		fixtureSnapshot(t, "copy", filepath.Join(dir, "agent", "memory.db"), snapshot)
		return fixtureVerify(t, snapshot)
	}

	run3 := "fixture token: amber-heron-937"
	err := fixtureNoteGuard(stored(t, run3), run3)
	require.Error(t, err, "the store redacts %q, so the guard reports it", run3)
	assert.Contains(t, err.Error(), "fixture input redacted")

	assert.NoError(t, fixtureNoteGuard(stored(t, fixtureNote), fixtureNote),
		"the store keeps %q as set", fixtureNote)
}
