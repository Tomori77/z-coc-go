package httpapi

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/Tomori77/z-coc/internal/auth"
	"github.com/Tomori77/z-coc/internal/settings"
	"github.com/Tomori77/z-coc/internal/validate"
)

const maxAdminBodyBytes = 64 * 1024

const (
	adminDefaultLimit = 50
	adminMaxLimit     = 200
	// adminMaxBatchIDs caps how many ids one batch mutation accepts.
	adminMaxBatchIDs = 100
)

func (s *Server) handleAdminAPI(w http.ResponseWriter, r *http.Request) {
	if !s.rateLimit(w, r, "admin", 120, 300) {
		return
	}
	adminUUID, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}

	switch r.URL.Query().Get("action") {
	case "overview":
		s.adminOverview(w, r)
	case "users.list":
		s.adminUsersList(w, r)
	case "user.detail":
		s.adminUserDetail(w, r)
	case "user.ban":
		s.adminUserBan(w, r, adminUUID)
	case "user.force_logout":
		s.adminUserForceLogout(w, r, adminUUID)
	case "user.set_role":
		s.adminUserSetRole(w, r, adminUUID)
	case "user.delete":
		s.adminUserDelete(w, r, adminUUID)
	case "rooms.list":
		s.adminRoomsList(w, r)
	case "room.close":
		s.adminRoomClose(w, r, adminUUID)
	case "modules.list":
		s.adminModulesList(w, r)
	case "module.delete":
		s.adminModuleDelete(w, r, adminUUID)
	case "audit.list":
		s.adminAuditList(w, r)
	case "settings.get":
		s.adminSettingsGet(w, r)
	case "settings.set":
		s.adminSettingsSet(w, r, adminUUID)
	default:
		errorJSON(w, http.StatusNotFound, "Unknown action")
	}
}

// requireAdmin resolves the caller uuid from the Bearer token and enforces the
// admin role. Banned or non-admin accounts receive a 403 Forbidden.
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) (string, bool) {
	token := auth.BearerToken(r)
	if token == "" || len(token) > 128 {
		errorJSON(w, http.StatusUnauthorized, "Login required")
		return "", false
	}
	tokenHash := auth.Sha256Hex(token)
	uuid, role, banned, found, err := s.store.FindAuthUserByToken(r.Context(), tokenHash, token)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "登录校验失败")
		return "", false
	}
	if !found || role != "admin" || banned {
		errorJSON(w, http.StatusForbidden, "Forbidden")
		return "", false
	}
	return uuid, true
}

// auditAdmin best-effort records an admin mutation; failures are logged only.
func (s *Server) auditAdmin(r *http.Request, adminUUID, action, target, detail string) {
	if err := s.store.InsertAudit(r.Context(), adminUUID, action, target, detail, clientIP(r)); err != nil {
		s.logger.Warn("admin audit insert failed", "error", err, "action", action)
	}
}

// adminPaging parses limit/offset query params with sane clamps.
func adminPaging(r *http.Request) (limit, offset int) {
	limit = int(phpAtoi(r.URL.Query().Get("limit")))
	if limit <= 0 {
		limit = adminDefaultLimit
	}
	if limit > adminMaxLimit {
		limit = adminMaxLimit
	}
	offset = int(phpAtoi(r.URL.Query().Get("offset")))
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func (s *Server) adminOverview(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	stats, err := s.store.AdminOverview(r.Context(), s.cfg.RoomTTLHours)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "后台概览读取失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"stats": map[string]any{
			"users":        stats.Users,
			"banned_users": stats.BannedUsers,
			"rooms_active": stats.RoomsActive,
			"rooms_public": stats.RoomsPublic,
			"modules":      stats.Modules,
			"audits":       stats.Audits,
		},
	})
}

