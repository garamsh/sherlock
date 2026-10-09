package gateway_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/gateway"
)

const (
	testIssuer   = "https://issuer.example"
	testAudience = "env-1"
	testSubject  = "grn:test:org:agent:sender"
)

// testClock is the verifier's and key sets' clock, moved by the test.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{now: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// signingKey is a P-256 key generated for the test, under a kid.
type signingKey struct {
	kid  string
	priv *ecdsa.PrivateKey
}

func newSigningKey(t *testing.T, kid string) signingKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return signingKey{kid: kid, priv: priv}
}

func (k signingKey) public() jose.JSONWebKey {
	return jose.JSONWebKey{Key: &k.priv.PublicKey, KeyID: k.kid, Algorithm: string(jose.ES256), Use: "sig"}
}

// writeKeySet writes a JWK Set document holding keys' public halves.
func writeKeySet(t *testing.T, path string, keys ...signingKey) {
	t.Helper()
	set := jose.JSONWebKeySet{}
	for _, k := range keys {
		set.Keys = append(set.Keys, k.public())
	}
	body, err := json.Marshal(set)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, body, 0o600))
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// claimsFor is a valid claims set for a message with id and content,
// issued now.
func claimsFor(id, content string, now time.Time) map[string]any {
	digest := sha256.Sum256([]byte(content))
	return map[string]any{
		"iss":            testIssuer,
		"sub":            testSubject,
		"aud":            testAudience,
		"org":            "org",
		"message_id":     id,
		"content_sha256": b64(digest[:]),
		"iat":            now.Unix(),
		"exp":            now.Add(5 * time.Minute).Unix(),
	}
}

func headerFor(k signingKey) map[string]any {
	return map[string]any{"alg": "ES256", "typ": "agent-message+jwt", "kid": k.kid}
}

// signToken signs claims under header with key, as a compact JWS.
func signToken(t *testing.T, key *ecdsa.PrivateKey, header, claims map[string]any) string {
	t.Helper()
	h, err := json.Marshal(header)
	require.NoError(t, err)
	p, err := json.Marshal(claims)
	require.NoError(t, err)
	input := b64(h) + "." + b64(p)
	digest := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	require.NoError(t, err)
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return input + "." + b64(sig)
}

// signed signs a valid token for id and content with k at the clock's now.
func signed(t *testing.T, k signingKey, clock *testClock, id, content string) string {
	t.Helper()
	return signToken(t, k.priv, headerFor(k), claimsFor(id, content, clock.Now()))
}

// body is an ingress body; an empty id or token leaves the member out.
func body(t *testing.T, id, content, token string) string {
	t.Helper()
	members := map[string]any{"content": content}
	if id != "" {
		members["id"] = id
	}
	if token != "" {
		members["signature"] = token
	}
	b, err := json.Marshal(members)
	require.NoError(t, err)
	return string(b)
}

// fileVerifier trusts testIssuer through a keys-file holding keys, loaded.
func fileVerifier(t *testing.T, clock *testClock, required bool, keys ...signingKey) (*gateway.Verifier, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "keys.json")
	writeKeySet(t, path, keys...)
	set := gateway.NewFileKeySet(testIssuer, path, clock.Now, newTestLogger())
	require.NoError(t, set.Load(context.Background()))
	return gateway.NewVerifier(testAudience, []*gateway.KeySet{set}, required, clock.Now), path
}

func serve(t *testing.T, q *gateway.Queue, v *gateway.Verifier, logger *slog.Logger) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(gateway.NewHandler(q, v, gateway.Instance{}, logger))
	t.Cleanup(srv.Close)
	return srv
}

