package httpapi

import "net/http"

// handleHealth mirrors health.php: SELECT 1 -> 200 {status:ok}, else 503.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	var one int
	if err := s.store.Pool.QueryRow(r.Context(), "SELECT 1").Scan(&one); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
