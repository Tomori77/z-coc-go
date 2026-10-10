package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Tomori77/z-coc/internal/auth"
	"github.com/Tomori77/z-coc/internal/validate"
)

const maxRoomSmallBodyBytes = 64 * 1024

// hostOnlyMessageTypes is the host-only whitelist from room_api.php.
var hostOnlyMessageTypes = map[string]bool{
	"sync_state":              true,
	"sync_log_append":         true,
	"update_last_message":     true,
	"update_last_message_full": true,
	"sync_module":             true,
	"sync_investigators":      true,
	"sync_assignments":        true,
	"game_start":              true,
	"game_ended":              true,
	"room_closed":             true,
	"grant_cot_access":        true,
	"coj_toggle":              true,
	"generating_start":        true,
	"generating_end":          true,
	"regenerate_start":        true,
	"distribute_reports":      true,
	"kick_player":             true,
	"move_to_spectator":       true,
}

func isHostOnlyMessage(messageType string) bool { return hostOnlyMessageTypes[messageType] }

// isPlayerLogAppend mirrors is_player_log_append().
func isPlayerLogAppend(payload map[string]any, senderID string) bool {
	if asString(payload["type"]) != "sync_log_append" {
		return false
	}
	message, ok := payload["message"].(map[string]any)
	if !ok {
		return false
	}
	role := asString(message["role"])
	if role != "user" && role != "whisper" {
		return false
	}
	if isTruthy(message["isAiGenerated"]) {
		return false
	}
	claimed := message["senderId"]
	if claimed == nil {
		claimed = message["id"]
	}
	return claimed != nil && asString(claimed) == senderID
}

func (s *Server) handleRoomAPI(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Query().Get("action") {
	case "create":
		s.roomCreate(w, r)
	case "join":
		s.roomJoin(w, r)
	case "push":
		s.roomPush(w, r)
	case "pull":
		s.roomPull(w, r)
	default:
		errorJSON(w, http.StatusNotFound, "Unknown action")
	}
}

func (s *Server) roomCreate(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !s.rateLimit(w, r, "room_create", 30, 300) {
		return
	}
	data, ok := readJSONBody(w, r, maxRoomSmallBodyBytes)
	if !ok {
		return
	}
	roomID, ok := validate.RoomID(asString(data["room_id"]))
	if !ok {
		errorJSON(w, http.StatusBadRequest, "Invalid room id")
		return
	}
	hostID, ok := validate.SenderID(asString(data["user_id"]))
	if !ok {
		errorJSON(w, http.StatusBadRequest, "Invalid sender")
		return
	}

	hostToken := auth.NewToken()
	hostHash := auth.Sha256Hex(hostToken)

	exists, err := s.store.RoomExistsActive(r.Context(), roomID, s.cfg.RoomTTLHours)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "Room create failed")
		return
	}
	if exists {
		errorJSON(w, http.StatusConflict, "Room already exists")
		return
	}
	if err := s.store.UpsertRoom(r.Context(), roomID, hostID, hostHash); err != nil {
		errorJSON(w, http.StatusInternalServerError, "Room create failed")
		return
	}
	if err := s.store.DeleteRoomMessages(r.Context(), roomID); err != nil {
		errorJSON(w, http.StatusInternalServerError, "Room reset failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "success",
		"last_id":    0,
		"host_token": hostToken,
	})
}

func (s *Server) roomJoin(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !s.rateLimit(w, r, "room_join", 120, 300) {
		return
	}
	data, ok := readJSONBody(w, r, maxRoomSmallBodyBytes)
	if !ok {
		return
	}
	roomID, ok := validate.RoomID(asString(data["room_id"]))
	if !ok {
		errorJSON(w, http.StatusBadRequest, "Invalid room id")
		return
	}
	room, err := s.store.GetActiveRoom(r.Context(), roomID, s.cfg.RoomTTLHours)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "Room not found or expired")
		return
	}
	if room == nil {
		errorJSON(w, http.StatusNotFound, "Room not found or expired")
		return
	}
	_ = s.store.TouchRoom(r.Context(), roomID)
	lastID, err := s.store.MaxMessageID(r.Context(), roomID)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "Room not found or expired")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "last_id": lastID})
}

