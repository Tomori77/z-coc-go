# z-coc WebSocket 房间协议（Phase 3a · 后端）

> 对应实现：`internal/realtime`（`hub.go` / `room.go` / `protocol.go`）、`internal/roommsg`。
> 目标：为选择实时通道的客户端提供房间消息推送，替代 1.5s HTTP 轮询；**HTTP `room_api.php?action=push|pull` 保持不变，作为回退通道**。
> 消息仍持久化到 `coc_room_messages`，因此 HTTP `pull` 依旧能取到同一批消息。

---

## 1. 端点

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/room_ws` | WebSocket 升级端点（主） |
| GET | `/ws/room` | 同上，别名 |

- 同一端口、同一容器，与 HTTP/静态资源同源，避免额外 CORS/证书问题。
- 升级失败（非 WebSocket 请求、Origin 不被允许等）由底层库直接拒绝。
- **Origin 校验**：与 HTTP CORS 一致。同源始终允许；跨域仅放行 `APP_ALLOWED_ORIGINS` 中列出的来源（原样作为 Origin 模式匹配，如 `https://localhost`）。移动端（Capacitor，源 `https://localhost`）需将该源列入 `APP_ALLOWED_ORIGINS`。

---

## 2. 连接流程

```
客户端                                服务端
  │  建立 WS 连接                       │
  │  ── text: hello ──────────────────▶ │  校验房间存在/未过期、room_id/user_id 合法
  │  ◀───────────────── text: ready ──  │  {last_id, members}
  │  ◀──────────────── text: presence ─ │  广播（可选，连接/断开时触发）
  │                                     │
  │  ── text: send ────────────────────▶ │  校验 → 落库 → 广播 message
  │  ◀───────────────── text: message ── │  房间内所有人
  │                                     │
  │  ── text: ping ────────────────────▶ │
  │  ◀───────────────── text: pong ────  │
  │                                     │
  │  断线后重连                          │
  │  ── text: hello ──────────────────▶ │
  │  ◀───────────────── text: ready ───  │
  │  ── text: resume {since} ──────────▶ │  补发 id > since 的消息
  │  ◀──────────────── text: message ──  │  （升序、受 pull 同款 limit 限制）
  │  ◀──────────────── text: resumed ──  │  {last_id}
```

**首帧必须是 `hello`**。任何非 `hello` 首帧 → `error` 后关闭。
连接建立即视为“在线”，断开即视为“离线”。

---

## 3. 消息类型总览

### 客户端 → 服务端

| type | 字段 | 含义 |
|---|---|---|
| `hello` | `room_id`, `user_id`, `host_token?` | 握手，标识房间与自身身份 |
| `send` | `payload` | 发送一条房间消息（payload 与 HTTP push 同构） |
| `resume` | `since` | 断线重连后请求补发 `id > since` 的消息 |
| `ping` | — | 应用层心跳，服务端回 `pong` |

### 服务端 → 客户端

| type | 字段 | 含义 |
|---|---|---|
| `ready` | `last_id`, `members` | hello 成功应答 |
| `message` | `id`, `sender_id`, `payload` | 房间消息（发送/补发/广播） |
| `resumed` | `last_id` | 补发完成 |
| `presence` | `members` | 成员在线状态变化 |
| `error` | `code`, `message` | 校验失败或非法帧 |
| `pong` | — | 应用层心跳应答 |

---

## 4. 客户端 → 服务端 帧

### 4.1 hello（首帧，必需）

```json
{"type":"hello","room_id":"ABC123","user_id":"host1","host_token":"optional"}
```

- `room_id`：`^[A-Z0-9_-]{6,16}$`（大小写归一为大写）。
- `user_id`：`^[a-zA-Z0-9_-]+$`，长度 ≤ 64。
- `host_token`：可选；来自 HTTP `create` 返回的 `host_token`。主机类消息需要它。

成功应答：

```json
{"type":"ready","last_id":42,"members":[{"user_id":"host1","online":true}]}
```

`last_id` 为当前房间最大消息 id（房间为空为 0）。`members` 为当前已知成员的在线表。

失败应答（随后关闭）：

```json
{"type":"error","code":"ROOM_NOT_FOUND","message":"Room not found or expired"}
```

### 4.2 send（发送消息）

```json
{"type":"send","payload":{"type":"chat","user":{"id":"host1"},"text":"hi"}}
```

- `room_id` 与 `sender_id` 取自 `hello`，无需在 `send` 中重复。
- `host_token` 取自 `hello`，用于主机类消息鉴权。
- 服务端对 `payload` 应用与 HTTP `push` **完全相同**的校验（见 §7）。

成功：落库后向房间所有连接广播：

```json
{"type":"message","id":43,"sender_id":"host1","payload":{"type":"chat","text":"hi"}}
```

失败：仅回给发送者：

```json
{"type":"error","code":"HOST_AUTH_FAILED","message":"Only host can send this message"}
```

### 4.3 resume（断线重连补发）

```json
{"type":"resume","since":40}
```

服务端按 `id > since` **升序**补发（每条一个 `message` 帧，数量受 `poll_limit` 限制，与 HTTP `pull` 一致），随后：

```json
{"type":"resumed","last_id":43}
```

若积压超过 `poll_limit`，客户端可再次发送 `resume {since: last_id}` 继续拉取。

### 4.4 ping

