package gateway_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/gateway"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

// statusReply is the decoded body of a status GET.
type statusReply struct {
	ContractVersion int       `json:"contract_version"`
	AgentID         string    `json:"agent_id"`
	AssignmentEpoch string    `json:"assignment_epoch"`
	Generation      string    `json:"generation"`
	ConfigRevision  string    `json:"config_revision"`
	State           string    `json:"state"`
	StartedAt       time.Time `json:"started_at"`
}

// fetchAgentStatus reads the status route of agent "env-1".
func fetchAgentStatus(base string) (statusReply, error) {
	resp, err := http.Get(base + "/agents/env-1/status")
	if err != nil {
		return statusReply{}, err
	}
	defer resp.Body.Close()
	var reply statusReply
	err = json.NewDecoder(resp.Body).Decode(&reply)
	return reply, err
}

// serving is a Serve run on a free loopback port: stop is SIGTERM, and
// done carries Serve's result.
type serving struct {
	base string
	stop context.CancelFunc
	done <-chan error
}

// startMemoryServe runs Serve over the real runtime with its memory store
// at path and returns once the listener answers. A turn the test leaves
// unanswered is answered at cleanup, so a failing test cannot hold the
// queue's close open.
func startMemoryServe(t *testing.T, path string) (*heldModel, serving) {
	t.Helper()
	model := newHeldModel()
	released := make(chan struct{})
	t.Cleanup(func() { close(released) })
	q := newMemoryQueue(t, path, model)
	t.Cleanup(func() { go answerEveryTurn(model, released) })
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- gateway.Serve(ctx, addr, nil, q, noIssuers(), gateway.Instance{}, newTestLogger()) }()
	base := "http://" + addr
	require.Eventually(t, func() bool {
		_, err := fetchAgentStatus(base)
		return err == nil
	}, 5*time.Second, 5*time.Millisecond, "the listener never answered")
	return model, serving{base: base, stop: cancel, done: done}
}

// answerEveryTurn ends every turn that reaches model until released
// closes.
func answerEveryTurn(model *heldModel, released <-chan struct{}) {
	for {
		select {
		case model.turns <- nil:
		case <-released:
			return
		}
	}
}

// awaitState waits for the status route to report state.
func awaitState(t *testing.T, base, state string) {
	t.Helper()
	require.Eventually(t, func() bool {
		reply, err := fetchAgentStatus(base)
		return err == nil && reply.State == state
	}, 5*time.Second, 5*time.Millisecond, "the agent never reported %s", state)
}

// awaitServeReturns waits for Serve to return and requires it returned
// no error.
func awaitServeReturns(t *testing.T, s serving) {
	t.Helper()
	select {
	case err := <-s.done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after the drain")
	}
}

// committed reports whether memory.db at path holds the sender-less
// message id with content, opening the store after Serve released it.
func committed(t *testing.T, path, id, content string) bool {
	t.Helper()
	store, err := sqlite.OpenConversation(path, wordEmbedder{})
	require.NoError(t, err)
	defer store.Close()
	held, same, err := store.HoldsInbound(context.Background(), "", "", id, content)
	require.NoError(t, err)
	return held && same
}

// TestServe_DrainRefusesPostsWhileTheInFlightTurnCommits holds C4 steps
// 1–3: on SIGTERM the state turns draining, the listener stays open and
// answers a new post 503 draining, and the turn in flight runs to
// completion and commits. The control is the same post accepted before
// the drain.
func TestServe_DrainRefusesPostsWhileTheInFlightTurnCommits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	model, s := startMemoryServe(t, path)

	code, _ := sendMessage(t, http.DefaultClient, s.base, `{"id":"m1","content":"hello"}`)
	require.Equal(t, http.StatusAccepted, code, "the control: a post before the drain is accepted")
	awaitStarted(t, model, 1)

	s.stop()
	awaitState(t, s.base, "draining")
	code, reply := sendMessage(t, http.DefaultClient, s.base, `{"id":"m2","content":"late"}`)
	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.Equal(t, messageReply{Error: "draining"}, reply)

	model.turn(t, nil)
	awaitServeReturns(t, s)
	assert.True(t, committed(t, path, "m1", "hello"), "the in-flight turn committed its sources")
	assert.False(t, committed(t, path, "m2", "late"), "the refused post was never taken")
}

// TestServe_NoQueuedTurnStartsAfterSIGTERM holds C4 step 4: a message
// queued behind the in-flight turn starts no turn once the drain begins
// and stays uncommitted. The control is the in-flight turn, which runs
// and commits.
func TestServe_NoQueuedTurnStartsAfterSIGTERM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	model, s := startMemoryServe(t, path)

	code, _ := sendMessage(t, http.DefaultClient, s.base, `{"id":"m1","content":"running"}`)
	require.Equal(t, http.StatusAccepted, code)
	awaitStarted(t, model, 1)
	code, _ = sendMessage(t, http.DefaultClient, s.base, `{"id":"m2","content":"queued"}`)
	require.Equal(t, http.StatusAccepted, code)
	require.Equal(t, "pending", lookupMessage(t, http.DefaultClient, s.base, "m2"), "the setup: m2 waits behind m1")

	s.stop()
	awaitState(t, s.base, "draining")
	// Answer every turn from here on, so a queued turn that did start
	// would finish and show up below rather than hold the drain open.
	answered := make(chan struct{})
	t.Cleanup(func() { close(answered) })
	go answerEveryTurn(model, answered)

	awaitServeReturns(t, s)
	assert.Len(t, model.started(), 1, "only the in-flight turn ran")
	assert.True(t, committed(t, path, "m1", "running"), "the control: the in-flight turn committed")
	assert.False(t, committed(t, path, "m2", "queued"), "the queued message stays uncommitted")
}

// TestServe_WriterLockIsReleasedOnlyAfterMemoryCloses holds C4 step 5:
// through the drain the store's writer lock stays held, and it is free
// once Serve has closed memory. The control is the same open after
// Serve returns, which succeeds.
func TestServe_WriterLockIsReleasedOnlyAfterMemoryCloses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	model, s := startMemoryServe(t, path)

	code, _ := sendMessage(t, http.DefaultClient, s.base, `{"id":"m1","content":"hello"}`)
	require.Equal(t, http.StatusAccepted, code)
	awaitStarted(t, model, 1)
	s.stop()
	awaitState(t, s.base, "draining")

	_, err := sqlite.Open(path)
	assert.ErrorIs(t, err, sqlite.ErrStoreLocked, "the lock is held while the drain runs")

	model.turn(t, nil)
	awaitServeReturns(t, s)
	store, err := sqlite.Open(path)
	require.NoError(t, err, "the control: the lock is free once memory closed")
	require.NoError(t, store.Close())
}
