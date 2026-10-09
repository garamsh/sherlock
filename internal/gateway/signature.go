package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// The JWS a signed message carries (ADR 0026 §2, ADR-0101 §The format).
const (
	signatureType = "agent-message+jwt"
	// skewAllowance is the whole clock skew a signature's validity allows.
	skewAllowance = 60 * time.Second
)

// The check a refused signature failed, named in the refusal (ADR 0026 §4).
const (
	checkMalformed = "malformed"
	checkIssuer    = "issuer"
	checkHeader    = "header"
	checkKey       = "key"
	checkSignature = "signature"
	checkAudience  = "audience"
	checkTime      = "time"
	checkContent   = "content"
	checkMessageID = "message id"
)

// ErrSignatureRequired refuses an unsigned message when signatures are
// required.
var ErrSignatureRequired = errors.New("signature required")

// ErrIssuerUnavailable refuses a message from a configured issuer that has
// no usable key set: never read, or past its grace.
var ErrIssuerUnavailable = errors.New("issuer keys unavailable")

// Refusal is a signature that failed one of the checks. Issuer and KeyID
// are the values the token names, verified or not, for the log.
type Refusal struct {
	Check  string
	Issuer string
	KeyID  string
}

func (r *Refusal) Error() string { return "signature rejected: " + r.Check }

// Verifier decides who a posted message is from (ADR 0026). A message
// without a signature is from its transport principal, unless signatures
// are required. A message with one is from the issuer and subject the
// signature verifies, after ADR-0101's checks in its order, with the
// agent's own ID as the audience. It runs once, at ingress.
type Verifier struct {
	audience string
	required bool
	sets     map[string]*KeySet
	now      func() time.Time
}

// NewVerifier returns a verifier for the agent audience, trusting the
// issuers of sets. required refuses an unsigned message.
func NewVerifier(audience string, sets []*KeySet, required bool, now func() time.Time) *Verifier {
	byIssuer := make(map[string]*KeySet, len(sets))
	for _, set := range sets {
		byIssuer[set.Issuer()] = set
	}
	return &Verifier{audience: audience, required: required, sets: byIssuer, now: now}
}

// Origin returns who the message poster posted under id with content is
// from. signature is nil when the body carried none. It returns
// ErrSignatureRequired, ErrIssuerUnavailable, or a *Refusal.
func (v *Verifier) Origin(ctx context.Context, poster string, signature *string, id, content string) (Origin, error) {
	if signature == nil {
		if v.required {
			return Origin{}, ErrSignatureRequired
		}
		return TransportOrigin(poster), nil
	}
	issuer, subject, err := v.verify(ctx, *signature, id, content)
	if err != nil {
		return Origin{}, err
	}
	return signedOrigin(issuer, subject, poster), nil
}

// verify performs the checks in ADR-0101's order and returns the verified
// iss and sub. Only iss is read before the signature verifies, and only
// to choose a key set.
func (v *Verifier) verify(ctx context.Context, token, id, content string) (issuer, subject string, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", "", &Refusal{Check: checkMalformed}
	}
	unverified, err := decodeSegment(parts[1])
	if err != nil {
		return "", "", &Refusal{Check: checkMalformed}
	}
	iss, ok := stringClaim(unverified, "iss")
	set := v.sets[iss]
	if !ok || set == nil {
		return "", "", &Refusal{Check: checkIssuer, Issuer: iss}
	}
	header, err := decodeSegment(parts[0])
	if err != nil {
		return "", "", &Refusal{Check: checkMalformed, Issuer: iss}
	}
	kid, err := checkProtectedHeader(header)
	if err != nil {
		return "", "", &Refusal{Check: checkHeader, Issuer: iss, KeyID: kid}
	}
	key, err := set.key(ctx, kid)
	if err != nil {
		return "", "", err
	}
	jws, err := jose.ParseSignedCompact(token, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		return "", "", &Refusal{Check: checkMalformed, Issuer: iss, KeyID: kid}
	}
	payload, err := jws.Verify(key)
	if err != nil {
		return "", "", &Refusal{Check: checkSignature, Issuer: iss, KeyID: kid}
	}
	refuse := func(check string) (string, string, error) {
		return "", "", &Refusal{Check: check, Issuer: iss, KeyID: kid}
	}
	var claims map[string]json.RawMessage
	if err := json.Unmarshal(payload, &claims); err != nil {
		return refuse(checkMalformed)
	}
	sub, ok := stringClaim(claims, "sub")
	if !ok || sub == "" {
		return refuse(checkMalformed)
	}
	if aud, ok := stringClaim(claims, "aud"); !ok || aud != v.audience {
		return refuse(checkAudience)
	}
	if !v.current(claims) {
		return refuse(checkTime)
	}
	digest := sha256.Sum256([]byte(content))
	if sum, ok := stringClaim(claims, "content_sha256"); !ok || sum != base64.RawURLEncoding.EncodeToString(digest[:]) {
		return refuse(checkContent)
	}
	if messageID, ok := stringClaim(claims, "message_id"); !ok || id == "" || messageID != id {
		return refuse(checkMessageID)
	}
	return iss, sub, nil
}

// current reports whether exp is later than now minus the skew allowance
// and iat earlier than now plus it.
func (v *Verifier) current(claims map[string]json.RawMessage) bool {
	exp, okExp := numberClaim(claims, "exp")
	iat, okIat := numberClaim(claims, "iat")
	if !okExp || !okIat {
		return false
	}
	// NumericDates are seconds that may carry a fraction; comparing them
	// as seconds keeps an out-of-range value from overflowing a time.
	now := float64(v.now().UnixNano()) / float64(time.Second)
	skew := skewAllowance.Seconds()
	return exp > now-skew && iat < now+skew
}

// checkProtectedHeader requires exactly alg ES256, typ agent-message+jwt
// and a kid, and returns the kid.
func checkProtectedHeader(header map[string]json.RawMessage) (string, error) {
	kid, _ := stringClaim(header, "kid")
	alg, _ := stringClaim(header, "alg")
	typ, _ := stringClaim(header, "typ")
	if len(header) != 3 || kid == "" || alg != string(jose.ES256) || typ != signatureType {
		return kid, errors.New("header")
	}
	return kid, nil
}

func decodeSegment(segment string) (map[string]json.RawMessage, error) {
	raw, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return nil, err
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		return nil, err
	}
	return members, nil
}

func stringClaim(members map[string]json.RawMessage, name string) (string, bool) {
	raw, ok := members[name]
	if !ok {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func numberClaim(members map[string]json.RawMessage, name string) (float64, bool) {
	raw, ok := members[name]
	if !ok {
		return 0, false
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, false
	}
	return value, true
}
