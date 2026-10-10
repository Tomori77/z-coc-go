package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Room is a coc_rooms row.
type Room struct {
	RoomID        string
	HostID        string
	HostTokenHash *string
}

// RoomExistsActive reports whether a non-expired room with roomID exists.
func (s *Store) RoomExistsActive(ctx context.Context, roomID string, ttlHours int) (bool, error) {
	var existing string
	err := s.Pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT room_id FROM coc_rooms WHERE room_id = $1 AND updated_at >= (NOW() - INTERVAL '%d hours') LIMIT 1`, ttlHours),
		roomID).Scan(&existing)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// UpsertRoom inserts or refreshes a room, matching create's ON CONFLICT clause.
func (s *Store) UpsertRoom(ctx context.Context, roomID, hostID, hostTokenHash string, isPublic bool) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO coc_rooms (room_id, host_id, host_token_hash, is_public, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, NOW(), NOW())
		 ON CONFLICT (room_id) DO UPDATE SET
		   host_id = EXCLUDED.host_id,
		   host_token_hash = EXCLUDED.host_token_hash,
		   is_public = EXCLUDED.is_public,
		   created_at = EXCLUDED.created_at,
		   updated_at = EXCLUDED.updated_at`,
		roomID, hostID, hostTokenHash, isPublic)
	return err
}

// PublicRoom is a visible lobby entry joined with the host's public profile.
type PublicRoom struct {
	RoomID      string
	HostID      string
	HostName    string
	HostAvatar  string
	MemberCount int64
	CreatedAt   int64
	UpdatedAt   int64
}

// ListPublicRooms returns active public rooms, newest first (max 100).
func (s *Store) ListPublicRooms(ctx context.Context, ttlHours int) ([]PublicRoom, error) {
	// friendProfileColumns embeds a literal '%', so it must not pass through
	// fmt.Sprintf; format the interval fragment separately instead.
	interval := fmt.Sprintf(`NOW() - INTERVAL '%d hours'`, ttlHours)
	rows, err := s.Pool.Query(ctx,
		`SELECT r.room_id, r.host_id, `+friendProfileColumns+`,
			(SELECT COUNT(*) FROM coc_room_presence p WHERE p.room_id = r.room_id AND p.last_seen >= NOW() - INTERVAL '120 seconds') AS member_count,
			EXTRACT(EPOCH FROM r.created_at) AS created_at,
			EXTRACT(EPOCH FROM r.updated_at) AS updated_at
		 FROM coc_rooms r
		 LEFT JOIN users u ON u.user_uuid = r.host_id
		 LEFT JOIN user_data ud ON ud.user_id = u.user_uuid
		 WHERE r.is_public = TRUE AND r.updated_at >= (`+interval+`)
		 ORDER BY r.created_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PublicRoom
	for rows.Next() {
		var room PublicRoom
		var name, avatar *string
		var createdAt, updatedAt float64
		if err := rows.Scan(&room.RoomID, &room.HostID, &name, &avatar, &room.MemberCount, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if name != nil {
			room.HostName = *name
		}
		if avatar != nil {
			room.HostAvatar = *avatar
		}
		room.CreatedAt = int64(createdAt)
		room.UpdatedAt = int64(updatedAt)
		out = append(out, room)
	}
	return out, rows.Err()
}

// SetRoomPublic toggles a room's lobby visibility.
func (s *Store) SetRoomPublic(ctx context.Context, roomID string, isPublic bool) error {
	_, err := s.Pool.Exec(ctx,
		`UPDATE coc_rooms SET is_public = $2, updated_at = NOW() WHERE room_id = $1`,
		roomID, isPublic)
	return err
}

// DeleteRoomMessages clears a room's message log.
func (s *Store) DeleteRoomMessages(ctx context.Context, roomID string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM coc_room_messages WHERE room_id = $1`, roomID)
	return err
}

// GetActiveRoom returns a non-expired room, or nil when missing/expired.
func (s *Store) GetActiveRoom(ctx context.Context, roomID string, ttlHours int) (*Room, error) {
	row := s.Pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT room_id, host_id, host_token_hash FROM coc_rooms WHERE room_id = $1 AND updated_at >= (NOW() - INTERVAL '%d hours') LIMIT 1`, ttlHours),
		roomID)
	var r Room
	err := row.Scan(&r.RoomID, &r.HostID, &r.HostTokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// TouchRoom bumps updated_at at most once every 30 seconds.
func (s *Store) TouchRoom(ctx context.Context, roomID string) error {
	_, err := s.Pool.Exec(ctx,
		`UPDATE coc_rooms SET updated_at = NOW() WHERE room_id = $1 AND updated_at < (NOW() - INTERVAL '30 seconds')`,
		roomID)
	return err
}

// MaxMessageID returns the highest message id for a room (0 when empty).
func (s *Store) MaxMessageID(ctx context.Context, roomID string) (int64, error) {
	var lastID int64
	err := s.Pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(id), 0) AS last_id FROM coc_room_messages WHERE room_id = $1`, roomID).Scan(&lastID)
	if err != nil {
		return 0, err
	}
	return lastID, nil
}

// Message is a stored room message.
type Message struct {
	ID       int64
	SenderID string
	Payload  string
}

// ListMessages returns up to limit messages with id > since, ascending.
func (s *Store) ListMessages(ctx context.Context, roomID string, since int64, limit int) ([]Message, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, sender_id, payload FROM coc_room_messages WHERE room_id = $1 AND id > $2 ORDER BY id ASC LIMIT $3`,
		roomID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SenderID, &m.Payload); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

// InsertMessage stores a message and returns its id.
func (s *Store) InsertMessage(ctx context.Context, roomID, senderID, payload string) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx,
		`INSERT INTO coc_room_messages (room_id, sender_id, payload) VALUES ($1, $2, $3) RETURNING id`,
		roomID, senderID, payload).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// PruneMessages deletes messages beyond the newest maxMessages for a room.
func (s *Store) PruneMessages(ctx context.Context, roomID string, maxMessages int) error {
	var cutoffID int64
	err := s.Pool.QueryRow(ctx,
		`SELECT id FROM coc_room_messages WHERE room_id = $1 ORDER BY id DESC LIMIT 1 OFFSET $2`,
		roomID, maxMessages).Scan(&cutoffID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `DELETE FROM coc_room_messages WHERE room_id = $1 AND id <= $2`, roomID, cutoffID)
	return err
}

// CleanupRooms removes stale messages, rooms and orphaned presence rows.
func (s *Store) CleanupRooms(ctx context.Context, ttlHours int) error {
	if _, err := s.Pool.Exec(ctx,
		fmt.Sprintf(`DELETE FROM coc_room_messages m WHERE NOT EXISTS (SELECT 1 FROM coc_rooms r WHERE r.room_id = m.room_id) OR m.created_at < (NOW() - INTERVAL '%d hours')`, ttlHours)); err != nil {
		return err
	}
	if _, err := s.Pool.Exec(ctx,
		fmt.Sprintf(`DELETE FROM coc_rooms WHERE updated_at < (NOW() - INTERVAL '%d hours')`, ttlHours)); err != nil {
		return err
	}
	_, err := s.Pool.Exec(ctx,
		`DELETE FROM coc_room_presence p WHERE NOT EXISTS (SELECT 1 FROM coc_rooms r WHERE r.room_id = p.room_id)`)
	return err
}
