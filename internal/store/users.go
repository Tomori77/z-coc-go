package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// CreateUser inserts a new account. A unique-violation on email is surfaced so
// the caller can return 409, matching db_api.php register.
func (s *Store) CreateUser(ctx context.Context, email, passwordHash, userUUID, tokenHash string) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO users (email, password, user_uuid, auth_token) VALUES ($1, $2, $3, $4)`,
		email, passwordHash, userUUID, tokenHash)
	return err
}

// IsUniqueViolation reports whether err is a PostgreSQL unique_violation (23505).
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

// FindUserByEmail returns the stored password hash and uuid for an email.
func (s *Store) FindUserByEmail(ctx context.Context, email string) (passwordHash, userUUID string, found bool, err error) {
	row := s.Pool.QueryRow(ctx, `SELECT password, user_uuid FROM users WHERE email = $1 LIMIT 1`, email)
	err = row.Scan(&passwordHash, &userUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return passwordHash, userUUID, true, nil
}

// GetUserRoleByUUID returns a user's role, defaulting to "user" when the stored
// role is empty, and whether the account exists.
func (s *Store) GetUserRoleByUUID(ctx context.Context, userUUID string) (role string, found bool, err error) {
	row := s.Pool.QueryRow(ctx, `SELECT role FROM users WHERE user_uuid = $1 LIMIT 1`, userUUID)
	err = row.Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if role == "" {
		role = "user"
	}
	return role, true, nil
}

// FindUserByToken looks up a user by uuid accepting either the hashed or the
// legacy plaintext token, matching require_user_token().
func (s *Store) FindUserByToken(ctx context.Context, userUUID, tokenHash, token string) (id int64, storedToken string, found bool, err error) {
	var stored *string
	row := s.Pool.QueryRow(ctx,
		`SELECT id, auth_token FROM users WHERE user_uuid = $1 AND (auth_token = $2 OR auth_token = $3) LIMIT 1`,
		userUUID, tokenHash, token)
	err = row.Scan(&id, &stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", false, nil
	}
	if err != nil {
		return 0, "", false, err
	}
	if stored != nil {
		storedToken = *stored
	}
	return id, storedToken, true, nil
}

// FindUserUUIDByToken resolves a user uuid from a bearer token, accepting
// either the sha256 hash or the legacy plaintext token. It matches the query
// semantics of require_user_token() without knowing the user upfront.
func (s *Store) FindUserUUIDByToken(ctx context.Context, tokenHash, token string) (uuid string, found bool, err error) {
	row := s.Pool.QueryRow(ctx,
		`SELECT user_uuid FROM users WHERE auth_token = $1 OR auth_token = $2 LIMIT 1`,
		tokenHash, token)
	err = row.Scan(&uuid)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return uuid, true, nil
}

// UpdateAuthToken stores the sha256 hash of the current token.
func (s *Store) UpdateAuthToken(ctx context.Context, userUUID, tokenHash string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE users SET auth_token = $1 WHERE user_uuid = $2`, tokenHash, userUUID)
	return err
}

// ClearAuthToken nulls the stored token, matching logout.
func (s *Store) ClearAuthToken(ctx context.Context, userUUID string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE users SET auth_token = NULL WHERE user_uuid = $1`, userUUID)
	return err
}
