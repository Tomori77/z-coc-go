package httpapi

import (
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// mediaAllowedExts is the allowlist of file extensions served from the mounted
// media directory. Anything else 404s so the mount cannot leak arbitrary host
// files (e.g. .php, .env, source files).
var mediaAllowedExts = map[string]bool{
	"webp":  true,
	"png":   true,
	"jpg":   true,
	"jpeg":  true,
	"gif":   true,
	"svg":   true,
	"mp3":   true,
	"ogg":   true,
	"wav":   true,
	"woff2": true,
	"json":  true,
}

// handleMedia serves user-uploaded item/media files from the host-mounted
// MEDIA_DIR at /media/<relative-path>. It is read-only (GET/HEAD),
// extension-allowlisted, never lists directories and is path-traversal safe.
// When MEDIA_DIR is empty the feature is off and every request 404s.
func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		errorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	root := strings.TrimSpace(s.cfg.MediaDir)
	if root == "" {
		errorJSON(w, http.StatusNotFound, "Not found")
		return
	}

	rel := strings.TrimPrefix(r.URL.Path, "/media/")
	if rel == "" || strings.HasSuffix(rel, "/") || strings.Contains(rel, "..") {
		errorJSON(w, http.StatusNotFound, "Not found")
		return
	}

	// Clean the relative path and reject any surviving traversal segment.
	clean := strings.TrimPrefix(path.Clean("/"+rel), "/")
	if clean == "" || clean == "." {
		errorJSON(w, http.StatusNotFound, "Not found")
		return
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == "" || seg == ".." || seg == "." {
			errorJSON(w, http.StatusNotFound, "Not found")
			return
		}
	}

	// Extension allowlist (lowercased).
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(clean), "."))
	if !mediaAllowedExts[ext] {
		errorJSON(w, http.StatusNotFound, "Not found")
		return
	}

	// Resolve within MediaDir and confirm the result cannot escape it.
	full := filepath.Join(root, filepath.FromSlash(clean))
	resolved, err := filepath.Abs(full)
	if err != nil {
		errorJSON(w, http.StatusNotFound, "Not found")
		return
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		errorJSON(w, http.StatusNotFound, "Not found")
		return
	}
	if inside, err := filepath.Rel(rootAbs, resolved); err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		errorJSON(w, http.StatusNotFound, "Not found")
		return
	}

	info, err := os.Stat(resolved)
	if err != nil || info.IsDir() {
		errorJSON(w, http.StatusNotFound, "Not found")
		return
	}

	f, err := os.Open(resolved)
	if err != nil {
		errorJSON(w, http.StatusNotFound, "Not found")
		return
	}
	defer f.Close()

	h := w.Header()
	applyStaticSecurityHeaders(h)
	h.Set("Cache-Control", "public, max-age=604800, stale-while-revalidate=86400")
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(resolved)))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	h.Set("Content-Type", contentType)
	http.ServeContent(w, r, filepath.Base(resolved), info.ModTime(), f)
}
