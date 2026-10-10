package httpapi

import (
	"net/http"
	"time"

	"github.com/Tomori77/z-coc/internal/auth"
	"github.com/Tomori77/z-coc/internal/validate"
)

const maxFriendBodyBytes = 64 * 1024

func (s *Server) handleFriendAPI(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Query().Get("action") {
	case "search":
		s.friendSearch(w, r)
	case "request":
		s.friendRequest(w, r)
	case "list":
		s.friendList(w, r)
	case "accept":
		s.friendAccept(w, r)
	case "reject":
		s.friendReject(w, r)
	case "invite":
		s.friendInvite(w, r)
	case "respond":
		s.friendRespond(w, r)
	default:
		errorJSON(w, http.StatusNotFound, "Unknown action")
	}
}

// requireAuthUser resolves the caller uuid from the Bearer token only. It never
// trusts a client-supplied identity.
func (s *Server) requireAuthUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	token := auth.BearerToken(r)
	if token == "" || len(token) > 128 {
		errorJSON(w, http.StatusUnauthorized, "Login required")
		return "", false
	}
	tokenHash := auth.Sha256Hex(token)
	uuid, found, err := s.store.FindUserUUIDByToken(r.Context(), tokenHash, token)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "登录校验失败")
		return "", false
	}
	if !found {
		errorJSON(w, http.StatusUnauthorized, "Login expired")
		return "", false
	}
	return uuid, true
}

func (s *Server) friendSearch(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if !s.rateLimit(w, r, "friend_search", 60, 60) {
		return
	}
	caller, ok := s.requireAuthUser(w, r)
	if !ok {
		return
	}
	_ = s.store.UpsertUserPresence(r.Context(), caller)

	target, valid := validate.UserID(r.URL.Query().Get("q"))
	if !valid {
		errorJSON(w, http.StatusNotFound, "未找到该同伴")
		return
	}
	if target == caller {
		errorJSON(w, http.StatusBadRequest, "不能添加自己")
		return
	}
	profile, found, err := s.store.FindUserProfileByUUID(r.Context(), target)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "好友查询失败")
		return
	}
	if !found {
		errorJSON(w, http.StatusNotFound, "未找到该同伴")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"user": map[string]any{
			"uuid":   profile.UUID,
			"name":   profile.Name,
			"avatar": profile.Avatar,
		},
	})
}

func (s *Server) friendRequest(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !s.rateLimit(w, r, "friend_request", 30, 300) {
		return
	}
	caller, ok := s.requireAuthUser(w, r)
	if !ok {
		return
	}
	_ = s.store.UpsertUserPresence(r.Context(), caller)

	data, ok := readJSONBody(w, r, maxFriendBodyBytes)
	if !ok {
		return
	}
	target, valid := validate.UserID(asString(data["target_uuid"]))
	if !valid {
		errorJSON(w, http.StatusNotFound, "未找到该同伴")
		return
	}
	if target == caller {
		errorJSON(w, http.StatusBadRequest, "不能添加自己")
		return
	}
	_, found, err := s.store.FindUserProfileByUUID(r.Context(), target)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "好友查询失败")
		return
	}
	if !found {
		errorJSON(w, http.StatusNotFound, "未找到该同伴")
		return
	}
	friends, err := s.store.AreFriends(r.Context(), caller, target)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "好友请求失败")
		return
	}
	if friends {
		errorJSON(w, http.StatusConflict, "你们已经是同伴")
		return
	}
	sent, err := s.store.HasPendingRequest(r.Context(), caller, target)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "好友请求失败")
		return
	}
	if sent {
		errorJSON(w, http.StatusConflict, "请求已发送，等待对方确认")
		return
	}
	reverse, err := s.store.HasPendingRequest(r.Context(), target, caller)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "好友请求失败")
		return
	}
	if reverse {
		errorJSON(w, http.StatusConflict, "对方已向你发送请求，请在列表中确认")
		return
	}
	if err := s.store.CreateFriendRequest(r.Context(), caller, target); err != nil {
		errorJSON(w, http.StatusInternalServerError, "好友请求失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

func (s *Server) friendList(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if !s.rateLimit(w, r, "friend_list", 240, 300) {
		return
	}
	caller, ok := s.requireAuthUser(w, r)
	if !ok {
		return
	}
	_ = s.store.UpsertUserPresence(r.Context(), caller)

	friendUUIDs, err := s.store.ListFriendUUIDs(r.Context(), caller)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "好友列表读取失败")
		return
	}
	presence, err := s.store.UserPresenceEpochs(r.Context(), friendUUIDs)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "好友列表读取失败")
		return
	}

	now := time.Now().Unix()
	friends := make([]map[string]any, 0, len(friendUUIDs))
	for _, friendUUID := range friendUUIDs {
		profile, found, err := s.store.FindUserProfileByUUID(r.Context(), friendUUID)
		if err != nil {
			errorJSON(w, http.StatusInternalServerError, "好友列表读取失败")
			return
		}
		if !found {
			continue
		}
		status := "offline"
		isOnline := false
		var lastSeen any
		if seen, present := presence[friendUUID]; present {
			lastSeen = seen
			elapsed := now - seen
			if elapsed < 120 {
				status = "online"
				isOnline = true
			} else if elapsed < 600 {
				status = "away"
			}
		}
		friends = append(friends, map[string]any{
			"uuid":      profile.UUID,
			"name":      profile.Name,
			"avatar":    profile.Avatar,
			"status":    status,
			"is_online": isOnline,
			"last_seen": lastSeen,
		})
	}

	requests, err := s.store.ListIncomingRequests(r.Context(), caller)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "好友列表读取失败")
		return
	}
	requestOut := make([]map[string]any, 0, len(requests))
	for _, req := range requests {
		requestOut = append(requestOut, map[string]any{
			"id":         req.ID,
			"uuid":       req.FromUUID,
			"name":       req.Name,
			"avatar":     req.Avatar,
			"created_at": req.CreatedAt,
		})
	}

	invites, err := s.store.ListIncomingInvites(r.Context(), caller)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "好友列表读取失败")
		return
	}
	inviteOut := make([]map[string]any, 0, len(invites))
	for _, inv := range invites {
		inviteOut = append(inviteOut, map[string]any{
			"id":         inv.ID,
			"uuid":       inv.FromUUID,
			"name":       inv.Name,
			"avatar":     inv.Avatar,
			"room_id":    inv.RoomID,
			"created_at": inv.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "success",
		"friends":  friends,
		"requests": requestOut,
		"invites":  inviteOut,
	})
}

