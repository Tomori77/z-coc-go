# z-coc Go 后端技术选型 · 迁移计划 · 路线图

> 版本：v0.1 ｜ 生成日期：2026-10-10 ｜ 状态：**规划中，尚未执行**
> 本文只做**技术选型 + 迁移计划 + 需求路线图**，不含任何代码实现，不修改现有文件。
> 配套文档：总纲 [`PROJECT_GUIDE.md`](./PROJECT_GUIDE.md)、拆分计划 [`SPLIT_PLAN.md`](./SPLIT_PLAN.md)、好友功能 [`FRIENDS_FEATURE_TASK.md`](./FRIENDS_FEATURE_TASK.md)。
> 文中行号为当前快照，重构后会漂移，以符号名/文件名为准。

---

## 1. 背景与目标

### 1.1 现状

- 后端为 **PHP 8.3 + Apache**（`Dockerfile` 基于 `php:8.3-apache-bookworm`），数据库已迁移到 **PostgreSQL**（PDO `pgsql`，见 `lib/Database.php`）。
- 应用逻辑按领域拆在 5 个入口：`db_api.php`（账号 + 云存档）、`room_api.php`（多人房间）、`library_api.php`（模组库）、`health.php`（健康检查）、`dl/download.php`（角色卡导出）。
- 多人同步当前是 **HTTP 轮询**：前端每 **1.5 秒**拉一次 `room_api.php?action=pull`（`index.html` 的 `startRoomPolling` / `pollRoomMessages`，`roomPollTimer = setInterval(pollRoomMessages, 1500)`）。
- 移动端为 **Capacitor Android** 壳（`capacitor.config.json`），`webDir=www`，WebView 源 `https://localhost`，API 指向生产域名。

### 1.2 痛点

1. **内存占用高**：Apache + mod_php（`php-production.ini` 里 `memory_limit = 192M`）常驻进程多、每请求 fork/线程开销大，小机器上偏重。
2. **部署复杂**：需要维护 Apache 站点配置（`docker/apache-z-coc.conf`）、`.htaccess`（根 `.htaccess` + `lib/.htaccess`）、PHP 扩展（`pdo_pgsql`）。
3. **响应慢**：PHP 每请求重新初始化 + 轮询间隔固定 1.5s，实时性受制于轮询节奏。
4. **游玩体验**：轮询带来可见延迟、无谓流量与数据库压力（详见 §7）。

### 1.3 目标（用户明确诉求）

| 目标 | 含义 |
|---|---|
| **省内存** | 单进程、低 RSS，替换 Apache + PHP |
| **部署简单** | 一个容器同时提供静态前端 + API，无 Apache/.htaccess |
| **快** | 更短启动、更低单请求开销 |
| **响应速度** | 消除轮询延迟，改推送 |
| **游玩体验** | 多人同步近实时、掉线/在线状态更准 |

### 1.4 已决策方向（Decisions）

- **后端整体重写为 Go**。
- **单容器同时托管静态前端与 API**，彻底移除 PHP + Apache。
- **数据库保持 PostgreSQL**，沿用现有表结构，不迁移数据。
- **多人通信从 HTTP 轮询改为 WebSocket**。

---

## 2. 技术选型

### 2.1 推荐 Go 技术栈

| 关注点 | 选型 | 理由 |
|---|---|---|
| Go 版本 | **Go 1.23+**（建议 1.23 或 1.24） | 稳定、`net/http` 增强路由已可用、`log/slog` 成熟 |
| HTTP 路由 | **标准库 `net/http` 增强路由**（Go 1.22+ `GET /path/{id}` 模式） | 零依赖、够用；如需要中间件链/子路由再引入 `chi` |
| 路由备选 | `github.com/go-chi/chi/v5` | 轻量、中间件生态好；**仅在标准库不够用时启用** |
| PostgreSQL 驱动 | **`github.com/jackc/pgx/v5`**（+ `pgxpool`） | 原生协议、性能好、连接池内建，替代 PDO |
| WebSocket | **`github.com/coder/websocket`**（原 nhooyr.io/websocket） | 现代、`context` 优先、依赖少、维护活跃；纯 `net/http` 兼容 |
| WebSocket 备选 | `github.com/gorilla/websocket` | 生态老牌、资料多；但 API 偏回调、依赖略重，作为备选 |
| 密码哈希 | **`golang.org/x/crypto/bcrypt`** | 与 PHP `password_hash(PASSWORD_DEFAULT)` 的 bcrypt 输出**可互相验证**，保证老用户仍能登录 |
| 配置 | **环境变量**：`PGSQL_*`、`DB_*`、`DATABASE_URL`（沿用 `lib/Config.php` 的优先级） | 与 Zeabur / docker-compose 注入方式一致，`config.local.php` 可废弃 |
| 结构化日志 | **标准库 `log/slog`**（JSON handler） | 零依赖，输出可被容器日志采集 |
| 迁移 | **保留 `CREATE TABLE IF NOT EXISTS` 幂等建表**（首期）；后续可选 `github.com/pressly/goose` | 首期与 PHP 行为等价、零风险；独立 SQL 迁移是长期目标（对应 `SPLIT_PLAN.md` 阶段 4） |

