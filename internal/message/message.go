package message

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
type Message struct {
	AgentID string `json:"agent_id"`
	Channel string `json:"channel"`
	Target  string `json:"target"`
	Content string `json:"content"`
}