// TestPostMessage_RefusesEachFailedCheck refuses a message for each check
// that fails, naming the check, and runs nothing. The control is the same
// server accepting the unmodified token first, so each refusal answers to
// the one thing its case changed.
func TestPostMessage_RefusesEachFailedCheck(t *testing.T) {
	clock := newTestClock()
	key := newSigningKey(t, "k1")
	stranger := newSigningKey(t, "k1")
	v, _ := fileVerifier(t, clock, false, key)
	session := &fakeSession{}
	srv := serve(t, newTestQueue(t, session), v, newTestLogger())

	code, reply := sendMessage(t, http.DefaultClient, srv.URL, body(t, "m0", "hello", signed(t, key, clock, "m0", "hello")))
	require.Equal(t, http.StatusAccepted, code, "control: the unmodified token is accepted: %s", reply.Error)
	require.Eventually(t, func() bool { return len(session.startedTasks()) == 1 }, 5*time.Second, time.Millisecond)

	now := clock.Now()
	withClaims := func(edit func(map[string]any)) string {
		claims := claimsFor("m1", "hello", now)
		edit(claims)
		return signToken(t, key.priv, headerFor(key), claims)
	}
	withHeader := func(edit func(map[string]any)) string {
		header := headerFor(key)
		edit(header)
		return signToken(t, key.priv, header, claimsFor("m1", "hello", now))
	}
	cases := []struct {
		name  string
		body  string
		check string
	}{
		{"not a compact JWS", body(t, "m1", "hello", "not-a-jws"), "malformed"},
		{"payload not JSON", body(t, "m1", "hello", "e30."+b64([]byte("x"))+".e30"), "malformed"},
		{"untrusted issuer", body(t, "m1", "hello", withClaims(func(c map[string]any) { c["iss"] = testIssuer + "/" })), "issuer"},
		{"issuer is checked before the header", body(t, "m1", "hello", signToken(t, key.priv,
			map[string]any{"alg": "ES256", "typ": "JWT", "kid": "k1"},
			func() map[string]any { c := claimsFor("m1", "hello", now); c["iss"] = "https://other"; return c }())), "issuer"},
		{"wrong typ", body(t, "m1", "hello", withHeader(func(h map[string]any) { h["typ"] = "JWT" })), "header"},
		{"wrong alg", body(t, "m1", "hello", withHeader(func(h map[string]any) { h["alg"] = "ES384" })), "header"},
		{"extra header member", body(t, "m1", "hello", withHeader(func(h map[string]any) { h["jku"] = "https://evil.example" })), "header"},
		{"unknown kid", body(t, "m1", "hello", withHeader(func(h map[string]any) { h["kid"] = "k9" })), "key"},
		{"another key under the kid", body(t, "m1", "hello", signToken(t, stranger.priv, headerFor(key), claimsFor("m1", "hello", now))), "signature"},
		{"another audience", body(t, "m1", "hello", withClaims(func(c map[string]any) { c["aud"] = "env-2" })), "audience"},
		{"audience as a list", body(t, "m1", "hello", withClaims(func(c map[string]any) { c["aud"] = []string{testAudience} })), "audience"},
		{"audience is checked before time", body(t, "m1", "hello", withClaims(func(c map[string]any) {
			c["aud"] = "env-2"
			c["exp"] = now.Add(-time.Hour).Unix()
		})), "audience"},
		{"expired past the allowance", body(t, "m1", "hello", withClaims(func(c map[string]any) { c["exp"] = now.Add(-61 * time.Second).Unix() })), "time"},
		{"issued past the allowance", body(t, "m1", "hello", withClaims(func(c map[string]any) { c["iat"] = now.Add(61 * time.Second).Unix() })), "time"},
		{"no exp", body(t, "m1", "hello", withClaims(func(c map[string]any) { delete(c, "exp") })), "time"},
		{"other content", body(t, "m1", "hello!", withClaims(func(map[string]any) {})), "content"},
		{"digest of the stored, redacted form", body(t, "m1", "api_key=sk-abcdefghijklmnop", withClaims(func(c map[string]any) {
			digest := sha256.Sum256([]byte("api_key=[REDACTED]"))
			c["content_sha256"] = b64(digest[:])
		})), "content"},
		{"other message id", body(t, "m2", "hello", withClaims(func(map[string]any) {})), "message id"},
		{"no id in the body", body(t, "", "hello", withClaims(func(map[string]any) {})), "message id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, reply := sendMessage(t, http.DefaultClient, srv.URL, tc.body)
			assert.Equal(t, http.StatusForbidden, code)
			assert.Equal(t, "signature rejected: "+tc.check, reply.Error)
		})
	}
	assert.Len(t, session.startedTasks(), 1, "no refused message ran")
}