**关于 bcrypt 兼容**：PHP `password_hash($password, PASSWORD_DEFAULT)` 默认即 bcrypt（`$2y$` 前缀）。Go `bcrypt.CompareHashAndPassword` 可校验 `$2y$` 哈希，无需强制老用户改密。新注册可用 Go 生成 bcrypt 哈希，PHP 侧同样可校验，因此 **Phase 1 双跑期账号体系互通**。

**关于配置兼容**：`lib/Config.php` 的读取顺序为 `PGSQL_HOST/PGSQL_PORT/PGSQL_USERNAME/PGSQL_PASSWORD/PGSQL_DATABASE` → `DB_*` → `DATABASE_URL` 解析；`APP_ALLOWED_ORIGINS` 控制 CORS。Go 侧**原样复刻**该优先级，保证同一套环境变量无缝切换。

**关于迁移策略**：`db_api.php` / `room_api.php` / `library_api.php` / `lib/RateLimit.php` 目前用 `ensure_*_schema()` + `app_run_periodically()` 幂等建表。Go 首期**继续用 `CREATE TABLE IF NOT EXISTS`**（内嵌 SQL 常量，启动时执行一次），保持零结构差异；是否引入 `goose` 属于开放问题（§13）。

### 2.2 为什么是 Go

- **单个静态二进制**：`CGO_ENABLED=0` 编译，无运行时、无扩展、无 `php.ini`。
- **低内存**：空载 RSS 通常 **10–30MB**，远低于 Apache + mod_php 的多进程常驻。
- **极小镜像**：多阶段构建 + `gcr.io/distroless/static` / `scratch`，镜像约 **15–25MB**。
- **无 Apache/.htaccess**：静态头、gzip/brotli、SPA 回退、安全头全部在 Go 中间件里实现（§6）。
- **天然适合长连接**：goroutine + `net/http` 处理 WebSocket 极其轻量，一个实例可轻松承载目标规模的房间连接。
- **部署简单**：一个容器、一个端口，`docker compose up -d` 即可；健康检查走内置 `/health`。

### 2.3 备选方案对比（简短）

| 维度 | Go（推荐） | Node.js | Bun |
|---|---|---|---|
| 单二进制 | ✅ 静态二进制 | ❌ 需 Node 运行时 | ⚠️ 可编译但仍偏运行时 |
| 空载 RSS | ~10–30MB | ~40–80MB | ~30–60MB |
| 镜像体积 | ~15–25MB | ~100MB+ | ~90MB+ |
| 长连接并发 | 极强（goroutine） | 强（事件循环） | 强 |
| 类型安全 | ✅ 编译期 | ⚠️ 需 TS | ⚠️ 需 TS |
| 与现有 PHP 契约迁移成本 | 低（纯 HTTP/JSON） | 低 | 低 |
| 团队/生态熟悉度 | 中 | 高 | 低 |

结论：**Go 在资源占用、镜像体积、部署简单度上全面胜出**，且与「省内存 + 简单部署」的目标最契合。

---

## 3. 目标架构

```
                          ┌─────────────────────────────────────────────┐
                          │              单个 Docker 容器                 │
                          │   （Go 静态二进制，无 Apache / 无 PHP）        │
   浏览器 / PWA            │                                             │
   ┌──────────┐   HTTPS    │   ┌───────────────┐   ┌─────────────────┐  │
   │ index.html│──────────▶│   │  静态资源层    │   │   JSON API      │  │
   │ Workshop  │           │   │  (embed.FS)   │   │  /db_api …      │  │
   │ Library   │           │   │  gzip/brotli  │   │  /room_api …    │  │
   └──────────┘           │   │  缓存/安全头   │   │  /library_api … │  │
        │                 │   │  SPA fallback │   │  /health        │  │
        │ fetch(?action=) │   └───────────────┘   └─────────────────┘  │
        │ WebSocket       │                                             │
        │                 │   ┌───────────────────────────────┐         │
        ▼                 │   │  realtime：连接 Hub + 房间通道  │         │
   ┌──────────┐           │   │  presence / 心跳 / 断线续传     │         │
   │ Capacitor│──────────▶│   └───────────────┬───────────────┘         │
   │ Android  │  HTTPS    └───────────────────┼─────────────────────────┘
   └──────────┘                              │ pgxpool
                                             ▼
                                   ┌───────────────────┐
                                   │  PostgreSQL（外部） │  ← 沿用现有库/表
                                   └───────────────────┘

   宿主机（1Panel）：openresty 负责 TLS 终止 + 反向代理（Upgrade 透传）→ 容器:8080
```

**关键点**

- 前端（`index.html` / `Workshop` / `Library` / `404.html` / `assets/`）与 Capacitor 壳**无需改动**，只要 **API 契约与静态头保持不变**（§4、§6）。
- 原生端通过 `ZCOC_RUNTIME.apiUrl()` 解析 API 地址（`assets/js/app-runtime.js`），仍指向生产域名，Go 服务天然兼容。
- WebSocket 与 HTTP 走同一容器、同一端口（同域），避免额外 CORS 与证书问题。

---

## 4. API 契约保留

