# z-coc-go

z-coc（Call of Cthulhu 跑团辅助）的 **Go 后端 + 前端静态资源**独立仓库。原 PHP 后端不在本仓库，已迁移为 Go 实现（同 API 契约、同 PostgreSQL）。

## 技术栈

- **后端**：Go 1.26 + `pgx`（PostgreSQL）+ 标准库 `net/http`；`golang.org/x/crypto/bcrypt` 兼容 PHP `$2y$` 密码。
- **前端**：静态 Vue 3（CDN 全局版）+ Tailwind，由 `index.html` / `Workshop/` / `Library/` 提供。
- **数据库**：PostgreSQL（表结构由服务启动时 `CREATE TABLE IF NOT EXISTS` 幂等创建）。

## 目录结构

```
z-coc-go/
├─ cmd/server/          程序入口（组装配置、store、路由、启动服务）
├─ internal/
│  ├─ config/           环境变量解析（PGSQL_* / DB_* / DATABASE_URL）
│  ├─ store/            pgxpool + 各领域查询 + schema（建表）
│  ├─ httpapi/          路由、中间件、JSON 信封、各 action 处理器
│  ├─ auth/             bcrypt、token 生成/哈希、Bearer 解析
│  ├─ validate/         user_id / room_id / owner_token 校验
│  └─ ratelimit/        固定窗口限流（api_rate_limits 表）
├─ web 静态资源：index.html / 404.html / assets/ / Workshop/ / Library/ / manifest.json / sw.js
├─ tools/               api-smoke.ps1（验收）、build-mobile.mjs、optimize_art.py
├─ android/             Capacitor Android 壳
├─ Dockerfile           多阶段构建 → distroless
└─ docs/                方案与任务清单
```

## 本地运行

```bash
# 环境变量（或用 .env.example 复制为 .env）
export PGSQL_HOST=... PGSQL_PORT=5432 PGSQL_USERNAME=... PGSQL_PASSWORD=... PGSQL_DATABASE=z-coc
export APP_PORT=8080

go run ./cmd/server
```

## Docker

```bash
docker build -t z-coc-go .
docker run --rm -p 8080:8080 \
  -e PGSQL_HOST=... -e PGSQL_PORT=5432 \
  -e PGSQL_USERNAME=... -e PGSQL_PASSWORD=... -e PGSQL_DATABASE=z-coc \
  z-coc-go
```

## 验收

```powershell
powershell -ExecutionPolicy Bypass -File tools/api-smoke.ps1 -BaseUrl http://127.0.0.1:8080
# 期望：27 passed, 0 failed
```

## 状态

- **Phase 1 完成**：Go 实现与 PHP 相同的 API 契约（`db_api` / `room_api` / `library_api` / `health` / `dl/download`），smoke **27/27**。
- 待办：Phase 2（Go 托管静态 + 换镜像）、Phase 3（WebSocket 实时化）及后续需求路线图，见 `docs/GO_BACKEND_TASKS.md`。

## 文档

- `docs/GO_BACKEND_PLAN.md` — 技术选型、迁移计划、后续需求路线图
- `docs/GO_BACKEND_TASKS.md` — 可勾选的实现任务清单