// TestPostMessage_AcceptsWithinTheSkewAllowance holds the time check to
// its allowance: a token expired 59 seconds ago, or issued 59 seconds
// ahead, is accepted.
func TestPostMessage_AcceptsWithinTheSkewAllowance(t *testing.T) {
	clock := newTestClock()
	key := newSigningKey(t, "k1")
	v, _ := fileVerifier(t, clock, false, key)
	srv := serve(t, newTestQueue(t, &fakeSession{}), v, newTestLogger())
	now := clock.Now()

	expired := claimsFor("m1", "hello", now)
	expired["exp"] = now.Add(-59 * time.Second).Unix()
	code, reply := sendMessage(t, http.DefaultClient, srv.URL, body(t, "m1", "hello", signToken(t, key.priv, headerFor(key), expired)))
	assert.Equal(t, http.StatusAccepted, code, reply.Error)

	ahead := claimsFor("m2", "hello", now)
	ahead["iat"] = now.Add(59 * time.Second).Unix()
	code, reply = sendMessage(t, http.DefaultClient, srv.URL, body(t, "m2", "hello", signToken(t, key.priv, headerFor(key), ahead)))
	assert.Equal(t, http.StatusAccepted, code, reply.Error)
}

// TestPostMessage_DigestIsOverContentAsReceived accepts a message carrying
// a credential, whose stored form is redacted: the digest is the one over
// the bytes that arrived.
func TestPostMessage_DigestIsOverContentAsReceived(t *testing.T) {
	clock := newTestClock()
	key := newSigningKey(t, "k1")
	v, _ := fileVerifier(t, clock, false, key)
	model := newHeldModel()
	q := newMemoryQueue(t, filepath.Join(t.TempDir(), "memory.db"), model)
	srv := serve(t, q, v, newTestLogger())
	content := "api_key=sk-abcdefghijklmnop and favorite tea is cedar"

	code, reply := sendMessage(t, http.DefaultClient, srv.URL, body(t, "m1", content, signed(t, key, clock, "m1", content)))
	require.Equal(t, http.StatusAccepted, code, reply.Error)
	model.turn(t, nil)
	awaitStatus(t, http.DefaultClient, srv.URL, "m1", "completed")

	code, reply = sendMessage(t, http.DefaultClient, srv.URL, body(t, "m1", content, signed(t, key, clock, "m1", content)))
	assert.Equal(t, http.StatusOK, code, "a redelivery after the store redacted it still verifies: %s", reply.Error)
}

// TestPostMessage_SignatureMustBeAString answers a signature member of
// another JSON type as an unreadable body.
func TestPostMessage_SignatureMustBeAString(t *testing.T) {
	v, _ := fileVerifier(t, newTestClock(), false, newSigningKey(t, "k1"))
	srv := serve(t, newTestQueue(t, &fakeSession{}), v, newTestLogger())

	code, reply := sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m1","content":"hello","signature":42}`)

	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, reply.Error, "invalid message body")
}

// TestPostMessage_RequireSignature refuses an unsigned message when
// signatures are required, and accepts a signed one. The control is the
// same message on a verifier that does not require them, which accepts it
// unchanged under its transport principal.
func TestPostMessage_RequireSignature(t *testing.T) {
	clock := newTestClock()
	key := newSigningKey(t, "k1")

	optional, _ := fileVerifier(t, clock, false, key)
	srv := serve(t, newTestQueue(t, &fakeSession{}), optional, newTestLogger())
	code, reply := sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m1","content":"hello"}`)
	require.Equal(t, http.StatusAccepted, code, "control: an unsigned message is accepted when none is required: %s", reply.Error)

	required, _ := fileVerifier(t, clock, true, key)
	srv = serve(t, newTestQueue(t, &fakeSession{}), required, newTestLogger())
	code, reply = sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m1","content":"hello"}`)
	assert.Equal(t, http.StatusForbidden, code)
	assert.Equal(t, "signature required", reply.Error)

	code, reply = sendMessage(t, http.DefaultClient, srv.URL, body(t, "m1", "hello", signed(t, key, clock, "m1", "hello")))
	assert.Equal(t, http.StatusAccepted, code, reply.Error)
}

