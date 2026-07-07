package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hertz-iam/authmw-go/middleware"

	"github.com/TypeDreamMoon/dreamreader-sync/internal/store"
)

// syncDocView is the response body for GET and for a 409 conflict on PUT.
type syncDocView struct {
	Doc       json.RawMessage `json:"doc"`                  // null when the user has no document yet
	ETag      string          `json:"etag"`                 // "" when no document yet
	UpdatedAt string          `json:"updated_at,omitempty"` // RFC3339
}

// handleGetSync returns the caller's current sync document (or null).
func (a *API) handleGetSync(w http.ResponseWriter, r *http.Request) {
	uid, ok := callerUID(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, codeUnauthorized, "unauthenticated")
		return
	}
	d, exists, err := a.st.Get(r.Context(), uid)
	if err != nil {
		a.log.Error("get sync", "uid", uid, "err", err)
		writeErr(w, http.StatusInternalServerError, codeInternal, "internal error")
		return
	}
	view := syncDocView{Doc: json.RawMessage("null")}
	if exists {
		view.Doc = json.RawMessage(d.Body)
		view.ETag = d.ETag
		view.UpdatedAt = d.UpdatedAt.Format(time.RFC3339Nano)
		w.Header().Set("ETag", d.ETag)
	}
	writeJSON(w, http.StatusOK, envelope{Code: codeOK, Msg: "ok", Data: view})
}

// handlePutSync stores the caller's sync document under optimistic concurrency.
// The client presents the last ETag it saw via If-Match (omit it, or send an
// empty value, for the first-ever push). A stale ETag yields 409 with the
// current server document so the client can merge and retry.
func (a *API) handlePutSync(w http.ResponseWriter, r *http.Request) {
	uid, ok := callerUID(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, codeUnauthorized, "unauthenticated")
		return
	}

	// Cap the body to defend against oversized uploads (memory / disk abuse).
	limited := http.MaxBytesReader(w, r.Body, a.cfg.MaxDocBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeErr(w, http.StatusRequestEntityTooLarge, codePayloadTooBig, "document too large")
			return
		}
		writeErr(w, http.StatusBadRequest, codeBadRequest, "cannot read body")
		return
	}
	// Body is opaque to the server but must be a single well-formed JSON value.
	if !json.Valid(body) {
		writeErr(w, http.StatusBadRequest, codeBadRequest, "body must be valid JSON")
		return
	}

	saved, err := a.st.Put(r.Context(), uid, body, parseIfMatch(r.Header.Get("If-Match")))
	if errors.Is(err, store.ErrConflict) {
		w.Header().Set("ETag", saved.ETag)
		view := syncDocView{Doc: json.RawMessage("null"), ETag: saved.ETag}
		if len(saved.Body) > 0 {
			view.Doc = json.RawMessage(saved.Body)
			view.UpdatedAt = saved.UpdatedAt.Format(time.RFC3339Nano)
		}
		writeJSON(w, http.StatusConflict, envelope{Code: codeConflict, Msg: "etag conflict", Data: view})
		return
	}
	if err != nil {
		a.log.Error("put sync", "uid", uid, "err", err)
		writeErr(w, http.StatusInternalServerError, codeInternal, "internal error")
		return
	}
	w.Header().Set("ETag", saved.ETag)
	writeJSON(w, http.StatusOK, envelope{Code: codeOK, Msg: "ok", Data: map[string]any{
		"etag":       saved.ETag,
		"updated_at": saved.UpdatedAt.Format(time.RFC3339Nano),
	}})
}

// callerUID pulls the validated IAM subject out of the request context.
func callerUID(r *http.Request) (string, bool) {
	id, ok := middleware.FromContext(r.Context())
	if !ok || id.UID == "" {
		return "", false
	}
	return id.UID, true
}

// parseIfMatch normalizes an If-Match header into a bare ETag. Our contract:
// the value is the last ETag the client saw; an absent/empty header means
// "I expect no prior document". A weak indicator (W/) and quotes are stripped.
func parseIfMatch(h string) string {
	h = strings.TrimSpace(h)
	if h == "" {
		return ""
	}
	h = strings.TrimPrefix(h, "W/")
	return strings.Trim(h, `"`)
}
