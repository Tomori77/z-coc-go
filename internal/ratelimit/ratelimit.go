package ratelimit

import (
	"context"
	"log/slog"
	"math/rand"
	"regexp"
	"time"

	"github.com/Tomori77/z-coc/internal/store"
)

var scopeSanitizeRe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// Limiter implements the DB-backed fixed-window limiter from lib/RateLimit.php.
type Limiter struct {
	store  *store.Store
	logger *slog.Logger
}

// New builds a Limiter.
func New(s *store.Store, logger *slog.Logger) *Limiter {
	return &Limiter{store: s, logger: logger}
}

// Check consumes a hit for ip:scope. It returns allowed=false and a positive
// Retry-After value (seconds) when the limit is exceeded.
func (l *Limiter) Check(ctx context.Context, ip, scope string, limit, windowSeconds int) (allowed bool, retryAfter int) {
	scope = scopeSanitizeRe.ReplaceAllString(scope, "")
	if limit < 1 {
		limit = 1
	}
	if windowSeconds < 10 {
		windowSeconds = 10
	}
	bucket := ip + ":" + scope

	if rand.Intn(100) == 0 {
		if err := l.store.RateLimitCleanup(ctx); err != nil {
			l.logger.Warn("rate limit cleanup failed", "error", err)
		}
	}

	hits, startedAt, found, err := l.store.RateLimitGet(ctx, bucket)
	if err != nil {
		l.logger.Error("rate limit read failed", "error", err)
		return true, 0
	}

	now := time.Now().Unix()
	if !found || (now-startedAt) >= int64(windowSeconds) {
		if err := l.store.RateLimitReset(ctx, bucket); err != nil {
			l.logger.Error("rate limit reset failed", "error", err)
		}
		return true, 0
	}

	if hits >= int64(limit) {
		retry := windowSeconds - int(now-startedAt)
		if retry < 1 {
			retry = 1
		}
		return false, retry
	}

	if err := l.store.RateLimitIncrement(ctx, bucket); err != nil {
		l.logger.Error("rate limit increment failed", "error", err)
	}
	return true, 0
}