> 迁移的**第一原则是契约不变**：保留 `?action=` 路由、统一 JSON 信封、Bearer 鉴权、HTTP 状态码、限流 scope。`tools/api-smoke.ps1` 是验收基准（**27 条断言**，见 §12）。

### 4.1 统一约定（必须复刻）

- **信封**：成功 `{ "status": "success", … }`；失败 `{ "status": "error", "message": "…" }`（`lib/Http.php` 的 `app_json_response`）。
- **鉴权**：`Authorization: Bearer <token>`；`lib/Http.php` 的 `app_get_bearer_token`。
- **方法校验**：不匹配返回 **405**（`app_require_method`）。
- **请求体上限**：超限返回 **413**（`app_require_content_length`）；非法 JSON 返回 **400**（`app_read_json_body`）。
- **CORS**：默认同域不设；`APP_ALLOWED_ORIGINS` 逗号分隔白名单，**禁用 `*`**；`OPTIONS` 返回 **204**。
- **限流**：按 `IP:scope` 固定窗口，表 `api_rate_limits`；超限返回 **429** 并带 `Retry-After`（`lib/RateLimit.php`）。
- **ID 校验**：`app_normalize_user_id`（`^[a-zA-Z0-9_-]{1,64}$`）、房间号 `^[A-Z0-9_-]{6,16}$`、owner token `^[a-f0-9]{32,128}$`。

### 4.2 `db_api.php`（账号 + 云存档）

| action | 方法 | 认证 | 参数 | 成功响应 | 关键状态码 |
|---|---|---|---|---|---|
| `register` | POST | 无 | email, password | `200 {status:success, user_id, auth_token}` | `400` 邮箱/密码不合法；`409` 邮箱已注册 |
| `login` | POST | 无 | email, password | `200 {status:success, user_id, auth_token}` | `400/401` 账号或密码错误 |
| `pull` | GET | Bearer | user_id | 有数据：`200 {settings, investigators, saves, tavern_novels, revision, updated_at}`；**无数据：`200` 裸 `null`** | `401` 未登录/过期 |
| `sync` | POST | Bearer | user_id, settings, investigators, saves, tavern_novels, base_revision | `200 {status:success, revision}` | **`409 {status:conflict, message, revision, updated_at}`** 乐观锁冲突；`400` base_revision 缺失/字段非数组 |
| `logout` | POST | Bearer | user_id | `200 {status:success}` | `401` |

- **必须保留 `pull` 返回裸 `null`**（历史契约，smoke 断言 `$null -eq $pullEmpty.Body`）。
- `sync` 使用 `SELECT … FOR UPDATE` 事务 + `revision` 乐观锁；不匹配返回 409 并回滚。
- token 只存 sha256；登录/注册轮换 token，旧设备失效（`users.auth_token`）。
- 限流 scope：`auth`(register 20/300, login 30/300)、`cloud_pull` 240/300、`cloud_sync` 120/300、`auth_logout` 60/300。
- `settings` 落库前要 `sanitize_cloud_settings`（剥离 `customApiKey` / `imageGenApiKey` / `apiPresets[].key`）。

### 4.3 `room_api.php`（多人房间）

| action | 方法 | 认证 | 参数 | 成功响应 | 关键状态码 |
|---|---|---|---|---|---|
| `create` | POST | 无 | room_id, user_id | `200 {status:success, last_id:0, host_token}` | `400` 非法 id；`409` 房间已存在 |
| `join` | POST | 无 | room_id | `200 {status:success, last_id}` | `404` 房间不存在/过期 |
| `push` | POST | 主机类消息需 host_token | room_id, sender_id, payload, [host_token] | `200 {status:success, id}` | **`403 {code:"HOST_AUTH_FAILED"}`**；`403` sender mismatch；`413` payload 过大；`400` |
| `pull` | GET | 无 | room_id, since, user_id | `200 {status:success, messages, presence, now}` | `404` |

- **主机类消息白名单**（`is_host_only_message`）需逐一保留：`sync_state`、`sync_log_append`、`update_last_message`、`update_last_message_full`、`sync_module`、`sync_investigators`、`sync_assignments`、`game_start`、`game_ended`、`room_closed`、`grant_cot_access`、`coj_toggle`、`generating_start`、`generating_end`、`regenerate_start`、`distribute_reports`、`kick_player`、`move_to_spectator`。
- 例外：玩家本人 `sync_log_append`（role=`user`/`whisper`，非 AI，senderId 匹配）可绕过主机校验（`is_player_log_append`）。
- `pull` 需返回 `messages[{id,sender_id,payload}]`、`presence[{user_id,seen_at}]`、`now`（秒级 epoch），并顺带 UPSERT `coc_room_presence`。
- 房间 TTL 默认 2 小时（`ttl_hours`）；消息裁剪上限 240（`max_room_messages`）；`poll_limit` 80；`max_payload_bytes` 512KB。
- 限流：`room_create` 30/300、`room_join` 120/300、`room_push` 600/300、`room_pull` 900/300。
- **Phase 3 之后**：`pull`/`push` 由 WebSocket 承担，但 HTTP action 应保留一个过渡期，供旧客户端回退（见 §8）。

