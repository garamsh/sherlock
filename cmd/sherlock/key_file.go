package main

import (
	"errors"
	"os"
	"strings"
)

// keyFile is the model's API key read from model.api-key-file on every
// call, so a key the platform rewrites in place takes effect on the next
// request (ADR 0029). It satisfies the Credential interface of both
// provider packages.
type keyFile struct {
	path string
}

// Read returns the file's content with surrounding whitespace trimmed.
func (k keyFile) Read() (string, error) {
	raw, err := os.ReadFile(k.path)
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(string(raw))
	if key == "" {
		return "", errors.New("the file is empty")
	}
	return key, nil
}

// Source names the setting and the path, never the key.
func (k keyFile) Source() string {
	return "model.api-key-file " + k.path
}
