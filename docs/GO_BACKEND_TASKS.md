# z-coc Go 后端实现任务清单

> 配套方案：[`GO_BACKEND_PLAN.md`](./GO_BACKEND_PLAN.md)。本文件是可勾选跟踪的任务表。
> 原则：**小步、可回滚、契约不变**；每阶段独立验收、独立提交/独立镜像 tag。
> 状态：`[ ]` 待办 · `[~]` 进行中 · `[x]` 完成。

---

## Phase 0 — 基线固化（前置）

- [x] 0.1 打 tag 记录当前 PHP 版本
- [x] 0.2 跑通 `tools/api-smoke.ps1`，记录 **27/27** 基线
- **验收**：smoke 全绿、tag 已打。

## Phase 1 — Go 实现同一 API 契约（对同一 Postgres）

### 骨架与公共层
- [x] 1.1 初始化 Go module（`go.mod`，确定 module path）
- [x] 1.2 建目录骨架 `cmd/server` + `internal/*`
- [x] 1.3 `internal/config`：`PGSQL_*` / `DB_*` / `DATABASE_URL` / `APP_ALLOWED_ORIGINS` / `APP_PORT`（复刻 `lib/Config.php` 优先级）
- [x] 1.4 `internal/store/pool`（`pgxpool`）+ `schema.go`（复刻现有表，`CREATE TABLE IF NOT EXISTS`）
- [x] 1.5 `internal/httpapi/respond`（JSON 信封）+ `middleware`（CORS、方法、body 上限、日志、recover）
- [x] 1.6 `internal/ratelimit`：固定窗口，复刻 `api_rate_limits` 语义与各 scope
- [x] 1.7 `internal/validate`：`user_id` / `room_id` / `owner_token` 校验
- [x] 1.8 `internal/auth`：bcrypt 校验（兼容 `$2y$`）、token 生成 + sha256、Bearer 解析

### 接口实现（严格对齐 `GO_BACKEND_PLAN.md` §4）
- [x] 1.9 `db_api`：register / login / pull / sync / logout
  - 含 `pull` 裸 `null`、`sync` 409 乐观锁、`settings` 脱敏
- [x] 1.10 `room_api`：create / join / push / pull
  - 含 18 类主机白名单、`HOST_AUTH_FAILED`、presence、TTL、消息裁剪
- [x] 1.11 `library_api`：list / detail / create / delete / increment_downloads
  - 含 type 白名单、`content_length`、私有模组鉴权
- [x] 1.12 `health` + `dl/download`
  - 注意 `dl/download` 读**表单** `data`（`application/x-www-form-urlencoded`），非 JSON

### 部署与验收
- [x] 1.13 多阶段 `Dockerfile`；与旧 PHP 服务**并行**部署对照
- [x] 1.14 验收：smoke **27/27**、老账号 bcrypt 互通、跨设备云存档
- **验收**：`tools/api-smoke.ps1` 对 Go 服务 27/27；同一账号在 PHP 与 Go 间可互相登录。

## Phase 2 — Go 同时托管静态，替换镜像

- [ ] 2.1 静态嵌入（`web/` 目录 或 根 `embed.go`，见 `GO_BACKEND_PLAN.md` §5）
- [ ] 2.2 静态中间件：缓存头（7 天 / 启动图 immutable）、gzip/brotli、安全头 + **CSP 保持原样**、SPA 回退、`404.html`
- [ ] 2.3 同步 `sw.js` 的 `APP_SHELL` 与 `tools/build-mobile.mjs` 路径（若选 `web/` 方案）
- [ ] 2.4 切换 Dockerfile 为 Go 镜像；**保留旧 PHP 镜像 tag 作回滚**
- [ ] 2.5 验收：静态头/CSP 与旧站一致、SPA 回退、smoke、移动端联调
- **验收**：静态头与 CSP 一致；SPA 回退与 `404.html` 正常；smoke 27/27。

## Phase 3 — WebSocket 实时化

- [ ] 3.1 `internal/realtime`：`hub` / `room` / `protocol`
- [ ] 3.2 WS 升级端点 + 主机鉴权（复用 `host_token`）
- [ ] 3.3 消息**落库** + 内存广播（保证 HTTP `pull` 回退可用）
- [ ] 3.4 presence 改由**连接生命周期**派生（替代每次轮询 UPSERT）
- [ ] 3.5 心跳（ping/pong）+ 断线重连 `resume(since)` 续传
- [ ] 3.6 前端 `startRoomPolling`（`setInterval(…,1500)`）→ WS 客户端，保留 HTTP 回退开关
- [ ] 3.7 反代配置：透传 `Upgrade`/`Connection`，放宽 `proxy_read_timeout`
- [ ] 3.8 验收：延迟明显低于 1.5s、在线状态准确、续传正确、空闲流量符合预期
- **验收**：6 人房间近实时；掉线/上线准确；断线重连续传正确；空闲流量符合 §7.3 预期。

## Phase 4 — 后续需求（路线图，无先后）

- [ ] 4.1 完善好友功能（见 `FRIENDS_FEATURE_TASK.md`）
- [ ] 4.2 多人实时游戏大厅（公开房间）
- [ ] 4.3 管理员后台
- [ ] 4.4 剧情对话战斗界面
- [ ] 4.5 剧情对话地图

---

## 依赖关系

- `1.x` 必须先于 `2.x`。
- `3.x` 依赖 `1.x`（房间契约），且是 `4.1` / `4.2` 的前置。
- `4.3` 可在 Phase 2 之后任意时点。
- `4.4` / `4.5` 主要是前端 Vue；后端只做**状态与同步**。

## 验收总基准

- [ ] `tools/api-smoke.ps1` 对 Go 服务 **27/27 全绿**
- [ ] 跨设备云存档：A 存档 → B 登录恢复
- [ ] 多设备多人房间：建房 → 加入 → 推送 → 同步（Phase 3 后为 WS）
- [ ] 空载 RSS **10–30MB**、镜像 **15–25MB**
- [ ] 老账号 bcrypt 可登录；同一账号在 PHP 与 Go 间互通
- [ ] Capacitor 移动端联调通过

## 进度登记

| 阶段 | 状态 | 负责 | 备注 |
|---|---|---|---|
| Phase 0 基线 | 完成 | | baseline tag + smoke 27/27 |
| Phase 1 Go API | 完成 | | Go 服务，smoke 27/27（e4beb80） |
| Phase 2 静态托管 | 未开始 | | |
| Phase 3 WebSocket | 未开始 | | |
| Phase 4 路线图 | 未开始 | | |
