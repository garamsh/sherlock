// Package shell implements the tool execution boundary: an Executor that
// runs tool child processes as sub-processes dropped to the agent UID
// per ADR 0006 and ADR 0012, plus the thin shell_exec tool.Tool adapter
// over it. shell_exec children run in a fresh, empty network namespace;
// script-tool children keep network access (ADR 0014).
package shell
