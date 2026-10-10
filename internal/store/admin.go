package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// AdminUserRow is one account row for the admin user browser. It carries only
// metadata: no cloud-data bodies are ever selected.
type AdminUserRow struct {
	UUID         string
	Email        string
	Role         string
	Banned       bool
	CreatedAt    int64
	DataRevision int64
	HasData      bool
}

// AdminUserDetail is the metadata-only detail view of a single account. The
// byte sizes describe cloud-data columns without exposing their contents.
type AdminUserDetail struct {
	UUID               string
	Email              string
	Role               string
	Banned             bool
	CreatedAt          int64
	DataRevision       int64
	UpdatedAt          int64
	SettingsBytes      int64
	InvestigatorsBytes int64
	SavesBytes         int64
	TavernNovelsBytes  int64
}

// AdminRoom is one active room row joined with the host's public profile.
type AdminRoom struct {
	RoomID      string
	HostID      string
	HostName    string
	HostAvatar  string
	IsPublic    bool
	Name        string
	MemberCount int64
	CreatedAt   int64
	UpdatedAt   int64
}

// AdminModule is one library module row (including private ones).
type AdminModule struct {
	ID            int64
	Title         string
	Type          string
	AuthorID      string
	AuthorName    string
	Downloads     int64
	CreatedAt     int64
	ContentLength int64
}

// AuditRow is one coc_admin_audit entry joined with the admin's email.
type AuditRow struct {
	ID         int64
	AdminUUID  string
	AdminEmail string
	Action     string
	Target     string
	Detail     string
	IP         string
	CreatedAt  int64
}

// AdminOverview holds the aggregate counters shown on the dashboard.
type AdminOverview struct {
	Users       int64
	BannedUsers int64
	RoomsActive int64
	RoomsPublic int64
	Modules     int64
	Audits      int64
}

// PromoteAdminsByEmail grants the admin role to every account whose email is in
// emails and returns the number of rows updated. An empty list promotes nobody.
func (s *Store) PromoteAdminsByEmail(ctx context.Context, emails []string) (int64, error) {
	if len(emails) == 0 {
		return 0, nil
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE users SET role = 'admin' WHERE email = ANY($1)`, emails)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// SetUserRole updates a user's role (expected to be 'user' or 'admin').
func (s *Store) SetUserRole(ctx context.Context, userUUID, role string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE users SET role = $2 WHERE user_uuid = $1`, userUUID, role)
	return err
}

// SetUserBanned toggles the banned flag for a user.
func (s *Store) SetUserBanned(ctx context.Context, userUUID string, banned bool) error {
	_, err := s.Pool.Exec(ctx, `UPDATE users SET banned = $2 WHERE user_uuid = $1`, userUUID, banned)
	return err
}

// FindAuthUserByToken resolves a bearer token to its owner's uuid, role and ban
// state, accepting either the sha256 hash or the legacy plaintext token.
func (s *Store) FindAuthUserByToken(ctx context.Context, tokenHash, token string) (uuid, role string, banned bool, found bool, err error) {
	row := s.Pool.QueryRow(ctx,
		`SELECT user_uuid, role, banned FROM users WHERE auth_token = $1 OR auth_token = $2 LIMIT 1`,
		tokenHash, token)
	err = row.Scan(&uuid, &role, &banned)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, false, nil
	}
	if err != nil {
		return "", "", false, false, err
	}
	return uuid, role, banned, true, nil
}

// IsUserBannedByUUID reports the ban state for a user, and whether it exists.
func (s *Store) IsUserBannedByUUID(ctx context.Context, uuid string) (banned bool, found bool, err error) {
	row := s.Pool.QueryRow(ctx, `SELECT banned FROM users WHERE user_uuid = $1 LIMIT 1`, uuid)
	err = row.Scan(&banned)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return banned, true, nil
}