func (s *Server) adminUsersList(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 254 {
		q = q[:254]
	}
	limit, offset := adminPaging(r)
	users, total, err := s.store.ListUsersAdmin(r.Context(), q, limit, offset)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "用户列表读取失败")
		return
	}
	out := make([]map[string]any, 0, len(users))
	for _, u := range users {
		out = append(out, map[string]any{
			"uuid":          u.UUID,
			"email":         u.Email,
			"role":          u.Role,
			"banned":        u.Banned,
			"created_at":    u.CreatedAt,
			"data_revision": u.DataRevision,
			"has_data":      u.HasData,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"users":  out,
		"total":  total,
	})
}

func (s *Server) adminUserDetail(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	userID, ok := validate.UserID(r.URL.Query().Get("user_id"))
	if !ok {
		errorJSON(w, http.StatusBadRequest, "Invalid user id")
		return
	}
	detail, found, err := s.store.GetUserDetailAdmin(r.Context(), userID)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "用户详情读取失败")
		return
	}
	if !found {
		errorJSON(w, http.StatusNotFound, "User not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"user": map[string]any{
			"uuid":                detail.UUID,
			"email":               detail.Email,
			"role":                detail.Role,
			"banned":              detail.Banned,
			"created_at":          detail.CreatedAt,
			"data_revision":       detail.DataRevision,
			"updated_at":          detail.UpdatedAt,
			"settings_bytes":      detail.SettingsBytes,
			"investigators_bytes": detail.InvestigatorsBytes,
			"saves_bytes":         detail.SavesBytes,
			"tavern_novels_bytes": detail.TavernNovelsBytes,
		},
	})
}

func (s *Server) adminUserBan(w http.ResponseWriter, r *http.Request, adminUUID string) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	data, ok := readJSONBody(w, r, maxAdminBodyBytes)
	if !ok {
		return
	}
	userID, ok := validate.UserID(asString(data["user_id"]))
	if !ok {
		errorJSON(w, http.StatusBadRequest, "Invalid user id")
		return
	}
	banned := isTruthy(data["banned"])
	if err := s.store.SetUserBanned(r.Context(), userID, banned); err != nil {
		errorJSON(w, http.StatusInternalServerError, "用户封禁失败")
		return
	}
	// Banning also revokes the current session.
	if banned {
		_ = s.store.ClearAuthToken(r.Context(), userID)
	}
	detail := "banned=false"
	if banned {
		detail = "banned=true"
	}
	s.auditAdmin(r, adminUUID, "user.ban", userID, detail)
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

func (s *Server) adminUserForceLogout(w http.ResponseWriter, r *http.Request, adminUUID string) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	data, ok := readJSONBody(w, r, maxAdminBodyBytes)
	if !ok {
		return
	}
	userID, ok := validate.UserID(asString(data["user_id"]))
	if !ok {
		errorJSON(w, http.StatusBadRequest, "Invalid user id")
		return
	}
	if err := s.store.ClearAuthToken(r.Context(), userID); err != nil {
		errorJSON(w, http.StatusInternalServerError, "强制下线失败")
		return
	}
	s.auditAdmin(r, adminUUID, "user.force_logout", userID, "")
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

func (s *Server) adminUserSetRole(w http.ResponseWriter, r *http.Request, adminUUID string) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	data, ok := readJSONBody(w, r, maxAdminBodyBytes)
	if !ok {
		return
	}
	userID, ok := validate.UserID(asString(data["user_id"]))
	if !ok {
		errorJSON(w, http.StatusBadRequest, "Invalid user id")
		return
	}
	role := strings.TrimSpace(asString(data["role"]))
	if role != "user" && role != "admin" {
		errorJSON(w, http.StatusBadRequest, "Invalid role")
		return
	}
	if userID == adminUUID {
		errorJSON(w, http.StatusBadRequest, "Cannot change your own role")
		return
	}
	if err := s.store.SetUserRole(r.Context(), userID, role); err != nil {
		errorJSON(w, http.StatusInternalServerError, "角色修改失败")
		return
	}
	s.auditAdmin(r, adminUUID, "user.set_role", userID, role)
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

