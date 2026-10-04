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

// platformStamped are the fields message_send stamps from its own
// process rather than from the call: the sender (ADR 0016), the entry's
// ID, and the messages the turn replies to.
var platformStamped = []string{"agent_id", "id", "in_reply_to"}

// TestMessage_CarriesTheSenderTheManifestDoesNotAskFor is the
// schema/struct drift guard, and it states a relationship rather than an
// equality: the manifest's input_schema is what the model sends, and a
// delivered Message is that plus what the platform stamps.
//
// The two halves both matter. If Message loses a field the model sends,
// a delivered file drops it silently. If input_schema gains a stamped
// field, it becomes something the model states — and a model that can
// state its own identity can state someone else's, which is the whole
// value of the field. message_send reads them from its environment and
// its own write instead, so the platform's fact reaches the file and the
// model's claim cannot.
func TestMessage_CarriesTheSenderTheManifestDoesNotAskFor(t *testing.T) {
	inputSchema, _ := tooltest.ManifestSchemas(t, tooltest.ReadManifest(t, "message_send"))

	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(inputSchema, &schema))

	want := make([]string, 0, len(schema.Properties)+len(platformStamped))
	for name := range schema.Properties {
		want = append(want, name)
	}
	for _, name := range platformStamped {
		assert.NotContains(t, schema.Properties, name,
			"%s is the platform's to stamp, never a field the model fills in", name)
		want = append(want, name)
	}
	sort.Strings(want)

	assert.Equal(t, want, jsonTags(reflect.TypeOf(message.Message{})),
		"a delivered message is what the model sent plus what the platform stamps")
}

// jsonTags returns typ's json tag names, sorted.
func jsonTags(typ reflect.Type) []string {
	tags := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		tags = append(tags, name)
	}
	sort.Strings(tags)
	return tags
}
