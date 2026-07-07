package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hertz-iam/authmw-go/middleware"

	"github.com/TypeDreamMoon/dreamreader-sync/internal/config"
	"github.com/TypeDreamMoon/dreamreader-sync/internal/store"
)

const (
	testIssuer   = "https://iam.test"
	testAudience = "dreamreader"
	testKID      = "test-key-1"
	testUID      = "user-abc"
)

// newTestAPI wires the real IAM validator against an in-process JWKS server plus
// a temp-file store, so tests exercise the true auth + persistence paths.
func newTestAPI(t *testing.T) (http.Handler, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}

	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pub := key.PublicKey
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA",
			"kid": testKID,
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}}})
	}))
	t.Cleanup(jwks.Close)

	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store open: %v", err)
	}
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	cfg := &config.Config{MaxDocBytes: 1 << 20} // 1 MiB cap for the size test
	v := middleware.NewValidator(middleware.Config{
		Issuer:   testIssuer,
		Audience: testAudience,
		JWKSURI:  jwks.URL,
	})
	return New(cfg, st, v, slog.New(slog.NewTextHandler(io.Discard, nil))), key
}

// mint signs an IAM-shaped access token; mut lets a test tweak the claims.
func mint(t *testing.T, key *rsa.PrivateKey, mut func(jwt.MapClaims)) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss": testIssuer,
		"aud": testAudience,
		"sub": testUID,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	if mut != nil {
		mut(claims)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = testKID
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

type env struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func do(t *testing.T, h http.Handler, method, path, token, ifMatch string, body []byte) (*httptest.ResponseRecorder, env) {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var e env
	_ = json.Unmarshal(rr.Body.Bytes(), &e)
	return rr, e
}

func TestHealthz(t *testing.T) {
	h, _ := newTestAPI(t)
	rr, e := do(t, h, http.MethodGet, "/healthz", "", "", nil)
	if rr.Code != http.StatusOK || e.Code != 0 {
		t.Fatalf("healthz: status=%d code=%d body=%s", rr.Code, e.Code, rr.Body)
	}
}

func TestAuthRequired(t *testing.T) {
	h, key := newTestAPI(t)
	otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)

	cases := []struct {
		name  string
		token string
	}{
		{"no token", ""},
		{"garbage", "not-a-jwt"},
		{"wrong audience", mint(t, key, func(c jwt.MapClaims) { c["aud"] = "someone-else" })},
		{"wrong issuer", mint(t, key, func(c jwt.MapClaims) { c["iss"] = "https://evil.test" })},
		{"expired", mint(t, key, func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() })},
		{"foreign signer", signWith(t, otherKey)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr, _ := do(t, h, http.MethodGet, "/api/v1/sync", tc.token, "", nil)
			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("want 401, got %d (%s)", rr.Code, rr.Body)
			}
		})
	}
}

// signWith mints a token signed by a key the JWKS server does NOT publish.
func signWith(t *testing.T, key *rsa.PrivateKey) string {
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": testIssuer, "aud": testAudience, "sub": testUID,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = testKID
	s, _ := tok.SignedString(key)
	return s
}

