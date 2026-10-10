package httpapi

import "net/http"

const (
	maxDownloadDataBytes = 2 * 1024 * 1024
	maxDownloadBodyBytes = maxDownloadDataBytes + 32768
)

// handleDownload mirrors dl/download.php: it reads the form field `data`
// (application/x-www-form-urlencoded) and echoes it back as a JSON attachment.
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if r.ContentLength > maxDownloadBodyBytes {
		errorJSON(w, http.StatusRequestEntityTooLarge, "Request body too large")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxDownloadBodyBytes)
	if err := r.ParseForm(); err != nil {
		writePlain(w, http.StatusBadRequest, "Missing data")
		return
	}
	data := r.PostFormValue("data")
	if data == "" {
		writePlain(w, http.StatusBadRequest, "Missing data")
		return
	}
	if len(data) > maxDownloadDataBytes {
		writePlain(w, http.StatusRequestEntityTooLarge, "File too large")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.Header().Set("Content-Disposition", `attachment; filename="character_card.json"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(data))
}

// writePlain writes a bare text response (used by download.php error paths).
func writePlain(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
