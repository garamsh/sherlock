package tool

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// DescriptorHashForTest exposes the descriptorHash function for tests.
func DescriptorHashForTest(d Descriptor) (string, error) {
	return descriptorHash(d)
}

// descriptorHash computes the descriptor's content hash. The hash is the
// SHA-256 of the canonical JSON encoding (sorted keys, no whitespace) so
// the platform can pin a fetched descriptor and refuse drift on the
// next startup (ADR 0018 §Tool contract).
func descriptorHash(d Descriptor) (string, error) {
	canonical, err := canonicalJSON(d)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// canonicalJSON encodes v as a JSON object with sorted keys and no
// whitespace. The descriptor hash is the only API that depends on this
// canonical form; the rest of the platform hands raw JSON around.
func canonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	return canonicalize(generic)
}

// canonicalize re-encodes the decoded value with sorted keys for
// objects. Primitives and arrays are passed through unchanged.
func canonicalize(v any) ([]byte, error) {
	switch t := v.(type) {
	case map[string]any:
		out := []byte{'{'}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
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
			inner, err := canonicalize(t[k])
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
			b, err := canonicalize(item)
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