func (s *Server) roomPush(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !s.rateLimit(w, r, "room_push", 600, 300) {
		return
	}
	data, ok := readJSONBody(w, r, s.cfg.MaxPayloadBytes+32768)
	if !ok {
		return
	}
	roomID, ok := validate.RoomID(asString(data["room_id"]))
	if !ok {
		errorJSON(w, http.StatusBadRequest, "Invalid room id")
		return
	}
	senderID, ok := validate.SenderID(asString(data["sender_id"]))
	if !ok {
		errorJSON(w, http.StatusBadRequest, "Invalid sender")
		return
	}
	room, err := s.store.GetActiveRoom(r.Context(), roomID, s.cfg.RoomTTLHours)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "Room not found or expired")
		return
	}
	if room == nil {
		errorJSON(w, http.StatusNotFound, "Room not found or expired")
		return
	}

	payloadRaw, present := data["payload"]
	if !present || payloadRaw == nil {
		errorJSON(w, http.StatusBadRequest, "Missing payload")
		return
	}
	var payload map[string]any
	switch t := payloadRaw.(type) {
	case map[string]any:
		payload = t
	case []any:
		payload = map[string]any{}
	default:
		errorJSON(w, http.StatusBadRequest, "Missing payload")
		return
	}

	messageType := asString(payload["type"])
	if messageType == "" || len(messageType) > 64 {
		errorJSON(w, http.StatusBadRequest, "Invalid message type")
		return
	}

	playerLogAppend := isPlayerLogAppend(payload, senderID)

	if isHostOnlyMessage(messageType) && !playerLogAppend {
		hostToken := strings.TrimSpace(asString(data["host_token"]))
		hostTokenHash := auth.Sha256Hex(hostToken)
		if senderID != room.HostID || hostToken == "" || room.HostTokenHash == nil || *room.HostTokenHash != hostTokenHash {
			writeJSON(w, http.StatusForbidden, map[string]any{
				"status":  "error",
				"code":    "HOST_AUTH_FAILED",
				"message": "Only host can send this message",
			})
			return
		}
	}

	if !isHostOnlyMessage(messageType) || playerLogAppend {
		var claimed any
		if playerLogAppend {
			message := payload["message"].(map[string]any)
			claimed = message["senderId"]
			if claimed == nil {
				claimed = message["id"]
			}
		} else {
			if user, ok := payload["user"].(map[string]any); ok {
				claimed = user["id"]
			}
			if claimed == nil {
				claimed = payload["userId"]
			}
		}
		if claimed != nil && asString(claimed) != senderID {
			errorJSON(w, http.StatusForbidden, "Sender mismatch")
			return
		}
	}

	payloadJSON, err := marshalJSON(payload)
	if err != nil {
		errorJSON(w, http.StatusBadRequest, "Payload encode failed")
		return
	}
	if int64(len(payloadJSON)) > s.cfg.MaxPayloadBytes {
		errorJSON(w, http.StatusRequestEntityTooLarge, "Payload too large")
		return
	}

	newID, err := s.store.InsertMessage(r.Context(), roomID, senderID, string(payloadJSON))
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "Message send failed")
		return
	}
	_ = s.store.PruneMessages(r.Context(), roomID, s.cfg.MaxRoomMessages)
	_ = s.store.TouchRoom(r.Context(), roomID)

	writeJSON(w, http.StatusOK, map[string]any{"status": "success", "id": newID})
}

func (s *Server) roomPull(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if !s.rateLimit(w, r, "room_pull", 900, 300) {
		return
	}
	roomID, ok := validate.RoomID(r.URL.Query().Get("room_id"))
	if !ok {
		errorJSON(w, http.StatusBadRequest, "Invalid room id")
		return
	}
	room, err := s.store.GetActiveRoom(r.Context(), roomID, s.cfg.RoomTTLHours)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "Room not found or expired")
		return
	}
	if room == nil {
		errorJSON(w, http.StatusNotFound, "Room not found or expired")
		return
	}

	since := phpAtoi(r.URL.Query().Get("since"))
	if since < 0 {
		since = 0
	}

	rows, err := s.store.ListMessages(r.Context(), roomID, since, s.cfg.PollLimit)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "Room pull failed")
		return
	}
	messages := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		var payload any
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			payload = nil
		}
		messages = append(messages, map[string]any{
			"id":        row.ID,
			"sender_id": row.SenderID,
			"payload":   payload,
		})
	}

	if presenceUser, ok := validate.SenderID(r.URL.Query().Get("user_id")); ok {
		_ = s.store.UpsertPresence(r.Context(), roomID, presenceUser)
	}

	presenceRows, err := s.store.ListPresence(r.Context(), roomID)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "Room pull failed")
		return
	}
	presence := make([]map[string]any, 0, len(presenceRows))
	for _, p := range presenceRows {
		presence = append(presence, map[string]any{"user_id": p.UserID, "seen_at": p.SeenAt})
	}

	_ = s.store.TouchRoom(r.Context(), roomID)

	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "success",
		"messages": messages,
		"presence": presence,
		"now":      time.Now().Unix(),
	})
}
