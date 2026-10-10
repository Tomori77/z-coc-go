package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

// PublicMember is one online participant of a lobby room (for avatars).
type PublicMember struct {
	UUID   string
	Name   string
	Avatar string
	IsHost bool
}

const (
	// maxLobbyMembers caps how many members are attached to one lobby entry.
	maxLobbyMembers = 8
	// maxLobbyAvatarChars drops oversized avatars from lobby payloads.
	maxLobbyAvatarChars = 40000
)

// PublicRoom is a visible lobby entry joined with the host's public profile.
type PublicRoom struct {
	RoomID      string
	HostID      string
	HostName    string
	HostAvatar  string
	MemberCount int64
	ModuleName  string
	Name        string
	Members     []PublicMember
	CreatedAt   int64
	UpdatedAt   int64
}

// ListPublicRooms returns active public rooms whose host is currently online,
// newest first (max 60), each with its current module and online members.
func (s *Store) ListPublicRooms(ctx context.Context, ttlHours int) ([]PublicRoom, error) {
	// friendProfileColumns embeds a literal '%', so it must not pass through
	// fmt.Sprintf; format the interval fragment separately instead.
	interval := fmt.Sprintf(`NOW() - INTERVAL '%d hours'`, ttlHours)
	rows, err := s.Pool.Query(ctx,
		`SELECT r.room_id, r.host_id, `+friendProfileColumns+`,
			(SELECT COUNT(*) FROM coc_room_presence p WHERE p.room_id = r.room_id AND p.last_seen >= NOW() - INTERVAL '120 seconds') AS member_count,
			r.name,
			EXTRACT(EPOCH FROM r.created_at) AS created_at,
			EXTRACT(EPOCH FROM r.updated_at) AS updated_at
		 FROM coc_rooms r
		 LEFT JOIN users u ON u.user_uuid = r.host_id
		 LEFT JOIN user_data ud ON ud.user_id = u.user_uuid
		 WHERE r.is_public = TRUE AND r.updated_at >= (`+interval+`)
		   AND EXISTS (SELECT 1 FROM coc_room_presence hp WHERE hp.room_id = r.room_id AND hp.user_id = r.host_id AND hp.last_seen >= NOW() - INTERVAL '120 seconds')
		 ORDER BY r.created_at DESC LIMIT 60`)
	if err != nil {
		return nil, err
	}

	var out []PublicRoom
	customNames := make([]*string, 0)
	index := make(map[string]int)
	for rows.Next() {
		var room PublicRoom
		var name, avatar, customName *string
		var createdAt, updatedAt float64
		if err := rows.Scan(&room.RoomID, &room.HostID, &name, &avatar, &room.MemberCount, &customName, &createdAt, &updatedAt); err != nil {
			rows.Close()
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
		index[room.RoomID] = len(out)
		out = append(out, room)
		customNames = append(customNames, customName)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}

	roomIDs := make([]string, 0, len(out))
	for _, room := range out {
		roomIDs = append(roomIDs, room.RoomID)
	}

	if err := s.loadPublicMembers(ctx, roomIDs, index, out); err != nil {
		return nil, err
	}
	if err := s.loadPublicModuleNames(ctx, roomIDs, index, out); err != nil {
		return nil, err
	}
	// A room's display name is the host's custom name when set, otherwise it
	// follows the latest announced module name.
	for i := range out {
		if customNames[i] != nil {
			if custom := strings.TrimSpace(*customNames[i]); custom != "" {
				out[i].Name = custom
				continue
			}
		}
		out[i].Name = out[i].ModuleName
	}
	return out, nil
}

// loadPublicMembers attaches up to maxLobbyMembers online participants to each
// room, host first. Oversized avatars are dropped to keep lobby polls small.
func (s *Store) loadPublicMembers(ctx context.Context, roomIDs []string, index map[string]int, out []PublicRoom) error {
	rows, err := s.Pool.Query(ctx,
		`SELECT p.room_id, p.user_id, `+friendProfileColumns+`, (p.user_id = r.host_id) AS is_host
		 FROM coc_room_presence p
		 JOIN coc_rooms r ON r.room_id = p.room_id
		 LEFT JOIN users u ON u.user_uuid = p.user_id
		 LEFT JOIN user_data ud ON ud.user_id = u.user_uuid
		 WHERE p.room_id = ANY($1) AND p.last_seen >= NOW() - INTERVAL '120 seconds'
		 ORDER BY p.room_id, is_host DESC, p.last_seen DESC`, roomIDs)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var roomID string
		var member PublicMember
		var name, avatar *string
		if err := rows.Scan(&roomID, &member.UUID, &name, &avatar, &member.IsHost); err != nil {
			return err
		}
		i, ok := index[roomID]
		if !ok || len(out[i].Members) >= maxLobbyMembers {
			continue
		}
		if name != nil {
			member.Name = *name
		}
		if avatar != nil {
			member.Avatar = *avatar
		}
		if len(member.Avatar) > maxLobbyAvatarChars {
			member.Avatar = ""
		}
		out[i].Members = append(out[i].Members, member)
	}
	return rows.Err()
}

// loadPublicModuleNames attaches the latest announced module name per room.
func (s *Store) loadPublicModuleNames(ctx context.Context, roomIDs []string, index map[string]int, out []PublicRoom) error {
	rows, err := s.Pool.Query(ctx,
		`SELECT DISTINCT ON (m.room_id) m.room_id,
		   CASE WHEN m.payload LIKE '{%' THEN m.payload::jsonb ->> 'moduleName' END AS module_name
		 FROM coc_room_messages m
		 WHERE m.room_id = ANY($1) AND m.payload LIKE '%"moduleName"%'
		 ORDER BY m.room_id, m.id DESC`, roomIDs)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var roomID string
		var moduleName *string
		if err := rows.Scan(&roomID, &moduleName); err != nil {
			return err
		}
		if moduleName == nil {
			continue
		}
		if i, ok := index[roomID]; ok {
			out[i].ModuleName = *moduleName
		}
	}
	return rows.Err()
}

// SetRoomName stores a host-chosen display name; an empty name reverts the
// room to its automatic module name.
func (s *Store) SetRoomName(ctx context.Context, roomID, name string) error {
	_, err := s.Pool.Exec(ctx,
		`UPDATE coc_rooms SET name = NULLIF($2, ''), updated_at = NOW() WHERE room_id = $1`,
		roomID, name)
	return err
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

// CloseRoom deletes a room and its related messages and presence rows in a
// single transaction, so the room disappears immediately instead of waiting
// for TTL cleanup.
func (s *Store) CloseRoom(ctx context.Context, roomID string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM coc_room_messages WHERE room_id = $1`, roomID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM coc_room_presence WHERE room_id = $1`, roomID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM coc_rooms WHERE room_id = $1`, roomID); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
	if _, err := s.Pool.Exec(ctx,
		`DELETE FROM coc_rooms r WHERE r.is_public = TRUE AND NOT EXISTS (
		   SELECT 1 FROM coc_room_presence p
		   WHERE p.room_id = r.room_id AND p.user_id = r.host_id
		     AND p.last_seen >= NOW() - INTERVAL '10 minutes')`); err != nil {
		return err
	}
	_, err := s.Pool.Exec(ctx,
		`DELETE FROM coc_room_presence p WHERE NOT EXISTS (SELECT 1 FROM coc_rooms r WHERE r.room_id = p.room_id)`)
	return err
}
