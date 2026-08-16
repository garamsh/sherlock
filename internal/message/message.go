package message

// Message is one outbound message: which channel, which target, what
// content. The sender, another person, and another agent are all the same
// kind of target (docs/architecture/gateway.md).
type Message struct {
	Channel string `json:"channel"`
	Target  string `json:"target"`
	Content string `json:"content"`
}
