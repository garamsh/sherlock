package tool

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// manifestDir is the directory containing the manifest. The Command path
// is resolved against this directory so the manifest is self-contained
// and the operator can move the tool tree without breaking paths.
type manifestDir struct {
	dir      string
	bytes    []byte
	manifest Manifest
	hash     string
}

// ParseManifest decodes the manifest bytes without validating the
// embedded schemas. Tests use it to assert the failure modes of the
// shape itself; startup uses it after a successful raw read.
func ParseManifest(raw []byte) (Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	if m.InputSchema == nil {
		m.InputSchema = json.RawMessage(`{"type":"object"}`)
	}
	if m.OutputSchema == nil {
		m.OutputSchema = json.RawMessage(`{"type":"object"}`)
	}
	return m, nil
}

// ValidateManifest checks the structural rules in ADR 0009 §Decision and
// compiles both schemas as JSON Schema documents. Exported so callers
// can assert the validation contract in isolation; the file-level
// loader uses it internally.
func ValidateManifest(m Manifest) (Manifest, error) {
	if m.Name == "" {
		return m, errors.New("manifest: name is required")
	}
	if m.Command == "" {
		return m, fmt.Errorf("manifest %q: command is required", m.Name)
	}
	if err := validateSchemaDocument(m.InputSchema); err != nil {
		return m, fmt.Errorf("manifest %q: input_schema: %w", m.Name, err)
	}
	if err := validateSchemaDocument(m.OutputSchema); err != nil {
		return m, fmt.Errorf("manifest %q: output_schema: %w", m.Name, err)
	}
	return m, nil
}

// loadManifestDir loads the manifest from dir/manifest.json and records
// the directory on the result so the runner can resolve Command relative
// to the manifest's location.
func loadManifestDir(dir string) (manifestDir, error) {
	path := filepath.Join(dir, "manifest.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return manifestDir{}, fmt.Errorf("read manifest: %w", err)
	}
	m, err := ParseManifest(raw)
	if err != nil {
		return manifestDir{}, err
	}
	if m, err = ValidateManifest(m); err != nil {
		return manifestDir{}, err
	}
	hash := ManifestHash(raw)
	return manifestDir{dir: dir, bytes: raw, manifest: m, hash: hash}, nil
}

// ManifestHash returns the SHA-256 hex digest of the canonical manifest
// bytes. The hash is the drift pin the platform refuses to load on a
// mismatch (ADR 0009 §Decision, invariant 2).
func ManifestHash(raw []byte) string {
	sum := sha256.Sum256(canonicalManifest(raw))
	return hex.EncodeToString(sum[:])
}

// canonicalManifest returns the canonical form of the manifest bytes:
// JSON-decoded and re-encoded with sorted keys and no whitespace, so
// whitespace and key order do not contribute to the hash.
func canonicalManifest(raw []byte) []byte {
	// Decoding into any is what puts nested objects in front of the
	// recursion below: a map[string]json.RawMessage matches neither of
	// its cases, so every value would pass through as its original
	// bytes and only the top level would sort (#559). UseNumber keeps
	// each number as its literal rather than a float64, so two
	// manifests that differ only past float64's exact range stay two
	// manifests.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return raw
	}
	// A decoder stops at the end of the first value. Anything after it
	// would not reach the hash, so bytes that differ there would share
	// one — fall back to hashing the raw bytes instead.
	if dec.More() {
		return raw
	}
	out, err := canonicalizeManifest(doc)
	if err != nil {
		return raw
	}
	return out
}

// canonicalizeManifest re-encodes v with sorted object keys at every
// depth. Array order is preserved: a list's order is contract — a
// schema's `required` lists fields, and its `enum` lists the accepted
// values — so two manifests differing in it are two manifests, not one
// reformatted.
func canonicalizeManifest(v any) ([]byte, error) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := []byte{'{'}
		for i, k := range keys {
			if i > 0 {
				out = append(out, ',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			out = append(out, kb...)
			out = append(out, ':')
			inner, err := canonicalizeManifest(t[k])
			if err != nil {
				return nil, err
			}
			out = append(out, inner...)
		}
		out = append(out, '}')
		return out, nil
	case []any:
		out := []byte{'['}
		for i, item := range t {
			if i > 0 {
				out = append(out, ',')
			}
			b, err := canonicalizeManifest(item)
			if err != nil {
				return nil, err
			}
			out = append(out, b...)
		}
		out = append(out, ']')
		return out, nil
	default:
		return json.Marshal(v)
	}
}
