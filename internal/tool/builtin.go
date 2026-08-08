package tool

// BuiltinToolNames returns the canonical names of the sidecar's built-in
// tools (ADR 0018): files and shell_exec. The list is sorted
// alphabetically — same order the registry emits via List — so test
// assertions and downstream consumers can compare it directly.
func BuiltinToolNames() []string {
	return []string{"files", "shell_exec"}
}
