package message_test

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/message"
	"github.com/garamsh/sherlock/internal/tool/tooltest"
)

// TestMessage_CarriesTheSenderTheManifestDoesNotAskFor is the
// schema/struct drift guard, and it states a relationship rather than an
// equality: the manifest's input_schema is what the model sends, and a
// delivered Message is that plus the sending agent's ID (ADR 0016).
//
// The two halves both matter. If Message loses a field the model sends,
// a delivered file drops it silently. If input_schema gains agent_id,
// the sender becomes something the model states — and a model that can
// state its own identity can state someone else's, which is the whole
// value of the field. message_send reads it from its environment
// instead, so the platform's fact reaches the file and the model's
// claim cannot.
func TestMessage_CarriesTheSenderTheManifestDoesNotAskFor(t *testing.T) {
	inputSchema, _ := tooltest.ManifestSchemas(t, tooltest.ReadManifest(t, "message_send"))

	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(inputSchema, &schema))

	sent := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		sent = append(sent, name)
	}
	sort.Strings(sent)

	assert.NotContains(t, schema.Properties, "agent_id",
		"the sender is the platform's to stamp, never a field the model fills in")

	delivered := jsonTags(reflect.TypeOf(message.Message{}))
	assert.Equal(t, append(append([]string{}, sent...), "agent_id"), delivered,
		"a delivered message is what the model sent plus the agent that sent it")
}

// jsonTags returns typ's json tag names, sorted, with "agent_id" last so
// the assertion above reads as "what was sent, then who sent it".
func jsonTags(typ reflect.Type) []string {
	tags := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		tags = append(tags, name)
	}
	sort.Slice(tags, func(i, j int) bool {
		if (tags[i] == "agent_id") != (tags[j] == "agent_id") {
			return tags[j] == "agent_id"
		}
		return tags[i] < tags[j]
	})
	return tags
}