### 4.4 `library_api.php`（模组库）

| action | 方法 | 认证 | 参数 | 成功响应 | 关键状态码 |
|---|---|---|---|---|---|
| `list` | GET | 受限区需 Bearer owner_token | section(reading/restricted), q, user_id | `200 {status:success, modules[]}` | — |
| `detail` | GET | 私有模组需 Bearer | id, user_id | `200 {status:success, module}` | `403` 无权访问；`404` 不存在 |
| `create` | POST | 无 | title, description, content, author_id, owner_token, type, min/max_players, author_name | `200 {status:success, id}` | `400/413` |
| `delete` | POST | owner_token | id, author_id, owner_token | `200 {status:success}` | `403` 非本浏览器；`404` |
| `increment_downloads` | POST | 无 | id | `200 {status:success}` | `400` 非法 id |

- `type` 白名单：`original` / `reprint` / `ai_adapt` / `ai_adapt_private`。
- `list` 的 `reading` 返回 `original`+`reprint`；`restricted` 返回 `ai_adapt` + （本人且 owner 匹配的）`ai_adapt_private`；`q` 走 `title ILIKE`。
- `module` 字段需含 `content_length`（`CHAR_LENGTH(content)`），`detail` 才含 `content`。
- 限流：`library_list` 240/300、`library_detail` 300/300、`library_create` 60/300、`library_delete` 60/300、`library_download` 300/300。

### 4.5 `health.php` 与 `dl/download.php`

| 入口 | 方法 | 行为 |
|---|---|---|
| `health.php` | GET | DB `SELECT 1` 成功 → `200 {status:ok}`；失败 → `503 {status:error}` |
| `dl/download.php` | POST（**表单 `application/x-www-form-urlencoded`**，字段 `data`） | 回显 `data` 为 `application/json` 附件，`Content-Disposition: attachment; filename="character_card.json"`；`400 Missing data`；`413 File too large`（>2MB） |

> 注意 `dl/download.php` 读的是 `$_POST['data']`，**不是 JSON body**，Go 侧须用 `ParseForm` 兼容。

### 4.6 认证模型（三套独立，均 token + sha256 存储）

1. **账号**：email + password（bcrypt），登录返回一次性明文 `auth_token`，库存 sha256。
2. **房间主机**：`create` 返回一次性 `host_token`，库存 sha256。
3. **模组归属**：浏览器生成的 `owner_token`，库存 sha256。

---

## 5. Go 项目目录结构（建议）

```
z-coc/
├─ go.mod / go.sum
├─ cmd/
│  └─ server/
│     └─ main.go              # 组装配置、store、router、启动 HTTP 服务
├─ internal/
│  ├─ config/                 # 环境变量解析（PGSQL_* / DB_* / DATABASE_URL / APP_ALLOWED_ORIGINS）
│  ├─ httpapi/
│  │  ├─ router.go            # 路由注册（?action= 分发 + WS 升级点）
│  │  ├─ middleware.go        # CORS、方法、body 上限、限流、日志、recover
│  │  ├─ respond.go           # JSON 信封、错误、状态码
│  │  ├─ db_api.go            # register/login/pull/sync/logout
│  │  ├─ room_api.go          # create/join/push/pull（Phase 1–2 保留）
│  │  ├─ library_api.go       # list/detail/create/delete/increment_downloads
│  │  ├─ health.go
│  │  └─ download.go
│  ├─ store/                  # pgxpool + 各领域查询
│  │  ├─ pool.go
│  │  ├─ users.go
│  │  ├─ userdata.go
│  │  ├─ rooms.go
│  │  ├─ library.go
│  │  ├─ presence.go
│  │  ├─ ratelimit.go
│  │  └─ schema.go            # CREATE TABLE IF NOT EXISTS（幂等）
│  ├─ auth/                   # bcrypt、token 生成/哈希、Bearer 解析
│  ├─ realtime/               # Hub、房间通道、presence、心跳、续传（Phase 3）
│  │  ├─ hub.go
│  │  ├─ room.go
│  │  └─ protocol.go
│  ├─ validate/               # user_id / room_id / owner_token 校验（对应 lib/Validation.php）
│  └─ ratelimit/              # 固定窗口限流（对应 lib/RateLimit.php）
├─ web/                       # 静态前端（可整体 embed；见 §6 的两种方案）
│  ├─ index.html
│  ├─ Workshop/index.html
│  ├─ Library/index.html
│  ├─ 404.html
│  ├─ assets/…
│  ├─ manifest.json
│  └─ favicon.svg
├─ migrations/                # 可选：goose SQL（首期可空，沿用 schema.go 内联 DDL）
├─ tools/
│  └─ api-smoke.ps1           # 沿用，作为验收工具
├─ Dockerfile                 # 多阶段构建（§10）
└─ docker-compose.yml
```

**静态资源的两种嵌入方式**（`go:embed` 要求文件位于模块目录内）：

- **方案 A（推荐）**：把现有根目录静态文件迁到 `web/`，构建工具（`tailwind.config.js`、`tools/build-mobile.mjs`）与 `sw.js` 的路径同步调整。目录干净。
- **方案 B（最小改动）**：静态文件留在仓库根，根目录放一个 `embed.go` 用 `//go:embed index.html Workshop Library assets 404.html manifest.json favicon.svg` 嵌入。改动小，但需排除 `*.php` / 构建文件。

