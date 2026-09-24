package memory_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	agentmemory "github.com/garamsh/gagent/internal/agent/memory"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/tool"
)

// resolvingModel answers the read step's one call with a scripted
// address, and records what it was shown.
type resolvingModel struct {
	subject  string
	question string
	// prose makes the model answer without the reserved call, which is
	// what a provider that ignores the tool does.
	prose bool
	calls int
	seen  []tool.Descriptor
	sent  []agent.Message
}

func (m *resolvingModel) Chat(_ context.Context, msgs []agent.Message, tools []tool.Descriptor) (agent.Response, error) {
	m.calls++
	m.seen = tools
	m.sent = msgs
	if m.prose {
		return agent.Response{Content: "<think>I am not sure.</think>"}, nil
	}
	args, err := json.Marshal(struct {
		Subject  string `json:"subject"`
		Question string `json:"question"`
	}{m.subject, m.question})
	if err != nil {
		return agent.Response{}, err
	}
	return agent.Response{ToolCalls: []agent.ToolCall{{
		ID: "call-1", Name: agent.MemoryReadTool, Input: args,
	}}}, nil
}

// countingService records which of the store's two reads were made, so
// a test can hold the order ADR 0019 fixes rather than only its result.
type countingService struct {
	memory.Service
	at, under int
}

func (c *countingService) At(ctx context.Context, a memory.Address) ([]memory.Memory, error) {
	c.at++
	return c.Service.At(ctx, a)
}

func (c *countingService) UnderSubject(ctx context.Context, subject string) ([]memory.Memory, error) {
	c.under++
	return c.Service.UnderSubject(ctx, subject)
}

// storeTelescope writes one value at the telescope address.
func storeTelescope(t *testing.T, svc memory.Service, value string) {
	t.Helper()
	_, err := svc.Store(context.Background(), memory.StoreInput{
		Address:   telescopeAddress(),
		Value:     value,
		Operation: memory.OpJoin,
		Source:    "test",
	})
	require.NoError(t, err)
}

// TestRecall_AddressedReadAnswersFromTheAddress is the ordinary path:
// the question resolves to an address, the address holds something, and
// the subject is never read.
func TestRecall_AddressedReadAnswersFromTheAddress(t *testing.T) {
	svc := &countingService{Service: newMemoryService(t)}
	storeTelescope(t, svc, "the Orion XT8")
	model := &resolvingModel{subject: "user", question: "what is the user's favorite telescope"}

	got, err := agentmemory.Recall{Svc: svc, Model: model}.
		Recall(context.Background(), "which telescope do I like?")
	require.NoError(t, err)

	assert.Equal(t, agent.RecallAddressed, got.Outcome)
	require.Len(t, got.Memories, 1)
	assert.Contains(t, got.Memories[0].Content, "the Orion XT8")
	assert.Contains(t, got.Memories[0].Content, "what is the user's favorite telescope",
		"the injected line names the question the value answers")
	assert.Equal(t, 1, svc.at)
	assert.Zero(t, svc.under, "a populated address is not widened past")
	assert.Equal(t, 1, model.calls, "one model call per read step")
}

// TestRecall_EmptyAddressWidensToTheSubject is ADR 0019's widening. It
// fires because the address held nothing — not because the model was
// unsure, which is a threshold this design refuses.
func TestRecall_EmptyAddressWidensToTheSubject(t *testing.T) {
	svc := &countingService{Service: newMemoryService(t)}
	storeTelescope(t, svc, "the Orion XT8")
	// A question the write never used, under the right subject.
	model := &resolvingModel{subject: "user", question: "which telescope does the user own"}

	got, err := agentmemory.Recall{Svc: svc, Model: model}.
		Recall(context.Background(), "which telescope do I own?")
	require.NoError(t, err)

	assert.Equal(t, agent.RecallWidened, got.Outcome)
	require.Len(t, got.Memories, 1)
	assert.Contains(t, got.Memories[0].Content, "the Orion XT8",
		"the subject read reaches the fact the wrong question missed")
	assert.Equal(t, 1, svc.at)
	assert.Equal(t, 1, svc.under)
}

// TestRecall_SubjectAloneSkipsTheAddressRead is the third outcome the
// step can place: a question about a subject in general.
func TestRecall_SubjectAloneSkipsTheAddressRead(t *testing.T) {
	svc := &countingService{Service: newMemoryService(t)}
	storeTelescope(t, svc, "the Orion XT8")
	model := &resolvingModel{subject: "user"}

	got, err := agentmemory.Recall{Svc: svc, Model: model}.
		Recall(context.Background(), "what do you know about me?")
	require.NoError(t, err)

	assert.Equal(t, agent.RecallWidened, got.Outcome)
	require.Len(t, got.Memories, 1)
	assert.Zero(t, svc.at, "there is no address to read when none was placed")
	assert.Equal(t, 1, svc.under)
}

