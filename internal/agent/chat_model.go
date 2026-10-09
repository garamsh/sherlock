package agent

import (
	"context"

	"github.com/garamsh/sherlock/internal/tool"
)

// Model is the agent's narrow view of a chat model (ADR 0004): it receives
// the conversation plus the registered tool schemas and returns either a
// final response or tool-call requests. A turn ends when the model calls
// the reserved MemoryWriteTool, whose arguments are the document
// ResponseSchema describes, parsed by ParseWriteback.
type Model interface {
	Chat(ctx context.Context, messages []Message, tools []tool.Descriptor) (Response, error)
}
