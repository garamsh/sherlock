package openai_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/agent/openai"
)

const okReply = `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`

// gate is a chat-completions endpoint that accepts one bearer token and
// answers every other with 401, recording each request's Authorization
// and turn header.
type gate struct {
	URL string

	mu       sync.Mutex
	accepted string
	auths    []string
	turnIDs  []string
}

func newGate(t *testing.T, accepted string) *gate {
	t.Helper()
	g := &gate{accepted: accepted}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		auth := r.Header.Get("Authorization")
		g.auths = append(g.auths, auth)
		g.turnIDs = append(g.turnIDs, r.Header.Get(agent.TurnIDHeader))
		if auth != "Bearer "+g.accepted {
			http.Error(w, `{"error":{"message":"invalid token"}}`, http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(okReply))
	}))
	t.Cleanup(srv.Close)
	g.URL = srv.URL + "/v1"
	return g
}

// accept changes the token the gate accepts.
func (g *gate) accept(token string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.accepted = token
}

// seen returns the Authorization and turn header of every request so far.
func (g *gate) seen() (auths, turnIDs []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.auths...), append([]string(nil), g.turnIDs...)
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

func newCredentialModel(t *testing.T, url string, credential openai.Credential) *openai.Model {
	t.Helper()
	model, err := openai.New(openai.Config{BaseURL: url, Model: "auto", Credential: credential})
	require.NoError(t, err)
	return model
}

func chatOnce(ctx context.Context, model *openai.Model) error {
	_, err := model.Chat(ctx, []agent.Message{{Role: agent.RoleUser, Content: "hi"}}, nil)
	return err
}

func TestChat_TurnIDTravelsAsTheTurnHeader(t *testing.T) {
	g := newGate(t, "k")
	model := newCredentialModel(t, g.URL, &rotatingCredential{keys: []string{"k"}})

	require.NoError(t, chatOnce(agent.WithTurnID(t.Context(), "turn-1"), model))
	require.NoError(t, chatOnce(t.Context(), model))

	_, turnIDs := g.seen()
	assert.Equal(t, []string{"turn-1", ""}, turnIDs, "a request outside a turn carries no turn header")
}

func TestChat_CredentialIsReadForEveryRequest(t *testing.T) {
	g := newGate(t, "first")
	credential := &rotatingCredential{keys: []string{"first", "second"}}
	model := newCredentialModel(t, g.URL, credential)

	require.NoError(t, chatOnce(t.Context(), model))
	g.accept("second")
	require.NoError(t, chatOnce(t.Context(), model))

	auths, _ := g.seen()
	assert.Equal(t, []string{"Bearer first", "Bearer second"}, auths,
		"the second request carries the key as it reads now, with no new client")
}

// TestChat_RejectedCredentialIsReadAgainAndRetriedOnce pins ADR 0029's
// recovery: a 401 reads the credential again and sends the request once
// more, under the same turn. The fixed-key control shows the retry
// belongs to the credential: a key that cannot change is not retried.
func TestChat_RejectedCredentialIsReadAgainAndRetriedOnce(t *testing.T) {
	ctx := agent.WithTurnID(t.Context(), "turn-1")

	g := newGate(t, "rotated")
	model := newCredentialModel(t, g.URL, &rotatingCredential{keys: []string{"expired", "rotated"}})
	require.NoError(t, chatOnce(ctx, model), "the re-read key recovers the call")
	auths, turnIDs := g.seen()
	assert.Equal(t, []string{"Bearer expired", "Bearer rotated"}, auths)
	assert.Equal(t, []string{"turn-1", "turn-1"}, turnIDs, "the retry stays in the turn")

	fixed := newGate(t, "rotated")
	fixedModel, err := openai.New(openai.Config{BaseURL: fixed.URL, Model: "auto", APIKey: "expired"})
	require.NoError(t, err)
	require.Error(t, chatOnce(ctx, fixedModel))
	fixedAuths, _ := fixed.seen()
	assert.Len(t, fixedAuths, 1, "a fixed key is sent once")
}

func TestChat_SecondRejectionFailsTheCallNamingTheSource(t *testing.T) {
	g := newGate(t, "never-issued")
	model := newCredentialModel(t, g.URL, &rotatingCredential{keys: []string{"expired"}})

	err := chatOnce(t.Context(), model)
	require.Error(t, err)

	var apiErr *openai.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
	auths, _ := g.seen()
	assert.Len(t, auths, 2, "one request and one retry, no more")
	assert.Contains(t, err.Error(), "test-source /run/key")
	assert.NotContains(t, err.Error(), "expired", "the error names the source, never the key")
}

func TestChat_UnreadableCredentialFailsTheCallNamingTheSource(t *testing.T) {
	g := newGate(t, "k")
	model := newCredentialModel(t, g.URL, &rotatingCredential{err: errors.New("no such file")})

	err := chatOnce(t.Context(), model)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "test-source /run/key")
	assert.Contains(t, err.Error(), "no such file")
	auths, _ := g.seen()
	assert.Empty(t, auths, "nothing is sent without a key")
}

func TestNew_APIKeyAndCredentialAreExclusive(t *testing.T) {
	_, err := openai.New(openai.Config{
		BaseURL: "http://x", Model: "m", APIKey: "k", Credential: &rotatingCredential{keys: []string{"k"}},
	})
	assert.ErrorContains(t, err, "both set")
}