func (s *Server) friendAccept(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !s.rateLimit(w, r, "friend_generic", 240, 300) {
		return
	}
	caller, ok := s.requireAuthUser(w, r)
	if !ok {
		return
	}
	_ = s.store.UpsertUserPresence(r.Context(), caller)

	data, ok := readJSONBody(w, r, maxFriendBodyBytes)
	if !ok {
		return
	}
	id := phpIntCast(data["request_id"])
	if id < 1 {
		errorJSON(w, http.StatusNotFound, "请求不存在")
		return
	}
	_, ok, err := s.store.AcceptFriendRequest(r.Context(), id, caller)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "好友请求处理失败")
		return
	}
	if !ok {
		errorJSON(w, http.StatusNotFound, "请求不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

func (s *Server) friendReject(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !s.rateLimit(w, r, "friend_generic", 240, 300) {
		return
	}
	caller, ok := s.requireAuthUser(w, r)
	if !ok {
		return
	}
	_ = s.store.UpsertUserPresence(r.Context(), caller)

	data, ok := readJSONBody(w, r, maxFriendBodyBytes)
	if !ok {
		return
	}
	id := phpIntCast(data["request_id"])
	if id < 1 {
		errorJSON(w, http.StatusNotFound, "请求不存在")
		return
	}
	ok, err := s.store.RejectFriendRequest(r.Context(), id, caller)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "好友请求处理失败")
		return
	}
	if !ok {
		errorJSON(w, http.StatusNotFound, "请求不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

func (s *Server) friendInvite(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !s.rateLimit(w, r, "friend_invite", 60, 300) {
		return
	}
	caller, ok := s.requireAuthUser(w, r)
	if !ok {
		return
	}
	_ = s.store.UpsertUserPresence(r.Context(), caller)

	data, ok := readJSONBody(w, r, maxFriendBodyBytes)
	if !ok {
		return
	}
	friendUUID, _ := validate.UserID(asString(data["friend_uuid"]))
	friends, err := s.store.AreFriends(r.Context(), caller, friendUUID)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "邀请失败")
		return
	}
	if !friends {
		errorJSON(w, http.StatusForbidden, "只能邀请同伴")
		return
	}
	roomID, ok := validate.RoomID(asString(data["room_id"]))
	if !ok {
		errorJSON(w, http.StatusNotFound, "房间不存在或已过期")
		return
	}
	exists, err := s.store.RoomExistsActive(r.Context(), roomID, s.cfg.RoomTTLHours)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "邀请失败")
		return
	}
	if !exists {
		errorJSON(w, http.StatusNotFound, "房间不存在或已过期")
		return
	}
	if err := s.store.CreateFriendInvite(r.Context(), caller, friendUUID, roomID); err != nil {
		errorJSON(w, http.StatusInternalServerError, "邀请失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

func (s *Server) friendRespond(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !s.rateLimit(w, r, "friend_generic", 240, 300) {
		return
	}
	caller, ok := s.requireAuthUser(w, r)
	if !ok {
		return
	}
	_ = s.store.UpsertUserPresence(r.Context(), caller)

	data, ok := readJSONBody(w, r, maxFriendBodyBytes)
	if !ok {
		return
	}
	id := phpIntCast(data["invite_id"])
	if id < 1 {
		errorJSON(w, http.StatusNotFound, "邀请不存在")
		return
	}
	ok, err := s.store.RespondInvite(r.Context(), id, caller, isTruthy(data["accept"]))
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "邀请处理失败")
		return
	}
	if !ok {
		errorJSON(w, http.StatusNotFound, "邀请不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success"})
}
