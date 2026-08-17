package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/tool"
)

// maxErrorBody caps how much of an error response body is kept in APIError.
const maxErrorBody = 512

// messagesRequest is the Messages API request body. MaxTokens is required
// by the API, and System is a top-level field rather than a message role.
type messagesRequest struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	System    string        `json:"system,omitempty"`
	Messages  []wireMessage `json:"messages"`
	Tools     []wireTool    `json:"tools,omitempty"`
}

// wireMessage is one conversation entry. Content is a block list rather
// than a string: one message carries text, tool calls and tool results
// together, which is what lets consecutive same-role entries merge.
type wireMessage struct {
	Role    string      `json:"role"`
	Content []wireBlock `json:"content"`
}

// wireBlock is one content block. The API discriminates on Type, and the
// fields that do not belong to that type are omitted rather than sent
// empty: a text block carrying a null input is rejected.
type wireBlock struct {
	Type string `json:"type"`
	// Text carries a "text" block's body.
	Text string `json:"text,omitempty"`
	// ID, Name and Input carry a "tool_use" block.
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// ToolUseID and Content carry a "tool_result" block.
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
}

// wireTool advertises one registered tool. The API takes the JSON Schema
// as-is, so no strict-mode projection happens here: that projection is a
// property of chat-completions' constrained decoding, not of this
// protocol, and applying it would send a shape this API never asked for.
type wireTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// messagesResponse is the subset of the response the loop consumes.
type messagesResponse struct {
	Content    []wireBlock `json:"content"`
	StopReason string      `json:"stop_reason"`
}

// Chat maps the conversation and tool descriptors to a Messages request
// and maps the content blocks back to an agent.Response: text blocks
// concatenate into Content, tool_use blocks become ToolCalls.
//
// The request carries no response schema. The Messages API has no
// response_format field, so the writeback document is what the contract
// prose asks for rather than what the wire constrains — unlike
// chat-completions, where agent.ResponseSchema is enforced (ADR 0018).
// How the writeback should request structured output on a protocol that
// cannot constrain it is issue #615's decision, not this package's, so
// this implements what the seam already gives and states the difference
// in README.md rather than inventing a mechanism here.
func (m *Model) Chat(ctx context.Context, messages []agent.Message, tools []tool.Descriptor) (agent.Response, error) {
	system, wire := toWireMessages(messages)
	if len(wire) == 0 {
		return agent.Response{}, fmt.Errorf("anthropic provider: no messages to send")
	}
	body, err := json.Marshal(messagesRequest{
		Model:     m.model,
		MaxTokens: m.maxTokens,
		System:    system,
		Messages:  wire,
		Tools:     toWireTools(tools),
	})
	if err != nil {
		return agent.Response{}, fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(body))
	if err != nil {
		return agent.Response{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", m.apiKey)
	req.Header.Set("Anthropic-Version", apiVersion)

	resp, err := m.http.Do(req)
	if err != nil {
		return agent.Response{}, fmt.Errorf("messages: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return agent.Response{}, &APIError{StatusCode: resp.StatusCode, Body: string(snippet)}
	}
	var decoded messagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return agent.Response{}, fmt.Errorf("decode response: %w", err)
	}
	if decoded.StopReason == "max_tokens" {
		return agent.Response{}, ErrResponseTruncated
	}
	if len(decoded.Content) == 0 {
		return agent.Response{}, errNoContent
	}
	var out agent.Response
	var text strings.Builder
	for _, block := range decoded.Content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			input := block.Input
			if len(input) == 0 {
				input = json.RawMessage("{}")
			}
			out.ToolCalls = append(out.ToolCalls, agent.ToolCall{
				ID:    block.ID,
				Name:  block.Name,
				Input: input,
			})
		}
	}
	out.Content = text.String()
	return out, nil
}

// toWireMessages splits the history into the top-level system prompt and
// the message list.
//
// Three shape differences from chat-completions are resolved here, and
// each is forced by the API rather than chosen:
//
//   - There is no system role. The system messages that open the history
//     become the top-level system field. A system message appearing later
//     is runtime-injected context whose position carries meaning (the
//     frame block precedes the task it frames), so it becomes a user
//     message rather than being hoisted out of order.
//   - A tool result is a user message carrying a tool_result block, not a
//     role of its own.
//   - Roles must alternate, so consecutive same-role entries merge into
//     one message holding every block. This is what puts a turn's tool
//     results into a single user message, which is the shape the API
//     expects for a multi-call turn.
func toWireMessages(messages []agent.Message) (string, []wireMessage) {
	var system []string
	var out []wireMessage
	leading := true

	appendBlocks := func(role string, blocks []wireBlock) {
		if len(blocks) == 0 {
			return
		}
		if n := len(out); n > 0 && out[n-1].Role == role {
			out[n-1].Content = append(out[n-1].Content, blocks...)
			return
		}
		out = append(out, wireMessage{Role: role, Content: blocks})
	}

	for _, msg := range messages {
		switch msg.Role {
		case agent.RoleSystem:
			if leading {
				if msg.Content != "" {
					system = append(system, msg.Content)
				}
				continue
			}
			appendBlocks("user", textBlocks(msg.Content))
		case agent.RoleUser:
			leading = false
			appendBlocks("user", textBlocks(msg.Content))
		case agent.RoleAssistant:
			leading = false
			blocks := textBlocks(msg.Content)
			for _, call := range msg.ToolCalls {
				input := call.Input
				if len(input) == 0 {
					input = json.RawMessage("{}")
				}
				blocks = append(blocks, wireBlock{
					Type:  "tool_use",
					ID:    call.ID,
					Name:  call.Name,
					Input: input,
				})
			}
			appendBlocks("assistant", blocks)
		case agent.RoleTool:
			leading = false
			appendBlocks("user", []wireBlock{{
				Type:      "tool_result",
				ToolUseID: msg.ToolCallID,
				Content:   msg.Content,
			}})
		}
	}
	return strings.Join(system, "\n\n"), out
}

// textBlocks returns the one text block content carries, or nothing when
// it is empty: the API rejects an empty text block, and an assistant step
// that only requests tool calls carries no text.
func textBlocks(content string) []wireBlock {
	if content == "" {
		return nil
	}
	return []wireBlock{{Type: "text", Text: content}}
}

// toWireTools exposes each descriptor's name, description and input
// schema. The schema is sent unmodified: this API validates the document
// it is given rather than requiring the strict-mode subset.
func toWireTools(tools []tool.Descriptor) []wireTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]wireTool, len(tools))
	for i, d := range tools {
		out[i] = wireTool{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: d.InputSchema,
		}
	}
	return out
}
