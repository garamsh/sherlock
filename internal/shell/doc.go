// Package shell is the agent's process-execution boundary (ADR 0018):
// an Executor that runs tool child processes as sub-processes dropped
// to the agent UID per ADR 0008 and ADR 0010. Tools (and the agent's
// own internal code paths) import this package and call Executor.Run;
// the model never sees "shell_exec" as a tool — process exec is the
// agent's native capability.
package shell
