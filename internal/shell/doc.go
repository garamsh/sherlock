// Package shell implements the shell_exec tool: a thin tool.Tool adapter
// over an Executor that runs agent-supplied commands in the platform's
// own process, dropping to the agent UID per ADR 0006 and ADR 0012.
package shell
