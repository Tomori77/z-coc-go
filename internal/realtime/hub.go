package realtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"github.com/Tomori77/z-coc/internal/config"
	"github.com/Tomori77/z-coc/internal/roommsg"
	"github.com/Tomori77/z-coc/internal/store"
	"github.com/Tomori77/z-coc/internal/validate"
	"github.com/coder/websocket"
)

const (
	sendBufferSize = 64
	pingInterval   = 30 * time.Second // 服务端 WS ping 周期
	pongTimeout    = 60 * time.Second // 约 2 个心跳周期内未收到 pong 即断开
	writeTimeout   = 10 * time.Second
)

// Hub 是本实例的房间连接注册表：map[roomID]*room。
type Hub struct {
	mu     sync.Mutex
	rooms  map[string]*room
	closed bool

	cfg    *config.Config
	store  *store.Store
	logger *slog.Logger

	wg sync.WaitGroup
}

// New 构造一个每实例唯一的 Hub。
func New(cfg *config.Config, st *store.Store, logger *slog.Logger) *Hub {
	return &Hub{
		rooms:  make(map[string]*room),
		cfg:    cfg,
		store:  st,
		logger: logger,
	}
}

// ServeHTTP 处理 /room_ws 的 WebSocket 升级与整条连接生命周期。
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	opts := &websocket.AcceptOptions{}
	// 允许同源；跨域仅放行 APP_ALLOWED_ORIGINS 中的来源。
	opts.OriginPatterns = append(opts.OriginPatterns, h.cfg.AllowedOrigins...)
	ws, err := websocket.Accept(w, r, opts)
	if err != nil {
		return
	}
	h.serveConn(ws)
}

func (h *Hub) serveConn(ws *websocket.Conn) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer ws.CloseNow()
	defer func() {
		if rec := recover(); rec != nil {
			h.logger.Error("realtime panic recovered", "panic", rec, "stack", string(debug.Stack()))
		}
	}()

	ws.SetReadLimit(h.cfg.MaxPayloadBytes + 32768)

	if h.isClosed() {
		h.reject(ctx, ws, roommsg.CodeRoomNotFound, "Server shutting down")
		return
	}

	hf, err := h.readHello(ctx, ws)
	if err != nil {
		h.reject(ctx, ws, roommsg.CodeInvalidMessage, "Expected hello frame")
		return
	}
	roomID, ok := validate.RoomID(hf.RoomID)
	if !ok {
		h.reject(ctx, ws, roommsg.CodeInvalidMessage, "Invalid room id")
		return
	}
	userID, ok := validate.SenderID(hf.UserID)
	if !ok {
		h.reject(ctx, ws, roommsg.CodeInvalidMessage, "Invalid sender")
		return
	}
	info, err := h.store.GetActiveRoom(ctx, roomID, h.cfg.RoomTTLHours)
	if err != nil || info == nil {
		h.reject(ctx, ws, roommsg.CodeRoomNotFound, "Room not found or expired")
		return
	}
	_ = h.store.TouchRoom(ctx, roomID)
	// 连接即登记在线，保证 HTTP pull 的 presence 仍一致。
	_ = h.store.UpsertPresence(ctx, roomID, userID)
	lastID, err := h.store.MaxMessageID(ctx, roomID)
	if err != nil {
		lastID = 0
	}

	c := &client{
		ws:        ws,
		roomID:    roomID,
		userID:    userID,
		hostToken: hf.HostToken,
		roomInfo:  info,
		send:      make(chan []byte, sendBufferSize),
		cancel:    cancel,
		closed:    make(chan struct{}),
	}

	rm := h.register(c)
	if rm == nil {
		h.reject(ctx, ws, roommsg.CodeRoomNotFound, "Server shutting down")
		return
	}

	sendFrame(c, readyFrame{Type: TypeReady, LastID: lastID, Members: rm.presence()})
	h.broadcastPresence(roomID)

	go func() {
		defer h.wg.Done()
		defer h.recoverPump()
		h.writePump(ctx, c)
	}()
	go func() {
		defer h.wg.Done()
		defer h.recoverPump()
		h.pingPump(ctx, c)
	}()

	h.readPump(ctx, c)

	h.removeClient(c)
	c.close()
}