---

## 6. 静态资源托管

Go 在同一个二进制里托管前端，需**等价移植现有 `.htaccess` + `apache-z-coc.conf` 行为**：

| 现有规则（来源） | Go 侧实现 |
|---|---|
| 静态缓存 7 天 + `stale-while-revalidate=86400`（根 `.htaccess`） | 对 `css/js/svg/webp/png/jpg/jpeg/gif/woff2/mp3` 设 `Cache-Control: public, max-age=604800, stale-while-revalidate=86400` |
| 启动图 `launch-ritual-moon-archive-v1.webp` immutable 一年 | 单独匹配 → `Cache-Control: public, max-age=31536000, immutable` |
| `index.html` 预加载启动图（`Link` preload） | 对 `index.html` 响应加 `Link: </assets/art/generated/launch-ritual-moon-archive-v1.webp>; rel=preload; as=image; type=image/webp; fetchpriority=high` |
| gzip/deflate（`mod_deflate`） | 中间件压缩：**gzip**（兼容面广）+ 可选 **brotli**（`andybalholm/brotli`）；对 `text/*`、`application/json`、`application/javascript`、`image/svg+xml`、`application/manifest+json` |
| 安全头（根 `.htaccess`） | `X-Content-Type-Options: nosniff`、`Referrer-Policy: strict-origin-when-cross-origin`、`X-Frame-Options: SAMEORIGIN`、`Permissions-Policy: geolocation=(), microphone=(), camera=()`、`X-Permitted-Cross-Domain-Policies: none`、`Content-Security-Policy: base-uri 'self'; object-src 'none'; frame-ancestors 'self'; form-action 'self'` |
| `Options -Indexes` | 不做目录列表（embed 本就不支持） |
| 禁止访问 `config*` / `api_bootstrap.php` / `*.php` 等敏感文件 | 静态层**只暴露白名单**；API 路由单独注册，绝不把 `.php`、`config*`、`Dockerfile` 暴露为静态文件 |
| `LimitRequestBody 18874368`（18MB） | 按 action 的 body 上限分别实现（沿用 `app_require_content_length` 的值） |
| `ErrorDocument` / `404.html` | **SPA 回退**：非 API、非静态文件的路径 → 返回 `index.html`；对明确的未知顶层路径可返回 `404.html`（保持主题化错误页） |

**其他要点**

- **SPA 回退**：`/`、`/Workshop/`、`/Library/` 等导航请求回退到对应 `index.html`；`/api/*` 或 `*.php` 路径**绝不**回退（避免把 API 404 变成 HTML）。
- **Service Worker 排除**：`sw.js` 的 `APP_SHELL` 只缓存壳文件；原生端（Capacitor）**有意不注册 SW**（`index.html` 内注册逻辑已有 `isNative` 判断）。Go 侧无需特殊处理，只需保证 `sw.js`、`manifest.json` 可被静态访问，且 `Cache-Control` 合理（`sw.js` 建议 `no-cache` 以便更新）。
- **CSP 与 CDN**：当前前端大量使用 CDN 全局库（Vue / marked / DOMPurify / mammoth / FileSaver / html-docx-js）。现有 CSP 只限制 `base-uri/object-src/frame-ancestors/form-action`，**未限制 `script-src`**，因此 CDN 脚本仍可加载。迁移时**保持 CSP 原样**，不要顺手加 `script-src 'self'`，否则会打断 CDN 依赖。
- **Accept-Encoding 协商**：仅当客户端支持时才压缩；已压缩内容（webp/mp3）不重复压缩。

---

## 7. 实时方案（WebSocket）

### 7.1 现状：HTTP 轮询

前端 `startRoomPolling` 每 **1500ms** 调用 `room_api.php?action=pull`（`index.html` 的 `pollRoomMessages`）。每次 pull 服务端执行：

1. `SELECT` 增量消息（`id > since`）
2. `UPSERT coc_room_presence`（登记本人最后在线）
3. `SELECT coc_room_presence`（返回全房间在线表）
4. `touch_room`（每 30s 一次条件更新）

### 7.2 目标：WebSocket 连接 Hub

**设计**

- **每实例一个连接 Hub**：`map[roomID]*Room`，每个 `Room` 持有本房间的活跃连接集合。
- **房间通道**：`create`/`join` 成功后，客户端用 `room_id` + 会话 id 建立 WS；Hub 按 `room_id` 归组。
- **消息协议**：客户端发送与现有 `push` 的 `payload` 同构；服务端广播 `{id, sender_id, payload}`。示例：

```json
// 客户端 → 服务端（发送一条消息）
{ "type": "send", "room_id": "ABC123", "sender_id": "p1", "host_token": "…", "payload": { "type": "chat", "user": { "id": "p1" }, "text": "hello" } }

// 服务端 → 房间广播
{ "type": "message", "id": 42, "sender_id": "p1", "payload": { "type": "chat", "text": "hello" } }

// 服务端 → 客户端（在线状态，连接生命周期驱动）
{ "type": "presence", "members": [ { "user_id": "p1", "online": true }, { "user_id": "p2", "online": false } ] }
```

