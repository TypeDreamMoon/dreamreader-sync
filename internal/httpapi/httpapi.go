// Package httpapi wires the dreamreader-sync HTTP surface: an unauthenticated
// health probe and a JWT-gated per-user sync document endpoint. Authentication
// is delegated to the IAM token validator (authmw-go): every /api route requires
// a valid IAM access token whose audience is this app's client_id.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/TypeDreamMoon/dreamreader-sync/internal/authmw/middleware"

	"github.com/TypeDreamMoon/dreamreader-sync/internal/config"
	"github.com/TypeDreamMoon/dreamreader-sync/internal/store"
)

// Stable contract error codes carried in the response envelope. 0 is success.
const (
	codeOK            = 0
	codeInternal      = 1
	codeUnauthorized  = 40100
	codeBadRequest    = 40001
	codePayloadTooBig = 40002
	codeConflict      = 40901
)

// API holds handler dependencies.
type API struct {
	cfg *config.Config
	st  *store.Store
	log *slog.Logger
}

// New builds the top-level http.Handler with routing, auth, and CORS wired.
func New(cfg *config.Config, st *store.Store, v *middleware.Validator, log *slog.Logger) http.Handler {
	a := &API{cfg: cfg, st: st, log: log}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.handleHealthz)

	// Protected sync routes. v.Require validates the Bearer token and injects the
	// caller Identity into the request context; the handler reads uid from it.
	mux.Handle("GET /api/v1/sync", v.Require(http.HandlerFunc(a.handleGetSync)))
	mux.Handle("PUT /api/v1/sync", v.Require(http.HandlerFunc(a.handlePutSync)))

	return a.withCORS(mux)
}

func (a *API) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, envelope{Code: codeOK, Msg: "ok", Data: map[string]string{"status": "ok"}})
}

// withCORS applies a browser-origin allowlist and answers preflight. Native app
// clients send no Origin header and pass through untouched.
func (a *API) withCORS(next http.Handler) http.Handler {
	allowed := map[string]bool{}
	wildcard := false
	for _, o := range a.cfg.CORSOrigins {
		if o == "*" {
			wildcard = true
		}
		allowed[o] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (wildcard || allowed[origin]) {
			if wildcard {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, If-Match")
			w.Header().Set("Access-Control-Expose-Headers", "ETag")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- response envelope (shape mirrors the hertz-iam ecosystem: code/msg/data) ---

type envelope struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, body envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, status, code int, msg string) {
	writeJSON(w, status, envelope{Code: code, Msg: msg})
}
