// Package shell is the workspace's process-execution boundary (ADR
// 0018): an Executor that runs child processes dropped to the exec UID
// per ADR 0008 and ADR 0010. The workspace gRPC server owns the
// Executor and serves it through the Exec RPC; the agent executes
// nothing itself — its shell_exec tool calls the workspace over the
// gRPC client.
package shell
