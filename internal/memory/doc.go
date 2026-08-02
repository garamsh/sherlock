// Package memory implements one environment's long-term memory: the
// store, hybrid search, and the write operations behind the writeback
// block and platform maintenance per docs/architecture/memory.md. The
// memory tool exposes only the read-only search op (ADR 0015).
package memory
