package gateway

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// The key-set cache's bounds (ADR 0026 §1, from ADR-0101 §What a verifier
// checks).
const (
	// defaultKeysLifetime is how long a set is held when its source states
	// no max-age, and always for a file.
	defaultKeysLifetime = time.Hour
	// keysGrace is how long past its lifetime a held set stays usable
	// while reads fail.
	keysGrace = 24 * time.Hour
	// keysReadInterval bounds how often one issuer's set is read again,
	// so a forged kid cannot make the agent read faster than this.
	keysReadInterval = time.Minute
	// keysFetchTimeout bounds one fetch of a keys-url.
	keysFetchTimeout = 10 * time.Second
	// maxKeySetBody caps a key-set document.
	maxKeySetBody = 1 << 20
)

// readKeys reads a key-set document and the lifetime its source gives it.
type readKeys func(ctx context.Context) (body []byte, lifetime time.Duration, err error)

// KeySet is one trusted issuer's verification keys, cached (ADR 0026 §1).
// A set is held for its lifetime, read again when it lapses or when a
// message names a kid it lacks — at most once a minute — and replaced
// whole by each successful read. While reads fail the held set stays
// usable for keysGrace past its lifetime, and after that the issuer is
// unavailable until a read succeeds. KeySet is safe for concurrent use.
type KeySet struct {
	issuer string
	read   readKeys
	now    func() time.Time
	logger *slog.Logger

	mu        sync.Mutex
	keys      map[string]*ecdsa.PublicKey // nil until a read succeeds
	expires   time.Time                   // the held set's lifetime ends
	attempted time.Time                   // the last read, successful or not
}

// NewFileKeySet returns issuer's key set, read from a JWK Set document at
// path that the launcher renders. A file's lifetime is defaultKeysLifetime.
func NewFileKeySet(issuer, path string, now func() time.Time, logger *slog.Logger) *KeySet {
	read := func(context.Context) ([]byte, time.Duration, error) {
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, 0, fmt.Errorf("read keys-file: %w", err)
		}
		return body, defaultKeysLifetime, nil
	}
	return &KeySet{issuer: issuer, read: read, now: now, logger: logger}
}

// NewURLKeySet returns issuer's key set, fetched from the https URL
// rawURL over transport (nil: the default, verifying against the system
// roots). The response's max-age is the set's lifetime. A redirect is
// refused: the set's location is the configured one and no other.
func NewURLKeySet(issuer, rawURL string, transport http.RoundTripper, now func() time.Time, logger *slog.Logger) *KeySet {
	client := &http.Client{
		Transport: transport,
		Timeout:   keysFetchTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	read := func(ctx context.Context) ([]byte, time.Duration, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, 0, fmt.Errorf("fetch keys-url: %w", err)
		}
		req.Header.Set("Accept", "application/jwk-set+json, application/json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, fmt.Errorf("fetch keys-url: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return nil, 0, fmt.Errorf("fetch keys-url: status %d", resp.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxKeySetBody+1))
		if err != nil {
			return nil, 0, fmt.Errorf("fetch keys-url: %w", err)
		}
		if len(body) > maxKeySetBody {
			return nil, 0, errors.New("fetch keys-url: document exceeds 1 MiB")
		}
		return body, maxAge(resp.Header.Get("Cache-Control")), nil
	}
	return &KeySet{issuer: issuer, read: read, now: now, logger: logger}
}

// Issuer is the iss value this set verifies.
func (k *KeySet) Issuer() string { return k.issuer }

// Load reads the set now and returns why it could not. The agent calls it
// at startup: a keys-file that fails refuses the start, a keys-url that
// fails leaves the issuer unavailable until a later read succeeds.
func (k *KeySet) Load(ctx context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.readNow(ctx, k.now())
}

// key returns the key kid names, reading the set again when it has lapsed
// or lacks kid and the last read is a minute old. It returns
// ErrIssuerUnavailable when no set is usable, and a key refusal when a
// usable set has no such kid.
func (k *KeySet) key(ctx context.Context, kid string) (*ecdsa.PublicKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	now := k.now()
	if k.keys == nil || !now.Before(k.expires) {
		k.refresh(ctx, now)
	}
	if k.keys == nil || now.After(k.expires.Add(keysGrace)) {
		return nil, ErrIssuerUnavailable
	}
	if key, ok := k.keys[kid]; ok {
		return key, nil
	}
	if k.refresh(ctx, now) {
		if key, ok := k.keys[kid]; ok {
			return key, nil
		}
	}
	return nil, &Refusal{Check: checkKey, Issuer: k.issuer, KeyID: kid}
}

// refresh reads the set unless the last read is less than a minute old,
// and reports whether it read. A failed read keeps the held set and is
// logged.
func (k *KeySet) refresh(ctx context.Context, now time.Time) bool {
	if !k.attempted.IsZero() && now.Sub(k.attempted) < keysReadInterval {
		return false
	}
	if err := k.readNow(ctx, now); err != nil {
		k.logger.Warn("issuer key set read failed",
			"op", "keys.read", "iss", k.issuer, "error", err)
	}
	return true
}

func (k *KeySet) readNow(ctx context.Context, now time.Time) error {
	k.attempted = now
	body, lifetime, err := k.read(ctx)
	if err != nil {
		return err
	}
	keys, err := parseKeySet(body)
	if err != nil {
		return err
	}
	k.keys = keys
	k.expires = now.Add(lifetime)
	return nil
}

// parseKeySet reads a JWK Set document (RFC 7517) and keeps the keys a
// message can be signed with: P-256 public keys with a kid, whose alg and
// use, when stated, are ES256 and sig. Any other entry is ignored, so an
// issuer may publish keys of other kinds beside them.
func parseKeySet(body []byte) (map[string]*ecdsa.PublicKey, error) {
	var doc struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse key set: %w", err)
	}
	if doc.Keys == nil {
		return nil, errors.New("parse key set: no keys member")
	}
	keys := make(map[string]*ecdsa.PublicKey, len(doc.Keys))
	for _, raw := range doc.Keys {
		var jwk jose.JSONWebKey
		if err := jwk.UnmarshalJSON(raw); err != nil {
			continue
		}
		pub, ok := jwk.Key.(*ecdsa.PublicKey)
		if !ok || pub.Curve != elliptic.P256() || jwk.KeyID == "" {
			continue
		}
		if (jwk.Algorithm != "" && jwk.Algorithm != string(jose.ES256)) || (jwk.Use != "" && jwk.Use != "sig") {
			continue
		}
		keys[jwk.KeyID] = pub
	}
	return keys, nil
}

// maxAge returns the max-age directive of a Cache-Control value, or
// defaultKeysLifetime when it states none.
func maxAge(cacheControl string) time.Duration {
	for _, directive := range strings.Split(cacheControl, ",") {
		name, value, ok := strings.Cut(strings.TrimSpace(directive), "=")
		if !ok || !strings.EqualFold(name, "max-age") {
			continue
		}
		seconds, err := strconv.Atoi(strings.Trim(value, `"`))
		if err != nil || seconds < 0 {
			continue
		}
		return time.Duration(seconds) * time.Second
	}
	return defaultKeysLifetime
}
