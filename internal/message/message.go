package message

import "time"

// Message is one delivered outbound message: who sent it, which
// channel, which target, what content. The sender, another person, and
// another agent are all the same kind of target
// (docs/architecture/gateway.md).
//
// AgentID is the sending agent's identity under the name every other
// boundary uses (ADR 0016). It is the platform's fact rather than the
// model's claim: message_send reads it from its environment and the
// manifest's input_schema does not offer the model a field for it, so
// a delivered file says who sent it and a model cannot say otherwise.
// Without it a reader of an outbox holding two agents' files has only
// the path to go on, which stops working the moment they share a mount.
//
// ID, CreatedAt and InReplyTo are the platform's as well. ID names the
// outbox entry: assigned once when message_send writes it, unique per
// entry, and the entry's file name is ID plus ".json". ID is opaque; a
// reader compares it whole and never parses it. CreatedAt is the agent
// host's wall-clock instant the entry was minted at, in UTC, and encodes
// as RFC 3339 with nanoseconds; it is zero, meaning unknown, on an entry
// written before the field existed. InReplyTo holds the IDs of the
// inbound messages the turn that sent it drained.
type Message struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	AgentID   string    `json:"agent_id"`
	// AssignmentEpoch and Generation record the entry's origin: the
	// launcher's assignment epoch and the runtime start that wrote it
	// (ADR 0016). They are provenance, not a credential, and empty when
	// the process was started without them.
	AssignmentEpoch string   `json:"assignment_epoch"`
	Generation      string   `json:"generation"`
	InReplyTo       []string `json:"in_reply_to"`
	Channel         string   `json:"channel"`
	Target          string   `json:"target"`
	Content         string   `json:"content"`
}
