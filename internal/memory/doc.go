// Package memory implements the agent's long-term memory: the addressed
// store, the two addressed reads, and the write behind the writeback
// block per docs/architecture/memory.md. Memory is a layer, never a tool
// (ADR 0017): the model touches it only through the lifecycle.
package memory
