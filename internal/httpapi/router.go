package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/Tomori77/z-coc/internal/config"
	"github.com/Tomori77/z-coc/internal/ratelimit"
	"github.com/Tomori77/z-coc/internal/realtime"
	"github.com/Tomori77/z-coc/internal/store"
)

// Server holds shared dependencies for the HTTP API.
type Server struct {
	cfg     *config.Config
	store   *store.Store
	limiter *ratelimit.Limiter
	logger  *slog.Logger
	hub     *realtime.Hub
}

// New constructs a Server.
func New(cfg *config.Config, st *store.Store, limiter *ratelimit.Limiter, logger *slog.Logger) *Server {
	return &Server{cfg: cfg, store: st, limiter: limiter, logger: logger, hub: realtime.New(cfg, st, logger)}
}

// Close stops the WebSocket hub. Call it during graceful shutdown.
func (s *Server) Close() {
	if s.hub != nil {
		s.hub.Close()
	}
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

	friendAPI := s.wrap(s.withCORS("GET, POST, OPTIONS", s.handleFriendAPI))
	mux.HandleFunc("/friend_api.php", friendAPI)
	mux.HandleFunc("/friend_api", friendAPI)

	adminAPI := s.wrap(s.withCORS("GET, POST, OPTIONS", s.handleAdminAPI))
	mux.HandleFunc("/admin_api.php", adminAPI)
	mux.HandleFunc("/admin_api", adminAPI)

	configAPI := s.wrap(s.withCORS("GET, OPTIONS", s.handleConfigAPI))
	mux.HandleFunc("/config_api.php", configAPI)
	mux.HandleFunc("/config_api", configAPI)

	download := s.wrap(s.withCORS("POST, OPTIONS", s.handleDownload))
	mux.HandleFunc("/dl/download.php", download)
	mux.HandleFunc("/download.php", download)

	// WebSocket 实时房间端点：必须在静态 catch-all 之前注册。
	roomWS := s.wrap(s.handleRoomWS)
	mux.HandleFunc("/room_ws", roomWS)
	mux.HandleFunc("/ws/room", roomWS)

	// Static frontend catch-all: registered last so the specific API patterns
	// above always win. It also owns the SPA fallback and .php 404s.
	mux.HandleFunc("/", s.wrap(s.handleStatic))

	return mux
}

// handleRoomWS 把 /room_ws 升级交给每实例唯一的实时 Hub。
func (s *Server) handleRoomWS(w http.ResponseWriter, r *http.Request) {
	s.hub.ServeHTTP(w, r)
}
