package config

import (
	"fmt"
	"net/url"
	"os"
)

// IssuerConfig is one signer whose messages the gateway accepts
// (ADR 0026): the exact iss value, and exactly one source for its key
// set. The source comes from here and never from the message or the
// issuer's name.
type IssuerConfig struct {
	// Issuer is the iss value, compared byte for byte.
	Issuer string `mapstructure:"issuer"`
	// KeysFile is the path to a JWK Set document the launcher renders.
	KeysFile string `mapstructure:"keys-file"`
	// KeysURL is an https URL serving a JWK Set document.
	KeysURL string `mapstructure:"keys-url"`
}

// issuersKey and issuersEnv name the issuer list's config key and the
// environment variable it would bind to, which is refused.
const (
	issuersKey = "issuers"
	issuersEnv = "SHERLOCK_ISSUERS"
)

// RequireSignatureKey names the setting Config.RequireSignature resolves
// from: a flag, SHERLOCK_REQUIRE_SIGNATURE, or the config file's key.
const RequireSignatureKey = "require-signature"

// checkIssuersEnv refuses the environment form of the issuer list: it is
// a list of entries, and one variable carries one value.
func checkIssuersEnv() error {
	if _, ok := os.LookupEnv(issuersEnv); !ok {
		return nil
	}
	return fmt.Errorf("config: %s is set, and the environment cannot express %s: it is a list of entries, "+
		"and one variable carries one value. Write the issuers in the agent's config file and unset %s",
		issuersEnv, issuersKey, issuersEnv)
}

// checkIssuers refuses an issuer list the gateway could not act on, and a
// signature requirement no message could meet.
func checkIssuers(issuers []IssuerConfig, requireSignature bool) error {
	seen := make(map[string]bool, len(issuers))
	for i, entry := range issuers {
		if entry.Issuer == "" {
			return fmt.Errorf("config: %s[%d] names no issuer", issuersKey, i)
		}
		if seen[entry.Issuer] {
			return fmt.Errorf("config: %s names issuer %q twice", issuersKey, entry.Issuer)
		}
		seen[entry.Issuer] = true
		switch {
		case entry.KeysFile != "" && entry.KeysURL != "":
			return fmt.Errorf("config: issuer %q names both keys-file and keys-url; name exactly one", entry.Issuer)
		case entry.KeysFile == "" && entry.KeysURL == "":
			return fmt.Errorf("config: issuer %q names neither keys-file nor keys-url; name exactly one", entry.Issuer)
		case entry.KeysURL != "":
			if err := checkKeysURL(entry); err != nil {
				return err
			}
		}
	}
	if requireSignature && len(issuers) == 0 {
		return fmt.Errorf("config: %s is on and no issuer is configured, so every message would be refused; "+
			"configure %s or turn %s off", RequireSignatureKey, issuersKey, RequireSignatureKey)
	}
	return nil
}

func checkKeysURL(entry IssuerConfig) error {
	u, err := url.Parse(entry.KeysURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("config: issuer %q keys-url %q is not an https URL", entry.Issuer, entry.KeysURL)
	}
	return nil
}
