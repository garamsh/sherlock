package agent_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
)

func TestParseWriteback_ReplyAndNote(t *testing.T) {
	note := func(s string) *string { return &s }
	tests := []struct {
		name    string
		content string
		reply   string
		note    *string
	}{
		{name: "null note leaves it unchanged", content: `{"reply":" hi ","note":null}`, reply: "hi"},
		{name: "absent note leaves it unchanged", content: `{"reply":"hi"}`, reply: "hi"},
		{name: "string note replaces it, trimmed", content: `{"reply":"hi","note":"  tabs, never spaces \n"}`, reply: "hi", note: note("tabs, never spaces")},
		{name: "empty string clears it", content: `{"reply":"hi","note":""}`, reply: "hi", note: note("")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reply, got, err := agent.ParseWriteback(tt.content)
			require.NoError(t, err)
			assert.Equal(t, tt.reply, reply)
			assert.Equal(t, tt.note, got)
		})
	}
}

// TestParseWriteback_NoteTooLong refuses a note past the bound with its own
// error, and accepts one at the bound as the control.
func TestParseWriteback_NoteTooLong(t *testing.T) {
	at := strings.Repeat("é", agent.MaxNoteLength)
	_, note, err := agent.ParseWriteback(`{"reply":"ok","note":"` + at + `"}`)
	require.NoError(t, err, "a note of exactly the bound is accepted")
	require.NotNil(t, note)
	assert.Equal(t, at, *note)

	_, note, err = agent.ParseWriteback(`{"reply":"ok","note":"` + at + `x"}`)
	require.Error(t, err)
	assert.True(t, errors.Is(err, agent.ErrNoteTooLong))
	assert.False(t, errors.Is(err, agent.ErrMalformedWriteback), "an overgrown note is not a malformed call")
	assert.Nil(t, note)
}

// TestParseWriteback_MalformedArguments fails arguments that are not the
// document, without leaking the parser's Go field names (issue #77).
func TestParseWriteback_MalformedArguments(t *testing.T) {
	for name, content := range map[string]string{
		"prose instead of the document": "still useful",
		"truncated document":            `{"reply":"answer","note":`,
		"numeric reply":                 `{"reply":1,"note":null}`,
		"numeric note":                  `{"reply":"x","note":7}`,
		"empty arguments":               ``,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := agent.ParseWriteback(content)
			require.Error(t, err)
			assert.True(t, errors.Is(err, agent.ErrMalformedWriteback), "got %v", err)
			for _, bad := range []string{"Go struct field", "writebackJSON"} {
				assert.NotContains(t, err.Error(), bad)
			}
		})
	}
}

// TestResponseSchema_ShapeIsEnforceable pins the terminal call's shape: a
// reply and a note that is always stated as a bounded string, and nothing
// else.
func TestResponseSchema_ShapeIsEnforceable(t *testing.T) {
	var schema struct {
		Type                 string                     `json:"type"`
		Required             []string                   `json:"required"`
		AdditionalProperties bool                       `json:"additionalProperties"`
		Properties           map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal([]byte(agent.ResponseSchema), &schema))
	assert.Equal(t, "object", schema.Type)
	assert.ElementsMatch(t, []string{"reply", "note"}, schema.Required)
	assert.False(t, schema.AdditionalProperties)
	assert.Len(t, schema.Properties, 2, "reply and note only; memory_ops is gone")

	var note struct {
		Type      string `json:"type"`
		MaxLength int    `json:"maxLength"`
	}
	require.NoError(t, json.Unmarshal(schema.Properties["note"], &note))
	assert.Equal(t, "string", note.Type, "a note is restated every turn, never left null")
	assert.Equal(t, agent.MaxNoteLength, note.MaxLength)
}