// adminRawIDList collects the raw id strings from a batch body that may carry
// either an array field (preferred) or a single scalar field, matching the
// single/batch call shapes accepted by the admin UI.
func adminRawIDList(data map[string]any, arrayKey, singleKey string) []string {
	if arr, ok := data[arrayKey].([]any); ok {
		out := make([]string, 0, len(arr))
		for _, v := range arr {
			out = append(out, asString(v))
		}
		return out
	}
	if single, ok := data[singleKey]; ok {
		return []string{asString(single)}
	}
	return nil
}

// adminValidateBatch normalizes raw ids with validateFn, skipping invalid
// entries and duplicates, and caps the result at adminMaxBatchIDs.
func adminValidateBatch(raw []string, validateFn func(string) (string, bool)) []string {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		id, ok := validateFn(v)
		if !ok {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
		if len(out) >= adminMaxBatchIDs {
			break
		}
	}
	return out
}

// truncateAuditTarget clamps an audit target to the 128-char column bound.
func truncateAuditTarget(s string) string {
	if len(s) > 120 {
		return s[:120]
	}
	return s
}

// truncateAuditDetail clamps an audit detail string to a sane length.
func truncateAuditDetail(s string) string {
	if len(s) > 500 {
		return s[:500]
	}
	return s
}

func (s *Server) adminUserDelete(w http.ResponseWriter, r *http.Request, adminUUID string) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	data, ok := readJSONBody(w, r, maxAdminBodyBytes)
	if !ok {
		return
	}
	ids := adminValidateBatch(adminRawIDList(data, "user_ids", "user_id"), validate.UserID)
	// An admin must never delete their own account through this endpoint.
	filtered := ids[:0]
	for _, id := range ids {
		if id != adminUUID {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) == 0 {
		errorJSON(w, http.StatusBadRequest, "No valid users to delete")
		return
	}
	deleted, err := s.store.DeleteUsersAdmin(r.Context(), filtered)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "用户删除失败")
		return
	}
	s.auditAdmin(r, adminUUID, "user.delete",
		truncateAuditTarget(strings.Join(filtered, ",")), fmt.Sprintf("deleted %d", deleted))
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "deleted": deleted})
}

func (s *Server) adminRoomsList(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	rooms, err := s.store.ListActiveRoomsAdmin(r.Context(), s.cfg.RoomTTLHours)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "房间列表读取失败")
		return
	}
	out := make([]map[string]any, 0, len(rooms))
	for _, room := range rooms {
		out = append(out, map[string]any{
			"room_id":      room.RoomID,
			"host_id":      room.HostID,
			"host_name":    room.HostName,
			"host_avatar":  room.HostAvatar,
			"is_public":    room.IsPublic,
			"name":         room.Name,
			"member_count": room.MemberCount,
			"created_at":   room.CreatedAt,
			"updated_at":   room.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "rooms": out})
}

func (s *Server) adminRoomClose(w http.ResponseWriter, r *http.Request, adminUUID string) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	data, ok := readJSONBody(w, r, maxAdminBodyBytes)
	if !ok {
		return
	}
	roomIDs := adminValidateBatch(adminRawIDList(data, "room_ids", "room_id"), validate.RoomID)
	if len(roomIDs) == 0 {
		errorJSON(w, http.StatusBadRequest, "No valid rooms")
		return
	}
	closed, err := s.store.CloseRoomsAdmin(r.Context(), roomIDs)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "房间关闭失败")
		return
	}
	s.auditAdmin(r, adminUUID, "room.close",
		truncateAuditTarget(strings.Join(roomIDs, ",")), fmt.Sprintf("closed %d", closed))
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "closed": closed})
}

