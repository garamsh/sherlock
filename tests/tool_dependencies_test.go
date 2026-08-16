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
const module = "github.com/garamsh/gagent/"

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
// internal/workspace/shell and internal/workspace/files are in the two
// workspace-coupled sets and neither tool calls them: internal/workspace
// holds the generated stubs and server.go in one package, and server.go
// imports both backends, so importing the wire types links the exec
// backend too. That is #525, and this test is where it is visible;
// fixing it turns those two lines red, which is the point.
func TestToolDependencies_AreTheDeclaredOnes(t *testing.T) {
	for _, tc := range []struct {
		tool string
		want []string
	}{
		{
			tool: "files",
			want: []string{
				"internal/workspace",
				"internal/workspace/files",
				"internal/workspace/shell",
			},
		},
		{
			tool: "shell_exec",
			want: []string{
				"internal/workspace",
				"internal/workspace/files",
				"internal/workspace/shell",
			},
		},
		// A tool that needs no workspace links none of it.
		{tool: "message_send", want: []string{}},
		{tool: "github", want: []string{}},
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
