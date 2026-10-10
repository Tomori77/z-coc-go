# Zarkto's Table 美术素材包

这组素材用于在不改变原有克苏鲁暗色主题的前提下，增加真实材质、历史感和场景叙事。素材已接入主站、图书馆、调查员工坊和 404 页面。

## 使用原则

- 场景图作为低对比背景或章节封面，前景叠加深色渐变，避免影响文字可读性。
- 真实馆藏照片只在资料库、档案页或历史引用区使用，不伪装成项目原创照片。
- 主色维持近黑海军蓝、木炭灰、旧纸灰；铜色和氧化青只作为小面积高光。
- 不使用明显怪物正面图、发光符文和夸张触手，恐怖感来自空间、时代痕迹和留白。
- 网页优先引用 `.webp`；同名 `.png` 或 `.tif` 是母版。

## 素材清单

| 文件 | 建议用途 | 画面角色 |
| --- | --- | --- |
| `generated/scene-new-england-street-1926.webp` | 首页、章节入口、城镇导航 | 1926 年新英格兰海滨街景 |
| `generated/library-reading-room-night.webp` | 图书馆、资料检索、知识库 | 夜间大学阅览室 |
| `generated/workshop-investigator-dossier.webp` | 角色卡、调查员工坊 | 档案袋与调查工具静物 |
| `generated/tavern-after-hours-1924.webp` | 轶闻 Log、酒馆叙事页 | 打烊后的 1920 年代旅店酒吧 |
| `generated/radio-receiver-1923.webp` | 在线、广播、联络模块 | 1923 年电子管无线电台 |
| `generated/cephalopod-archive-plate.webp` | 空状态、水印、规则手册装饰 | 深海头足类自然史图谱 |
| `generated/dark-archive-cloth-texture.webp` | 全局底纹、卡片内部、侧栏 | 低对比档案布纹 |
| `generated/observatory-chart-1890s.webp` | 侧栏、设置详情、卡片水印 | 19 世纪天文观测草图 |
| `generated/marine-algae-herbarium.webp` | 空状态、图书馆卡片、抽屉装饰 | 海藻腊叶标本页 |
| `generated/investigation-evidence-vignette.webp` | 章节分隔、广播与轶闻页 | 调查证物横向静物 |
| `generated/library-forbidden-archive-v1.webp` | 密大图书馆页面背景 | 封闭式古籍档案库 |
| `generated/mode-multiplayer-occult-link-v2.webp` | 多人联机模式卡片 | 1920 年代收音机与协作档案 |
| `generated/launch-ritual-moon-archive-v1.webp` | 启动页动画 | 月相档案与仪式构图 |
| `generated/brand-occult-brass-seal-v1.webp` | 品牌徽章 | 黑化黄铜馆藏印章 |
| `generated/archive-corner-brass.webp` | 卡片与模块左上角件 | 黑化黄铜档案柜角件，透明背景 |
| `generated/museum-label-holder.webp` | 横幅编号、档案标签 | 黄铜博物馆抽屉标签框，透明背景 |
| `generated/evidence-binding-strip.webp` | 卡片底边、章节横带 | 带封蜡的旧档案装订带，透明背景 |
| `generated/archive-index-tab.webp` | 备用档案标签素材，不直接铺设为导航控件 | 黑色书布与黄铜包边索引标签，透明背景 |
| `generated/instrument-status-medallion.webp` | 状态图标、勋章与强调按钮 | 黑化黄铜科学仪器圆形表圈，透明背景 |
| `generated/brass-calibration-rule.webp` | 备用陈列素材，不直接压缩为卡片边线 | 无编号黄铜仪器刻度条，透明背景 |
| `generated/stitched-archive-patch.webp` | 备用档案注释素材，不作为正文或空状态底板 | 缝在黑色书布上的旧纸片，透明背景 |
| `public-domain/loc-reading-room-1920.webp` | 历史资料卡、图书馆实物引用 | 约 1920 年美国国会图书馆阅览室 |
| `user-edited/tavern-candle-archive-edited.webp` | 异闻酒馆、轶闻 Log 主叙事背景 | 用户提供横版插画的暗色档案化调色版本 |

