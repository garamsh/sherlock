package agent_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
)

func TestParseWriteback_Operations(t *testing.T) {
	tests := []struct {
		name    string
		content string
		visible string
		ops     []agent.WritebackOp
	}{
		{
			name: "address value and operation extracted",
			content: `{"reply":"summary","memory_ops":[` +
				`{"address":{"subject":"user","question":"what is the user's favorite telescope","restriction":null},` +
				`"value":"the Kepler-9 refractor","operation":"replace"}]}`,
			visible: "summary",
			ops: []agent.WritebackOp{{
				Address: agent.MemoryAddress{
					Subject:  "user",
					Question: "what is the user's favorite telescope",
				},
				Value:     "the Kepler-9 refractor",
				Operation: agent.OpReplace,
			}},
		},
		{
			name: "a restriction narrows the address it lands on",
			content: `{"reply":"noted","memory_ops":[` +
				`{"address":{"subject":"Hugo","question":"which project does Hugo lead","restriction":"on thursdays"},` +
				`"value":"pewter-sedge","operation":null}]}`,
			visible: "noted",
			ops: []agent.WritebackOp{{
				Address: agent.MemoryAddress{
					Subject:     "Hugo",
					Question:    "which project does Hugo lead",
					Restriction: "on thursdays",
				},
				Value:     "pewter-sedge",
				Operation: agent.OpJoin,
			}},
		},
		{
			name:    "an empty list is an answer, not an omission",
			content: `{"reply":"done","memory_ops":[]}`,
			visible: "done",
			ops:     []agent.WritebackOp{},
		},
		{
			name:    "the reply is trimmed",
			content: `{"reply":"  spaced out  ","memory_ops":[]}`,
			visible: "spaced out",
			ops:     []agent.WritebackOp{},
		},
		{
			name:    "a step with no content states nothing",
			content: "",
			visible: "",
			ops:     nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			visible, ops, warn, err := agent.ParseWriteback(tt.content)
			require.NoError(t, err)
			require.NoError(t, warn)
			assert.Equal(t, tt.visible, visible)
			assert.Equal(t, tt.ops, ops)
		})
	}
}

// TestParseWriteback_UnreadableOperationJoins is ADR 0018 §Where a
// writeback must err: a wrong join is repaired by the next correct
// replacement at that address, a wrong replacement is repaired by
// nothing. Every operation the parser cannot read joins — the field
// absent, null, empty, or a word the contract does not name — and none
// of them fails the turn.
func TestParseWriteback_UnreadableOperationJoins(t *testing.T) {
	tests := []struct {
		name      string
		operation string
	}{
		{"field omitted", ""},
		{"explicit null", `"operation":null,`},
		{"empty string", `"operation":"",`},
		{"word the contract does not name", `"operation":"supersede",`},
		{"the whole vocabulary in one field", `"operation":"replace or join",`},
		{"not a string at all", `"operation":1,`},
		{"an object where a word belongs", `"operation":{"kind":"replace"},`},
		{"a boolean", `"operation":true,`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := `{"reply":"ok","memory_ops":[{"address":{"subject":"user",` +
				`"question":"where does the user keep the backups","restriction":null},` +
				tt.operation + `"value":"the attic drive"}]}`

			_, ops, warn, err := agent.ParseWriteback(content)
			require.NoError(t, err, "an unreadable operation never fails the turn")
			require.NoError(t, warn, "an unreadable operation is not a dropped writeback")
			require.Len(t, ops, 1)
			assert.Equal(t, agent.OpJoin, ops[0].Operation,
				"where the operation cannot be read, the write joins")
		})
	}
}

// TestParseWriteback_UppercaseReplaceIsRead keeps the one coercion the
// parser does: case and surrounding space are not a different word.
func TestParseWriteback_UppercaseReplaceIsRead(t *testing.T) {
	content := `{"reply":"ok","memory_ops":[{"address":{"subject":"user",` +
		`"question":"which team does the user sit with","restriction":null},` +
		`"value":"platform","operation":" Replace "}]}`

	_, ops, warn, err := agent.ParseWriteback(content)
	require.NoError(t, err)
	require.NoError(t, warn)
	require.Len(t, ops, 1)
	assert.Equal(t, agent.OpReplace, ops[0].Operation)
}

// TestParseWriteback_LenientParseFailure covers issue #93: a model
// response that is not the schema's document no longer fails the turn.
// The content reaches the caller with no operations, no fatal error, and
// a logged warning the session can surface.
func TestParseWriteback_LenientParseFailure(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		wantVisible    string
		wantWarnSubstr string
	}{
		{
			name:           "prose instead of the document",
			content:        "still useful",
			wantVisible:    "still useful",
			wantWarnSubstr: "invalid JSON",
		},
		{
			name:           "truncated document",
			content:        `{"reply":"answer","memory_ops":[`,
			wantVisible:    `{"reply":"answer","memory_ops":[`,
			wantWarnSubstr: "invalid JSON",
		},
		{
			name:           "type mismatch",
			content:        `{"reply":1,"memory_ops":[]}`,
			wantVisible:    `{"reply":1,"memory_ops":[]}`,
			wantWarnSubstr: "invalid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf, swap := captureSlog(t)
			defer swap()

			visible, ops, warn, err := agent.ParseWriteback(tt.content)
			require.NoError(t, err, "parse failure is non-fatal")
			assert.Equal(t, tt.wantVisible, visible, "the content survives")
			assert.Nil(t, ops, "no operations when the response cannot be parsed")
			require.Error(t, warn, "the parse miss is surfaced as a warning")
			assert.True(t, errors.Is(warn, agent.ErrMalformedWriteback),
				"warning matches ErrMalformedWriteback, got %v", warn)
			assert.Contains(t, warn.Error(), tt.wantWarnSubstr)

			logged := buf.String()
			assert.Contains(t, logged, "writeback dropped",
				"the parser logs a structured warning")
			assert.Contains(t, logged, "parse_writeback",
				"the warning carries the op tag for filtering")
			assert.Contains(t, logged, "WARN",
				"the warning uses the WARN level, not Error or Info")
		})
	}
}

