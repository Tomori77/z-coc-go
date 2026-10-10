// Package roommsg 抽取 HTTP push 与 WebSocket send 共用的房间消息校验逻辑，
// 避免白名单/鉴权规则在两条通道间出现分歧。
package roommsg

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/Tomori77/z-coc/internal/auth"
)

// 校验错误码（HTTP push 与 WS send 共用）。
const (
	CodeHostAuthFailed  = "HOST_AUTH_FAILED"
	CodeSenderMismatch  = "SENDER_MISMATCH"
	CodePayloadTooLarge = "PAYLOAD_TOO_LARGE"
	CodeInvalidMessage  = "INVALID_MESSAGE"
	CodeRoomNotFound    = "ROOM_NOT_FOUND"
)

// Error 描述一条被拒绝的房间消息。
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

// hostOnlyMessageTypes 复刻 room_api.php 的 is_host_only_message() 白名单。
var hostOnlyMessageTypes = map[string]bool{
	"sync_state":               true,
	"sync_log_append":          true,
	"update_last_message":      true,
	"update_last_message_full": true,
	"sync_module":              true,
	"sync_investigators":       true,
	"sync_assignments":         true,
	"game_start":               true,
	"game_ended":               true,
	"room_closed":              true,
	"grant_cot_access":         true,
	"coj_toggle":               true,
	"generating_start":         true,
	"generating_end":           true,
	"regenerate_start":         true,
	"distribute_reports":       true,
	"kick_player":              true,
	"move_to_spectator":        true,
}

// IsHostOnlyMessage 报告消息类型是否属于主机专属白名单。
func IsHostOnlyMessage(messageType string) bool { return hostOnlyMessageTypes[messageType] }

// IsPlayerLogAppend 复刻 is_player_log_append()：玩家本人可绕过主机校验的日志追加。
func IsPlayerLogAppend(payload map[string]any, senderID string) bool {
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

// ValidatePush 应用 room_api.php push 的全部校验规则，供 HTTP push 与 WS send 共用。
// raw 为解码后的 payload，present 表示 payload 字段是否存在。
// 校验通过时返回规范化后的 payload 及其 JSON 编码；否则返回 *Error。
func ValidatePush(raw any, present bool, senderID, hostToken, hostID string, hostTokenHash *string, maxPayloadBytes int64) (map[string]any, []byte, *Error) {
	var payload map[string]any
	if !present {
		return nil, nil, &Error{Code: CodeInvalidMessage, Message: "Missing payload"}
	}
	switch t := raw.(type) {
	case map[string]any:
		payload = t
	case []any:
		payload = map[string]any{}
	default:
		return nil, nil, &Error{Code: CodeInvalidMessage, Message: "Missing payload"}
	}

	messageType := asString(payload["type"])
	if messageType == "" || len(messageType) > 64 {
		return nil, nil, &Error{Code: CodeInvalidMessage, Message: "Invalid message type"}
	}

	playerLogAppend := IsPlayerLogAppend(payload, senderID)

	if IsHostOnlyMessage(messageType) && !playerLogAppend {
		trimmed := strings.TrimSpace(hostToken)
		hostTokenHashValue := auth.Sha256Hex(trimmed)
		if senderID != hostID || trimmed == "" || hostTokenHash == nil || *hostTokenHash != hostTokenHashValue {
			return nil, nil, &Error{Code: CodeHostAuthFailed, Message: "Only host can send this message"}
		}
	}

	if !IsHostOnlyMessage(messageType) || playerLogAppend {
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
			return nil, nil, &Error{Code: CodeSenderMismatch, Message: "Sender mismatch"}
		}
	}

	payloadJSON, err := marshalJSON(payload)
	if err != nil {
		return nil, nil, &Error{Code: CodeInvalidMessage, Message: "Payload encode failed"}
	}
	if int64(len(payloadJSON)) > maxPayloadBytes {
		return nil, nil, &Error{Code: CodePayloadTooLarge, Message: "Payload too large"}
	}
	return payload, payloadJSON, nil
}

// asString 复刻 PHP 对 JSON 标量的 (string) 转换（与 httpapi 的实现保持一致）。
func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == math.Trunc(t) && !math.IsInf(t, 0) && !math.IsNaN(t) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "1"
		}
		return ""
	default:
		return ""
	}
}

// isTruthy 复刻 PHP empty()/布尔真值判断。
func isTruthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != "" && t != "0"
	case float64:
		return t != 0
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	default:
		return false
	}
}

// marshalJSON 紧凑编码并禁用 HTML 转义，等价于 PHP 的 JSON_UNESCAPED_UNICODE。
func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