- **presence 由连接生命周期派生**：连接建立 = 在线，断开 = 离线，**取代**每次轮询的 `UPSERT coc_room_presence`。仍可保留 `coc_room_presence` 表做**落库兜底**（例如多实例或进程重启后的短暂恢复），但写入频率从「每 1.5s 每人一次」降到「连接/断开各一次」。
- **主机鉴权**：主机类消息仍走 `host_token` 校验（同 `push` 规则），在 WS 握手或首条消息时验证。
- **心跳**：服务端定期 `ping`（如每 30s），客户端回 `pong`；或应用层 `{type:"ping"}`。超时（如 2×心跳）判定掉线并广播离线。
- **断线重连 + 续传**：客户端记录 `lastMessageId`，重连后发送 `{type:"resume", since: lastId}`；服务端从 DB 拉 `id > since` 的消息补发（复用现有 `pull` 查询）。消息 id 天然单调递增，续传语义清晰。
- **落库**：发送的消息仍写入 `coc_room_messages`（保证持久化与 HTTP `pull` 回退可用），但广播走内存通道，不依赖轮询。

### 7.3 流量 / 负载分析

**当前轮询（6 人房间）**

| 指标 | 计算 | 结果 |
|---|---|---|
| 每客户端请求频率 | 3600s ÷ 1.5s | **2400 req/hour/client** |
| 6 人房间总请求 | 6 × 2400 | **14,400 req/hour** |
| 房间总流量 | 每响应约 1.3KB × 14,400 | **≈ 19MB/hour** |
| 数据库操作 | 每请求约 3 次（SELECT/UPSERT/SELECT） | **≈ 40k ops/hour** |

**WebSocket 空闲**

| 指标 | 计算 | 结果 |
|---|---|---|
| 心跳流量 | 每 30s 一次 × ~50B × 120/hour | **≈ 6–12KB/hour/client** |
| 数据库访问 | 仅消息/连接状态**变化**时写 1 行 | 空闲时 **≈ 0** |
| 延迟 | 推送 | **近实时（<100ms 量级）** |

**收益**

- 按**每客户端空闲流量**对比：3.17MB/hour ÷ 12KB/hour ≈ **260×** 降幅（这就是「≈260x」的来源）。
- 数据库负载从 **40k ops/hour/房间** 降到**仅在变化时**访问，房间越大收益越明显。
- 延迟从「最多 1.5s」降到「推送即达」。

### 7.4 权衡

| 权衡 | 说明 |
|---|---|
| 连接状态 | 服务端需维护内存连接表；需处理半开连接（靠心跳） |
| 单实例 Hub | 单容器内 Hub 只能覆盖本实例连接。**本规模（单实例）足够** |
| 横向扩展 | 若未来多实例，需 Redis pub/sub 广播跨实例消息 —— **当前规模不需要** |
| 代理/超时 | 反代（openresty/nginx）必须透传 `Upgrade`/`Connection` 并放宽读超时（见 §11） |
| 回退 | 过渡期保留 HTTP `push`/`pull`，WS 不可用时前端可退回轮询 |

---

## 8. 迁移阶段

> 原则：**小步、可回滚、契约不变**。每阶段独立验收、独立回滚。

### Phase 0 — 基线固化（前置）

- 打 tag 记录当前 PHP 版本（对照 `SPLIT_PLAN.md` 阶段 0）。
- 跑通 `tools/api-smoke.ps1`，记录 **27/27** 基线。
- **验收**：smoke 全绿，tag 已打。

### Phase 1 — Go 实现同一 API 契约（对同一 Postgres）

- 用 Go 实现 §4 全部入口，读写**同一个 PostgreSQL**、**同一套表**。
- bcrypt 兼容（老账号可登录）；环境变量解析与 `lib/Config.php` 一致。
- 部署方式：Go 服务与旧 PHP 服务**并行**（不同端口/域名），用 smoke 对照。
- **验收**：`tools/api-smoke.ps1` 对 Go 服务 **27/27**；跨设备云存档可读写；同一账号在 PHP 与 Go 间可互相登录。

### Phase 2 — Go 同时托管静态资源，替换镜像

- 把静态前端并入 Go（§6），去掉 Apache/PHP。
- 切换 `Dockerfile` 为多阶段 Go 构建；**保留旧 PHP 镜像 tag 作为回滚**。
- **验收**：静态头（缓存/CSP/gzip）与旧站一致；SPA 回退、`404.html` 正常；smoke 27/27；移动端联调通过。

### Phase 3 — WebSocket 实时化

- 引入 `internal/realtime`，房间 `push`/`pull` 迁移到 WS。
- 前端把 `startRoomPolling`（`setInterval(…, 1500)`）替换为 WS 客户端；保留 HTTP 回退开关。
- presence 改由连接生命周期驱动。
- **验收**：6 人房间消息近实时（延迟明显低于 1.5s）；掉线/上线状态准确；断线重连续传正确；空闲流量符合 §7.3 预期。

### Phase 4 — 后续需求路线图（§9）

- 按 §9 表格逐项排期实施。

