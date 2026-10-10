package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// clientIP extracts the remote address, truncated to 45 chars like app_client_ip().
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if len(host) > 45 {
		host = host[:45]
	}
	return host
}

// requireMethod enforces a single HTTP method, matching app_require_method().
func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		errorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return false
	}
	return true
}

// readJSONBody enforces the body limit and decodes a JSON object, matching
// app_require_content_length() + app_read_json_body().
func readJSONBody(w http.ResponseWriter, r *http.Request, maxBytes int64) (map[string]any, bool) {
	if maxBytes < 1 {
		maxBytes = 1
	}
	if r.ContentLength > maxBytes {
		errorJSON(w, http.StatusRequestEntityTooLarge, "Request body too large")
		return nil, false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		errorJSON(w, http.StatusBadRequest, "Invalid JSON")
		return nil, false
	}
	if int64(len(raw)) > maxBytes {
		errorJSON(w, http.StatusRequestEntityTooLarge, "Request body too large")
		return nil, false
	}
	if strings.TrimSpace(string(raw)) == "" {
		return map[string]any{}, true
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		errorJSON(w, http.StatusBadRequest, "Invalid JSON")
		return nil, false
	}
	switch v := decoded.(type) {
	case map[string]any:
		return v, true
	case []any:
		return map[string]any{}, true
	default:
		errorJSON(w, http.StatusBadRequest, "Invalid JSON")
		return nil, false
	}
}

// asString mimics PHP's (string) cast for JSON-decoded scalars.
func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == math.Trunc(t) && !math.IsInf(t, 0) && !math.IsNaN(t) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "1"
		}
		return ""
	default:
		return ""
	}
}

// isTruthy mimics PHP's empty()/boolean truthiness for decoded JSON values.
func isTruthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != "" && t != "0"
	case float64:
		return t != 0
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	default:
		return false
	}
}

// phpAtoi mimics PHP's (int) cast of a string, parsing a leading integer.
func phpAtoi(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	i := 0
	if s[0] == '-' || s[0] == '+' {
		i = 1
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == start {
		return 0
	}
	n, err := strconv.ParseInt(s[:i], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// phpIntCast mimics PHP's (int) cast for decoded JSON scalars.
func phpIntCast(v any) int64 {
	switch t := v.(type) {
	case nil:
		return 0
	case bool:
		if t {
			return 1
		}
		return 0
	case float64:
		return int64(t)
	case string:
		return phpAtoi(t)
	default:
		return 0
	}
}

// marshalJSON encodes a value compactly with HTML escaping disabled.
func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// phpify recursively converts empty JSON objects into empty arrays, matching
// how PHP's json_decode(..., true) + json_encode round-trips `{}` as `[]`.
func phpify(v any) any {
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 0 {
			return []any{}
		}
		for k, val := range t {
			t[k] = phpify(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = phpify(val)
		}
		return t
	default:
		return v
	}
}

// sanitizeCloudSettings strips secrets from the cloud settings blob.
func sanitizeCloudSettings(v any) any {
	switch t := v.(type) {
	case map[string]any:
		if api, ok := t["api"].(map[string]any); ok {
			delete(api, "customApiKey")
			delete(api, "imageGenApiKey")
			if presets, ok := api["apiPresets"].([]any); ok {
				for _, preset := range presets {
					if pm, ok := preset.(map[string]any); ok {
						delete(pm, "key")
					}
				}
			}
		}
		return t
	case []any:
		return t
	default:
		return []any{}
	}
}

// jsonColumn decodes a nullable text column, returning nil on absence/error.
func jsonColumn(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(*value), &decoded); err != nil {
		return nil
	}
	return decoded
}

// listJSONColumn decodes a nullable text column that must hold an array/object.
func listJSONColumn(value *string, field string) (any, bool) {
	if value == nil || *value == "" {
		return []any{}, true
	}
	var decoded any
	if err := json.Unmarshal([]byte(*value), &decoded); err != nil {
		return nil, false
	}
	switch decoded.(type) {
	case []any, map[string]any:
		return decoded, true
	default:
		return nil, false
	}
}

// validEmail approximates PHP's FILTER_VALIDATE_EMAIL for the shapes we accept.
func validEmail(email string) bool {
	if email == "" || strings.ContainsAny(email, " \t\r\n") {
		return false
	}
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 {
		return false
	}
	local := email[:at]
	domain := email[at+1:]
	if local == "" || domain == "" || strings.Contains(local, "@") {
		return false
	}
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}
	if strings.HasPrefix(domain, "[") && strings.HasSuffix(domain, "]") {
		return true
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" {
			return false
		}
	}
	return true
}
