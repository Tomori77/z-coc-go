# 道具 / 媒体图片使用指南（挂载目录 /media）

> 目的：新增或更换**道具图片**等媒体文件，**无需重建镜像**。
> 适用：`z-coc-go`（Go 后端 + 静态前端）。相关代码：`internal/httpapi/media.go`、`internal/config/config.go`（`MEDIA_DIR`）、`internal/settings/settings.go`（`items.defs`）。

---

## 1. 两套图片机制

| 机制 | 位置 | 加/换图 | 说明 |
|---|---|---|---|
| **内嵌（默认）** | `web/assets/art/user-edited/*.webp` | **必须重建镜像** | 经 `go:embed` 打进二进制，随版本发布、可回滚 |
| **挂载（推荐热更新）** | 宿主机 `z-coc-go/media/…` | **无需重建** | 只读挂进容器，`GET /media/*` 直接提供 |

两者可共存：道具定义里的 `image` 指向哪个，就用哪个。

---

## 2. 挂载机制

- 环境变量 **`MEDIA_DIR`**（默认 `/media`；`docker-compose*.yml` 已设 `MEDIA_DIR=/media`）。为空则关闭该路由。
- 路由：`GET|HEAD /media/*`，从 `MEDIA_DIR` 读盘提供。
- **扩展名白名单**：`webp / png / jpg / jpeg / gif / svg / mp3 / ogg / wav / woff2 / json`（其它一律 404）。
- 安全：**禁止目录列举**、**防路径穿越**（`..` 拒绝）；响应带安全头与 `Cache-Control` 7 天。
- 目录结构（宿主机）：

```
z-coc-go/
├─ media/                 ← 宿主机目录（.gitignore 已忽略）
│  └─ items/
│     ├─ sing.webp
│     └─ success.webp
└─ docker-compose.yml     ← volumes: ./media:/media:ro
```

对应 URL：`/media/items/sing.webp`。

---

## 3. 新增 / 更换道具图片（推荐：零重建）

1. **准备源图**：把 PNG 放到 `docs\未处理图片\`（任意文件名）。
2. **转 WebP**：
   ```powershell
   powershell -ExecutionPolicy Bypass -File tools\prepare-item-art.ps1 -All
   # 或指定输出名： ... -Name sing
   # 可选输出目录： ... -OutDir D:\...\z-coc-go\media\items
   ```
   - 默认：512 长边、质量 82、保留透明；输出到 `<repo>\media\items\`，并打印 `URL:`（如 `/media/items/sing.webp`）。
   - 依赖 Python(PIL) 可选；无则原样拷 PNG 并提示。
3. **上传到服务器**：把生成的 webp 放进服务器 `…/z-coc-go/media/items/`（1Panel 文件管理器或 scp 均可）。目录只读挂载，请从宿主机放入。
4. **后台配置**：打开 `https://<域名>/Admin/` → **设置 → 道具清单**：
   - 可为已有道具改 `image`（如 `/media/items/sing.webp`）；
   - 也可新增道具行（`key / emoji / label / image`）。
   - 保存**即时生效**，前端下次拉取 `config_api` 设置即生效。
5. 完成：道具栏缩略图与使用特效都会用它。

> 想「秒改」：直接替换服务器上同名 webp 即可（浏览器缓存 7 天，必要时改名或强刷）。

---

## 4. 内嵌图片（随版本发布，需重建）

若希望图片随 git / 版本走：

1. 把 webp 放 `web/assets/art/user-edited/<key>.webp`。
2. 更新两处：
   - 前端 `ITEM_IMAGES`（`web/index.html`）；
   - 后端默认 `items.defs`（`internal/settings/settings.go`）里的 `image` 字段。
3. `git pull && docker compose up -d --build`（`go:embed` 重新打包）。

---

## 5. 道具数据模型（`items.defs`）

后台可编辑的服务端配置，每项：

```json
{ "key": "sing", "emoji": "🎤", "label": "唱歌", "image": "/media/items/sing.webp" }
```

- 前端渲染优先级：`image` → 内嵌 `ITEM_IMAGES[key]` → `emoji`。
- 相关配置项（同页可改）：`items.enabled`、`items.cooldown_ms`（冷却，默认 30000ms）；弹幕：`danmaku.enabled`、`danmaku.cooldown_ms`；群聊/大厅/好友开关。

> 若某次后台保存写入了 `items.defs` 覆盖行，它会**遮蔽代码默认值**；想恢复代码默认：删掉 `coc_app_settings` 中 `key='items.defs'` 的行。

---

## 6. 部署与运维

- 服务器：`git pull && docker compose up -d --build`（首次会创建 `./media` 并只读挂载）。
- `.env` 可选：`MEDIA_DIR=/media`（compose 已默认）。
- **`media/` 不在 git**（已忽略）→ 请**自行备份**该目录。
- 排障：
  - 图 404：确认 `MEDIA_DIR` 已设、文件在挂载目录、扩展名在白名单、URL 以 `/media/` 开头。
  - 反代/HTTPS：`/media/*` 与普通静态同源，无需额外配置。

---

## 7. 相关文件

| 文件 | 作用 |
|---|---|
| `internal/httpapi/media.go` | `/media/*` 处理（白名单/防穿越/缓存/安全头） |
| `internal/config/config.go` | `MEDIA_DIR` 解析 |
| `internal/settings/settings.go` | `items.defs` 等默认配置 |
| `web/index.html` | 道具栏/特效渲染（`it.image`）与 `ITEM_IMAGES` |
| `web/Admin/index.html` | 后台「设置 → 道具清单」编辑（含 `image`） |
| `tools/prepare-item-art.ps1` | 本地 PNG → WebP 转换脚本 |
| `docker-compose.yml` / `docker-compose.full.yml` | `./media:/media:ro` 挂载与 `MEDIA_DIR` |
