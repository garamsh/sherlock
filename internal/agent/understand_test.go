package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
)

func understandStep(document string) agent.ScriptedStep {
	return agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: agent.MemoryUnderstandTool, Input: json.RawMessage(document)}}}
}

// understandInput decodes the document the understanding step was shown.
func understandInput(t *testing.T, messages []agent.Message) (note string, conversation []agent.Message, input string) {
	t.Helper()
	require.Len(t, messages, 2)
	var doc struct {
		Note         string          `json:"note"`
		Conversation []agent.Message `json:"conversation"`
		Input        string          `json:"input"`
	}
	require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &doc))
	return doc.Note, doc.Conversation, doc.Input
}

func TestSessionRun_UnderstandingResolvesBeforeRecall(t *testing.T) {
	store := &noteStore{note: "Current work: planning the Lisbon offsite."}
	recaller := &fakeRecaller{}
	model := &recordingModel{inner: agent.NewScriptedModel(
		understandStep(`{"restatement":"How many people did I say to book the Lisbon offsite venue for?","queries":["Lisbon offsite headcount"," venue booking "],"note":"Current work: planning the Lisbon offsite; confirming the headcount."}`),
		finalStep("42 people."),
	)}
	events := &fakeEvents{}
	s := agent.NewSession(model, newFakeClient(), agent.Options{Conversation: store, Recaller: recaller, Events: events})

	reply, err := s.Run(context.Background(), "How many did I say to book it for?")
	require.NoError(t, err)
	assert.Equal(t, "42 people.", reply)

	note, _, input := understandInput(t, model.seen[0])
	assert.Equal(t, "Current work: planning the Lisbon offsite.", note, "the step reads the saved note")
	assert.Equal(t, "How many did I say to book it for?", input)
	assert.Equal(t, []string{"How many people did I say to book the Lisbon offsite venue for?\nLisbon offsite headcount\nvenue booking"}, recaller.tasks,
		"recall ranks by the restatement and the queries, not the raw input")
	assert.Equal(t, []string{"Current work: planning the Lisbon offsite; confirming the headcount."}, store.setNotes)

	act := model.seen[1]
	assert.Contains(t, act[0].Content, "## Your note\nCurrent work: planning the Lisbon offsite; confirming the headcount.", "the act call leads with the note the step wrote")
	assert.Contains(t, act[1].Content, "## Current task\nHow many did I say to book it for?")
	assert.Contains(t, act[1].Content, "## Understood as\nHow many people did I say to book the Lisbon offsite venue for?")
	assert.Equal(t, "How many did I say to book it for?", act[2].Content, "the person's own words stay the user message")

	require.Equal(t, agent.EventUnderstood, events.events[1].Kind)
	assert.Equal(t, []string{"Lisbon offsite headcount", "venue booking"}, events.events[1].Queries)
	assert.Empty(t, events.events[1].Error)
}

// TestSessionRun_UnderstandingSeesRecentVisibleConversation shows the step
// the newest sixteen user and assistant messages and nothing the model did
// not say or hear: no instructions, frames or tool traffic.
func TestSessionRun_UnderstandingSeesRecentVisibleConversation(t *testing.T) {
	var history []agent.Message
	for i := 1; i <= 20; i++ {
		history = append(history, agent.Message{Role: agent.RoleUser, Content: fmt.Sprintf("question %d", i)}, agent.Message{Role: agent.RoleAssistant, Content: fmt.Sprintf("answer %d", i)})
	}
	history = append(history,
		agent.Message{Role: agent.RoleSystem, Content: "## Current task\nold frame"},
		agent.Message{Role: agent.RoleAssistant, Content: "calling", ToolCalls: []agent.ToolCall{{ID: "c1", Name: "echo"}}},
		agent.Message{Role: agent.RoleTool, Name: "echo", ToolCallID: "c1", Content: "tool output"},
	)
	model := &recordingModel{inner: agent.NewScriptedModel(
		understandStep(`{"restatement":"next","queries":["next"],"note":null}`),
		finalStep("ok"),
	)}
	s := agent.NewSession(model, newFakeClient(), agent.Options{Conversation: &noteStore{}, InitialHistory: history})

	_, err := s.Run(context.Background(), "next")
	require.NoError(t, err)
	_, conversation, _ := understandInput(t, model.seen[0])
	require.Len(t, conversation, 16)
	assert.Equal(t, agent.Message{Role: agent.RoleUser, Content: "question 13"}, conversation[0])
	assert.Equal(t, agent.Message{Role: agent.RoleAssistant, Content: "answer 20"}, conversation[15])
	for _, m := range conversation {
		assert.NotContains(t, []string{"old frame", "calling", "tool output"}, m.Content)
	}
}