// TestPostMessage_UnavailableIssuer answers 503 while a trusted issuer has
// no key set, and accepts once a fetch a minute later succeeds. It also
// carries a keys-url fetch end to end over TLS.
func TestPostMessage_UnavailableIssuer(t *testing.T) {
	clock := newTestClock()
	key := newSigningKey(t, "k1")
	var serving sync.Mutex
	up := false
	keys := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serving.Lock()
		defer serving.Unlock()
		if !up {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Cache-Control", "max-age=3600")
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{key.public()}})
	}))
	defer keys.Close()
	set := gateway.NewURLKeySet(testIssuer, keys.URL+"/keys", keys.Client().Transport, clock.Now, newTestLogger())
	require.Error(t, set.Load(context.Background()), "setup: the first fetch fails")
	v := gateway.NewVerifier(testAudience, []*gateway.KeySet{set}, false, clock.Now)
	srv := serve(t, newTestQueue(t, &fakeSession{}), v, newTestLogger())

	code, reply := sendMessage(t, http.DefaultClient, srv.URL, body(t, "m1", "hello", signed(t, key, clock, "m1", "hello")))
	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.Equal(t, "issuer keys unavailable", reply.Error)

	serving.Lock()
	up = true
	serving.Unlock()
	code, _ = sendMessage(t, http.DefaultClient, srv.URL, body(t, "m1", "hello", signed(t, key, clock, "m1", "hello")))
	assert.Equal(t, http.StatusServiceUnavailable, code, "a read less than a minute after the last is not made")

	clock.Advance(time.Minute)
	code, reply = sendMessage(t, http.DefaultClient, srv.URL, body(t, "m1", "hello", signed(t, key, clock, "m1", "hello")))
	assert.Equal(t, http.StatusAccepted, code, reply.Error)
}

// TestKeySet_UnknownKidReadsTheSetAgainAtMostOnceAMinute is rotation: a
// key added to the file is taken up by the first message naming it once
// the last read is a minute old, and not before.
func TestKeySet_UnknownKidReadsTheSetAgainAtMostOnceAMinute(t *testing.T) {
	clock := newTestClock()
	current := newSigningKey(t, "k1")
	next := newSigningKey(t, "k2")
	v, path := fileVerifier(t, clock, false, current)
	srv := serve(t, newTestQueue(t, &fakeSession{}), v, newTestLogger())

	writeKeySet(t, path, current, next)
	clock.Advance(30 * time.Second)
	code, reply := sendMessage(t, http.DefaultClient, srv.URL, body(t, "m1", "hello", signed(t, next, clock, "m1", "hello")))
	assert.Equal(t, http.StatusForbidden, code)
	assert.Equal(t, "signature rejected: key", reply.Error, "the set was read under a minute ago")

	clock.Advance(30 * time.Second)
	code, reply = sendMessage(t, http.DefaultClient, srv.URL, body(t, "m1", "hello", signed(t, next, clock, "m1", "hello")))
	assert.Equal(t, http.StatusAccepted, code, "the new kid is read a minute after the last read: %s", reply.Error)

	writeKeySet(t, path, next)
	clock.Advance(time.Hour)
	code, reply = sendMessage(t, http.DefaultClient, srv.URL, body(t, "m2", "hello", signed(t, current, clock, "m2", "hello")))
	assert.Equal(t, http.StatusForbidden, code)
	assert.Equal(t, "signature rejected: key", reply.Error, "a key gone from the file leaves at the read after the set's lifetime")
}

// TestKeySet_FileGraceIs24HoursPastTheLifetime keeps a held set usable
// while its file cannot be read, for 24 hours past its one-hour lifetime,
// and then refuses the issuer as unavailable.
func TestKeySet_FileGraceIs24HoursPastTheLifetime(t *testing.T) {
	clock := newTestClock()
	key := newSigningKey(t, "k1")
	v, path := fileVerifier(t, clock, false, key)
	srv := serve(t, newTestQueue(t, &fakeSession{}), v, newTestLogger())
	require.NoError(t, os.Remove(path))

	clock.Advance(time.Hour + 24*time.Hour)
	code, reply := sendMessage(t, http.DefaultClient, srv.URL, body(t, "m1", "hello", signed(t, key, clock, "m1", "hello")))
	assert.Equal(t, http.StatusAccepted, code, "inside the grace the held set verifies: %s", reply.Error)

	clock.Advance(time.Second)
	code, reply = sendMessage(t, http.DefaultClient, srv.URL, body(t, "m2", "hello", signed(t, key, clock, "m2", "hello")))
	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.Equal(t, "issuer keys unavailable", reply.Error)

	writeKeySet(t, path, key)
	clock.Advance(time.Minute)
	code, reply = sendMessage(t, http.DefaultClient, srv.URL, body(t, "m2", "hello", signed(t, key, clock, "m2", "hello")))
	assert.Equal(t, http.StatusAccepted, code, "a successful read ends the unavailability: %s", reply.Error)
}

