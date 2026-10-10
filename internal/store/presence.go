package store

import "context"

// Presence is a coc_room_presence row with its last_seen epoch seconds.
type Presence struct {
	UserID string
	SeenAt int64
}

// UpsertPresence records the caller's last-seen time.
func (s *Store) UpsertPresence(ctx context.Context, roomID, userID string) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO coc_room_presence (room_id, user_id, last_seen) VALUES ($1, $2, NOW())
		 ON CONFLICT (room_id, user_id) DO UPDATE SET last_seen = EXCLUDED.last_seen`,
		roomID, userID)
	return err
}

// ListPresence returns every presence row for a room.
func (s *Store) ListPresence(ctx context.Context, roomID string) ([]Presence, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT user_id, EXTRACT(EPOCH FROM last_seen) AS seen_at FROM coc_room_presence WHERE room_id = $1`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Presence
	for rows.Next() {
		var userID string
		var seenAt float64
		if err := rows.Scan(&userID, &seenAt); err != nil {
			return nil, err
		}
		out = append(out, Presence{UserID: userID, SeenAt: int64(seenAt)})
	}
	return out, rows.Err()
}
