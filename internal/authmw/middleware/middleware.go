// Package middleware validates a signed IAM access token (issuer, audience,
// expiration, RS256 signature via cached JWKS) and injects the stable identity
// into the request context. It never reads IAM storage.
//
// Vendored from github.com/hertz-iam/authmw-go/middleware so dreamreader-sync
// builds as a fully self-contained repo (no sibling hertz-iam checkout needed).
// Keep in sync with upstream if IAM's token rules change.
package middleware

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"github.com/TypeDreamMoon/dreamreader-sync/internal/authmw/jwks"
)

// Identity is the validated identity exposed to product code.
type Identity struct {
	UID       string
	ClientID  string
	SID       string
	Scopes    []string
	ExpiresAt int64
	AuthTime  int64
}

type ctxKey struct{}

// FromContext returns the validated identity injected by the middleware.
func FromContext(ctx context.Context) (*Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(*Identity)
	return id, ok
}

// Config configures the middleware.
type Config struct {
	Issuer   string // expected token issuer
	Audience string // expected audience (the consumer's client_id)
	JWKSURI  string // provider jwks_uri
	keys     *jwks.Cache
}

// Validator validates bearer tokens and extracts identity.
type Validator struct {
	issuer   string
	audience string
	keys     *jwks.Cache
}

// NewValidator builds a Validator with a JWKS cache for the issuer.
func NewValidator(cfg Config) *Validator {
	cache := cfg.keys
	if cache == nil {
		cache = jwks.New(cfg.JWKSURI)
	}
	return &Validator{issuer: cfg.Issuer, audience: cfg.Audience, keys: cache}
}

// ErrUnauthorized is returned when a token is missing or invalid.
var ErrUnauthorized = errors.New("unauthorized")

// Validate parses and verifies a raw bearer token, returning the identity.
func (v *Validator) Validate(ctx context.Context, raw string) (*Identity, error) {
	type claims struct {
		jwt.RegisteredClaims
		SID      string `json:"sid"`
		ClientID string `json:"client_id"`
		Scope    string `json:"scope"`
		AuthTime int64  `json:"auth_time"`
	}
	c := &claims{}
	_, err := jwt.ParseWithClaims(raw, c, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		kid, _ := t.Header["kid"].(string)
		var key *rsa.PublicKey
		key, err := v.keys.Key(ctx, kid)
		if err != nil {
			return nil, err
		}
		return key, nil
	}, jwt.WithIssuer(v.issuer), jwt.WithExpirationRequired())
	if err != nil {
		return nil, ErrUnauthorized
	}

	// Audience is mandatory: a validator with no configured audience rejects every
	// token rather than failing open, so a misconfigured consumer cannot be made
	// to accept tokens minted for a different consumer (audience confusion).
	ok := false
	for _, a := range c.Audience {
		if v.audience != "" && a == v.audience {
			ok = true
			break
		}
	}
	if !ok {
		return nil, ErrUnauthorized
	}

	id := &Identity{
		UID:      c.Subject,
		ClientID: c.ClientID,
		SID:      c.SID,
		AuthTime: c.AuthTime,
	}
	if c.ExpiresAt != nil {
		id.ExpiresAt = c.ExpiresAt.Unix()
	}
	if c.Scope != "" {
		id.Scopes = strings.Fields(c.Scope)
	}
	return id, nil
}

// Require returns middleware that rejects unauthenticated requests and injects
// the validated identity into the context for downstream handlers.
func (v *Validator) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authz := r.Header.Get("Authorization")
		if !strings.HasPrefix(authz, "Bearer ") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		raw := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
		id, err := v.Validate(r.Context(), raw)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