// ListUsersAdmin returns a page of accounts matching an optional search term
// (email substring or exact uuid), newest first, plus the total match count.
func (s *Store) ListUsersAdmin(ctx context.Context, q string, limit, offset int) ([]AdminUserRow, int64, error) {
	var total int64
	if err := s.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM users u
		 WHERE ($1 = '' OR u.email ILIKE '%' || $1 || '%' OR u.user_uuid = $1)`, q).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.Pool.Query(ctx,
		`SELECT u.user_uuid, u.email, u.role, u.banned,
		   EXTRACT(EPOCH FROM u.created_at)::bigint AS created_at,
		   COALESCE(ud.revision, 0) AS data_revision,
		   (ud.settings IS NOT NULL OR ud.investigators IS NOT NULL OR ud.saves IS NOT NULL OR ud.tavern_novels IS NOT NULL) AS has_data
		 FROM users u
		 LEFT JOIN user_data ud ON ud.user_id = u.user_uuid
		 WHERE ($1 = '' OR u.email ILIKE '%' || $1 || '%' OR u.user_uuid = $1)
		 ORDER BY u.id DESC LIMIT $2 OFFSET $3`, q, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []AdminUserRow
	for rows.Next() {
		var row AdminUserRow
		if err := rows.Scan(&row.UUID, &row.Email, &row.Role, &row.Banned, &row.CreatedAt, &row.DataRevision, &row.HasData); err != nil {
			return nil, 0, err
		}
		out = append(out, row)
	}
	return out, total, rows.Err()
}

// GetUserDetailAdmin returns metadata for a single account (never cloud-data
// contents) plus the byte sizes of each cloud-data column.
func (s *Store) GetUserDetailAdmin(ctx context.Context, uuid string) (AdminUserDetail, bool, error) {
	row := s.Pool.QueryRow(ctx,
		`SELECT u.user_uuid, u.email, u.role, u.banned,
		   EXTRACT(EPOCH FROM u.created_at)::bigint AS created_at,
		   COALESCE(ud.revision, 0) AS data_revision,
		   COALESCE(EXTRACT(EPOCH FROM ud.updated_at)::bigint, 0) AS updated_at,
		   COALESCE(octet_length(ud.settings), 0) AS settings_bytes,
		   COALESCE(octet_length(ud.investigators), 0) AS investigators_bytes,
		   COALESCE(octet_length(ud.saves), 0) AS saves_bytes,
		   COALESCE(octet_length(ud.tavern_novels), 0) AS tavern_novels_bytes
		 FROM users u
		 LEFT JOIN user_data ud ON ud.user_id = u.user_uuid
		 WHERE u.user_uuid = $1 LIMIT 1`, uuid)
	var d AdminUserDetail
	err := row.Scan(&d.UUID, &d.Email, &d.Role, &d.Banned, &d.CreatedAt, &d.DataRevision,
		&d.UpdatedAt, &d.SettingsBytes, &d.InvestigatorsBytes, &d.SavesBytes, &d.TavernNovelsBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminUserDetail{}, false, nil
	}
	if err != nil {
		return AdminUserDetail{}, false, err
	}
	return d, true, nil
}

// ListActiveRoomsAdmin returns every non-expired room (public or private),
// newest first (max 200), with its host profile and online member count.
func (s *Store) ListActiveRoomsAdmin(ctx context.Context, ttlHours int) ([]AdminRoom, error) {
	// friendProfileColumns embeds a literal '%', so it must not pass through
	// fmt.Sprintf; format the interval fragment separately instead.
	interval := fmt.Sprintf(`NOW() - INTERVAL '%d hours'`, ttlHours)
	rows, err := s.Pool.Query(ctx,
		`SELECT r.room_id, r.host_id, `+friendProfileColumns+`, r.is_public,
		   r.name,
		   (SELECT COUNT(*) FROM coc_room_presence p WHERE p.room_id = r.room_id AND p.last_seen >= NOW() - INTERVAL '120 seconds') AS member_count,
		   EXTRACT(EPOCH FROM r.created_at)::bigint AS created_at,
		   EXTRACT(EPOCH FROM r.updated_at)::bigint AS updated_at
		 FROM coc_rooms r
		 LEFT JOIN users u ON u.user_uuid = r.host_id
		 LEFT JOIN user_data ud ON ud.user_id = u.user_uuid
		 WHERE r.updated_at >= (`+interval+`)
		 ORDER BY r.created_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AdminRoom
	for rows.Next() {
		var room AdminRoom
		var name, avatar, customName *string
		if err := rows.Scan(&room.RoomID, &room.HostID, &name, &avatar, &room.IsPublic, &customName,
			&room.MemberCount, &room.CreatedAt, &room.UpdatedAt); err != nil {
			return nil, err
		}
		if name != nil {
			room.HostName = *name
		}
		if avatar != nil {
			room.HostAvatar = *avatar
		}
		if customName != nil {
			room.Name = *customName
		}
		out = append(out, room)
	}
	return out, rows.Err()
}