// readHello 读取首帧并要求其为 hello。
func (h *Hub) readHello(ctx context.Context, ws *websocket.Conn) (clientFrame, error) {
	for {
		typ, data, err := ws.Read(ctx)
		if err != nil {
			return clientFrame{}, err
		}
		if typ != websocket.MessageText {
			continue
		}
		var f clientFrame
		if err := json.Unmarshal(data, &f); err != nil {
			return clientFrame{}, err
		}
		if f.Type != TypeHello {
			return clientFrame{}, errors.New("expected hello frame")
		}
		return f, nil
	}
}

// readPump 串行读取并分发后续帧；ctx 结束或读错误时返回。
func (h *Hub) readPump(ctx context.Context, c *client) {
	for {
		typ, data, err := c.ws.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		h.handleFrame(ctx, c, data)
	}
}

func (h *Hub) handleFrame(ctx context.Context, c *client, data []byte) {
	var f clientFrame
	if err := json.Unmarshal(data, &f); err != nil {
		sendFrame(c, errorFrame{Type: TypeError, Code: roommsg.CodeInvalidMessage, Message: "Invalid JSON"})
		return
	}
	switch f.Type {
	case TypePing:
		sendFrame(c, controlFrame{Type: TypePong})
	case TypeSend:
		h.handleSend(ctx, c, f)
	case TypeResume:
		h.handleResume(ctx, c, f)
	default:
		sendFrame(c, errorFrame{Type: TypeError, Code: roommsg.CodeInvalidMessage, Message: "Unknown frame type"})
	}
}

func (h *Hub) handleSend(ctx context.Context, c *client, f clientFrame) {
	var raw any
	present := len(f.Payload) > 0
	if present {
		if err := json.Unmarshal(f.Payload, &raw); err != nil {
			sendFrame(c, errorFrame{Type: TypeError, Code: roommsg.CodeInvalidMessage, Message: "Invalid payload"})
			return
		}
	}
	_, payloadJSON, verr := roommsg.ValidatePush(
		raw, present, c.userID, c.hostToken,
		c.roomInfo.HostID, c.roomInfo.HostTokenHash, h.cfg.MaxPayloadBytes,
	)
	if verr != nil {
		sendFrame(c, errorFrame{Type: TypeError, Code: verr.Code, Message: verr.Message})
		return
	}

	newID, err := h.store.InsertMessage(ctx, c.roomID, c.userID, string(payloadJSON))
	if err != nil {
		sendFrame(c, errorFrame{Type: TypeError, Code: roommsg.CodeInvalidMessage, Message: "Message send failed"})
		return
	}
	_ = h.store.PruneMessages(ctx, c.roomID, h.cfg.MaxRoomMessages)
	_ = h.store.TouchRoom(ctx, c.roomID)

	data, err := marshalFrame(messageFrame{
		Type:     TypeMessage,
		ID:       newID,
		SenderID: c.userID,
		Payload:  json.RawMessage(payloadJSON),
	})
	if err != nil {
		return
	}
	h.broadcast(c.roomID, data)
}

func (h *Hub) handleResume(ctx context.Context, c *client, f clientFrame) {
	since := f.Since
	if since < 0 {
		since = 0
	}
	rows, err := h.store.ListMessages(ctx, c.roomID, since, h.cfg.PollLimit)
	if err != nil {
		sendFrame(c, errorFrame{Type: TypeError, Code: roommsg.CodeRoomNotFound, Message: "Room not found or expired"})
		return
	}
	lastID := since
	for _, row := range rows {
		frame, merr := marshalFrame(messageFrame{
			Type:     TypeMessage,
			ID:       row.ID,
			SenderID: row.SenderID,
			Payload:  rawPayload(row.Payload),
		})
		if merr != nil {
			continue
		}
		c.enqueue(frame)
		lastID = row.ID
	}
	sendFrame(c, resumedFrame{Type: TypeResumed, LastID: lastID})
}

