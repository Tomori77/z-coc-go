# AGENTS.md — z-coc-go「AI 续作总结」

> 面向**新接手的 AI 模型 / 开发者**：读完本文件即可继续本项目。改代码前请先读本文件与
> [`docs/GO_BACKEND_PLAN.md`](./docs/GO_BACKEND_PLAN.md)，改完请按本文件「变更流程」自检。

---

## 1. 一句话定位

**z-coc 的 Go 后端 + 前端静态资源独立仓库**。原 PHP 后端在 `Tomori77/z-coc`，本仓库已用 Go **复刻其 API 契约**（`?action=` 路由、JSON 信封、Bearer 鉴权、PostgreSQL 表结构），前端静态资源与原站保持一致。

---

## 2. 技术栈

| 层 | 选型 |
|---|---|
| 后端 | **Go 1.26** + `pgx`(v5) + **PostgreSQL** + 标准库 `net/http` |
| 前端 | **Vue 3（CDN 全局版）** 静态资源（无构建框架，`web/` 内直接托管） |
| 实时 | `github.com/coder/websocket`（WebSocket 房间 Hub） |
| 密码 | `golang.org/x/crypto/bcrypt`，**兼容 PHP `$2y$` 哈希**（老账号可直接登录） |
| 日志/配置 | 标准库 `log/slog` + 环境变量（无 `config.local.php`） |

---

## 3. 目录地图

```
z-coc-go/
├─ cmd/server/            程序入口（组装 config / store / router，启动 HTTP 服务）
├─ internal/
│  ├─ config/             环境变量解析（PGSQL_* / DB_* / DATABASE_URL / APP_ALLOWED_ORIGINS / APP_PORT）
│  ├─ store/              pgxpool + 各领域查询 + schema（幂等建表）
│  ├─ httpapi/            路由、中间件、JSON 信封、各 action 处理器、静态托管
│  ├─ auth/               bcrypt、token 生成/哈希、Bearer 解析
│  ├─ validate/           user_id / room_id / owner_token 校验
│  ├─ ratelimit/          固定窗口限流（api_rate_limits 表）
│  ├─ realtime/           WebSocket Hub / 房间通道 / presence / 心跳 / 续传
│  └─ roommsg/            HTTP push 与 WS send 共用的 payload 校验
├─ web/                   前端静态资源（由 web/embed.go 经 go:embed 打进二进制）
├─ tools/                 api-smoke.ps1（验收）、build-mobile.mjs、optimize_art.py
├─ docs/                  GO_BACKEND_PLAN / GO_BACKEND_TASKS / WS_PROTOCOL / DEPLOYMENT
├─ Dockerfile             多阶段构建 → distroless（约 15–25MB）
├─ docker-compose.yml     方案一：应用单容器 + 连接「已有」PostgreSQL 容器
├─ docker-compose.full.yml 方案二：应用 + PostgreSQL 一起管理
└─ .env.example           环境变量示例（复制为 .env）
```

---

## 4. 如何构建 / 运行 / 测试

```bash
# 构建（编译全部包）
go build ./...

# 运行：先设置 PGSQL_* 环境变量（默认监听 8080）
go run ./cmd/server
```

**验收（HTTP API）**：

```powershell
tools/api-smoke.ps1 -BaseUrl http://127.0.0.1:8080
# 期望：27 passed, 0 failed（27/27）
```

**WebSocket**：端点为 `GET /room_ws`（别名 `GET /ws/room`），协议见
[`docs/WS_PROTOCOL.md`](./docs/WS_PROTOCOL.md)。仓库**暂无 WS 测试脚本**，可自行用任意
WebSocket 客户端按协议验证（首帧必须 `hello` → `ready` → `send`/`resume`/`ping`）。

---

## 5. 关键契约（改动务必保住）

- **路由**：`?action=` 风格（`db_api.php` / `room_api.php` / `library_api.php` / `health.php` / `dl/download.php`），同时保留无 `.php` 的别名路径。
- **信封**：成功 `{ "status": "success", … }`；失败 `{ "status": "error", "message": "…" }`。
- **鉴权**：`Authorization: Bearer <token>`；token 只存 sha256。
- **`pull`（云存档）无数据时返回裸 `null`**（历史契约，勿改成 `{}`）。
- **`sync` 乐观锁冲突返回 `409 {status:"conflict", …}`**。
- **房间主机类消息缺/错 `host_token` 返回 `403 {code:"HOST_AUTH_FAILED"}`**。
- **HTTP `push`/`pull` 保留为 WebSocket 的回退通道**，与 WS 写入同一张 `coc_room_messages` 表。

---

## 6. 约定

- **只改必要文件**，避免大范围重构。
- Go 代码注释**沿用英文风格**；**用户可见文案用中文**。
- **不要**把密钥/密码/连接串写进代码或提交到仓库；一律走环境变量（`.env` 已被 gitignore）。

---

## 7. 当前进度（对照 `docs/GO_BACKEND_TASKS.md`）

- **Phase 0 / 1 / 2 / 3 完成**：
  - Go API 契约 **27/27**；
  - Go 托管静态资源（`go:embed` + 缓存/安全头/CSP/SPA 回退）；
  - WebSocket 前后端（前端把 1.5s 轮询改为 WS，**保留 HTTP 回退**）。
- **Phase 4 待做**：好友功能、多人实时游戏大厅、管理员后台、战斗界面、地图。

---

## 8. 已知缺口 / 注意事项

- 前端 **WS 路径尚未做端到端浏览器实测**（后端协议与后端自测已完成）。
- 反向代理（openresty/nginx）需**透传 `Upgrade`/`Connection`** 并**放宽读超时**，否则长连接会被断开（见 `docs/WS_PROTOCOL.md` §9）。
- `.env` **不入库**；`.env.example` 仅供复制参考。

---

## 9. 变更流程建议

1. 改前先读本文件与 `docs/GO_BACKEND_PLAN.md`（契约以 `tools/api-smoke.ps1` 为准）。
2. 改后跑 `go build ./...`，并对运行中的服务跑 `tools/api-smoke.ps1`（期望 27/27）。
3. 涉及实时/静态头/部署的改动，相应更新 `docs/WS_PROTOCOL.md` 或 `docs/DEPLOYMENT.md`。
4. 按需更新本文档「当前进度」小节。