// ListModulesAdmin returns a page of all library modules (including private
// ones) matching an optional title search, newest first, plus the total count.
func (s *Store) ListModulesAdmin(ctx context.Context, q string, limit, offset int) ([]AdminModule, int64, error) {
	var total int64
	if err := s.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM coc_library_modules m
		 WHERE ($1 = '' OR m.title ILIKE '%' || $1 || '%')`, q).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.Pool.Query(ctx,
		`SELECT m.id, m.title, m.type, m.author_id, m.author_name, m.downloads,
		   EXTRACT(EPOCH FROM m.created_at)::bigint AS created_at,
		   CHAR_LENGTH(m.content) AS content_length
		 FROM coc_library_modules m
		 WHERE ($1 = '' OR m.title ILIKE '%' || $1 || '%')
		 ORDER BY m.created_at DESC LIMIT $2 OFFSET $3`, q, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []AdminModule
	for rows.Next() {
		var m AdminModule
		var authorName *string
		if err := rows.Scan(&m.ID, &m.Title, &m.Type, &m.AuthorID, &authorName, &m.Downloads, &m.CreatedAt, &m.ContentLength); err != nil {
			return nil, 0, err
		}
		if authorName != nil {
			m.AuthorName = *authorName
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

// DeleteModuleAdmin removes any module by id, returning true when a row matched.
func (s *Store) DeleteModuleAdmin(ctx context.Context, id int64) (bool, error) {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM coc_library_modules WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// DeleteUsersAdmin removes the given accounts together with every row that
// references them (friendships, requests, invites, presence, the rooms they
// host plus those rooms' messages/presence, and their cloud data) inside a
// single transaction. Admin audit rows and library modules are intentionally
// preserved. It returns the number of user rows actually deleted.
func (s *Store) DeleteUsersAdmin(ctx context.Context, uuids []string) (int64, error) {
	if len(uuids) == 0 {
		return 0, nil
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`DELETE FROM coc_friendships WHERE user_a = ANY($1) OR user_b = ANY($1)`, uuids); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM coc_friend_requests WHERE from_uuid = ANY($1) OR to_uuid = ANY($1)`, uuids); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM coc_friend_invites WHERE from_uuid = ANY($1) OR to_uuid = ANY($1)`, uuids); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM coc_user_presence WHERE user_uuid = ANY($1)`, uuids); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM coc_room_messages WHERE room_id IN (SELECT room_id FROM coc_rooms WHERE host_id = ANY($1))`, uuids); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM coc_room_presence WHERE room_id IN (SELECT room_id FROM coc_rooms WHERE host_id = ANY($1))`, uuids); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM coc_rooms WHERE host_id = ANY($1)`, uuids); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM user_data WHERE user_id = ANY($1)`, uuids); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM users WHERE user_uuid = ANY($1)`, uuids)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// CloseRoomsAdmin deletes the given rooms together with their messages and
// presence rows in a single transaction, returning the number of room rows
// removed. Unknown ids are simply not counted, so the call is idempotent.
func (s *Store) CloseRoomsAdmin(ctx context.Context, roomIDs []string) (int64, error) {
	if len(roomIDs) == 0 {
		return 0, nil
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`DELETE FROM coc_room_messages WHERE room_id = ANY($1)`, roomIDs); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM coc_room_presence WHERE room_id = ANY($1)`, roomIDs); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM coc_rooms WHERE room_id = ANY($1)`, roomIDs)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// InsertAudit records one admin mutation.
func (s *Store) InsertAudit(ctx context.Context, adminUUID, action, target, detail, ip string) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO coc_admin_audit (admin_uuid, action, target, detail, ip)
		 VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''))`,
		adminUUID, action, target, detail, ip)
	return err
}

// ListAudit returns a page of audit entries newest first, with the admin email.
func (s *Store) ListAudit(ctx context.Context, limit, offset int) ([]AuditRow, int64, error) {
	var total int64
	if err := s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM coc_admin_audit`).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.Pool.Query(ctx,
		`SELECT a.id, a.admin_uuid, u.email, a.action, a.target, a.detail, a.ip,
		   EXTRACT(EPOCH FROM a.created_at)::bigint AS created_at
		 FROM coc_admin_audit a
		 LEFT JOIN users u ON u.user_uuid = a.admin_uuid
		 ORDER BY a.id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []AuditRow
	for rows.Next() {
		var row AuditRow
		var email, target, detail, ip *string
		if err := rows.Scan(&row.ID, &row.AdminUUID, &email, &row.Action, &target, &detail, &ip, &row.CreatedAt); err != nil {
			return nil, 0, err
		}
		if email != nil {
			row.AdminEmail = *email
		}
		if target != nil {
			row.Target = *target
		}
		if detail != nil {
			row.Detail = *detail
		}
		if ip != nil {
			row.IP = *ip
		}
		out = append(out, row)
	}
	return out, total, rows.Err()
}

// AdminOverview computes the dashboard counters in a single round trip.
func (s *Store) AdminOverview(ctx context.Context, ttlHours int) (AdminOverview, error) {
	var o AdminOverview
	err := s.Pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT
		   (SELECT COUNT(*) FROM users),
		   (SELECT COUNT(*) FROM users WHERE banned = TRUE),
		   (SELECT COUNT(*) FROM coc_rooms WHERE updated_at >= (NOW() - INTERVAL '%d hours')),
		   (SELECT COUNT(*) FROM coc_rooms WHERE is_public = TRUE AND updated_at >= (NOW() - INTERVAL '%d hours')),
		   (SELECT COUNT(*) FROM coc_library_modules),
		   (SELECT COUNT(*) FROM coc_admin_audit)`, ttlHours, ttlHours)).
		Scan(&o.Users, &o.BannedUsers, &o.RoomsActive, &o.RoomsPublic, &o.Modules, &o.Audits)
	if err != nil {
		return AdminOverview{}, err
	}
	return o, nil
}
