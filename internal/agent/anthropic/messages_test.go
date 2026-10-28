package anthropic_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/agent/anthropic"
	"github.com/garamsh/sherlock/internal/tool"
)

// messagesRequest mirrors the provider's request body for assertions.
type messagesRequest struct {
	Model     string `json:"model"`
	MaxTokens int    `json:"max_tokens"`
	System    string `json:"system"`
	Messages  []struct {
		Role    string `json:"role"`
		Content []struct {
			Type      string          `json:"type"`
			Text      string          `json:"text"`
			ID        string          `json:"id"`
			Name      string          `json:"name"`
			Input     json.RawMessage `json:"input"`
			ToolUseID string          `json:"tool_use_id"`
			Content   string          `json:"content"`
		} `json:"content"`
	} `json:"messages"`
	Tools []struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	} `json:"tools"`
	// ResponseFormat is absent from this protocol; decoding it proves the
	// client does not send a field the API would reject.
	ResponseFormat json.RawMessage `json:"response_format"`
}

// fakeProvider records the last request and replies with a chosen body.
type fakeProvider struct {
	server  *httptest.Server
	request messagesRequest
	headers http.Header
}

func newFakeProvider(t *testing.T, status int, body string) *fakeProvider {
	t.Helper()
	f := &fakeProvider{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.headers = r.Header.Clone()
		require.NoError(t, json.NewDecoder(r.Body).Decode(&f.request))
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(f.server.Close)
	return f
}

func newModel(t *testing.T, f *fakeProvider) *anthropic.Model {
	t.Helper()
	m, err := anthropic.New(anthropic.Config{
		BaseURL: f.server.URL + "/v1",
		Model:   "claude-test",
		APIKey:  "test-key",
	})
	require.NoError(t, err)
	return m
}

const textOnlyReply = `{"content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn"}`

// TestChat_LeadingSystemMessagesBecomeTheTopLevelSystemField pins the
// first shape difference: the Messages API has no system role, so the
// instructions that open the history have to move to a top-level field.
// Sending them as a message would be rejected, and dropping them would
// remove the platform contract from the turn.
func TestChat_LeadingSystemMessagesBecomeTheTopLevelSystemField(t *testing.T) {
	f := newFakeProvider(t, http.StatusOK, textOnlyReply)
	_, err := newModel(t, f).Chat(t.Context(), []agent.Message{
		{Role: agent.RoleSystem, Content: "instructions"},
		{Role: agent.RoleUser, Content: "task"},
	}, nil)
	require.NoError(t, err)

	assert.Equal(t, "instructions", f.request.System,
		"the opening system message belongs in the top-level system field")
	require.Len(t, f.request.Messages, 1, "it must not also appear as a message")
	assert.Equal(t, "user", f.request.Messages[0].Role)
}

// TestChat_SystemMessageAfterTheConversationStartsStaysInPlace pins the
// half of the rule that a naive hoist gets wrong. The frame block is a
// system message the session appends before each task, and its meaning
// depends on standing next to that task. Hoisting it into the top-level
// field would reorder it ahead of every earlier turn.
func TestChat_SystemMessageAfterTheConversationStartsStaysInPlace(t *testing.T) {
	f := newFakeProvider(t, http.StatusOK, textOnlyReply)
	_, err := newModel(t, f).Chat(t.Context(), []agent.Message{
		{Role: agent.RoleSystem, Content: "instructions"},
		{Role: agent.RoleUser, Content: "first task"},
		{Role: agent.RoleAssistant, Content: "first answer"},
		{Role: agent.RoleSystem, Content: "frame block"},
		{Role: agent.RoleUser, Content: "second task"},
	}, nil)
	require.NoError(t, err)

	assert.Equal(t, "instructions", f.request.System,
		"only the opening system message is hoisted")
	require.Len(t, f.request.Messages, 3)
	last := f.request.Messages[2]
	assert.Equal(t, "user", last.Role, "a later system message becomes a user message")
	require.Len(t, last.Content, 2, "it merges with the task it frames")
	assert.Equal(t, "frame block", last.Content[0].Text,
		"and keeps its position ahead of that task")
	assert.Equal(t, "second task", last.Content[1].Text)
}

// TestChat_ToolResultsBecomeOneUserMessageOfToolResultBlocks pins the
// other two shape differences together: a tool result is a block on a
// user message rather than a role, and roles must alternate, so a turn
// that called two tools sends one user message carrying both results.
// Two consecutive user messages are rejected by the API.
func TestChat_ToolResultsBecomeOneUserMessageOfToolResultBlocks(t *testing.T) {
	f := newFakeProvider(t, http.StatusOK, textOnlyReply)
	_, err := newModel(t, f).Chat(t.Context(), []agent.Message{
		{Role: agent.RoleSystem, Content: "instructions"},
		{Role: agent.RoleUser, Content: "task"},
		{Role: agent.RoleAssistant, ToolCalls: []agent.ToolCall{
			{ID: "call_a", Name: "files", Input: json.RawMessage(`{"path":"a"}`)},
			{ID: "call_b", Name: "shell_exec", Input: json.RawMessage(`{"cmd":"ls"}`)},
		}},
		{Role: agent.RoleTool, ToolCallID: "call_a", Name: "files", Content: "result a"},
		{Role: agent.RoleTool, ToolCallID: "call_b", Name: "shell_exec", Content: "result b"},
	}, nil)
	require.NoError(t, err)

	require.Len(t, f.request.Messages, 3, "user, assistant, then one user message of results")

	assistant := f.request.Messages[1]
	assert.Equal(t, "assistant", assistant.Role)
	require.Len(t, assistant.Content, 2, "a step requesting only tool calls carries no text block")
	assert.Equal(t, "tool_use", assistant.Content[0].Type)
	assert.Equal(t, "files", assistant.Content[0].Name)
	assert.JSONEq(t, `{"path":"a"}`, string(assistant.Content[0].Input),
		"tool input is an object on this protocol, not a JSON-encoded string")

	results := f.request.Messages[2]
	assert.Equal(t, "user", results.Role, "a tool result is a user message here")
	require.Len(t, results.Content, 2, "both results merge into one message")
	assert.Equal(t, "tool_result", results.Content[0].Type)
	assert.Equal(t, "call_a", results.Content[0].ToolUseID)
	assert.Equal(t, "result a", results.Content[0].Content)
	assert.Equal(t, "call_b", results.Content[1].ToolUseID)
}

// TestChat_SendsTheProtocolHeadersAndRequiredMaxTokens pins what the API
// rejects a request without: its own auth header rather than a bearer
// token, a version header, and max_tokens, which chat-completions treats
// as optional.
func TestChat_SendsTheProtocolHeadersAndRequiredMaxTokens(t *testing.T) {
	f := newFakeProvider(t, http.StatusOK, textOnlyReply)
	_, err := newModel(t, f).Chat(t.Context(), []agent.Message{
		{Role: agent.RoleUser, Content: "task"},
	}, nil)
	require.NoError(t, err)

	assert.Equal(t, "test-key", f.headers.Get("X-Api-Key"))
	assert.Empty(t, f.headers.Get("Authorization"), "this protocol does not take a bearer token")
	assert.NotEmpty(t, f.headers.Get("Anthropic-Version"), "the API requires a version header")
	assert.Positive(t, f.request.MaxTokens, "max_tokens is required on this protocol")
	assert.Empty(t, f.request.ResponseFormat,
		"this protocol has no response_format; sending one would be rejected")
}

// TestChat_MapsTextAndToolUseBlocksOntoTheSeam pins the return direction:
// text blocks concatenate into Content and tool_use blocks become
// ToolCalls, which is what lets agent.Model stay unchanged.
func TestChat_MapsTextAndToolUseBlocksOntoTheSeam(t *testing.T) {
	body := `{"content":[
		{"type":"text","text":"thinking. "},
		{"type":"text","text":"done."},
		{"type":"tool_use","id":"call_1","name":"files","input":{"path":"x"}}
	],"stop_reason":"tool_use"}`
	f := newFakeProvider(t, http.StatusOK, body)
	resp, err := newModel(t, f).Chat(t.Context(), []agent.Message{
		{Role: agent.RoleUser, Content: "task"},
	}, nil)
	require.NoError(t, err)

	assert.Equal(t, "thinking. done.", resp.Content, "text blocks concatenate")
	require.Len(t, resp.ToolCalls, 1)
	assert.Equal(t, "call_1", resp.ToolCalls[0].ID)
	assert.Equal(t, "files", resp.ToolCalls[0].Name)
	assert.JSONEq(t, `{"path":"x"}`, string(resp.ToolCalls[0].Input))
}

// TestChat_TruncatedResponseIsAnErrorRatherThanAPartialDocument pins the
// consequence of max_tokens being required. The session parses Content as
// one document, so half a document returned as success would surface as a
// parse failure somewhere with no way back to the cause.
func TestChat_TruncatedResponseIsAnErrorRatherThanAPartialDocument(t *testing.T) {
	body := `{"content":[{"type":"text","text":"{\"repl"}],"stop_reason":"max_tokens"}`
	f := newFakeProvider(t, http.StatusOK, body)
	_, err := newModel(t, f).Chat(t.Context(), []agent.Message{
		{Role: agent.RoleUser, Content: "task"},
	}, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, anthropic.ErrResponseTruncated)
}

// TestChat_SendsToolSchemasUnmodified pins that no strict-mode projection
// happens on this protocol: that projection belongs to chat-completions'
// constrained decoding, and applying it here would send a shape this API
// never asked for.
func TestChat_SendsToolSchemasUnmodified(t *testing.T) {
	schema := `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`
	f := newFakeProvider(t, http.StatusOK, textOnlyReply)
	_, err := newModel(t, f).Chat(t.Context(), []agent.Message{
		{Role: agent.RoleUser, Content: "task"},
	}, []tool.Descriptor{{
		Name:        "files",
		Description: "read and write",
		InputSchema: json.RawMessage(schema),
	}})
	require.NoError(t, err)

	require.Len(t, f.request.Tools, 1)
	assert.Equal(t, "files", f.request.Tools[0].Name)
	assert.JSONEq(t, schema, string(f.request.Tools[0].InputSchema))
}

// TestChat_NonSuccessStatusIsReportedAsAPIError pins that the provider's
// own failure reaches the caller with its status rather than as a decode
// error against a body that is not a response.
func TestChat_NonSuccessStatusIsReportedAsAPIError(t *testing.T) {
	f := newFakeProvider(t, http.StatusUnauthorized, `{"error":{"message":"invalid x-api-key"}}`)
	_, err := newModel(t, f).Chat(t.Context(), []agent.Message{
		{Role: agent.RoleUser, Content: "task"},
	}, nil)
	require.Error(t, err)
	var apiErr *anthropic.APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
}

// keyGate is a Messages endpoint that accepts one x-api-key and answers
// every other with 401, recording each request's key.
type keyGate struct {
	URL string

	mu       sync.Mutex
	accepted string
	keys     []string
}

func newKeyGate(t *testing.T, accepted string) *keyGate {
	t.Helper()
	g := &keyGate{accepted: accepted}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		key := r.Header.Get("X-Api-Key")
		g.keys = append(g.keys, key)
		if key != g.accepted {
			http.Error(w, `{"type":"error","error":{"type":"authentication_error"}}`, http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(textOnlyReply))
	}))
	t.Cleanup(srv.Close)
	g.URL = srv.URL + "/v1"
	return g
}