---

## 9. 后续需求路线图

| # | 需求 | 目标 | 涉及前后端 | 依赖 | 建议阶段 | 风险 |
|---|---|---|---|---|---|---|
| 1 | **完善好友功能** | 端到端可用：搜索 / 请求 / 接受拒绝 / 列表 / 在线 / 邀请（见 `FRIENDS_FEATURE_TASK.md`） | 前端 `index.html` 好友视图接线 + 后端新增 `friend_api.php` 等 | 账号鉴权（已有）、在线状态（WS 连接生命周期，Phase 3 后更佳）、新表 `coc_friendships`/`coc_friend_requests`/`coc_user_presence` | Phase 3 之后（在线状态可复用 WS） | 隐私/枚举（仅精确匹配 + 限流）；邀请入房与房间满员/密码交互复杂；`rejectFriend` 缺失需补齐 |
| 2 | **多人实时游戏大厅（显示公开房间）** | 列出公开房间、快速加入 | 后端房间注册表（`is_public` + 大厅 API/WS 事件）+ 前端大厅 UI | Phase 3 的 WS Hub；房间生命周期/TTL（已有 `coc_rooms`） | Phase 3 之后 | 房间发现带来枚举/刷房；需过滤过期房间；大厅推送与房间消息的通道设计 |
| 3 | **管理员后台** | 内容/用户/房间管理 | 后端鉴权角色（`users.role`）+ admin API + 前端管理页 | 账号体系扩展（角色）；可能需审计日志 | Phase 2 之后任意阶段 | 权限模型（谁能成为管理员）；越权；管理操作不可逆 |
| 4 | **剧情对话战斗界面** | 剧情化对话式战斗 UI | **主要前端 Vue**；后端提供战斗状态/同步 | 房间同步（Phase 3 WS）；AI 端点（用户自配，前端直连） | Phase 3 之后 | 状态机复杂；前端 `setup()` 已巨大（拆分前先服务化，见 `SPLIT_PLAN.md` §5）；同步冲突 |
| 5 | **剧情对话地图** | 剧情推进 + 地图展示 | 前端 + 状态同步 | 同 #4（状态/同步层） | Phase 4（与 #4 协同） | 地图资源体积（webp/懒加载）；与战斗界面状态耦合 |

> #4 / #5 后端只做**状态与同步**，核心是前端 Vue；建议在 Phase 3 的 WS 之上统一「剧情状态同步」通道。

---

## 10. 部署与资源预期

### 10.1 多阶段 Dockerfile（示意）

```dockerfile
# 构建阶段
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/z-coc ./cmd/server

# 运行阶段（静态、无 shell、无 libc 依赖）
FROM gcr.io/distroless/static-debian12
COPY --from=build /out/z-coc /z-coc
EXPOSE 8080
ENTRYPOINT ["/z-coc"]
```

- 备选基础镜像：`scratch`（更小，但无 CA 证书，需手动 COPY）、`alpine`（体积略大，便于调试）。
- 静态文件用 `go:embed` 打进二进制，运行阶段**无需 COPY 前端文件**。

### 10.2 资源预期对比

| 指标 | 现状 PHP 8.3 + Apache | 目标 Go 单容器 |
|---|---|---|
| 镜像体积 | ~600MB+（php:8.3-apache 基础镜像） | **~15–25MB** |
| 空载 RSS | 多进程，数百 MB 量级 | **~10–30MB** |
| 启动 | 秒级（Apache + PHP 初始化） | 毫秒级 |
| 进程模型 | Apache worker + mod_php | 单进程 goroutine |
| 配置面 | `.htaccess` + Apache conf + `php.ini` | 环境变量 + 代码 |

### 10.3 环境变量

| 变量 | 用途 |
|---|---|
| `PGSQL_HOST` / `PGSQL_PORT` / `PGSQL_USERNAME` / `PGSQL_PASSWORD` / `PGSQL_DATABASE` | 首选数据库配置（沿用 `lib/Config.php`） |
| `DB_HOST` / `DB_PORT` / `DB_USER` / `DB_PASSWORD` / `DB_NAME` | 兼容别名 |
| `DATABASE_URL` | `postgres://…` 连接串（最低优先级） |
| `APP_ALLOWED_ORIGINS` | CORS 白名单（同域留空；禁用 `*`） |
| `APP_PORT` | 容器监听端口（默认 8080，与 `docker-compose.yml` 一致） |

### 10.4 健康检查与单容器

- 内置 `GET /health` → DB `SELECT 1` 成功 `{status:ok}`，失败 503。
- Docker `HEALTHCHECK` 直接请求 `/health`（distroless 无 shell，需用带二进制的健康检查方式，或在 compose 层做探针）。
- **单容器**：静态 + API + WebSocket 同进程同端口。
- **宿主机**：服务器已运行 Docker，且 **1Panel 的 openresty 可能负责 TLS 终止**；容器只监听 8080，由反代转发（注意 §11 的 Upgrade 透传）。

---

## 11. 风险与回滚

