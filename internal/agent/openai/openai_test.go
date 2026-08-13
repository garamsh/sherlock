package openai_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/agent/openai"
	"github.com/garamsh/gagent/internal/tool"
)

// chatRequest mirrors the provider's request body for assertions.
type chatRequest struct {
	Model             string `json:"model"`
	ParallelToolCalls bool   `json:"parallel_tool_calls"`
	Messages          []struct {
		Role       string `json:"role"`
		Content    string `json:"content"`
		Name       string `json:"name,omitempty"`
		ToolCallID string `json:"tool_call_id,omitempty"`
		ToolCalls  []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls,omitempty"`
	} `json:"messages"`
	Tools []struct {
		Type     string `json:"type"`
		Function struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
			Strict      bool            `json:"strict"`
		} `json:"function"`
	} `json:"tools"`
}

// fakeProvider records the last request and replies with handler-chosen
// bodies in order, at the configured HTTP status.
type fakeProvider struct {
	t       *testing.T
	request chatRequest
	auth    string
	status  int
	replies []string
	calls   int
}

func (f *fakeProvider) serveHTTP(w http.ResponseWriter, r *http.Request) {
	require.Equal(f.t, http.MethodPost, r.Method)
	require.Equal(f.t, "/v1/chat/completions", r.URL.Path)
	f.auth = r.Header.Get("Authorization")
	require.NoError(f.t, json.NewDecoder(r.Body).Decode(&f.request))
	reply := f.replies[f.calls]
	f.calls++
	w.Header().Set("Content-Type", "application/json")
	if f.status != 0 {
		w.WriteHeader(f.status)
	}
	_, _ = w.Write([]byte(reply))
}

func newFakeProvider(t *testing.T, replies ...string) (*fakeProvider, *httptest.Server) {
	t.Helper()
	f := &fakeProvider{t: t, replies: replies}
	return f, httptest.NewServer(http.HandlerFunc(f.serveHTTP))
}

func newTestModel(t *testing.T, server *httptest.Server) *openai.Model {
	t.Helper()
	model, err := openai.New(openai.Config{
		BaseURL: server.URL + "/v1",
		Model:   "test-model",
		APIKey:  "sk-test",
	})
	require.NoError(t, err)
	return model
}

func TestChat_FinalResponsePreservesWritebackBlock(t *testing.T) {
	final := "Done.\n```memory\n{\"add\":[{\"content\":\"user likes tea\",\"type\":\"semantic\"}]}\n```"
	fake, server := newFakeProvider(t,
		`{"choices":[{"message":{"content":`+jsonString(final)+`},"finish_reason":"stop"}]}`)
	defer server.Close()
	model := newTestModel(t, server)

	resp, err := model.Chat(t.Context(), []agent.Message{
		{Role: agent.RoleSystem, Content: "instructions"},
		{Role: agent.RoleUser, Content: "hello"},
	}, []tool.Descriptor{
		{
			Name:        "echo",
			Description: "echo the text back",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
		},
	})
	require.NoError(t, err)

	assert.Equal(t, "Bearer sk-test", fake.auth)
	assert.Equal(t, "test-model", fake.request.Model)
	require.Len(t, fake.request.Messages, 2)
	assert.Equal(t, "system", fake.request.Messages[0].Role)
	assert.Equal(t, "instructions", fake.request.Messages[0].Content)
	assert.Equal(t, "user", fake.request.Messages[1].Role)
	require.Len(t, fake.request.Tools, 1)
	assert.Equal(t, "function", fake.request.Tools[0].Type)
	assert.Equal(t, "echo", fake.request.Tools[0].Function.Name)
	assert.Equal(t, "echo the text back", fake.request.Tools[0].Function.Description)
	assert.JSONEq(t,
		`{"type":"object","properties":{"text":{"type":"string"}}}`,
		string(fake.request.Tools[0].Function.Parameters))

	// The content comes back verbatim, writeback block included, for the
	// session to strip and parse.
	assert.Equal(t, final, resp.Content)
	assert.Empty(t, resp.ToolCalls)
}

