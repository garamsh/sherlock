// Package script implements the universal subprocess adapter: a tool.Tool
// declared by a Spec that runs any executable with JSON input on stdin and
// returns JSON output on stdout. Execution goes through the shell
// executor, which drops the child to the agent UID (ADR 0010).
package script