func TestSyncFlow(t *testing.T) {
	h, key := newTestAPI(t)
	tok := mint(t, key, nil)

	// 1. Empty state -> doc null, no etag.
	_, e := do(t, h, http.MethodGet, "/api/v1/sync", tok, "", nil)
	var v0 syncDocView
	mustJSON(t, e.Data, &v0)
	if string(v0.Doc) != "null" || v0.ETag != "" {
		t.Fatalf("empty state: doc=%s etag=%q", v0.Doc, v0.ETag)
	}

	// 2. First push (no If-Match) -> 200 + etag.
	rr, e := do(t, h, http.MethodPut, "/api/v1/sync", tok, "", []byte(`{"v":1}`))
	if rr.Code != http.StatusOK {
		t.Fatalf("first put: %d %s", rr.Code, rr.Body)
	}
	etag1 := field(t, e.Data, "etag")
	if etag1 == "" || rr.Header().Get("ETag") != etag1 {
		t.Fatalf("etag1=%q header=%q", etag1, rr.Header().Get("ETag"))
	}

	// 3. Read back.
	_, e = do(t, h, http.MethodGet, "/api/v1/sync", tok, "", nil)
	var v1 syncDocView
	mustJSON(t, e.Data, &v1)
	if string(v1.Doc) != `{"v":1}` || v1.ETag != etag1 {
		t.Fatalf("read back: doc=%s etag=%q want etag=%q", v1.Doc, v1.ETag, etag1)
	}

	// 4. Stale push -> 409, returns the current server doc for merge.
	rr, e = do(t, h, http.MethodPut, "/api/v1/sync", tok, "stale-etag", []byte(`{"v":2}`))
	if rr.Code != http.StatusConflict {
		t.Fatalf("stale put: want 409 got %d", rr.Code)
	}
	var vc syncDocView
	mustJSON(t, e.Data, &vc)
	if string(vc.Doc) != `{"v":1}` || vc.ETag != etag1 {
		t.Fatalf("conflict payload: doc=%s etag=%q", vc.Doc, vc.ETag)
	}

	// 5. Correct If-Match -> 200 + new etag.
	rr, e = do(t, h, http.MethodPut, "/api/v1/sync", tok, etag1, []byte(`{"v":2}`))
	if rr.Code != http.StatusOK {
		t.Fatalf("update put: %d %s", rr.Code, rr.Body)
	}
	etag2 := field(t, e.Data, "etag")
	if etag2 == "" || etag2 == etag1 {
		t.Fatalf("etag2=%q etag1=%q (must differ)", etag2, etag1)
	}

	// 6. Final read reflects v2.
	_, e = do(t, h, http.MethodGet, "/api/v1/sync", tok, "", nil)
	var v2 syncDocView
	mustJSON(t, e.Data, &v2)
	if string(v2.Doc) != `{"v":2}` || v2.ETag != etag2 {
		t.Fatalf("final read: doc=%s etag=%q", v2.Doc, v2.ETag)
	}
}

func TestUserIsolation(t *testing.T) {
	h, key := newTestAPI(t)
	alice := mint(t, key, func(c jwt.MapClaims) { c["sub"] = "alice" })
	bob := mint(t, key, func(c jwt.MapClaims) { c["sub"] = "bob" })

	if rr, _ := do(t, h, http.MethodPut, "/api/v1/sync", alice, "", []byte(`{"who":"alice"}`)); rr.Code != http.StatusOK {
		t.Fatalf("alice put: %d", rr.Code)
	}
	// Bob must not see Alice's document.
	_, e := do(t, h, http.MethodGet, "/api/v1/sync", bob, "", nil)
	var vb syncDocView
	mustJSON(t, e.Data, &vb)
	if string(vb.Doc) != "null" {
		t.Fatalf("isolation breach: bob saw %s", vb.Doc)
	}
}

func TestValidation(t *testing.T) {
	h, key := newTestAPI(t)
	tok := mint(t, key, nil)

	// Malformed JSON -> 400.
	if rr, _ := do(t, h, http.MethodPut, "/api/v1/sync", tok, "", []byte(`{bad`)); rr.Code != http.StatusBadRequest {
		t.Fatalf("bad json: want 400 got %d", rr.Code)
	}
	// Oversized (> 1 MiB cap) -> 413. A JSON string of 2 MiB is well-formed but too big.
	big := []byte(`"` + strings.Repeat("a", 2<<20) + `"`)
	if rr, _ := do(t, h, http.MethodPut, "/api/v1/sync", tok, "", big); rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized: want 413 got %d", rr.Code)
	}
}

func mustJSON(t *testing.T, raw json.RawMessage, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
}

func field(t *testing.T, raw json.RawMessage, key string) string {
	t.Helper()
	m := map[string]any{}
	mustJSON(t, raw, &m)
	s, _ := m[key].(string)
	return s
}
