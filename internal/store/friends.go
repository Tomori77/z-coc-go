package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// friendProfileColumns safely extracts name/avatar from the settings JSON blob,
// guarding against non-JSON settings with a LIKE prefix check.
const friendProfileColumns = `CASE WHEN ud.settings LIKE '{%' THEN ud.settings::jsonb ->> 'name' END,
	CASE WHEN ud.settings LIKE '{%' THEN ud.settings::jsonb ->> 'avatar' END`

// FriendProfile is a user's public identity (uuid + optional name/avatar).
type FriendProfile struct {
	UUID   string
	Name   string
	Avatar string
}

// FriendRequestRow is a pending incoming friend request with its sender profile.
type FriendRequestRow struct {
	ID        int64
	FromUUID  string
	Name      string
	Avatar    string
	CreatedAt int64
}

// FriendInviteRow is a pending incoming room invite with its sender profile.
type FriendInviteRow struct {
	ID        int64
	FromUUID  string
	Name      string
	Avatar    string
	RoomID    string
	CreatedAt int64
}

// orderedPair normalizes a friendship key so user_a < user_b bytewise.
func orderedPair(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}

// FindUserProfileByUUID returns the caller-visible profile for a user.
func (s *Store) FindUserProfileByUUID(ctx context.Context, uuid string) (FriendProfile, bool, error) {
	row := s.Pool.QueryRow(ctx,
		`SELECT u.user_uuid, `+friendProfileColumns+`
		 FROM users u LEFT JOIN user_data ud ON ud.user_id = u.user_uuid
		 WHERE u.user_uuid = $1 LIMIT 1`, uuid)
	var profile FriendProfile
	var name, avatar *string
	err := row.Scan(&profile.UUID, &name, &avatar)
	if errors.Is(err, pgx.ErrNoRows) {
		return FriendProfile{}, false, nil
	}
	if err != nil {
		return FriendProfile{}, false, err
	}
	if name != nil {
		profile.Name = *name
	}
	if avatar != nil {
		profile.Avatar = *avatar
	}
	return profile, true, nil
}

// UpsertUserPresence records the caller's last-seen time.
func (s *Store) UpsertUserPresence(ctx context.Context, uuid string) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO coc_user_presence (user_uuid, last_seen) VALUES ($1, NOW())
		 ON CONFLICT (user_uuid) DO UPDATE SET last_seen = EXCLUDED.last_seen`, uuid)
	return err
}

// UserPresenceEpochs returns last_seen epoch seconds keyed by user uuid.
func (s *Store) UserPresenceEpochs(ctx context.Context, uuids []string) (map[string]int64, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT user_uuid, EXTRACT(EPOCH FROM last_seen) AS last_seen FROM coc_user_presence WHERE user_uuid = ANY($1)`, uuids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]int64)
	for rows.Next() {
		var uuid string
		var lastSeen float64
		if err := rows.Scan(&uuid, &lastSeen); err != nil {
			return nil, err
		}
		out[uuid] = int64(lastSeen)
	}
	return out, rows.Err()
}