// writePump 串行把 send 通道中的帧写到连接。
func (h *Hub) writePump(ctx context.Context, c *client) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.closed:
			return
		case data := <-c.send:
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.ws.Write(wctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				c.close()
				return
			}
		}
	}
}

// pingPump 周期性发送 WS ping，超时未收到 pong 则断开连接。
func (h *Hub) pingPump(ctx context.Context, c *client) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.closed:
			return
		case <-ticker.C:
			pctx, cancel := context.WithTimeout(ctx, pongTimeout)
			err := c.ws.Ping(pctx)
			cancel()
			if err != nil {
				c.close()
				return
			}
		}
	}
}

// register 把连接归组到对应房间；Hub 已关闭时返回 nil。
func (h *Hub) register(c *client) *room {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	rm := h.rooms[c.roomID]
	if rm == nil {
		rm = newRoom(c.roomID)
		h.rooms[c.roomID] = rm
	}
	rm.add(c)
	h.wg.Add(2)
	return rm
}

func (h *Hub) removeClient(c *client) {
	h.mu.Lock()
	rm := h.rooms[c.roomID]
	empty := false
	if rm != nil {
		empty = rm.remove(c)
		if empty {
			delete(h.rooms, c.roomID)
		}
	}
	h.mu.Unlock()
	if rm != nil && !empty {
		h.broadcastPresence(c.roomID)
	}
}

func (h *Hub) broadcast(roomID string, data []byte) {
	h.mu.Lock()
	rm := h.rooms[roomID]
	h.mu.Unlock()
	if rm != nil {
		rm.broadcast(data)
	}
}

func (h *Hub) broadcastPresence(roomID string) {
	h.mu.Lock()
	rm := h.rooms[roomID]
	h.mu.Unlock()
	if rm == nil {
		return
	}
	data, err := marshalFrame(presenceFrame{Type: TypePresence, Members: rm.presence()})
	if err != nil {
		return
	}
	rm.broadcast(data)
}

func (h *Hub) isClosed() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closed
}

// Close 停止 Hub 的所有连接与后台 goroutine。
func (h *Hub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	rooms := make([]*room, 0, len(h.rooms))
	for _, r := range h.rooms {
		rooms = append(rooms, r)
	}
	h.rooms = make(map[string]*room)
	h.mu.Unlock()

	for _, r := range rooms {
		r.closeAll()
	}
	h.wg.Wait()
}

// reject 发送错误帧后以策略关闭握手，用于 hello 阶段失败。
func (h *Hub) reject(ctx context.Context, ws *websocket.Conn, code, message string) {
	if data, err := marshalFrame(errorFrame{Type: TypeError, Code: code, Message: message}); err == nil {
		wctx, cancel := context.WithTimeout(ctx, writeTimeout)
		_ = ws.Write(wctx, websocket.MessageText, data)
		cancel()
	}
	_ = ws.Close(websocket.StatusPolicyViolation, code)
}

func (h *Hub) recoverPump() {
	if rec := recover(); rec != nil {
		h.logger.Error("realtime pump panic recovered", "panic", rec, "stack", string(debug.Stack()))
	}
}

func sendFrame(c *client, v any) {
	data, err := marshalFrame(v)
	if err != nil {
		return
	}
	c.enqueue(data)
}

// rawPayload 透传落库的 payload 文本；非法 JSON 退回 null（对齐 HTTP pull）。
func rawPayload(s string) json.RawMessage {
	if json.Valid([]byte(s)) {
		return json.RawMessage(s)
	}
	return json.RawMessage("null")
}

// marshalFrame 紧凑编码并禁用 HTML 转义，保持与落库 payload 一致。
func marshalFrame(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
