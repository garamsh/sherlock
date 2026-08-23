// Package memory implements the agent session's context hooks against
// the memory service (ADR 0006). It sits under the package declaring
// those interfaces, so the session's dependencies are constructed from
// one place and internal/agent itself keeps no dependency on the store.
//
// All three hooks the session declares are here: the writeback, which
// stores what a turn stated; the compactor, which now only drops the
// oldest messages because an addressed store has nowhere to put a
// summary of them (ADR 0018); and Recall, the read step that resolves a
// turn's task into an address before the store is read (ADR 0019),
// because asked in words an addressed store answers nothing.
package memory
