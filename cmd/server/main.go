package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Tomori77/z-coc/internal/config"
	"github.com/Tomori77/z-coc/internal/httpapi"
	"github.com/Tomori77/z-coc/internal/ratelimit"
	"github.com/Tomori77/z-coc/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()
	st, err := store.NewPool(ctx, cfg)
	if err != nil {
		logger.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	store.EnsureSchema(ctx, st, logger)

	limiter := ratelimit.New(st, logger)
	api := httpapi.New(cfg, st, limiter, logger)

	cleanupStop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(300 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-cleanupStop:
				return
			case <-ticker.C:
				if err := st.CleanupRooms(ctx, cfg.RoomTTLHours); err != nil {
					logger.Warn("room cleanup failed", "error", err)
				}
			}
		}
	}()
	if err := st.CleanupRooms(ctx, cfg.RoomTTLHours); err != nil {
		logger.Warn("initial room cleanup failed", "error", err)
	}

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("server listening", "port", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	<-signalCh

	close(cleanupStop)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Warn("graceful shutdown failed", "error", err)
	}
}