| 风险 | 影响 | 缓解 |
|---|---|---|
| 契约漂移 | 前端/原生端行为异常 | 以 `tools/api-smoke.ps1` 为准逐条对齐；Phase 1 与 PHP 并行对照；保留裸 `null`、409、`HOST_AUTH_FAILED` 等边角契约 |
| 静态头/CSP 差异 | 缓存失效、样式丢失、CDN 脚本被拦 | §6 逐条移植；CSP **保持原样**（不加 `script-src 'self'`）；对比响应头 |
| WebSocket 代理/超时 | 连接被反代断开 | nginx/openresty 必须设置 `proxy_set_header Upgrade $http_upgrade;`、`proxy_set_header Connection "upgrade";`，并放宽 `proxy_read_timeout`；启用应用层心跳 |
| 连接内存增长 | 大量半开连接占内存 | 心跳超时剔除；连接数上限；房间 TTL 清理 |
| bcrypt 兼容 | 老用户无法登录 | Phase 1 用同一库校验 `$2y$`；上线前用真实老账号验证 |
| 静态文件嵌入路径 | 构建后 404 | 选定 `web/` 或根 `embed.go` 方案后，同步更新 `sw.js` 的 `APP_SHELL` 与 `tools/build-mobile.mjs` |
| 回滚 | 新服务故障 | **保留旧 PHP 镜像 tag**，一键切回；数据库结构未变，双向兼容 |

**回滚策略**：Phase 1–3 每阶段独立提交/独立镜像 tag；出问题直接切回上一镜像，数据库无需回滚（结构兼容）。

---

## 12. 验收标准

- [ ] `tools/api-smoke.ps1` 对 Go 服务 **27/27 全绿**（含 `pull` 裸 `null`、`sync` 409、`HOST_AUTH_FAILED` 403）。
- [ ] 跨设备云存档同步：A 设备存档 → B 设备登录恢复。
- [ ] 多设备多人房间：建房 → 加入 → 推送 → 同步（Phase 3 后为 WS）。
- [ ] WebSocket 延迟：消息端到端明显低于原 1.5s 轮询；断线重连续传正确。
- [ ] 内存与镜像：空载 RSS 在 **10–30MB** 量级，镜像 **15–25MB** 量级。
- [ ] 静态头与 CSP：缓存头（7 天 / immutable）、安全头、gzip/brotli、SPA 回退、`404.html` 与旧站一致。
- [ ] 老账号 bcrypt 可登录；同一账号在 PHP 与 Go 间互通。
- [ ] Capacitor 移动端联调通过（`pnpm mobile:sync`，API 指向生产域名）。

---

## 13. 开放问题

1. **实时协议选型**：房间用 WebSocket；**大厅（公开房间列表）**是否也可用 SSE 更简单？还是统一走 WS？
2. **横向扩展**：是否需要多实例？若需要，引入 Redis pub/sub 的时机与成本（当前规模判定为不需要）。
3. **管理员鉴权模型**：`users.role` 字段？独立管理员表？如何授予/撤销管理员？
4. **路由风格**：长期是否把 `?action=` 迁移到 REST（`/api/rooms/{id}`）？迁移期如何双支持？
5. **Go module path**：仓库名 / 域名前缀（如 `github.com/<org>/z-coc`）待定。
6. **迁移工具**：首期继续内联 `CREATE TABLE IF NOT EXISTS`，还是直接引入 `goose` 版本化迁移？（`SPLIT_PLAN.md` 阶段 4 的长期目标）
7. **静态嵌入方式**：`web/` 目录迁移（方案 A）vs 根 `embed.go`（方案 B），需权衡对 `sw.js` / `build-mobile.mjs` 的影响。
8. **好友搜索维度**：仅 UUID，还是含 email/昵称（隐私与限流）？见 `FRIENDS_FEATURE_TASK.md` §5。
9. **回退保留期**：HTTP `push`/`pull` 在 Phase 3 后保留多久再移除？

---

## 附：涉及文件一览

| 文件 | 角色 |
|---|---|
| `db_api.php` | 账号 + 云存档契约（§4.2） |
| `room_api.php` | 多人房间契约（§4.3） |
| `library_api.php` | 模组库契约（§4.4） |
| `health.php` / `dl/download.php` | 健康检查 / 导出（§4.5） |
| `lib/Config.php` / `lib/Database.php` / `lib/Http.php` / `lib/Validation.php` / `lib/RateLimit.php` / `lib/Scheduler.php` | 需复刻的公共行为（配置、连接、信封、校验、限流、周期任务） |
| `.htaccess` / `docker/apache-z-coc.conf` | 需移植的静态头与站点规则（§6） |
| `Dockerfile` / `docker-compose.yml` / `.env.example` | 部署与环境变量（§10） |
| `index.html`（`startRoomPolling` / `pollRoomMessages`） | 轮询逻辑，Phase 3 改为 WS（§7） |
| `tools/api-smoke.ps1` | 验收基准（27 条断言） |
| `sw.js` / `manifest.json` / `capacitor.config.json` | 静态/SW/移动端约束（§6、§3） |
| `docs/FRIENDS_FEATURE_TASK.md` | 好友功能设计（§9 #1） |
| `docs/PROJECT_GUIDE.md` / `docs/SPLIT_PLAN.md` | 总纲与拆分计划（交叉引用） |
