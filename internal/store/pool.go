package store

import (
	"context"
	"fmt"
	"net"
	"net/url"

	"github.com/Tomori77/z-coc/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps a pgx connection pool and exposes per-domain query helpers.
type Store struct {
	Pool *pgxpool.Pool
}

// NewPool builds a pgxpool for the configured PostgreSQL database. It mirrors
// lib/Database.php: the connection timezone is forced to UTC so TIMESTAMP
// columns line up with PHP's time()/EXTRACT(EPOCH ...) semantics.
func NewPool(ctx context.Context, cfg *config.Config) (*Store, error) {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.DB.User, cfg.DB.Password),
		Host:   net.JoinHostPort(cfg.DB.Host, cfg.DB.Port),
		Path:   "/" + cfg.DB.Database,
	}
	query := u.Query()
	query.Set("sslmode", "prefer")
	u.RawQuery = query.Encode()

	poolCfg, err := pgxpool.ParseConfig(u.String())
	if err != nil {
		return nil, fmt.Errorf("parse database config: %w", err)
	}
	if poolCfg.ConnConfig.RuntimeParams == nil {
		poolCfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	poolCfg.ConnConfig.RuntimeParams["timezone"] = "UTC"

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect database: %w", err)
	}
	return &Store{Pool: pool}, nil
}

// Close releases the underlying pool.
func (s *Store) Close() { s.Pool.Close() }
