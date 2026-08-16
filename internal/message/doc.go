// Package message holds the shape of one outbound message: the JSON the
// message_send tool writes into the outbox directory, and the shape a
// reader of that directory decodes (docs/architecture/gateway.md). The
// tool itself is a script with a manifest under tools/message_send/
// (ADR 0017), and delivery is that write — the package carries no sink.
package message
