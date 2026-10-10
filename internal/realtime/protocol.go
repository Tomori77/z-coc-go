// Package realtime 提供每实例一个的 WebSocket 房间 Hub：连接归组、消息广播、
// presence 派生、心跳与断线续传。HTTP push/pull 作为回退通道保持不变。
package realtime

import "encoding/json"

// 帧类型（协议已冻结，改动需同步 docs/WS_PROTOCOL.md）。
const (
	TypeHello    = "hello"
	TypeReady    = "ready"
	TypeSend     = "send"
	TypeMessage  = "message"
	TypeResume   = "resume"
	TypeResumed  = "resumed"
	TypePresence = "presence"
	TypeError    = "error"
	TypePing     = "ping"
	TypePong     = "pong"
)

// Member 是 presence 帧里的一名成员。
type Member struct {
	UserID string `json:"user_id"`
	Online bool   `json:"online"`
}

// clientFrame 是客户端 → 服务端的联合帧结构，按 type 取用相应字段。
type clientFrame struct {
	Type      string          `json:"type"`
	RoomID    string          `json:"room_id"`
	UserID    string          `json:"user_id"`
	HostToken string          `json:"host_token"`
	Payload   json.RawMessage `json:"payload"`
	Since     int64           `json:"since"`
}

// readyFrame 是 hello 成功后服务端的应答。
type readyFrame struct {
	Type    string   `json:"type"`
	LastID  int64    `json:"last_id"`
	Members []Member `json:"members"`
}

// messageFrame 是一条被持久化并广播的房间消息；payload 原样透传落库内容。
type messageFrame struct {
	Type     string          `json:"type"`
	ID       int64           `json:"id"`
	SenderID string          `json:"sender_id"`
	Payload  json.RawMessage `json:"payload"`
}

// presenceFrame 在成员连接/断开时广播。
type presenceFrame struct {
	Type    string   `json:"type"`
	Members []Member `json:"members"`
}

// errorFrame 是校验失败或非法帧的应答。
type errorFrame struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// resumedFrame 在续传补发完成后发送。
type resumedFrame struct {
	Type   string `json:"type"`
	LastID int64  `json:"last_id"`
}

// controlFrame 是 ping/pong 这类无字段帧。
type controlFrame struct {
	Type string `json:"type"`
}
