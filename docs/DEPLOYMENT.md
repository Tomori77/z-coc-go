# z-coc-go 部署指南

> 面向**按步骤操作即可完成部署**的使用者。
> 默认推荐**方案一**（应用单独一个容器，连接已有的 PostgreSQL 容器）。

---

## 0. 前置条件

- 已安装 **Docker** 与 **Docker Compose**（`docker compose` 子命令可用）。
- 镜像由仓库根目录的 [`Dockerfile`](../Dockerfile) **多阶段构建**：
  - 构建阶段 `golang:1.26-alpine` 编译静态二进制；
  - 运行阶段 `gcr.io/distroless/static-debian12`（无 shell、无包管理器），最终镜像约 **15–25MB**。
- 前端静态资源已由 `go:embed` 打进二进制，运行阶段**无需额外拷贝前端文件**。

---

## 方案一（默认，推荐）：应用单独一个容器，连接「已有」PostgreSQL

适用于：服务器上**已有**一个 PostgreSQL 容器（可能在别的 compose 项目里），只想把 z-coc-go
作为一个独立容器加进来。

使用仓库根的 [`docker-compose.yml`](../docker-compose.yml)。

### 步骤

1. **创建 / 确认一个共享 Docker 网络**（两个容器必须接入同一网络才能互相解析主机名）：

   ```bash
   docker network create zcoc-net
   ```

2. **把已有的 PostgreSQL 容器接入该网络**：

   ```bash
   docker network connect zcoc-net <db容器名>
   ```

   之后在 `.env` 里把 `PGSQL_HOST` 填成**这个已有数据库的容器名**（Docker 网络内可直接用容器名互访）。

3. **准备环境变量**：

   ```bash
   cp .env.example .env
   ```

   编辑 `.env`，至少填好 `PGSQL_HOST` / `PGSQL_USERNAME` / `PGSQL_PASSWORD` / `PGSQL_DATABASE`
   （详见第 3 节「环境变量」）。

4. **构建并启动**：

   ```bash
   docker compose up -d --build
   ```

5. **验证健康检查**：

   ```bash
   curl http://127.0.0.1:8080/health.php
   # 期望返回：{"status":"ok"}
   ```

> `docker-compose.yml` 顶部的注释也提醒了：**已有数据库容器必须接入同一个外部网络**。

---

## 方案二：统一管理两个容器（应用 + PostgreSQL）

适用于：服务器上**没有**现成数据库，希望用一份 compose 同时管理应用与 PostgreSQL。

使用仓库根的 [`docker-compose.full.yml`](../docker-compose.full.yml)。

```bash
docker compose -f docker-compose.full.yml up -d --build
```

要点：

- 自带 `db`（`postgres:18-alpine`）+ `app` 两个服务；
- 数据库数据用**命名卷** `zcoc-pgdata` 持久化（删除容器不会丢数据）；
- `app` 通过 `depends_on: db: condition: service_healthy` 等 `db` **健康检查**通过后才启动；
- compose 内 `PGSQL_HOST` **固定为 `db`**（服务名，网络内可解析）；
- `.env` 里**仍需**填数据库账号密码与库名（`PGSQL_USERNAME` / `PGSQL_PASSWORD` / `PGSQL_DATABASE`）。

---

## 3. 环境变量

完整说明见仓库根 [`.env.example`](../.env.example)。常用项：

| 变量 | 必填 | 说明 |
|---|---|---|
| `PGSQL_HOST` | **必填** | 方案一：已有 PostgreSQL 容器名；方案二：由 compose 固定为 `db`（可忽略） |
| `PGSQL_USERNAME` | **必填** | 数据库用户 |
| `PGSQL_PASSWORD` | **必填** | 数据库密码 |
| `PGSQL_DATABASE` | **必填** | 数据库名 |
| `PGSQL_PORT` | 可选 | 默认 `5432` |
| `APP_PORT` | 可选 | 宿主机映射端口，默认 `8080` |
| `DOCKER_NETWORK` | 可选 | 方案一的外部网络名，默认 `zcoc-net` |
| `APP_ALLOWED_ORIGINS` | 可选 | CORS 白名单，**同域留空** |

> **`APP_ALLOWED_ORIGINS` 说明**：同域访问（前端与 API 同源）**留空**即可；跨域或原生端用**逗号分隔**的**完整 Origin**（如 `https://example.com,https://localhost`），**禁止使用 `*`**。Capacitor 原生端需包含 `https://localhost,capacitor://localhost`。

### 兼容别名（可选）

`PGSQL_*` 为首选；若未设置，会依次回退到 `DB_HOST/DB_PORT/DB_USER/DB_PASSWORD/DB_NAME`，
最后才是 `DATABASE_URL` 连接串。

---

## 4. 更新与回滚

- **更新**（改完代码后重新构建并滚动更新）：

  ```bash
  docker compose up -d --build
  # 方案二：docker compose -f docker-compose.full.yml up -d --build
  ```

- **回滚**：为镜像打 tag 以便回退。例如构建时保留旧版本：

  ```bash
  docker build -t z-coc-go:2026-10-10 .
  # 需要回滚时，把 compose 里的 image 指向旧 tag 后重启
  docker compose up -d
  ```

  数据库表结构向后兼容（幂等 `CREATE TABLE IF NOT EXISTS`），回滚镜像通常**无需回滚数据库**。

---

## 5. 部署后验证清单

- [ ] `curl http://127.0.0.1:8080/health.php` 返回 `{"status":"ok"}`
- [ ] 浏览器打开首页（`http://<host>:8080/`）静态页面正常加载
- [ ] `tools/api-smoke.ps1 -BaseUrl http://127.0.0.1:8080` → **27 passed, 0 failed**
- [ ] 跨设备云存档：A 设备存档 → B 设备登录恢复
- [ ] 多人房间：建房 → 加入 → 推送 → 同步（WebSocket 通道）

---

## 6. 反向代理（openresty / nginx）

WebSocket 必须**透传升级头**并**放宽读超时**，否则长连接会被反代切断。示例片段：

```nginx
location /room_ws {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_read_timeout 3600s;
    proxy_send_timeout 3600s;
}
```

- 必须透传 `Upgrade` 与 `Connection`。
- `proxy_read_timeout` 建议显著大于心跳周期（≥ 1h）。
- 若使用别名 `/ws/room`，需为其配置相同规则。
- 应用的房间实时消息走 WebSocket（端点 `/room_ws`，别名 `/ws/room`），反代必须正确透传以维持长连接。

---

## 7. 数据备份（方案二）

对命名卷里的 PostgreSQL 执行 `pg_dump`：

```bash
docker exec <db容器名> pg_dump -U <用户名> -d <库名> > backup.sql
# 例：docker exec z-coc-go-db pg_dump -U zcoc -d z-coc > backup.sql
```

恢复（示例）：

```bash
cat backup.sql | docker exec -i <db容器名> psql -U <用户名> -d <库名>
```

---

## 8. 安全提醒

- `.env` **不要提交到仓库**（已在 `.gitignore` 中忽略）；仓库只保留 `.env.example`。
- **数据库不要暴露公网**，仅通过 Docker 网络让应用容器访问。
- 定期**轮换密钥 / 数据库密码**；泄露时立即更换并重启容器。
- 生产环境建议在反向代理层启用 HTTPS/TLS 终止。
