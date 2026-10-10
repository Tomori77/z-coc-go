package httpapi

import (
	"net/http"
	"runtime/debug"
	"strconv"
	"time"
)

// rateLimit consumes a fixed-window hit, writing a 429 when exceeded.
func (s *Server) rateLimit(w http.ResponseWriter, r *http.Request, scope string, limit, windowSeconds int) bool {
	allowed, retryAfter := s.limiter.Check(r.Context(), clientIP(r), scope, limit, windowSeconds)
	if !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		errorJSON(w, http.StatusTooManyRequests, "Too many requests")
		return false
	}
	return true
}

// applyCORS reproduces app_send_cors_headers(): Vary: Origin plus an
// Access-Control-Allow-Origin only for origins in the allow-list.
func (s *Server) applyCORS(w http.ResponseWriter, r *http.Request, methods string) {
	if origin := r.Header.Get("Origin"); origin != "" {
		w.Header().Add("Vary", "Origin")
		if s.cfg.OriginAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
	}
	w.Header().Set("Access-Control-Allow-Methods", methods)
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Access-Control-Max-Age", "600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
}

// withCORS wraps a handler with CORS headers and OPTIONS short-circuiting.
func (s *Server) withCORS(methods string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.applyCORS(w, r, methods)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

// recoverMiddleware converts panics into 500 responses and logs them.
func (s *Server) recoverMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Error("panic recovered", "panic", rec, "stack", string(debug.Stack()))
				errorJSON(w, http.StatusInternalServerError, "Internal server error")
			}
		}()
		next(w, r)
	}
}

// statusRecorder captures the response status for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// logMiddleware emits one structured log line per request.
func (s *Server) logMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next(rec, r)
		s.logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"action", r.URL.Query().Get("action"),
			"status", rec.status,
			"ip", clientIP(r),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}
