package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ModuleRow is a coc_library_modules row. Nullable columns use pointers.
type ModuleRow struct {
	ID             int64
	CreatedAt      string
	Title          string
	Description    *string
	Content        *string
	AuthorName     *string
	AuthorID       string
	OwnerTokenHash string
	Type           string
	Downloads      int64
	MinPlayers     *int64
	MaxPlayers     *int64
	ContentLength  int64
}

const moduleColumns = `id, created_at::text, title, description, author_name, author_id, type, downloads, min_players, max_players, CHAR_LENGTH(content) AS content_length`

func scanModule(scan func(dest ...any) error) (ModuleRow, error) {
	var m ModuleRow
	err := scan(&m.ID, &m.CreatedAt, &m.Title, &m.Description, &m.AuthorName, &m.AuthorID,
		&m.Type, &m.Downloads, &m.MinPlayers, &m.MaxPlayers, &m.ContentLength)
	return m, err
}

// ListReadingModules returns original/reprint modules, optionally filtered by
// a case-insensitive title match.
func (s *Store) ListReadingModules(ctx context.Context, query string, limit int) ([]ModuleRow, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if query != "" {
		rows, err = s.Pool.Query(ctx,
			`SELECT `+moduleColumns+` FROM coc_library_modules
			 WHERE type IN ('original', 'reprint') AND title ILIKE $1
			 ORDER BY created_at DESC LIMIT $2`, "%"+query+"%", limit)
	} else {
		rows, err = s.Pool.Query(ctx,
			`SELECT `+moduleColumns+` FROM coc_library_modules
			 WHERE type IN ('original', 'reprint')
			 ORDER BY created_at DESC LIMIT $1`, limit)
	}
	if err != nil {
		return nil, err
	}
	return collectModules(rows)
}

// ListRestrictedModules returns ai_adapt modules plus the caller's own
// ai_adapt_private modules, optionally filtered by title.
func (s *Store) ListRestrictedModules(ctx context.Context, userID, ownerHash, query string, limit int) ([]ModuleRow, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if query != "" {
		rows, err = s.Pool.Query(ctx,
			`SELECT `+moduleColumns+` FROM coc_library_modules
			 WHERE (type = 'ai_adapt' OR (type = 'ai_adapt_private' AND author_id = $1 AND owner_token_hash = $2)) AND title ILIKE $3
			 ORDER BY created_at DESC LIMIT $4`, userID, ownerHash, "%"+query+"%", limit)
	} else {
		rows, err = s.Pool.Query(ctx,
			`SELECT `+moduleColumns+` FROM coc_library_modules
			 WHERE type = 'ai_adapt' OR (type = 'ai_adapt_private' AND author_id = $1 AND owner_token_hash = $2)
			 ORDER BY created_at DESC LIMIT $3`, userID, ownerHash, limit)
	}
	if err != nil {
		return nil, err
	}
	return collectModules(rows)
}

func collectModules(rows pgx.Rows) ([]ModuleRow, error) {
	defer rows.Close()
	var modules []ModuleRow
	for rows.Next() {
		m, err := scanModule(rows.Scan)
		if err != nil {
			return nil, err
		}
		modules = append(modules, m)
	}
	return modules, rows.Err()
}

// GetModule fetches a single module (including content), or nil if absent.
func (s *Store) GetModule(ctx context.Context, id int64) (*ModuleRow, error) {
	row := s.Pool.QueryRow(ctx,
		`SELECT id, created_at::text, title, description, content, author_name, author_id, owner_token_hash, type, downloads, min_players, max_players, CHAR_LENGTH(content) AS content_length
		 FROM coc_library_modules WHERE id = $1 LIMIT 1`, id)
	var m ModuleRow
	err := row.Scan(&m.ID, &m.CreatedAt, &m.Title, &m.Description, &m.Content, &m.AuthorName, &m.AuthorID,
		&m.OwnerTokenHash, &m.Type, &m.Downloads, &m.MinPlayers, &m.MaxPlayers, &m.ContentLength)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// CreateModule inserts a module and returns its new id.
func (s *Store) CreateModule(ctx context.Context, title, description, content, authorName, authorID, ownerHash, moduleType string, minPlayers, maxPlayers *int64) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx,
		`INSERT INTO coc_library_modules (title, description, content, author_name, author_id, owner_token_hash, type, min_players, max_players)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		title, description, content, authorName, authorID, ownerHash, moduleType, minPlayers, maxPlayers).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// ModuleOwner returns just the author id and owner token hash for a module.
func (s *Store) ModuleOwner(ctx context.Context, id int64) (authorID, ownerHash string, found bool, err error) {
	row := s.Pool.QueryRow(ctx, `SELECT author_id, owner_token_hash FROM coc_library_modules WHERE id = $1 LIMIT 1`, id)
	err = row.Scan(&authorID, &ownerHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return strings.TrimRight(authorID, " "), strings.TrimRight(ownerHash, " "), true, nil
}

// DeleteModule removes a module by id.
func (s *Store) DeleteModule(ctx context.Context, id int64) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM coc_library_modules WHERE id = $1`, id)
	return err
}

// IncrementDownloads bumps the download counter.
func (s *Store) IncrementDownloads(ctx context.Context, id int64) error {
	_, err := s.Pool.Exec(ctx, `UPDATE coc_library_modules SET downloads = downloads + 1 WHERE id = $1`, id)
	return err
}
