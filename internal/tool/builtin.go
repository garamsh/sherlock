package tool

// BuiltinToolNames returns the canonical names of the platform's four
// built-in tools (ADR 0012): memory, files, message_send, shell_exec.
// The list is sorted alphabetically — same order the registry emits via
// List — so test assertions and downstream consumers can compare it
// directly.
func BuiltinToolNames() []string {
	return []string{"files", "memory", "message_send", "shell_exec"}
}
