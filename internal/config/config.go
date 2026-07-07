// Package config loads dreamreader-sync configuration from the environment with
// safe local defaults. The service is a pure IAM resource server: it holds no
// client secret and validates access tokens only via the provider's JWKS.
package config

import (
	"os"
	"strconv"
	"strings"
)

// defaultMaxDocBytes caps a single sync document at 8 MiB.
const defaultMaxDocBytes = 8 * 1024 * 1024

// Config holds dreamreader-sync runtime configuration.
type Config struct {
	// HTTPAddr is the listen address, e.g. ":8090".
	HTTPAddr string

	// DBPath is the SQLite database file. A single file holds every user's sync
	// document; back the service up by copying this file (plus its -wal/-shm).
	DBPath string

	// IAM integration (OIDC resource server). The service validates IAM-issued
	// access tokens via JWKS and never reads IAM storage.
	IAMIssuer string // expected token issuer, e.g. https://iam.example.com
	JWKSURI   string // provider jwks_uri; derived from issuer when empty
	// ClientID is the app's registered IAM client_id and the mandatory audience
	// every access token must carry (fail-closed against audience confusion).
	ClientID string

	// MaxDocBytes caps a single sync document upload (abuse defense). Default 8 MiB.
	MaxDocBytes int64

	// CORSOrigins is the browser-origin allowlist (DREAMSYNC_CORS_ORIGINS,
	// comma-separated; "*" allows any). Native app clients send no Origin and are
	// unaffected; this only matters for a hypothetical web build.
	CORSOrigins []string
}

// Load reads configuration from environment variables with local defaults.
func Load() *Config {
	cfg := &Config{
		HTTPAddr:    env("DREAMSYNC_HTTP_ADDR", ":8090"),
		DBPath:      env("DREAMSYNC_DB_PATH", "./data/dreamsync.db"),
		IAMIssuer:   env("DREAMSYNC_IAM_ISSUER", "http://localhost:8080"),
		JWKSURI:     env("DREAMSYNC_JWKS_URI", ""),
		ClientID:    env("DREAMSYNC_CLIENT_ID", "dreamreader"),
		MaxDocBytes: int64(envInt("DREAMSYNC_MAX_DOC_BYTES", defaultMaxDocBytes)),
		CORSOrigins: envCSV("DREAMSYNC_CORS_ORIGINS"),
	}
	// Convention: hertz-iam publishes the user realm JWKS at /realms/user/jwks.
	if cfg.JWKSURI == "" {
		cfg.JWKSURI = strings.TrimRight(cfg.IAMIssuer, "/") + "/realms/user/jwks"
	}
	// A non-positive cap would make http.MaxBytesReader reject every upload as
	// "too large" (it treats n<=0 as a 0-byte limit). Clamp such a misconfig
	// (e.g. the "-1 means unlimited" convention, or a stray 0) back to the
	// default rather than silently breaking all writes.
	if cfg.MaxDocBytes <= 0 {
		cfg.MaxDocBytes = defaultMaxDocBytes
	}
	return cfg
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envCSV(k string) []string {
	v := os.Getenv(k)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
