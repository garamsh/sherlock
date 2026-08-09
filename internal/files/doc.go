// Package files is the workspace's filesystem backend (ADR 0018). The
// gRPC workspace server (internal/workspace) wraps FS to expose
// Read, Write, and List to the agent's tools. Paths are confined to
// the workspace root; per ADR 0005 this confinement is convenience,
// not a trust boundary.
package files

