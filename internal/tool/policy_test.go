package tool_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/tool"
)

func TestPolicy_AllowListPermits(t *testing.T) {
	p := tool.AllowList("echo", "files")
	assert.True(t, p.Permitted("echo"))
	assert.True(t, p.Permitted("files"))
	assert.False(t, p.Permitted("other"))
}

func TestPolicy_AllowListEmptyDenies(t *testing.T) {
	p := tool.AllowList()
	assert.False(t, p.Permitted("echo"))
}

func TestPolicy_AllowAllPermits(t *testing.T) {
	p := tool.AllowAll()
	assert.True(t, p.Permitted("echo"))
}

func TestPolicy_EnforceReturnsTypedDenial(t *testing.T) {
	p := tool.AllowList("echo")
	err := p.Enforce("ghost")
	require.Error(t, err)
	assert.True(t, errors.Is(err, tool.ErrPolicyDenied))
	assert.Contains(t, err.Error(), "ghost")
}

func TestPolicy_EnforceAllows(t *testing.T) {
	p := tool.AllowList("echo")
	assert.NoError(t, p.Enforce("echo"))
}

// TestClient_PolicyEnforcedAtCallBoundary pins the boundary: the
// policy check happens on every call, not at startup. A tool that
// passes the policy at startup but loses the policy post-startup
// (e.g. the operator updates the config) still fails the call.
func TestClient_PolicyEnforcedAtCallBoundary(t *testing.T) {
	client := newClientWithTools(t, tool.Options{Policy: tool.AllowList("echo")}, echoConfig("echo"))

	// First call passes.
	_, err := client.Call(context.Background(), "echo", []byte(`{"text":"hi"}`))
	require.NoError(t, err)
}