// AreFriends reports whether two users share a friendship.
func (s *Store) AreFriends(ctx context.Context, a, b string) (bool, error) {
	userA, userB := orderedPair(a, b)
	var one int
	err := s.Pool.QueryRow(ctx,
		`SELECT 1 FROM coc_friendships WHERE user_a = $1 AND user_b = $2 LIMIT 1`, userA, userB).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// HasPendingRequest reports whether from has an outstanding pending request to to.
func (s *Store) HasPendingRequest(ctx context.Context, from, to string) (bool, error) {
	var one int
	err := s.Pool.QueryRow(ctx,
		`SELECT 1 FROM coc_friend_requests WHERE from_uuid = $1 AND to_uuid = $2 AND status = 'pending' LIMIT 1`,
		from, to).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// CreateFriendRequest inserts a pending friend request.
func (s *Store) CreateFriendRequest(ctx context.Context, from, to string) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO coc_friend_requests (from_uuid, to_uuid) VALUES ($1, $2)`, from, to)
	return err
}

// ListIncomingRequests returns pending requests addressed to to, with sender profiles.
func (s *Store) ListIncomingRequests(ctx context.Context, to string) ([]FriendRequestRow, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT r.id, r.from_uuid, `+friendProfileColumns+`, EXTRACT(EPOCH FROM r.created_at) AS created_at
		 FROM coc_friend_requests r
		 LEFT JOIN users u ON u.user_uuid = r.from_uuid
		 LEFT JOIN user_data ud ON ud.user_id = u.user_uuid
		 WHERE r.to_uuid = $1 AND r.status = 'pending'
		 ORDER BY r.id ASC`, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FriendRequestRow
	for rows.Next() {
		var row FriendRequestRow
		var name, avatar *string
		var createdAt float64
		if err := rows.Scan(&row.ID, &row.FromUUID, &name, &avatar, &createdAt); err != nil {
			return nil, err
		}
		if name != nil {
			row.Name = *name
		}
		if avatar != nil {
			row.Avatar = *avatar
		}
		row.CreatedAt = int64(createdAt)
		out = append(out, row)
	}
	return out, rows.Err()
}

// AcceptFriendRequest atomically accepts a pending request addressed to to and
// creates the friendship (normalized user_a < user_b).
func (s *Store) AcceptFriendRequest(ctx context.Context, id int64, to string) (from string, ok bool, err error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback(ctx)

	var fromUUID, status string
	err = tx.QueryRow(ctx,
		`SELECT from_uuid, status FROM coc_friend_requests WHERE id = $1 AND to_uuid = $2 FOR UPDATE`,
		id, to).Scan(&fromUUID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if status != "pending" {
		return "", false, nil
	}

	userA, userB := orderedPair(fromUUID, to)
	if _, err = tx.Exec(ctx,
		`INSERT INTO coc_friendships (user_a, user_b) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userA, userB); err != nil {
		return "", false, err
	}
	if _, err = tx.Exec(ctx,
		`UPDATE coc_friend_requests SET status = 'accepted', updated_at = NOW() WHERE id = $1`, id); err != nil {
		return "", false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, err
	}
	return fromUUID, true, nil
}

// RejectFriendRequest rejects a pending request addressed to to.
func (s *Store) RejectFriendRequest(ctx context.Context, id int64, to string) (bool, error) {
	tag, err := s.Pool.Exec(ctx,
		`UPDATE coc_friend_requests SET status = 'rejected', updated_at = NOW() WHERE id = $1 AND to_uuid = $2 AND status = 'pending'`,
		id, to)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ListFriendUUIDs returns the other side of every friendship involving uuid.
func (s *Store) ListFriendUUIDs(ctx context.Context, uuid string) ([]string, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT CASE WHEN user_a = $1 THEN user_b ELSE user_a END AS friend_uuid
		 FROM coc_friendships WHERE user_a = $1 OR user_b = $1`, uuid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var friendUUID string
		if err := rows.Scan(&friendUUID); err != nil {
			return nil, err
		}
		out = append(out, friendUUID)
	}
	return out, rows.Err()
}

// CreateFriendInvite inserts a pending room invite.
func (s *Store) CreateFriendInvite(ctx context.Context, from, to, roomID string) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO coc_friend_invites (from_uuid, to_uuid, room_id) VALUES ($1, $2, $3)`, from, to, roomID)
	return err
}

// ListIncomingInvites returns pending invites addressed to to, with sender profiles.
func (s *Store) ListIncomingInvites(ctx context.Context, to string) ([]FriendInviteRow, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT i.id, i.from_uuid, `+friendProfileColumns+`, i.room_id, EXTRACT(EPOCH FROM i.created_at) AS created_at
		 FROM coc_friend_invites i
		 LEFT JOIN users u ON u.user_uuid = i.from_uuid
		 LEFT JOIN user_data ud ON ud.user_id = u.user_uuid
		 WHERE i.to_uuid = $1 AND i.status = 'pending'
		 ORDER BY i.id ASC`, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FriendInviteRow
	for rows.Next() {
		var row FriendInviteRow
		var name, avatar *string
		var createdAt float64
		if err := rows.Scan(&row.ID, &row.FromUUID, &name, &avatar, &row.RoomID, &createdAt); err != nil {
			return nil, err
		}
		if name != nil {
			row.Name = *name
		}
		if avatar != nil {
			row.Avatar = *avatar
		}
		row.CreatedAt = int64(createdAt)
		out = append(out, row)
	}
	return out, rows.Err()
}

// RespondInvite accepts or rejects a pending invite addressed to to.
func (s *Store) RespondInvite(ctx context.Context, id int64, to string, accept bool) (bool, error) {
	status := "rejected"
	if accept {
		status = "accepted"
	}
	tag, err := s.Pool.Exec(ctx,
		`UPDATE coc_friend_invites SET status = $1, updated_at = NOW() WHERE id = $2 AND to_uuid = $3 AND status = 'pending'`,
		status, id, to)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
