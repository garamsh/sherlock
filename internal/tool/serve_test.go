package tool_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_MalformedBody(t *testing.T) {
	stub := &stubTool{name: "exec", inSchema: json.RawMessage(objectSchema)}
	_, srv := newWireServer(t, stub)

	resp, err := http.Post(srv.URL+"/tools/exec/call", "application/json",
		strings.NewReader(`{"command":`))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var body struct {
		Kind string `json:"kind"`
		Tool string `json:"tool"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "bad_request", body.Kind, "malformed JSON yields a typed wire error, not a panic")
	assert.Equal(t, "exec", body.Tool)
	assert.False(t, stub.called, "the tool body must not run on a malformed body")
}

func TestHandler_UnknownToolRespondsNotFound(t *testing.T) {
	_, srv := newWireServer(t)

	resp, err := http.Post(srv.URL+"/tools/missing/call", "application/json",
		strings.NewReader(`{}`))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	var body struct {
		Kind string `json:"kind"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "not_found", body.Kind)
}
