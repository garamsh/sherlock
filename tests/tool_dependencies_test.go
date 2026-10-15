package tests

import (
	"os/exec"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// module is this repository's import path prefix.
const module = "github.com/garamsh/sherlock/"

// TestToolDependencies_AreTheDeclaredOnes pins how a tool reaches the
// workspace. A tool that needs files or exec dials the workspace's gRPC
// listener itself and holds the client (ADR 0017): there is no wrapper
// package between a tool and the wire, so the tool imports
// internal/workspace for the generated stubs and the request types.
// Nothing noticed when the documents said otherwise, which is #505.
//
// The sets are exact rather than "contains", so any change to what a
// tool links shows up as a failing test naming the package that moved,
// in either direction.
//
// internal/workspace holds the wire and nothing else: the backends and
// the server that wraps them live in internal/workspace/files,
// internal/workspace/shell and internal/workspace/server, which no tool
// imports — so importing the wire types links no exec backend. That is
// #525 fixed, and these sets are what keeps it fixed: a backend
// reaching a tool's dependency set turns the set red.
func TestToolDependencies_AreTheDeclaredOnes(t *testing.T) {
	for _, tc := range []struct {
		tool string
		want []string
	}{
		{tool: "file_read", want: []string{"internal/workspace"}},
		{tool: "file_write", want: []string{"internal/workspace"}},
		{tool: "file_list", want: []string{"internal/workspace"}},
		{tool: "shell_exec", want: []string{"internal/workspace"}},
		// message_send links internal/message and nothing else: that
		// package is the delivered record's one declaration, which the
		// tool marshals and the outbox reader decodes (#561). It is a
		// leaf of type declarations, so importing it drags in no
		// backend — the property #525 established for
		// internal/workspace.
		{tool: "message_send", want: []string{"internal/message"}},
		// A tool that needs nothing of the platform links nothing.
		{tool: "github_push", want: []string{}},
		{tool: "github_open_issue", want: []string{}},
		{tool: "github_open_pull_request", want: []string{}},
		{tool: "github_comment", want: []string{}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			assert.Equal(t, tc.want, inModuleDeps(t, module+"tools/"+tc.tool))
		})
	}
}

// inModuleDeps returns pkg's transitive dependencies inside this module,
// sorted, with the module prefix and the package itself removed.
func inModuleDeps(t *testing.T, pkg string) []string {
	t.Helper()

	out, err := exec.Command("go", "list", "-deps", pkg).Output()
	require.NoError(t, err, "go list -deps %s", pkg)

	deps := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if !strings.HasPrefix(line, module) {
			continue
		}
		if rel := strings.TrimPrefix(line, module); !strings.HasPrefix(rel, "tools/") {
			deps = append(deps, rel)
		}
	}
	sort.Strings(deps)
	return deps
}
