package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/Tomori77/z-coc/internal/config"
	"github.com/Tomori77/z-coc/internal/ratelimit"
	"github.com/Tomori77/z-coc/internal/store"
)

// Server holds shared dependencies for the HTTP API.
type Server struct {
	cfg     *config.Config
	store   *store.Store
	limiter *ratelimit.Limiter
	logger  *slog.Logger
}

// New constructs a Server.
func New(cfg *config.Config, st *store.Store, limiter *ratelimit.Limiter, logger *slog.Logger) *Server {
	return &Server{cfg: cfg, store: st, limiter: limiter, logger: logger}
}

// wrap applies the common middleware chain (recover + request logging).
func (s *Server) wrap(h http.HandlerFunc) http.HandlerFunc {
	return s.recoverMiddleware(s.logMiddleware(h))
}

// Handler builds the route table. API entries keep the original .php paths so
// the existing frontend/mobile clients work unchanged.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	health := s.wrap(s.handleHealth)
	mux.HandleFunc("/health.php", health)
	mux.HandleFunc("/health", health)

	dbAPI := s.wrap(s.withCORS("GET, POST, OPTIONS", s.handleDBAPI))
	mux.HandleFunc("/db_api.php", dbAPI)
	mux.HandleFunc("/db_api", dbAPI)

	roomAPI := s.wrap(s.withCORS("GET, POST, OPTIONS", s.handleRoomAPI))
	mux.HandleFunc("/room_api.php", roomAPI)
	mux.HandleFunc("/room_api", roomAPI)

	libraryAPI := s.wrap(s.withCORS("GET, POST, OPTIONS", s.handleLibraryAPI))
	mux.HandleFunc("/library_api.php", libraryAPI)
	mux.HandleFunc("/library_api", libraryAPI)

	download := s.wrap(s.withCORS("POST, OPTIONS", s.handleDownload))
	mux.HandleFunc("/dl/download.php", download)
	mux.HandleFunc("/download.php", download)

	return mux
}
