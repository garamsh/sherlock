package openai_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent/openai"
)

func TestEmbedder_SendsTextAndReadsVector(t *testing.T) {
	for _, tc := range []struct{ key, wantAuth string }{{"secret", "Bearer secret"}, {"", ""}} {
		var got struct {
			Path, Auth string
			Body       struct {
				Model string   `json:"model"`
				Input []string `json:"input"`
			}
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got.Path, got.Auth = r.URL.Path, r.Header.Get("Authorization")
			require.NoError(t, json.NewDecoder(r.Body).Decode(&got.Body))
			_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.25,-0.5,1]}]}`))
		}))
		embedder, err := openai.NewEmbedder(openai.Config{BaseURL: server.URL + "/v1/", Model: "bge", APIKey: tc.key})
		require.NoError(t, err)
		vector, err := embedder.Embed(context.Background(), "launch codename")
		server.Close()
		require.NoError(t, err)
		assert.Equal(t, []float32{0.25, -0.5, 1}, vector)
		assert.Equal(t, "/v1/embeddings", got.Path)
		assert.Equal(t, "bge", got.Body.Model)
		assert.Equal(t, []string{"launch codename"}, got.Body.Input)
		assert.Equal(t, tc.wantAuth, got.Auth, "an empty key sends no Authorization header")
	}
}

func TestEmbedder_RefusesFailedOrEmptyResponses(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"control: one vector": {http.StatusOK, `{"data":[{"embedding":[1]}]}`},
		"http error":          {http.StatusTooManyRequests, `{"error":"rate limited"}`},
		"no data":             {http.StatusOK, `{"data":[]}`},
		"empty vector":        {http.StatusOK, `{"data":[{"embedding":[]}]}`},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			embedder, err := openai.NewEmbedder(openai.Config{BaseURL: server.URL, Model: "bge"})
			require.NoError(t, err)
			_, err = embedder.Embed(context.Background(), "text")
			if name == "control: one vector" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			var apiErr *openai.APIError
			assert.Equal(t, tc.status != http.StatusOK, errors.As(err, &apiErr))
		})
	}
}