func TestChat_ToolCallRoundTrip(t *testing.T) {
	fake, server := newFakeProvider(t,
		`{"choices":[{"message":{"content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"echo","arguments":"{\"text\":\"hi\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"choices":[{"message":{"content":`+jsonString("The echo said hi.\n```memory\n{}\n```")+`},"finish_reason":"stop"}]}`)
	defer server.Close()
	model := newTestModel(t, server)

	first, err := model.Chat(t.Context(), []agent.Message{
		{Role: agent.RoleUser, Content: "echo hi"},
	}, nil)
	require.NoError(t, err)
	require.Len(t, first.ToolCalls, 1)
	assert.Equal(t, "call_1", first.ToolCalls[0].ID)
	assert.Equal(t, "echo", first.ToolCalls[0].Name)
	assert.JSONEq(t, `{"text":"hi"}`, string(first.ToolCalls[0].Input))

	// The loop feeds the assistant request and the tool result back; the
	// wire shape must echo the tool_call_id.
	second, err := model.Chat(t.Context(), []agent.Message{
		{Role: agent.RoleUser, Content: "echo hi"},
		{Role: agent.RoleAssistant, ToolCalls: first.ToolCalls},
		{Role: agent.RoleTool, Name: "echo", ToolCallID: "call_1", Content: `{"echo":"hi"}`},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, "The echo said hi.\n```memory\n{}\n```", second.Content)

	require.Len(t, fake.request.Messages, 3)
	assistant := fake.request.Messages[1]
	require.Len(t, assistant.ToolCalls, 1)
	assert.Equal(t, "call_1", assistant.ToolCalls[0].ID)
	assert.Equal(t, "function", assistant.ToolCalls[0].Type)
	assert.Equal(t, "echo", assistant.ToolCalls[0].Function.Name)
	assert.JSONEq(t, `{"text":"hi"}`, assistant.ToolCalls[0].Function.Arguments)
	toolMsg := fake.request.Messages[2]
	assert.Equal(t, "tool", toolMsg.Role)
	assert.Equal(t, "call_1", toolMsg.ToolCallID)
	assert.Equal(t, `{"echo":"hi"}`, toolMsg.Content)
}

func TestChat_Non2xxReturnsTypedError(t *testing.T) {
	fake, server := newFakeProvider(t, `{"error":{"message":"boom"}}`)
	fake.status = http.StatusTooManyRequests
	defer server.Close()
	model := newTestModel(t, server)

	_, err := model.Chat(t.Context(), []agent.Message{{Role: agent.RoleUser, Content: "hi"}}, nil)
	require.Error(t, err)
	var apiErr *openai.APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
	assert.Contains(t, apiErr.Body, "boom")
}

func TestChat_MalformedResponseFails(t *testing.T) {
	_, server := newFakeProvider(t, `not json`)
	defer server.Close()
	model := newTestModel(t, server)

	_, err := model.Chat(t.Context(), []agent.Message{{Role: agent.RoleUser, Content: "hi"}}, nil)
	assert.Error(t, err)
}

func TestChat_NoChoicesFails(t *testing.T) {
	_, server := newFakeProvider(t, `{"choices":[]}`)
	defer server.Close()
	model := newTestModel(t, server)

	_, err := model.Chat(t.Context(), []agent.Message{{Role: agent.RoleUser, Content: "hi"}}, nil)
	assert.Error(t, err)
}

func TestNew_MissingSettingsFailFast(t *testing.T) {
	tests := []struct {
		name string
		cfg  openai.Config
	}{
		{"no base URL", openai.Config{Model: "m", APIKey: "k"}},
		{"no model", openai.Config{BaseURL: "http://x", APIKey: "k"}},
		{"no API key", openai.Config{BaseURL: "http://x", Model: "m"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := openai.New(tt.cfg)
			assert.Error(t, err)
		})
	}
}

// TestChat_OutOfSubsetToolParametersAreSanitized reproduces issue #55:
// an external tool registers a schema that the OpenAI function-calling
// strict-mode API rejects (top-level allOf). The provider must sanitize
// the schema in toWireTools so the model call does not crash with HTTP
// 400 invalid_function_parameters. The strict-mode subset also rejects
// top-level oneOf (issue #313); the sanitizer drops every keyword in
// OpenAITopLevelForbidden so the API never sees any of them.
func TestChat_OutOfSubsetToolParametersAreSanitized(t *testing.T) {
	fake, server := newFakeProvider(t,
		`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	defer server.Close()
	model := newTestModel(t, server)

	// Out-of-subset: top-level allOf, no top-level type. The strict-mode
	// API rejects both.
	outOfSubsetSchema := json.RawMessage(`{
		"allOf": [
			{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]},
			{"type":"object","properties":{"b":{"type":"integer"}},"required":["b"]}
		]
	}`)
	_, err := model.Chat(t.Context(),
		[]agent.Message{{Role: agent.RoleUser, Content: "hi"}},
		[]tool.Descriptor{
			{Name: "external_tool", Description: "an external tool", InputSchema: outOfSubsetSchema},
		})
	require.NoError(t, err)

	require.Len(t, fake.request.Tools, 1)
	params := fake.request.Tools[0].Function.Parameters

	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(params, &doc))
	for _, kw := range openai.OpenAITopLevelForbidden {
		assert.NotContains(t, doc, kw, "the provider must drop top-level %q before the API sees it", kw)
	}
	assert.JSONEq(t, `"object"`, string(doc["type"]),
		"the sanitized parameters must declare top-level type object")
}

// jsonString renders s as a JSON string literal for embedding in replies.
func jsonString(s string) string {
	out, _ := json.Marshal(s)
	return string(out)
}

// TestChat_RequestBodyAdvertisesStrictModeAndDisablesParallelCalls pins
// the constraints ADR 0018 §Tool contract imposes on the openai provider
// at the wire level: each tool carries strict:true so the API applies
// constrained decoding, and the request body sets parallel_tool_calls to
// false because the strict-mode API rejects parallel calls. Issue #298.
func TestChat_RequestBodyAdvertisesStrictModeAndDisablesParallelCalls(t *testing.T) {
	fake, server := newFakeProvider(t,
		`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	defer server.Close()
	model := newTestModel(t, server)

	_, err := model.Chat(t.Context(),
		[]agent.Message{{Role: agent.RoleUser, Content: "hi"}},
		[]tool.Descriptor{
			{Name: "github", Description: "github", InputSchema: json.RawMessage(`{"type":"object","properties":{"operation":{"type":"string"}},"required":["operation"]}`)},
		})
	require.NoError(t, err)

	assert.False(t, fake.request.ParallelToolCalls,
		"the request body must carry parallel_tool_calls: false (issue #298)")
	require.Len(t, fake.request.Tools, 1)
	assert.True(t, fake.request.Tools[0].Function.Strict,
		"every tool definition must carry strict: true (issue #298)")
}
