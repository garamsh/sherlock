package message_test

import (
	"reflect"
	"testing"

	"github.com/garamsh/gagent/internal/message"
	"github.com/garamsh/gagent/internal/tool/tooltest"
)

// TestMessage_MatchesManifestInputSchema is the schema/struct drift
// guard. The model fills in the message_send manifest's input_schema and
// the agent decodes what comes back into Message, so the two declare the
// same fields or a field silently arrives empty.
//
// The manifest is the only copy of that contract — the registry loads it,
// hashes it, exposes it to the model, and validates calls against it — so
// this reads the manifest rather than a second document of its own.
func TestMessage_MatchesManifestInputSchema(t *testing.T) {
	input, _ := tooltest.ManifestSchemas(t, tooltest.ReadManifest(t, "message_send"))
	tooltest.CheckStructShape(t, input, reflect.TypeOf(message.Message{}))
}