// TestRecall_UnplacedQuestionReadsNothing pins the outcome a provider
// that ignores the reserved tool produces. It is reported rather than
// guessed at, and the store is not read at all: there is no address to
// read and no subject to widen to.
func TestRecall_UnplacedQuestionReadsNothing(t *testing.T) {
	svc := &countingService{Service: newMemoryService(t)}
	storeTelescope(t, svc, "the Orion XT8")
	model := &resolvingModel{prose: true}

	got, err := agentmemory.Recall{Svc: svc, Model: model}.
		Recall(context.Background(), "hello")
	require.NoError(t, err)

	assert.Equal(t, agent.RecallUnplaced, got.Outcome)
	assert.Empty(t, got.Memories)
	assert.Zero(t, svc.at)
	assert.Zero(t, svc.under)
}

// TestRecall_PlacedButEmptyIsNotUnplaced is the distinction ADR 0019
// requires the running system to carry: a question memory could not
// place is not a question memory had no answer to.
func TestRecall_PlacedButEmptyIsNotUnplaced(t *testing.T) {
	svc := &countingService{Service: newMemoryService(t)}
	model := &resolvingModel{subject: "user", question: "what is the user's favorite telescope"}

	got, err := agentmemory.Recall{Svc: svc, Model: model}.
		Recall(context.Background(), "which telescope do I like?")
	require.NoError(t, err)

	assert.Equal(t, agent.RecallEmpty, got.Outcome)
	assert.Empty(t, got.Memories)
	assert.Equal(t, 1, svc.under, "an empty address still widens before reporting empty")
}

// TestRecall_ShowsTheModelTheReservedReadToolAndNothingElse pins what
// the read call carries: the reserved tool alone, and a system prompt
// that is the write's own account of an address. A read prompt carrying
// more than the write could place a question the write never wrote
// (ADR 0019).
func TestRecall_ShowsTheModelTheReservedReadToolAndNothingElse(t *testing.T) {
	svc := newMemoryService(t)
	model := &resolvingModel{subject: "user", question: "q"}

	_, err := agentmemory.Recall{Svc: svc, Model: model}.
		Recall(context.Background(), "which telescope do I like?")
	require.NoError(t, err)

	require.Len(t, model.seen, 1)
	assert.Equal(t, agent.MemoryReadTool, model.seen[0].Name)

	require.Len(t, model.sent, 2)
	assert.Equal(t, agent.RoleSystem, model.sent[0].Role)
	assert.Equal(t, agent.ReadInstruction, model.sent[0].Content)
	assert.Equal(t, agent.RoleUser, model.sent[1].Role)
	assert.Equal(t, "which telescope do I like?", model.sent[1].Content)
}

// TestParseReadAddress_ReadsTheReservedCallOnly pins what counts as a
// placed address.
func TestParseReadAddress_ReadsTheReservedCallOnly(t *testing.T) {
	tests := []struct {
		name string
		resp agent.Response
		want agent.ReadAddress
	}{
		{
			name: "the reserved call",
			resp: agent.Response{ToolCalls: []agent.ToolCall{{
				Name:  agent.MemoryReadTool,
				Input: json.RawMessage(`{"subject":" user ","question":" q "}`),
			}}},
			want: agent.ReadAddress{Subject: "user", Question: "q"},
		},
		{
			name: "content alone places nothing",
			resp: agent.Response{Content: `{"subject":"user","question":"q"}`},
			want: agent.ReadAddress{},
		},
		{
			name: "another tool places nothing",
			resp: agent.Response{ToolCalls: []agent.ToolCall{{
				Name:  "files",
				Input: json.RawMessage(`{"subject":"user"}`),
			}}},
			want: agent.ReadAddress{},
		},
		{
			name: "unreadable arguments place nothing",
			resp: agent.Response{ToolCalls: []agent.ToolCall{{
				Name:  agent.MemoryReadTool,
				Input: json.RawMessage(`{"subject":`),
			}}},
			want: agent.ReadAddress{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := agent.ParseReadAddress(tt.resp)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.want.Subject != "", got.Placed())
		})
	}
}

// TestRecall_ModelFailureFailsTheRead pins that a provider error is not
// silently an empty recall: a turn whose memory could not be consulted
// because the call failed is different from one that placed nothing.
func TestRecall_ModelFailureFailsTheRead(t *testing.T) {
	svc := newMemoryService(t)
	_, err := agentmemory.Recall{Svc: svc, Model: failingModel{}}.
		Recall(context.Background(), "anything")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "resolve address"))
}

type failingModel struct{}

func (failingModel) Chat(context.Context, []agent.Message, []tool.Descriptor) (agent.Response, error) {
	return agent.Response{}, assert.AnError
}