## 公共领域来源

`loc-reading-room-1920.tif` 下载自美国国会图书馆条目 “A reading room in the Library of Congress, Washington, D.C.”。条目标注约 1920 年，摄影者 Harris & Ewing，权利说明为 “No known restrictions on publication”。网页版本裁掉了底部的历史摄影社署名，只保留在未修改的来源 TIFF 中。

- 条目：https://www.loc.gov/pictures/item/2011647046/
- 数字文件：https://cdn.loc.gov/master/pnp/cph/3a00000/3a09000/3a09300/3a09316u.tif
- 建议信用行：Library of Congress, Prints and Photographs Division; photograph by Harris & Ewing.

额外筛选但尚未下载的公共领域候选：

- 1924 年 West Roxbury 城市地图册：https://www.loc.gov/item/2008629007/
- Smithsonian Open Access：https://www.si.edu/openaccess
- NYPL Public Domain Collections：https://www.nypl.org/research/resources/public-domain-collections

引入额外馆藏时仍需逐条核对 Rights / Usage Conditions，不能只根据站点总体说明推断单个文件许可。

## 生成说明

原创素材使用 Codex 内置 GPT 图像生成功能分别生成，提示中明确排除了文字、标志、签名和水印，并按网页留白需求设计构图。嵌入式 UI 零件先在纯品红背景生成，再在本地去色键并导出透明 PNG/WebP。完整最终提示词见 `PROMPTS.md`。

第一张用户提供的横版烛光书房插画通过内置图像编辑功能进行了非破坏性的暗色调色：保留人物、构图与场景物件，只调整为蓝黑、旧铜、暗红与氧化青体系，并增加响应式边缘压暗。第二张用户图片及后续竖版替代图均未接入项目。

## 网页优化

`tools/optimize_art.py` 使用 Pillow 将母版转换为质量 88 的 WebP。再次添加 PNG 时可重新运行脚本；公共领域阅览室图会在导出时自动裁掉底部署名区域。

网页只引用经过压缩的 WebP。PNG/TIFF 母版与 `tmp/` 中间文件由 `.gitignore` 排除，不应上传到 GitHub 或生产服务器；它们仅用于本地重新导出素材。部署时保留本说明、来源记录和实际被页面引用的 WebP 文件。

## 当前接入方式

- 主站各模块通过 `.z-art-view` 与页面修饰类选择不同场景；渐变蒙版控制文字对比度。
- 桌面场景高度最高 560px，手机端最高 330px，并分别设置焦点位置，避免关键物体被裁掉。
- 场景统一做低饱和、轻微灰阶、压暗和慢速呼吸缩放；系统减少动态效果时自动关闭动画。
- 图书馆使用封闭式古籍档案库作为页面背景；夜间阅览室素材只用于内容窗口，避免同一素材重复承担背景与头图。
- 调查员工坊使用档案静物横幅；无线电、轶闻、存档、手册和 404 页分别使用对应素材。
- 天文草图进入侧栏、设置高级模块与工坊主卡片；海藻标本进入空状态、图书馆卡片与工坊抽屉。
- 调查证物横幅用于轶闻 Log 和广播页的章节分隔，保持低高度、两侧渐隐，不挤压正文。
- 黄铜角件、博物馆标签框和装订带分别用于卡片角部、模块编号与卡片底边；手机端自动缩小并降低不透明度。
- 索引标签承担侧栏与分段控件的真实选中底板，科学仪器表圈进入设置图标与工坊勋章，黄铜刻度条统一标题及进度层级，缝线纸片用于空状态与说明区。
- 暗色布纹以约 4.5% 不透明度和 `soft-light` 混合覆盖主站，不影响正文清晰度。
