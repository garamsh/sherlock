// Package memory implements the agent's long-term memory: the store,
// hybrid search, and the write operations behind the writeback block and
// platform maintenance per docs/architecture/memory.md. Memory is a
// layer, never a tool (ADR 0017): the model touches it only through the
// lifecycle.
package memory