// seen returns the key of every request so far.
func (g *keyGate) seen() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.keys...)
}

// rotatingCredential returns its keys in order, then repeats the last,
// standing in for a file the platform rewrites between reads.
type rotatingCredential struct {
	keys  []string
	reads int
	err   error
}

func (c *rotatingCredential) Read() (string, error) {
	if c.err != nil {
		return "", c.err
	}
	key := c.keys[min(c.reads, len(c.keys)-1)]
	c.reads++
	return key, nil
}

func (c *rotatingCredential) Source() string { return "test-source /run/key" }

func chatWithCredential(ctx context.Context, t *testing.T, url string, credential anthropic.Credential) error {
	t.Helper()
	m, err := anthropic.New(anthropic.Config{BaseURL: url, Model: "auto", Credential: credential})
	require.NoError(t, err)
	_, err = m.Chat(ctx, []agent.Message{{Role: agent.RoleUser, Content: "hi"}}, nil)
	return err
}

// TestChat_RejectedCredentialIsReadAgainAndRetriedOnce pins ADR 0029's
// recovery on this protocol. The fixed-key control shows the retry
// belongs to the credential: a key that cannot change is not retried.
func TestChat_RejectedCredentialIsReadAgainAndRetriedOnce(t *testing.T) {
	ctx := t.Context()

	g := newKeyGate(t, "rotated")
	require.NoError(t, chatWithCredential(ctx, t, g.URL, &rotatingCredential{keys: []string{"expired", "rotated"}}),
		"the re-read key recovers the call")
	assert.Equal(t, []string{"expired", "rotated"}, g.seen())

	fixed := newKeyGate(t, "rotated")
	m, err := anthropic.New(anthropic.Config{BaseURL: fixed.URL, Model: "auto", APIKey: "expired"})
	require.NoError(t, err)
	_, err = m.Chat(ctx, []agent.Message{{Role: agent.RoleUser, Content: "hi"}}, nil)
	require.Error(t, err)
	fixedKeys := fixed.seen()
	assert.Len(t, fixedKeys, 1, "a fixed key is sent once")
}

func TestChat_SecondRejectionFailsTheCallNamingTheSource(t *testing.T) {
	g := newKeyGate(t, "never-issued")

	err := chatWithCredential(t.Context(), t, g.URL, &rotatingCredential{keys: []string{"expired"}})
	require.Error(t, err)

	var apiErr *anthropic.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
	keys := g.seen()
	assert.Len(t, keys, 2, "one request and one retry, no more")
	assert.Contains(t, err.Error(), "test-source /run/key")
	assert.NotContains(t, err.Error(), "expired", "the error names the source, never the key")
}

func TestChat_UnreadableCredentialFailsTheCallNamingTheSource(t *testing.T) {
	g := newKeyGate(t, "k")

	err := chatWithCredential(t.Context(), t, g.URL, &rotatingCredential{err: errors.New("no such file")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "test-source /run/key")
	assert.Contains(t, err.Error(), "no such file")
	keys := g.seen()
	assert.Empty(t, keys, "nothing is sent without a key")
}
