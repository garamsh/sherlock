package openai

// Credential is an API key source the client reads before every request,
// so a key the platform rewrites reaches the next request without a
// restart (ADR 0029).
type Credential interface {
	// Read returns the key to send now.
	Read() (string, error)
	// Source names where the key is read from, for errors. It never
	// carries the key.
	Source() string
}
