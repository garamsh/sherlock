package agent_test

import (
	"bytes"
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
			name:    "no block returns content unchanged",
			content: "just a reply",
			visible: "just a reply",
			ops:     nil,
		},
		{
			name: "keep update delete add extracted",
			content: "summary\n```memory\n" +
				`{"keep":["m1"],"update":[{"id":"m2","content":"new"}],"delete":["m3"],"add":[{"content":"fact","type":"semantic"}]}` +
				"\n```",
			visible: "summary",
			ops: []agent.WritebackOp{
				{Kind: agent.OpKeep, ID: "m1"},
				{Kind: agent.OpUpdate, ID: "m2", Content: "new"},
				{Kind: agent.OpDelete, ID: "m3"},
				{Kind: agent.OpAdd, Content: "fact", Type: "semantic"},
			},
		},
		{
			name:    "empty block yields empty non-nil operations",
			content: "done\n```memory\n{}\n```",
			visible: "done",
			ops:     []agent.WritebackOp{},
		},
		{
			name:    "visible content is trimmed",
			content: "  spaced out  \n```memory\n{\"keep\":[\"m1\"]}\n```",
			visible: "spaced out",
			ops:     []agent.WritebackOp{{Kind: agent.OpKeep, ID: "m1"}},
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

// TestParseWriteback_LenientParseFailure covers issue #93: a model response
// whose ```memory fence is unterminated or whose JSON is malformed no longer
// fails the turn. The visible content reaches the caller with no operations,
// no fatal error, and a logged warning the session can surface.
func TestParseWriteback_LenientParseFailure(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		wantVisible    string
		wantWarnSubstr string
	}{
		{
			name:           "unterminated fence returns visible",
			content:        "got a partial reply\n```memory\n{}",
			wantVisible:    "got a partial reply",
			wantWarnSubstr: "unterminated",
		},
		{
			name:           "invalid JSON returns visible",
			content:        "still useful\n```memory\nnot json\n```",
			wantVisible:    "still useful",
			wantWarnSubstr: "invalid JSON",
		},
		{
			name:           "type mismatch returns visible",
			content:        "answer\n```memory\n{\"keep\":[1]}\n```",
			wantVisible:    "answer",
			wantWarnSubstr: "invalid JSON",
		},
		{
			name:           "truncated JSON returns visible",
			content:        "answer\n```memory\n{\"keep\":[1,\n```",
			wantVisible:    "answer",
			wantWarnSubstr: "invalid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf, swap := captureSlog(t)
			defer swap()

			visible, ops, warn, err := agent.ParseWriteback(tt.content)
			require.NoError(t, err, "parse failure is non-fatal")
			assert.Equal(t, tt.wantVisible, visible, "visible content survives")
			assert.Nil(t, ops, "no operations when the block cannot be parsed")
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
// behaviour (issue #93): a block whose JSON parses but violates the
// operation schema is still a fatal *WritebackError. The platform refuses
// to apply a contract-violating writeback; the model is expected to learn.
func TestParseWriteback_StrictSchemaValidation(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"keep missing id", "text\n```memory\n{\"keep\":[\"\"]}\n```"},
		{"update missing content", "text\n```memory\n{\"update\":[{\"id\":\"m1\"}]}\n```"},
		{"delete missing id", "text\n```memory\n{\"delete\":[\"\"]}\n```"},
		{"add missing content", "text\n```memory\n{\"add\":[{\"type\":\"episodic\"}]}\n```"},
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
// regression, kept current with issue #93: a type mismatch in the
// writeback JSON must surface a model-friendly warning without leaking
// the unexported Go struct field name. Since issue #93 downgraded type
// mismatches from fatal errors to warnings, the same model-facing
// message now reaches the caller via the warn return.
func TestParseWriteback_TypeErrorStripsInternalFieldName(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantNotIn []string
	}{
		{
			name:      "numeric keep id",
			body:      "\n```memory\n" + `{"keep":[1]}` + "\n```",
			wantNotIn: []string{"Go struct field", "writebackJSON"},
		},
		{
			name:      "boolean content",
			body:      "\n```memory\n" + `{"add":[{"content":true,"type":"semantic"}]}` + "\n```",
			wantNotIn: []string{"Go struct field", "writebackJSON"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, warn, err := agent.ParseWriteback(tc.body)
			require.NoError(t, err, "type mismatches are warnings, not errors (issue #93)")
			require.Error(t, warn)
			msg := warn.Error()
			for _, bad := range tc.wantNotIn {
				assert.NotContains(t, msg, bad,
					"internal Go field name %q must not leak", bad)
			}
		})
	}
}

// TestParseWriteback_SyntaxErrorPreserved keeps syntax errors informative
// (no "Go struct field" in the message in the first place).
func TestParseWriteback_SyntaxErrorPreserved(t *testing.T) {
	body := "\n```memory\n" + `{"keep":[1,` + "\n```"
	visible, ops, warn, err := agent.ParseWriteback(body)
	require.NoError(t, err, "syntax errors are warnings, not fatal errors (issue #93)")
	assert.Empty(t, visible)
	assert.Nil(t, ops)
	require.Error(t, warn)
	assert.Contains(t, warn.Error(), "invalid JSON")
	assert.NotContains(t, warn.Error(), "Go struct field")
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
