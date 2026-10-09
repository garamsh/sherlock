package gateway

import (
	"errors"
	"time"
)

// ErrDraining is returned when a message is enqueued once the queue has
// begun to drain: the process is shutting down and admits no new work.
var ErrDraining = errors.New("gateway: draining")

// ErrIDReused is returned when a sender's message ID is already queued,
// running or committed with different content.
var ErrIDReused = errors.New("gateway: id reused with different content")

// Status is where a sender's message stands, as this process can tell.
type Status int

const (
	// StatusUnknown: this process has not seen the message, or lost it
	// when it restarted, or its turn ended without committing it.
	StatusUnknown Status = iota
	// StatusPending: the message is queued or running in this process.
	StatusPending
	// StatusCompleted: the message's turn committed its sources, holding
	// this ID and this content, to memory.
	StatusCompleted
)

// Instance is what this process reports about itself on its status
// route, each value taken from what it loaded once startup succeeded. An
// empty AssignmentEpoch, Generation or ConfigRevision means unknown.
type Instance struct {
	// AssignmentEpoch is the launcher's opaque epoch for this activation.
	AssignmentEpoch string
	// Generation is this start's runtime nonce (ADR 0016).
	Generation string
	// ConfigRevision echoes the config file's revision key, never a
	// value computed from the configuration.
	ConfigRevision string
	// StartedAt is the process start time. It is not, by itself,
	// evidence that a revision was applied.
	StartedAt time.Time
}

// Inbound is one accepted message: it carries its sender's ID or one the
// gateway assigned, and waits for (or is in) the agent's serialized run.
// An ID is unique within its origin's scope.
type Inbound struct {
	ID       string
	Origin   Origin
	Content  string
	QueuedAt time.Time
}

// Origin is who a message is from and who posted it (ADR 0026). An
// unsigned message is from the transport principal that posted it; a
// signed one is from the issuer and subject its signature verified, and
// records the transport principal that posted it beside them. The two
// kinds never share a scope, because a signed origin's issuer is never
// empty.
type Origin struct {
	issuer, sender, poster string
}

// TransportOrigin is the origin of an unsigned message: the verified mTLS
// caller identity, or empty over plain-HTTP loopback.
func TransportOrigin(principal string) Origin {
	return Origin{sender: principal, poster: principal}
}

// signedOrigin is the origin of a message whose signature verified under
// issuer, which is never empty, for subject.
func signedOrigin(issuer, subject, poster string) Origin {
	return Origin{issuer: issuer, sender: subject, poster: poster}
}

// Signed reports whether the origin is a verified signature's.
func (o Origin) Signed() bool { return o.issuer != "" }

// Issuer is the verified iss, or empty for an unsigned message.
func (o Origin) Issuer() string { return o.issuer }

// Sender is the verified sub of a signed message, or the transport
// principal of an unsigned one.
func (o Origin) Sender() string { return o.sender }

// Poster is the transport principal that posted the message.
func (o Origin) Poster() string { return o.poster }

// Notice reports whether the message is its issuer's own: a signed
// message whose subject is its issuer has no sender to reply to.
func (o Origin) Notice() bool { return o.Signed() && o.sender == o.issuer }
