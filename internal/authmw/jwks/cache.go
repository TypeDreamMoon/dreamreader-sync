// Package jwks fetches and caches an IAM provider's JSON Web Key Set so this
// service can verify signed identities without reading IAM storage or calling
// IAM on every request.
//
// Vendored from github.com/hertz-iam/authmw-go/jwks so dreamreader-sync builds
// as a fully self-contained repo (no sibling hertz-iam checkout needed). Keep in
// sync with upstream if IAM's JWKS format changes.
package jwks

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// Cache fetches a JWKS document and caches parsed RSA public keys by kid. It
// refreshes when an unknown kid is requested (bounded by minRefresh) so key
// rotation is picked up without per-request fetches.
type Cache struct {
	jwksURI    string
	httpClient *http.Client
	minRefresh time.Duration

	mu          sync.RWMutex
	keys        map[string]*rsa.PublicKey
	lastFetched time.Time
}

// New constructs a JWKS cache for the given jwks_uri.
func New(jwksURI string) *Cache {
	return &Cache{
		jwksURI:    jwksURI,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		minRefresh: 30 * time.Second,
		keys:       map[string]*rsa.PublicKey{},
	}
}

// Key returns the RSA public key for kid, fetching/refreshing the JWKS if the
// key is not cached.
func (c *Cache) Key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	c.mu.RLock()
	k, ok := c.keys[kid]
	c.mu.RUnlock()
	if ok {
		return k, nil
	}
	if err := c.refresh(ctx); err != nil {
		return nil, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if k, ok := c.keys[kid]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("jwks: key id %q not found", kid)
}

func (c *Cache) refresh(ctx context.Context) error {
	c.mu.RLock()
	recent := time.Since(c.lastFetched) < c.minRefresh && len(c.keys) > 0
	c.mu.RUnlock()
	if recent {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.jwksURI, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks: fetch status %d", resp.StatusCode)
	}

	var doc struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return err
	}

	parsed := map[string]*rsa.PublicKey{}
	for _, k := range doc.Keys {
		if k.Kty != "RSA" {
			continue
		}
		pub, err := parseRSA(k.N, k.E)
		if err != nil {
			continue
		}
		parsed[k.Kid] = pub
	}
	if len(parsed) == 0 {
		return errors.New("jwks: no usable RSA keys")
	}

	c.mu.Lock()
	c.keys = parsed
	c.lastFetched = time.Now()
	c.mu.Unlock()
	return nil
}

func parseRSA(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, err
	}
	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)
	return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
}
