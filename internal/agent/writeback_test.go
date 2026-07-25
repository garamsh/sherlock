package agent_test

import (
	"errors"
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
			name:    "empty block yields no operations",
			content: "done\n```memory\n{}\n```",
			visible: "done",
			ops:     nil,
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
			visible, ops, err := agent.ParseWriteback(tt.content)
			require.NoError(t, err)
			assert.Equal(t, tt.visible, visible)
			assert.Equal(t, tt.ops, ops)
		})
	}
}

func TestParseWriteback_Malformed(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"unterminated fence", "text\n```memory\n{}"},
		{"invalid JSON", "text\n```memory\nnot json\n```"},
		{"keep missing id", "text\n```memory\n{\"keep\":[\"\"]}\n```"},
		{"update missing content", "text\n```memory\n{\"update\":[{\"id\":\"m1\"}]}\n```"},
		{"delete missing id", "text\n```memory\n{\"delete\":[\"\"]}\n```"},
		{"add missing content", "text\n```memory\n{\"add\":[{\"type\":\"episodic\"}]}\n```"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := agent.ParseWriteback(tt.content)
			require.Error(t, err)
			assert.True(t, errors.Is(err, agent.ErrMalformedWriteback),
				"expected ErrMalformedWriteback, got %v", err)
		})
	}
}
