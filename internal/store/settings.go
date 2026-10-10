package store

import "context"

// GetAppSettings returns every stored app-settings override, keyed by name.
// Keys absent from the map fall back to their Go-side defaults.
func (s *Store) GetAppSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.Pool.Query(ctx, `SELECT key, value FROM coc_app_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, rows.Err()
}

// UpsertAppSettings writes the given overrides inside a single transaction,
// inserting or updating each key. An empty map is a no-op.
func (s *Store) UpsertAppSettings(ctx context.Context, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for key, value := range values {
		if _, err := tx.Exec(ctx,
			`INSERT INTO coc_app_settings (key, value, updated_at) VALUES ($1, $2, NOW())
			 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`,
			key, value); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