// TestSessionRun_UnderstandingThatPlacesNothingFallsBack keeps the turn
// going on the raw input when the step answers in prose, and reports it.
func TestSessionRun_UnderstandingThatPlacesNothingFallsBack(t *testing.T) {
	store := &noteStore{note: "kept"}
	recaller := &fakeRecaller{}
	model := &recordingModel{inner: agent.NewScriptedModel(
		agent.ScriptedStep{Content: "I think they mean the offsite."},
		finalStep("ok"),
	)}
	events := &fakeEvents{}
	s := agent.NewSession(model, newFakeClient(), agent.Options{Conversation: store, Recaller: recaller, Events: events})

	_, err := s.Run(context.Background(), "How many did I say?")
	require.NoError(t, err)
	assert.Equal(t, []string{"How many did I say?"}, recaller.tasks, "recall falls back to the raw input")
	assert.Empty(t, store.setNotes, "the note is left as it was")
	assert.NotContains(t, model.seen[1][1].Content, "## Understood as")
	require.Equal(t, agent.EventUnderstood, events.events[1].Kind)
	assert.Contains(t, events.events[1].Error, agent.MemoryUnderstandTool)
}

// TestSessionRun_OvergrownNoteIsRefused keeps the saved note when the step
// writes one past the bound, while its restatement is still used; a note at
// the bound is the control.
func TestSessionRun_OvergrownNoteIsRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		length int
		saved  bool
	}{"past the bound": {agent.MaxNoteLength + 1, false}, "at the bound": {agent.MaxNoteLength, true}} {
		t.Run(name, func(t *testing.T) {
			note := strings.Repeat("é", tc.length)
			store := &noteStore{note: "kept"}
			recaller := &fakeRecaller{}
			model := &recordingModel{inner: agent.NewScriptedModel(
				understandStep(`{"restatement":"resolved","queries":["q"],"note":`+strconv.Quote(note)+`}`),
				finalStep("ok"),
			)}
			events := &fakeEvents{}
			s := agent.NewSession(model, newFakeClient(), agent.Options{Conversation: store, Recaller: recaller, Events: events})

			_, err := s.Run(context.Background(), "task")
			require.NoError(t, err)
			assert.Equal(t, []string{"resolved\nq"}, recaller.tasks)
			if tc.saved {
				assert.Equal(t, []string{note}, store.setNotes)
				assert.Empty(t, events.events[1].Error)
				return
			}
			assert.Empty(t, store.setNotes)
			assert.Equal(t, "kept", store.note)
			assert.Contains(t, events.events[1].Error, "note too long")
		})
	}
}

// TestSessionRun_NoUnderstandingWithoutConversationMemory makes no extra
// call for a session with no note and no older conversation.
func TestSessionRun_NoUnderstandingWithoutConversationMemory(t *testing.T) {
	model := &recordingModel{inner: agent.NewScriptedModel(finalStep("ok"))}
	events := &fakeEvents{}
	_, err := agent.NewSession(model, newFakeClient(), agent.Options{Events: events}).Run(context.Background(), "task")
	require.NoError(t, err)
	assert.Len(t, model.seen, 1)
	assert.NotContains(t, events.kinds(), agent.EventUnderstood)
}