```json
{"type":"ping"}
```

应答：

```json
{"type":"pong"}
```

---

## 5. 服务端 → 客户端 帧

### 5.1 message

```json
{"type":"message","id":43,"sender_id":"host1","payload":{"type":"chat","text":"hi"}}
```

`payload` 为落库的原始 JSON 对象，透传不改写。

### 5.2 presence

成员连接/断开时向房间内所有成员广播：

```json
{"type":"presence","members":[{"user_id":"host1","online":true},{"user_id":"p2","online":false}]}
```

### 5.3 error

```json
{"type":"error","code":"SENDER_MISMATCH","message":"Sender mismatch"}
```

错误码：

| code | 触发场景 | 对应 HTTP 语义 |
|---|---|---|
| `ROOM_NOT_FOUND` | 房间不存在/已过期；Hub 关闭 | 404 |
| `INVALID_MESSAGE` | 首帧非 hello、非法 JSON、缺 payload、消息类型非法、编码失败 | 400 |
| `HOST_AUTH_FAILED` | 主机类消息但 `host_token`/`sender_id` 不匹配 | 403 + `code` |
| `SENDER_MISMATCH` | payload 声明的发送者与连接身份不一致 | 403 |
| `PAYLOAD_TOO_LARGE` | payload 超 `max_payload_bytes` | 413 |

---

## 6. presence 语义

- **WS presence 由本实例内存连接集派生**：`online = 该成员当前有活跃 WS 连接`。
- 房间会记住曾出现过的成员：成员断开后仍会以 `online:false` 出现在后续 `presence` 帧中；房间内连接全部断开时，该房间从 Hub 移除。
- 连接建立时仍会 **UPSERT `coc_room_presence`**，因此 HTTP 轮询客户端（`pull` 返回 `presence[{user_id,seen_at}]`）看到的在线表与 WS 侧保持一致。
- 纯 HTTP 轮询的成员不会出现在 WS `presence` 帧中（反之亦然）；过渡期两套通道并存属预期。

---

## 7. 校验规则（与 HTTP push 完全共用）

共享实现位于 `internal/roommsg/validate.go`，HTTP `push` 与 WS `send` 调用同一函数，因此白名单/鉴权规则不会分叉：

1. `payload` 缺失或非对象/数组 → `INVALID_MESSAGE`（"Missing payload"）。
2. `payload.type` 为空或长度 > 64 → `INVALID_MESSAGE`（"Invalid message type"）。
3. **主机专属白名单**（18 类：`sync_state`、`sync_log_append`、`update_last_message`、`update_last_message_full`、`sync_module`、`sync_investigators`、`sync_assignments`、`game_start`、`game_ended`、`room_closed`、`grant_cot_access`、`coj_toggle`、`generating_start`、`generating_end`、`regenerate_start`、`distribute_reports`、`kick_player`、`move_to_spectator`）需 `sender_id == host_id` 且 `host_token` 的 sha256 与库中主机 token 哈希一致，否则 `HOST_AUTH_FAILED`。
4. **例外** `is_player_log_append`：玩家本人 `sync_log_append`（`role` 为 `user`/`whisper`、非 AI、`senderId`/`id` 匹配）可绕过第 3 条。
5. 非主机类消息（或上述例外）校验 payload 声明的发送者（`user.id` / `userId`，或日志的 `senderId`/`id`）与 `sender_id` 一致，否则 `SENDER_MISMATCH`。
6. payload 编码后超过 `max_payload_bytes` → `PAYLOAD_TOO_LARGE`。
7. 通过后落库 `coc_room_messages`、裁剪至 `max_room_messages`、`touch_room`，再广播。

> 校验通过的消息与 HTTP `push` 落库字节一致，HTTP `pull` 返回内容不变。

---

## 8. 心跳与断线

- **应用层**：客户端发 `{"type":"ping"}`，服务端回 `{"type":"pong"}`。
- **协议层**：服务端每约 **30s** 发送一次 WebSocket `ping`；若在约 **2 个周期（60s）** 内未收到 `pong`，判定掉线并关闭连接、广播 `presence`（该成员转为 `online:false`）。
- 连接关闭（正常/异常）都会从房间移除并触发 presence 广播。
- 客户端建议记录 `lastMessageId`，重连后走 `hello` → `resume` 补齐。

---

## 9. 反向代理配置（openresty / nginx）

WebSocket 需要代理透传升级头并放宽读超时，例如：

```nginx
location /room_ws {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_read_timeout 3600s;   # 长连接：放宽读超时
    proxy_send_timeout 3600s;
}
```

要点：

- 必须透传 `Upgrade` 与 `Connection` 头，否则升级失败。
- `proxy_read_timeout` 需显著大于心跳周期（建议 ≥ 1h），避免空闲连接被反代切断。
- 若 `/ws/room` 别名也对外使用，需为其配置相同规则（或统一前缀）。

---

## 10. 与 HTTP 回退通道的关系

- `room_api.php?action=create|join|push|pull` 的请求/响应契约**保持完全不变**。
- WS 与 HTTP 写入的是同一张 `coc_room_messages` 表，`pull` 能取到 WS 发送的消息。
- 客户端可在 WS 不可用时回退到 1.5s 轮询，互不影响。
- 单实例 Hub 仅覆盖本实例连接；当前单容器部署足够（多实例需引入跨实例广播，暂不需要）。
