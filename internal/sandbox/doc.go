// Package sandbox runs agent-supplied commands inside the environment's
// own container. The container mounts only the environment workspace,
// has no network egress, and is bounded by an externally injected
// resource policy; memory.db is never mounted.
package sandbox
