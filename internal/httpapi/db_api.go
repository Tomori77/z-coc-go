package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Tomori77/z-coc/internal/auth"
	"github.com/Tomori77/z-coc/internal/store"
	"github.com/Tomori77/z-coc/internal/validate"
)

const maxAuthBodyBytes = 64 * 1024

func (s *Server) handleDBAPI(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Query().Get("action") {
	case "register":
		s.dbRegister(w, r)
	case "login":
		s.dbLogin(w, r)
	case "pull":
		s.dbPull(w, r)
	case "sync":
		s.dbSync(w, r)
	case "logout":
		s.dbLogout(w, r)
	default:
		errorJSON(w, http.StatusNotFound, "Unknown action")
	}
}

func (s *Server) dbRegister(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !s.rateLimit(w, r, "auth", 20, 300) {
		return
	}
	data, ok := readJSONBody(w, r, maxAuthBodyBytes)
	if !ok {
		return
	}
	email := strings.TrimSpace(asString(data["email"]))
	password := asString(data["password"])

	if len(email) > 254 || !validEmail(email) {
		errorJSON(w, http.StatusBadRequest, "邮箱格式不正确")
		return
	}
	if len(password) < 8 || len(password) > 4096 {
		errorJSON(w, http.StatusBadRequest, "密码需要 8 至 4096 位")
		return
	}

	hashed, err := auth.HashPassword(password)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "注册暂时不可用")
		return
	}
	userID := auth.NewUserID()
	authToken := auth.NewToken()
	tokenHash := auth.Sha256Hex(authToken)

	if err := s.store.CreateUser(r.Context(), email, hashed, userID, tokenHash); err != nil {
		if store.IsUniqueViolation(err) {
			errorJSON(w, http.StatusConflict, "邮箱已被注册")
			return
		}
		errorJSON(w, http.StatusInternalServerError, "注册暂时不可用")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "success",
		"user_id":    userID,
		"auth_token": authToken,
	})
}

func (s *Server) dbLogin(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !s.rateLimit(w, r, "auth", 30, 300) {
		return
	}
	data, ok := readJSONBody(w, r, maxAuthBodyBytes)
	if !ok {
		return
	}
	email := strings.TrimSpace(asString(data["email"]))
	password := asString(data["password"])

	if len(email) > 254 || len(password) > 4096 {
		errorJSON(w, http.StatusBadRequest, "账号或密码错误")
		return
	}

	passwordHash, userUUID, found, err := s.store.FindUserByEmail(r.Context(), email)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "登录暂时不可用")
		return
	}
	if !found || !auth.VerifyPassword(passwordHash, password) {
		errorJSON(w, http.StatusUnauthorized, "账号或密码错误")
		return
	}

	authToken := auth.NewToken()
	tokenHash := auth.Sha256Hex(authToken)
	if err := s.store.UpdateAuthToken(r.Context(), userUUID, tokenHash); err != nil {
		errorJSON(w, http.StatusInternalServerError, "登录暂时不可用")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "success",
		"user_id":    userUUID,
		"auth_token": authToken,
	})
}

func (s *Server) dbPull(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if !s.rateLimit(w, r, "cloud_pull", 240, 300) {
		return
	}
	userID, ok := validate.UserID(r.URL.Query().Get("user_id"))
	if !ok {
		errorJSON(w, http.StatusBadRequest, "Invalid user id")
		return
	}
	if !s.requireUserToken(w, r, userID) {
		return
	}

	row, err := s.store.GetUserData(r.Context(), userID)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "云端数据读取失败")
		return
	}
	if row == nil {
		writeRawJSON(w, http.StatusOK, []byte("null"))
		return
	}

	settings := sanitizeCloudSettings(phpify(jsonColumn(row.Settings)))
	investigators, ok := listJSONColumn(row.Investigators, "investigators")
	if !ok {
		errorJSON(w, http.StatusInternalServerError, "Stored investigators data is invalid")
		return
	}
	saves, ok := listJSONColumn(row.Saves, "saves")
	if !ok {
		errorJSON(w, http.StatusInternalServerError, "Stored saves data is invalid")
		return
	}
	tavernNovels, ok := listJSONColumn(row.TavernNovels, "tavern_novels")
	if !ok {
		errorJSON(w, http.StatusInternalServerError, "Stored tavern_novels data is invalid")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"settings":      settings,
		"investigators": investigators,
		"saves":         saves,
		"tavern_novels": tavernNovels,
		"revision":      row.Revision,
		"updated_at":    row.UpdatedAt,
	})
}