// TestPostMessage_SignedRedeliveryIsAnsweredWithinItsPair answers a signed
// message's redelivery from the existing record: queued, then completed,
// and 409 for other content under the same pair. The same ID unsigned is
// another scope's and is a new message.
func TestPostMessage_SignedRedeliveryIsAnsweredWithinItsPair(t *testing.T) {
	clock := newTestClock()
	key := newSigningKey(t, "k1")
	v, _ := fileVerifier(t, clock, false, key)
	model := newHeldModel()
	srv := serve(t, newMemoryQueue(t, filepath.Join(t.TempDir(), "memory.db"), model), v, newTestLogger())

	code, reply := sendMessage(t, http.DefaultClient, srv.URL, body(t, "m1", "hello", signed(t, key, clock, "m1", "hello")))
	require.Equal(t, http.StatusAccepted, code, reply.Error)
	awaitStarted(t, model, 1)
	code, reply = sendMessage(t, http.DefaultClient, srv.URL, body(t, "m1", "hello", signed(t, key, clock, "m1", "hello")))
	assert.Equal(t, http.StatusAccepted, code)
	assert.Equal(t, "queued", reply.Status, "a redelivery while running is the queued message")

	model.turn(t, nil)
	awaitStatus(t, http.DefaultClient, srv.URL, "m1", "completed")
	code, reply = sendMessage(t, http.DefaultClient, srv.URL, body(t, "m1", "hello", signed(t, key, clock, "m1", "hello")))
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "completed", reply.Status)
	code, reply = sendMessage(t, http.DefaultClient, srv.URL, body(t, "m1", "other", signed(t, key, clock, "m1", "other")))
	assert.Equal(t, http.StatusConflict, code, "other content under the pair's ID: %s", reply.Error)

	code, reply = sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m1","content":"other"}`)
	assert.Equal(t, http.StatusAccepted, code, "the transport principal's m1 is another scope's: %s", reply.Error)
	assert.Equal(t, "queued", reply.Status)
	model.turn(t, nil)
	assert.Equal(t, 2, strings.Count(strings.Join(model.started(), "\n"), "[m1]"), "the signed m1 ran once and the unsigned m1 once")
}

// TestLookup_AnswersThePosterOfASignedMessage answers a signed message's
// lookup under the transport principal that posted it, and reads it as
// unknown under any other.
func TestLookup_AnswersThePosterOfASignedMessage(t *testing.T) {
	clock := newTestClock()
	key := newSigningKey(t, "k1")
	v, _ := fileVerifier(t, clock, false, key)
	model := newHeldModel()
	q := newMemoryQueue(t, filepath.Join(t.TempDir(), "memory.db"), model)
	srv := serve(t, q, v, newTestLogger())

	code, reply := sendMessage(t, http.DefaultClient, srv.URL, body(t, "m1", "hello", signed(t, key, clock, "m1", "hello")))
	require.Equal(t, http.StatusAccepted, code, reply.Error)
	awaitStarted(t, model, 1)
	assert.Equal(t, "pending", lookupMessage(t, http.DefaultClient, srv.URL, "m1"), "plain HTTP posted it, under the empty principal")
	other, err := q.Status(context.Background(), "mtls-peer", "m1")
	require.NoError(t, err)
	assert.Equal(t, gateway.StatusUnknown, other, "another poster does not see it while pending")

	model.turn(t, nil)
	awaitStatus(t, http.DefaultClient, srv.URL, "m1", "completed")
	other, err = q.Status(context.Background(), "mtls-peer", "m1")
	require.NoError(t, err)
	assert.Equal(t, gateway.StatusUnknown, other, "another poster does not see it once completed")
}