func (s *Server) adminModulesList(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 255 {
		q = q[:255]
	}
	limit, offset := adminPaging(r)
	modules, total, err := s.store.ListModulesAdmin(r.Context(), q, limit, offset)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "模组列表读取失败")
		return
	}
	out := make([]map[string]any, 0, len(modules))
	for _, m := range modules {
		out = append(out, map[string]any{
			"id":             m.ID,
			"title":          m.Title,
			"type":           m.Type,
			"author_id":      m.AuthorID,
			"author_name":    m.AuthorName,
			"downloads":      m.Downloads,
			"created_at":     m.CreatedAt,
			"content_length": m.ContentLength,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"modules": out,
		"total":   total,
	})
}

func (s *Server) adminModuleDelete(w http.ResponseWriter, r *http.Request, adminUUID string) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	data, ok := readJSONBody(w, r, maxAdminBodyBytes)
	if !ok {
		return
	}
	id := phpIntCast(data["id"])
	if id < 1 {
		errorJSON(w, http.StatusBadRequest, "Invalid module id")
		return
	}
	deleted, err := s.store.DeleteModuleAdmin(r.Context(), id)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "模组删除失败")
		return
	}
	if !deleted {
		errorJSON(w, http.StatusNotFound, "Module not found")
		return
	}
	s.auditAdmin(r, adminUUID, "module.delete", asString(data["id"]), "")
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

func (s *Server) adminAuditList(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	limit, offset := adminPaging(r)
	entries, total, err := s.store.ListAudit(r.Context(), limit, offset)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "审计日志读取失败")
		return
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{
			"id":          e.ID,
			"admin_uuid":  e.AdminUUID,
			"admin_email": e.AdminEmail,
			"action":      e.Action,
			"target":      e.Target,
			"detail":      e.Detail,
			"ip":          e.IP,
			"created_at":  e.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"entries": out,
		"total":   total,
	})
}

// adminSettingsGet returns every whitelisted setting (defaults merged with
// stored overrides) plus the definition list used by the admin UI.
func (s *Server) adminSettingsGet(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	stored, err := s.store.GetAppSettings(r.Context())
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "设置读取失败")
		return
	}
	values := make(map[string]any, len(settings.Definitions))
	defs := make([]map[string]any, 0, len(settings.Definitions))
	for _, def := range settings.Definitions {
		raw := def.Default
		if value, ok := stored[def.Key]; ok {
			raw = value
		}
		values[def.Key] = settings.Decode(def, raw)
		defs = append(defs, map[string]any{
			"key":     def.Key,
			"kind":    string(def.Kind),
			"default": settings.Decode(def, def.Default),
			"public":  def.Public,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "success",
		"settings": values,
		"defs":     defs,
	})
}

// adminSettingsSet validates and persists a batch of setting overrides. Every
// key must be whitelisted and every value must match its declared kind.
func (s *Server) adminSettingsSet(w http.ResponseWriter, r *http.Request, adminUUID string) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	data, ok := readJSONBody(w, r, maxAdminBodyBytes)
	if !ok {
		return
	}
	raw, ok := data["settings"].(map[string]any)
	if !ok || len(raw) == 0 {
		errorJSON(w, http.StatusBadRequest, "No settings provided")
		return
	}
	values := make(map[string]string, len(raw))
	keys := make([]string, 0, len(raw))
	for key, value := range raw {
		def, known := settings.Find(key)
		if !known {
			errorJSON(w, http.StatusBadRequest, "Unknown setting: "+key)
			return
		}
		stored, valid := settings.Validate(def.Key, value)
		if !valid {
			errorJSON(w, http.StatusBadRequest, "Invalid value for "+key)
			return
		}
		values[key] = stored
		keys = append(keys, key)
	}
	if err := s.store.UpsertAppSettings(r.Context(), values); err != nil {
		errorJSON(w, http.StatusInternalServerError, "设置保存失败")
		return
	}
	sort.Strings(keys)
	s.auditAdmin(r, adminUUID, "settings.set", "", truncateAuditDetail(strings.Join(keys, ",")))
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}
