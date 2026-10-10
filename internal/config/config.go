package config

import (
	"errors"
	"net/url"
	"os"
	"strings"
)

// Defaults mirror the PHP config.example.php values used by the reference
// implementation (see lib/Config.php and the per-entrypoint config lookups).
const (
	DefaultMaxSyncBytes    = 16 * 1024 * 1024
	DefaultMaxContentBytes = 4 * 1024 * 1024
	DefaultRoomTTLHours    = 2
	DefaultMaxPayloadBytes = 512 * 1024
	DefaultMaxRoomMessages = 240
	DefaultPollLimit       = 80
	DefaultListLimit       = 60
)

// DBConfig holds PostgreSQL connection settings resolved from the environment.
type DBConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
	Driver   string
}

// Config is the resolved runtime configuration.
type Config struct {
	DB             DBConfig
	AllowedOrigins []string
	Port           string

	MaxSyncBytes    int64
	MaxContentBytes int64
	RoomTTLHours    int
	MaxPayloadBytes int64
	MaxRoomMessages int
	PollLimit       int
	ListLimit       int
}

type parsedURL struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
	Driver   string
}

// firstEnv returns the first non-empty value among names, matching the PHP
// app_first_env() helper (empty strings are skipped).
func firstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func parseDatabaseURL(raw string) parsedURL {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return parsedURL{}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return parsedURL{}
	}
	var p parsedURL
	switch strings.ToLower(u.Scheme) {
	case "postgres", "postgresql", "pgsql":
		p.Driver = "pgsql"
	default:
		p.Driver = strings.ToLower(u.Scheme)
	}
	if u.Hostname() != "" {
		p.Host = u.Hostname()
	}
	if u.Port() != "" {
		p.Port = u.Port()
	}
	if u.User != nil {
		p.User = u.User.Username()
		if pass, ok := u.User.Password(); ok {
			p.Password = pass
		}
	}
	if path := strings.TrimPrefix(u.Path, "/"); path != "" {
		p.Database = path
	}
	return p
}

// Load resolves configuration using the same precedence as lib/Config.php:
// PGSQL_* > DB_* per key, then DATABASE_URL for any key still unset.
func Load() *Config {
	db := DBConfig{
		Host:     firstEnv("PGSQL_HOST", "DB_HOST"),
		Port:     firstEnv("PGSQL_PORT", "DB_PORT"),
		User:     firstEnv("PGSQL_USERNAME", "PGSQL_USER", "DB_USER"),
		Password: firstEnv("PGSQL_PASSWORD", "DB_PASSWORD"),
		Database: firstEnv("PGSQL_DATABASE", "DB_NAME", "DB_DATABASE"),
		Driver:   "pgsql",
	}

	urlDB := parseDatabaseURL(firstEnv("DATABASE_URL"))
	if db.Host == "" {
		db.Host = urlDB.Host
	}
	if db.Port == "" {
		db.Port = urlDB.Port
	}
	if db.User == "" {
		db.User = urlDB.User
	}
	if db.Password == "" {
		db.Password = urlDB.Password
	}
	if db.Database == "" {
		db.Database = urlDB.Database
	}
	if urlDB.Driver != "" {
		db.Driver = urlDB.Driver
	}
	if db.Port == "" {
		db.Port = "5432"
	}

	var origins []string
	if raw, ok := os.LookupEnv("APP_ALLOWED_ORIGINS"); ok {
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				origins = append(origins, part)
			}
		}
	}

	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}

	return &Config{
		DB:              db,
		AllowedOrigins:  origins,
		Port:            port,
		MaxSyncBytes:    DefaultMaxSyncBytes,
		MaxContentBytes: DefaultMaxContentBytes,
		RoomTTLHours:    DefaultRoomTTLHours,
		MaxPayloadBytes: DefaultMaxPayloadBytes,
		MaxRoomMessages: DefaultMaxRoomMessages,
		PollLimit:       DefaultPollLimit,
		ListLimit:       DefaultListLimit,
	}
}

// Validate mirrors the guards in lib/Database.php before a connection is made.
func (c *Config) Validate() error {
	if c.DB.Host == "" || strings.HasPrefix(c.DB.Host, "your_database_") {
		return errors.New("database host is not configured")
	}
	if c.DB.User == "" || strings.HasPrefix(c.DB.User, "your_database_") {
		return errors.New("database user is not configured")
	}
	if c.DB.Database == "" || strings.HasPrefix(c.DB.Database, "your_database_") {
		return errors.New("database name is not configured")
	}
	return nil
}

// OriginAllowed reports whether origin is present in the CORS allow-list.
func (c *Config) OriginAllowed(origin string) bool {
	for _, allowed := range c.AllowedOrigins {
		if allowed == origin {
			return true
		}
	}
	return false
}
