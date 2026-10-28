package agent_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
)

func TestParseWriteback_Reply(t *testing.T) {
	reply, err := agent.ParseWriteback(`{"reply":" hi "}`)
	require.NoError(t, err)
	assert.Equal(t, "hi", reply)
}

// TestParseWriteback_MalformedArguments fails arguments that are not the
// document, without leaking the parser's Go field names (issue #77).
func TestParseWriteback_MalformedArguments(t *testing.T) {
	for name, content := range map[string]string{
		"prose instead of the document": "still useful",
		"truncated document":            `{"reply":"answer"`,
		"numeric reply":                 `{"reply":1}`,
		"empty arguments":               ``,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := agent.ParseWriteback(content)
			require.Error(t, err)
			assert.True(t, errors.Is(err, agent.ErrMalformedWriteback), "got %v", err)
			for _, bad := range []string{"Go struct field", "writebackJSON"} {
				assert.NotContains(t, err.Error(), bad)
			}
		})
	}
}

// TestResponseSchema_ShapeIsEnforceable pins the terminal call's shape: a
// reply and nothing else.
func TestResponseSchema_ShapeIsEnforceable(t *testing.T) {
	var schema struct {
		Type                 string                     `json:"type"`
		Required             []string                   `json:"required"`
		AdditionalProperties bool                       `json:"additionalProperties"`
		Properties           map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal([]byte(agent.ResponseSchema), &schema))
	assert.Equal(t, "object", schema.Type)
	assert.Equal(t, []string{"reply"}, schema.Required)
	assert.False(t, schema.AdditionalProperties)
	assert.Len(t, schema.Properties, 1, "the reply only; the memory files have their own call")
}
