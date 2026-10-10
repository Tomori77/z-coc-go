package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// RateLimitGet returns the current hit count and window start epoch for a bucket.
func (s *Store) RateLimitGet(ctx context.Context, bucket string) (hits int64, startedAt int64, found bool, err error) {
	var started float64
	row := s.Pool.QueryRow(ctx,
		`SELECT hits, EXTRACT(EPOCH FROM window_start) AS started_at FROM api_rate_limits WHERE bucket = $1 LIMIT 1`, bucket)
	err = row.Scan(&hits, &started)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	return hits, int64(started), true, nil
}

// RateLimitReset starts a fresh window with a single hit.
func (s *Store) RateLimitReset(ctx context.Context, bucket string) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO api_rate_limits (bucket, hits, window_start) VALUES ($1, 1, NOW())
		 ON CONFLICT (bucket) DO UPDATE SET hits = EXCLUDED.hits, window_start = EXCLUDED.window_start`, bucket)
	return err
}

// RateLimitIncrement adds one hit to an existing bucket.
func (s *Store) RateLimitIncrement(ctx context.Context, bucket string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE api_rate_limits SET hits = hits + 1 WHERE bucket = $1`, bucket)
	return err
}

// RateLimitCleanup removes buckets older than one day.
func (s *Store) RateLimitCleanup(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM api_rate_limits WHERE window_start < (NOW() - INTERVAL '1 day')`)
	return err
}
