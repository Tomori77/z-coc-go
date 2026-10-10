package httpapi

import (
	"net/http"

	"github.com/Tomori77/z-coc/internal/settings"
)

// handleConfigAPI serves the public config endpoint. It exposes only the
// whitelisted settings marked Public, with no authentication.
func (s *Server) handleConfigAPI(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Query().Get("action") {
	case "settings":
		s.configSettings(w, r)
	default:
		errorJSON(w, http.StatusNotFound, "Unknown action")
	}
}

// configSettings merges Go-side defaults with stored overrides and returns the
// typed flat key->value map for public settings.
func (s *Server) configSettings(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if !s.rateLimit(w, r, "config", 600, 300) {
		return
	}
	stored, err := s.store.GetAppSettings(r.Context())
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "设置读取失败")
		return
	}
	out := make(map[string]any)
	for _, def := range settings.Definitions {
		if !def.Public {
			continue
		}
		raw := def.Default
		if value, ok := stored[def.Key]; ok {
			raw = value
		}
		out[def.Key] = settings.Decode(def, raw)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "success",
		"settings": out,
	})
}