// TestLookup_AnAmbiguousIDIsPendingWhileAnyMessageIs answers pending for
// a poster whose ID names a completed unsigned message and a running
// signed one, and completed once both are. The control is the lookup
// before the signed message, which reads the unsigned one's completed.
func TestLookup_AnAmbiguousIDIsPendingWhileAnyMessageIs(t *testing.T) {
	clock := newTestClock()
	key := newSigningKey(t, "k1")
	v, _ := fileVerifier(t, clock, false, key)
	model := newHeldModel()
	srv := serve(t, newMemoryQueue(t, filepath.Join(t.TempDir(), "memory.db"), model), v, newTestLogger())

	code, _ := sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m1","content":"unsigned"}`)
	require.Equal(t, http.StatusAccepted, code)
	model.turn(t, nil)
	awaitStatus(t, http.DefaultClient, srv.URL, "m1", "completed")

	code, reply := sendMessage(t, http.DefaultClient, srv.URL, body(t, "m1", "signed", signed(t, key, clock, "m1", "signed")))
	require.Equal(t, http.StatusAccepted, code, reply.Error)
	awaitStarted(t, model, 2)
	assert.Equal(t, "pending", lookupMessage(t, http.DefaultClient, srv.URL, "m1"))

	model.turn(t, nil)
	awaitStatus(t, http.DefaultClient, srv.URL, "m1", "completed")
}

// TestTurn_ShowsAVouchedSenderAndAnIssuersNotice renders a signed message
// by its subject and the issuer vouching for it, and one whose subject is
// its issuer as that issuer's notice with no sender to reply to.
func TestTurn_ShowsAVouchedSenderAndAnIssuersNotice(t *testing.T) {
	clock := newTestClock()
	key := newSigningKey(t, "k1")
	v, _ := fileVerifier(t, clock, false, key)
	gate := make(chan struct{})
	session := &fakeSession{gate: gate}
	srv := serve(t, newTestQueue(t, session), v, newTestLogger())

	code, _ := sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m0","content":"hold"}`)
	require.Equal(t, http.StatusAccepted, code)
	require.Eventually(t, func() bool { return len(session.startedTasks()) == 1 }, 5*time.Second, time.Millisecond)

	code, reply := sendMessage(t, http.DefaultClient, srv.URL, body(t, "m1", "hello", signed(t, key, clock, "m1", "hello")))
	require.Equal(t, http.StatusAccepted, code, reply.Error)
	notice := claimsFor("m2", "refused", clock.Now())
	notice["sub"] = testIssuer
	code, reply = sendMessage(t, http.DefaultClient, srv.URL, body(t, "m2", "refused", signToken(t, key.priv, headerFor(key), notice)))
	require.Equal(t, http.StatusAccepted, code, reply.Error)
	close(gate)

	require.Eventually(t, func() bool { return len(session.startedTasks()) == 2 }, 5*time.Second, time.Millisecond)
	turn := session.startedTasks()[1]
	assert.Contains(t, turn, "[m1] "+testSubject+" (vouched for by "+testIssuer+"): hello")
	assert.Contains(t, turn, "[m2] notice from "+testIssuer+" (no sender to reply to): refused")
	assert.Contains(t, session.startedTasks()[0], "[m0] unknown: hold", "an unsigned loopback message renders as before")
}

// TestRequestLog_AddsTheVerifiedIssuerAndSubject puts a signed message's
// iss and sub on its request line, and nothing of the kind on an unsigned
// one's.
func TestRequestLog_AddsTheVerifiedIssuerAndSubject(t *testing.T) {
	clock := newTestClock()
	key := newSigningKey(t, "k1")
	v, _ := fileVerifier(t, clock, false, key)
	var buf bytes.Buffer
	var mu sync.Mutex
	logger := slog.New(slog.NewJSONHandler(lockedWriter{w: &buf, mu: &mu}, nil))
	srv := serve(t, newTestQueue(t, &fakeSession{}), v, logger)

	code, _ := sendMessage(t, http.DefaultClient, srv.URL, `{"id":"m0","content":"hello"}`)
	require.Equal(t, http.StatusAccepted, code)
	code, _ = sendMessage(t, http.DefaultClient, srv.URL, body(t, "m1", "hello", signed(t, key, clock, "m1", "hello")))
	require.Equal(t, http.StatusAccepted, code)

	mu.Lock()
	defer mu.Unlock()
	var lines []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		if entry["msg"] == "http request" {
			lines = append(lines, entry)
		}
	}
	require.Len(t, lines, 2)
	assert.NotContains(t, lines[0], "iss", "an unsigned message's line has no issuer")
	assert.Equal(t, testIssuer, lines[1]["iss"])
	assert.Equal(t, testSubject, lines[1]["sub"])
}

type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
