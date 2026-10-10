package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// UserDataRow is a single user_data record. JSON columns are returned as raw
// text so the caller can decode them with PHP-compatible semantics.
type UserDataRow struct {
	Settings      *string
	Investigators *string
	Saves         *string
	TavernNovels  *string
	Revision      int64
	UpdatedAt     *string
}

// GetUserData returns the cloud snapshot for a user, or nil when absent.
func (s *Store) GetUserData(ctx context.Context, userID string) (*UserDataRow, error) {
	row := s.Pool.QueryRow(ctx,
		`SELECT settings, investigators, saves, tavern_novels, revision, updated_at::text
		 FROM user_data WHERE user_id = $1 LIMIT 1`, userID)
	var r UserDataRow
	err := row.Scan(&r.Settings, &r.Investigators, &r.Saves, &r.TavernNovels, &r.Revision, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// SyncResult reports the outcome of a sync transaction.
type SyncResult struct {
	Revision  int64
	UpdatedAt *string
	Conflict  bool
}

// SyncUserData performs the optimistic-lock write from db_api.php sync. It uses
// SELECT ... FOR UPDATE inside a transaction and returns Conflict=true (with the
// current revision/updated_at) when baseRevision does not match.
func (s *Store) SyncUserData(ctx context.Context, userID, settings, investigators, saves, tavernNovels string, baseRevision int64) (SyncResult, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return SyncResult{}, err
	}
	defer tx.Rollback(ctx)

	var currentRevision int64
	var currentUpdated *string
	err = tx.QueryRow(ctx,
		`SELECT revision, updated_at::text FROM user_data WHERE user_id = $1 FOR UPDATE`, userID).
		Scan(&currentRevision, &currentUpdated)
	exists := true
	if errors.Is(err, pgx.ErrNoRows) {
		exists = false
		currentRevision = 0
		currentUpdated = nil
		err = nil
	}
	if err != nil {
		return SyncResult{}, err
	}

	if baseRevision != currentRevision {
		_ = tx.Rollback(ctx)
		return SyncResult{Revision: currentRevision, UpdatedAt: currentUpdated, Conflict: true}, nil
	}

	nextRevision := currentRevision + 1
	if exists {
		_, err = tx.Exec(ctx,
			`UPDATE user_data SET settings = $1, investigators = $2, saves = $3, tavern_novels = $4, revision = $5, updated_at = NOW() WHERE user_id = $6`,
			settings, investigators, saves, tavernNovels, nextRevision, userID)
	} else {
		_, err = tx.Exec(ctx,
			`INSERT INTO user_data (user_id, settings, investigators, saves, tavern_novels, revision) VALUES ($1, $2, $3, $4, $5, $6)`,
			userID, settings, investigators, saves, tavernNovels, nextRevision)
	}
	if err != nil {
		return SyncResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SyncResult{}, err
	}
	return SyncResult{Revision: nextRevision}, nil
}
