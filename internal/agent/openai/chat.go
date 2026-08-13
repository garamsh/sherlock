package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/tool"
)

// maxErrorBody caps how much of an error response body is kept in APIError.
const maxErrorBody = 512

// chatRequest is the chat-completions request body.
type chatRequest struct {
	Model             string        `json:"model"`
	Messages          []chatMessage `json:"messages"`
	Tools             []chatTool    `json:"tools,omitempty"`
	ParallelToolCalls bool          `json:"parallel_tool_calls"`
}

// chatMessage is the wire shape of one conversation entry. The OpenAI API
// expects tool-call arguments as a JSON-encoded string, so Arguments is a
// plain string in both directions.
type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	Name       string         `json:"name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
}

// chatToolCall is one function-call request or result reference.
type chatToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

// chatFunction carries the callee name and its JSON-encoded arguments.
type chatFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// chatTool advertises one registered tool to the model.
type chatTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters"`
		Strict      bool            `json:"strict"`
	} `json:"function"`
}

// chatResponse is the subset of the chat-completions response the loop
// consumes: the first choice's message.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content   string         `json:"content"`
			ToolCalls []chatToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// Chat maps the conversation and tool descriptors to a chat-completions
// request and maps the first choice back to an agent.Response. Tool calls
// win over content when both are present.
//
// parallel_tool_calls is forced to false: ADR 0018 requires strict-mode
// constrained decoding (advertised as Strict: true on each tool), and the
// OpenAI chat-completions API flags strict mode as incompatible with
// parallel tool calls. Issue #298.
func (m *Model) Chat(ctx context.Context, messages []agent.Message, tools []tool.Descriptor) (agent.Response, error) {
	body, err := json.Marshal(chatRequest{
		Model:             m.model,
		Messages:          toWireMessages(messages),
		Tools:             toWireTools(tools),
		ParallelToolCalls: false,
	})
	if err != nil {
		return agent.Response{}, fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(body))
	if err != nil {
		return agent.Response{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.apiKey)

	resp, err := m.http.Do(req)
	if err != nil {
		return agent.Response{}, fmt.Errorf("chat completions: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return agent.Response{}, &APIError{StatusCode: resp.StatusCode, Body: string(snippet)}
	}
	var decoded chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return agent.Response{}, fmt.Errorf("decode response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return agent.Response{}, errNoChoices
	}
	msg := decoded.Choices[0].Message
	out := agent.Response{Content: msg.Content}
	for _, call := range msg.ToolCalls {
		args := call.Function.Arguments
		if args == "" {
			args = "{}"
		}
		out.ToolCalls = append(out.ToolCalls, agent.ToolCall{
			ID:    call.ID,
			Name:  call.Function.Name,
			Input: json.RawMessage(args),
		})
	}
	return out, nil
}

// toWireMessages maps agent roles one-to-one; they already use the API's
// vocabulary (ADR 0004 kept the interface provider-shaped).
func toWireMessages(messages []agent.Message) []chatMessage {
	out := make([]chatMessage, len(messages))
	for i, msg := range messages {
		wire := chatMessage{
			Role:       string(msg.Role),
			Content:    msg.Content,
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
		}
		for _, call := range msg.ToolCalls {
			wire.ToolCalls = append(wire.ToolCalls, chatToolCall{
				ID:   call.ID,
				Type: "function",
				Function: chatFunction{
					Name:      call.Name,
					Arguments: string(call.Input),
				},
			})
		}
		out[i] = wire
	}
	return out
}

// toWireTools exposes each descriptor's name, description, and input
// schema as the function's parameters document. Strict mode is on so the
// chat-completions API applies constrained decoding to every tool call
// (ADR 0018). Each schema is sanitized to the strict-compatible subset
// first, so tools whose schemas violate the constraint do not break the
// provider's model call (issue #55). Issue #298.
func toWireTools(tools []tool.Descriptor) []chatTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]chatTool, len(tools))
	for i, d := range tools {
		out[i].Type = "function"
		out[i].Function.Name = d.Name
		out[i].Function.Description = d.Description
		out[i].Function.Strict = true
		out[i].Function.Parameters = sanitizeToolParameters(d.Name, d.InputSchema)
	}
	return out
}