// TestParseWriteback_StrictSchemaValidation keeps the contract-strict
// behaviour (issue #93): a response that parses but states a write with
// nowhere to land, or nothing to hold, is still a fatal *WritebackError.
func TestParseWriteback_StrictSchemaValidation(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "no subject",
			content: `{"reply":"x","memory_ops":[{"address":{"subject":"",` +
				`"question":"what is the user's favorite telescope","restriction":null},` +
				`"value":"the Kepler-9 refractor","operation":null}]}`,
		},
		{
			name: "no question",
			content: `{"reply":"x","memory_ops":[{"address":{"subject":"user",` +
				`"question":"  ","restriction":null},` +
				`"value":"the Kepler-9 refractor","operation":null}]}`,
		},
		{
			name: "no value",
			content: `{"reply":"x","memory_ops":[{"address":{"subject":"user",` +
				`"question":"what is the user's favorite telescope","restriction":null},` +
				`"value":"","operation":null}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, warn, err := agent.ParseWriteback(tt.content)
			require.Error(t, err, "schema violations are fatal")
			assert.True(t, errors.Is(err, agent.ErrMalformedWriteback),
				"expected ErrMalformedWriteback, got %v", err)
			require.NoError(t, warn,
				"schema violations are not surfaced as warnings — they fail the turn")
		})
	}
}

// TestParseWriteback_TypeErrorStripsInternalFieldName is issue #77's
// regression, kept current with issue #93: a type mismatch must surface a
// model-friendly warning without leaking the unexported Go struct field
// name.
func TestParseWriteback_TypeErrorStripsInternalFieldName(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "numeric reply", body: `{"reply":1,"memory_ops":[]}`},
		{
			name: "boolean value",
			body: `{"reply":"x","memory_ops":[{"address":{"subject":"user",` +
				`"question":"q","restriction":null},"value":true,"operation":null}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, warn, err := agent.ParseWriteback(tc.body)
			require.NoError(t, err, "type mismatches are warnings, not errors (issue #93)")
			require.Error(t, warn)
			msg := warn.Error()
			for _, bad := range []string{"Go struct field", "writebackJSON"} {
				assert.NotContains(t, msg, bad,
					"internal Go field name %q must not leak", bad)
			}
		})
	}
}

// TestResponseSchema_ShapeIsEnforceable pins the three properties the
// decision rests on: the memory field cannot be absent from a response,
// the address is the subject and the question, and the operation is the
// one field a write may leave unstated (ADR 0018).
func TestResponseSchema_ShapeIsEnforceable(t *testing.T) {
	var schema struct {
		Type                 string   `json:"type"`
		Required             []string `json:"required"`
		AdditionalProperties bool     `json:"additionalProperties"`
		Properties           struct {
			Reply          json.RawMessage `json:"reply"`
			WorkingContext struct {
				Required             []string `json:"required"`
				AdditionalProperties bool     `json:"additionalProperties"`
			} `json:"working_context"`
			MemoryOps struct {
				Type  string `json:"type"`
				Items struct {
					Required             []string `json:"required"`
					AdditionalProperties bool     `json:"additionalProperties"`
					Properties           struct {
						Address struct {
							Required   []string `json:"required"`
							Properties struct {
								Restriction struct {
									Type []string `json:"type"`
								} `json:"restriction"`
							} `json:"properties"`
						} `json:"address"`
						Operation struct {
							Type []string          `json:"type"`
							Enum []json.RawMessage `json:"enum"`
						} `json:"operation"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"memory_ops"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal([]byte(agent.ResponseSchema), &schema),
		"the schema the provider sends must be valid JSON")

	assert.Equal(t, "object", schema.Type)
	assert.ElementsMatch(t, []string{"reply", "memory_ops", "working_context"}, schema.Required,
		"the memory field has no shape in which it is absent")
	assert.False(t, schema.AdditionalProperties,
		"strict mode rejects a response carrying anything else")
	assert.Equal(t, "array", schema.Properties.MemoryOps.Type)
	assert.ElementsMatch(t, []string{"status", "progress", "arrival", "resolved_attempts"}, schema.Properties.WorkingContext.Required)
	assert.False(t, schema.Properties.WorkingContext.AdditionalProperties)

	item := schema.Properties.MemoryOps.Items
	assert.ElementsMatch(t, []string{"address", "value", "operation"}, item.Required)
	assert.False(t, item.AdditionalProperties)
	assert.ElementsMatch(t, []string{"subject", "question", "restriction"},
		item.Properties.Address.Required,
		"the address is the subject and the question, not the subject and a topic")
	assert.Equal(t, []string{"string", "null"}, item.Properties.Operation.Type,
		"a statement answering its own question declares no operation")
	assert.Len(t, item.Properties.Operation.Enum, 3,
		"replace, join, and the unstated case")
	assert.Equal(t, []string{"string", "null"},
		item.Properties.Address.Properties.Restriction.Type,
		"most statements narrow nothing")
}

// captureSlog swaps slog.Default() to a text handler writing into the
// returned buffer for the lifetime of the test, and returns a cleanup
// that restores the previous default.
func captureSlog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	return buf, func() { slog.SetDefault(prev) }
}
