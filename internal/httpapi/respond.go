package httpapi

import (
	"encoding/json"
	"net/http"
)

// writeJSON writes a JSON response using the PHP-style envelope. HTML escaping
// is disabled to mirror json_encode() with JSON_UNESCAPED_UNICODE.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}

// writeRawJSON writes pre-encoded JSON (used for the literal null from pull).
func writeRawJSON(w http.ResponseWriter, status int, raw []byte) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

// errorJSON writes {"status":"error","message":...}.
func errorJSON(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"status": "error", "message": message})
}
