package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Tomori77/z-coc/internal/auth"
	"github.com/Tomori77/z-coc/internal/roommsg"
	"github.com/Tomori77/z-coc/internal/validate"
)

const maxRoomSmallBodyBytes = 64 * 1024

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
	_, payloadJSON, verr := roommsg.ValidatePush(
		payloadRaw, present, senderID, asString(data["host_token"]),
		room.HostID, room.HostTokenHash, s.cfg.MaxPayloadBytes,
	)
	if verr != nil {
		switch verr.Code {
		case roommsg.CodeHostAuthFailed:
			writeJSON(w, http.StatusForbidden, map[string]any{
				"status":  "error",
				"code":    verr.Code,
				"message": verr.Message,
			})
		case roommsg.CodeSenderMismatch:
			errorJSON(w, http.StatusForbidden, verr.Message)
		case roommsg.CodePayloadTooLarge:
			errorJSON(w, http.StatusRequestEntityTooLarge, verr.Message)
		default:
			errorJSON(w, http.StatusBadRequest, verr.Message)
		}
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