func (s *Server) dbSync(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !s.rateLimit(w, r, "cloud_sync", 120, 300) {
		return
	}
	data, ok := readJSONBody(w, r, s.cfg.MaxSyncBytes)
	if !ok {
		return
	}
	userID, ok := validate.UserID(asString(data["user_id"]))
	if !ok {
		errorJSON(w, http.StatusBadRequest, "Invalid user id")
		return
	}
	if !s.requireUserToken(w, r, userID) {
		return
	}

	settingsValue := any([]any{})
	if raw, present := data["settings"]; present && raw != nil {
		settingsValue = raw
	}
	settings := sanitizeCloudSettings(phpify(settingsValue))

	investigators, ok := requireCloudList(data, "investigators")
	if !ok {
		errorJSON(w, http.StatusBadRequest, "investigators must be an array")
		return
	}
	saves, ok := requireCloudList(data, "saves")
	if !ok {
		errorJSON(w, http.StatusBadRequest, "saves must be an array")
		return
	}
	tavernNovels, ok := requireCloudList(data, "tavern_novels")
	if !ok {
		errorJSON(w, http.StatusBadRequest, "tavern_novels must be an array")
		return
	}

	baseRevision, ok := requireBaseRevision(data["base_revision"])
	if !ok {
		errorJSON(w, http.StatusBadRequest, "base_revision is required")
		return
	}

	settingsJSON, ok := encodeOrFail(w, settings, "settings")
	if !ok {
		return
	}
	investigatorsJSON, ok := encodeOrFail(w, investigators, "investigators")
	if !ok {
		return
	}
	savesJSON, ok := encodeOrFail(w, saves, "saves")
	if !ok {
		return
	}
	tavernNovelsJSON, ok := encodeOrFail(w, tavernNovels, "tavern_novels")
	if !ok {
		return
	}

	result, err := s.store.SyncUserData(r.Context(), userID, settingsJSON, investigatorsJSON, savesJSON, tavernNovelsJSON, baseRevision)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "云端数据保存失败")
		return
	}
	if result.Conflict {
		writeJSON(w, http.StatusConflict, map[string]any{
			"status":     "conflict",
			"message":    "Cloud data changed on another device",
			"revision":   result.Revision,
			"updated_at": result.UpdatedAt,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "success",
		"revision": result.Revision,
	})
}

func (s *Server) dbLogout(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !s.rateLimit(w, r, "auth_logout", 60, 300) {
		return
	}
	data, ok := readJSONBody(w, r, maxAuthBodyBytes)
	if !ok {
		return
	}
	userID, ok := validate.UserID(asString(data["user_id"]))
	if !ok {
		errorJSON(w, http.StatusBadRequest, "Invalid user id")
		return
	}
	if !s.requireUserToken(w, r, userID) {
		return
	}
	if err := s.store.ClearAuthToken(r.Context(), userID); err != nil {
		errorJSON(w, http.StatusInternalServerError, "退出登录失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

// requireUserToken validates the Bearer token and migrates legacy plaintext
// tokens to their sha256 form, matching require_user_token().
func (s *Server) requireUserToken(w http.ResponseWriter, r *http.Request, userID string) bool {
	token := auth.BearerToken(r)
	if token == "" || len(token) > 128 {
		errorJSON(w, http.StatusUnauthorized, "Login required")
		return false
	}
	tokenHash := auth.Sha256Hex(token)
	_, stored, found, err := s.store.FindUserByToken(r.Context(), userID, tokenHash, token)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "登录校验失败")
		return false
	}
	if !found {
		errorJSON(w, http.StatusUnauthorized, "Login expired")
		return false
	}
	if stored == token {
		if err := s.store.UpdateAuthToken(r.Context(), userID, tokenHash); err != nil {
			errorJSON(w, http.StatusInternalServerError, "Token migration failed")
			return false
		}
	}
	return true
}

// requireCloudList validates that a field is an array (or object), mirroring
// require_cloud_list(). Missing/null fields default to an empty array.
func requireCloudList(data map[string]any, field string) (any, bool) {
	raw, present := data[field]
	if !present || raw == nil {
		return []any{}, true
	}
	switch raw.(type) {
	case []any, map[string]any:
		return phpify(raw), true
	default:
		return nil, false
	}
}

// requireBaseRevision parses a non-negative integer base revision.
func requireBaseRevision(v any) (int64, bool) {
	switch t := v.(type) {
	case nil:
		return 0, false
	case float64:
		if t < 0 || t != float64(int64(t)) {
			return 0, false
		}
		return int64(t), true
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		if err != nil || parsed < 0 {
			return 0, false
		}
		return parsed, true
	case bool:
		if t {
			return 1, true
		}
		return 0, false
	default:
		return 0, false
	}
}

// encodeOrFail marshals a value or writes the PHP-style 400 error.
func encodeOrFail(w http.ResponseWriter, v any, field string) (string, bool) {
	encoded, err := marshalJSON(v)
	if err != nil {
		errorJSON(w, http.StatusBadRequest, field+" contains invalid text")
		return "", false
	}
	return string(encoded), true
}
