// Package api wires the chi router and HTTP handlers for ember's JSON API.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/brandonhon/ember/internal/store"
)

// errorPayload is the wire format for an API error.
type errorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorEnvelope struct {
	Error errorPayload `json:"error"`
}

type dataEnvelope[T any] struct {
	Data T              `json:"data"`
	Meta map[string]any `json:"meta,omitempty"`
}

// writeJSON writes status + JSON body.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// writeData wraps a value in the standard data envelope.
func writeData(w http.ResponseWriter, status int, data any, meta map[string]any) {
	writeJSON(w, status, dataEnvelope[any]{Data: data, Meta: meta})
}

// writeError writes the standard error envelope.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorEnvelope{Error: errorPayload{Code: code, Message: msg}})
}

// mapStoreError translates a store error into an HTTP response and returns
// true if it handled the error.
func mapStoreError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, store.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "forbidden")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "resource already exists")
	default:
		slog.Default().Error("store error", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
		return true
	}
	return true
}

// maxJSONBody caps the size of any JSON request body. Articles/read bulk
// arrays are the largest legitimate payload (1000 int64 ids ≈ 12 KiB),
// 1 MiB leaves ample slack while shutting down body-bomb DoS attempts.
const maxJSONBody = 1 << 20

// internalError writes a generic 500 response and logs the actual error
// server-side. Replaces direct `writeError(w, 500, "internal", err.Error())`
// calls so SQLite error text, file paths, and constraint details don't leak
// to clients.
func internalError(w http.ResponseWriter, op string, err error) {
	slog.Default().Error("api: "+op, "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "internal error")
}

// decodeJSON decodes the request body, writes a 400 on failure, and reports
// whether decoding succeeded. Caps the body at maxJSONBody bytes via
// MaxBytesReader so a slow-loris or huge-body client can't tie up the
// connection or exhaust memory.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "empty body")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return false
	}
	return true
}
