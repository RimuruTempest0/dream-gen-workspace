# 幻灵图片中转站接入调研

调研日期：2026-10-11（Asia/Shanghai）。通过 Computer Use 读取已登录浏览器的生图页面、模型广场以及浏览器 DevTools 中已加载的公开客户端代码。没有创建密钥、上传图片或提交付费生图任务。

## 证据范围

- 生图页面：https://api.huanling.icu/image-playground
- 模型广场：https://api.huanling.icu/model-plaza
- 客户端实现线索：https://api.huanling.icu/assets/ImagePlaygroundView-ByrLLq1N.js
- 批量接口实现线索：https://api.huanling.icu/assets/batchImage-BeyQ1H0T.js
- 参数映射实现线索：https://api.huanling.icu/assets/index-DPDAQkXl.js

静态客户端代码能证明客户端构造了哪些请求，不能证明接口已开放给第三方、服务端完整参数约束、实际返回内容或模型供应来源。下列接口均尚未实测。

## 页面已确认的行为

- 支持文生图和图生图；按分组选择 API Key 和模型。
- 基础参数：提示词、比例、分辨率、模型、质量、张数。
- 高级参数：背景、输出格式、输入保真度。
- 图生图页面最多添加 4 张参考图，支持文件选择和粘贴；自动缩至长边 1536px；PNG/WebP 保留透明通道。这是网页预处理规则，不能直接当成 API 的服务端硬限制。
- 网页任务保留 24 小时，按登录用户汇总；切换或删除 Key 不影响历史读取；刷新或断网不会取消后台生成。
- 图片链接另有有效期，页面要求及时下载。未确认具体有效期。
- 初始选中的 gpt-plus 分组提示没有可用 Key，模型选择与生图按钮禁用；这不代表整个账户没有 Key。

## 模型与分组

模型广场当时列出的名称是中转站标识，尚未验证上游来源与真实可用性。

- gpt生图：gpt-image-2、gpt-image-2-w、gpt-image-2.5-flare、gpt-image-2.5-sunburst。
- gpt生图（adobe原生4k）：gpt-image-2-adobe、gpt-image-2.5-flare、gpt-image-2.5-sunburst。
- gemini生图-adobe / gemini生图-pro：gemini-3-pro-image-preview、gemini-nano-banana-2.1、gemini-3.1-flash-image-preview。

普通 GPT 分组的备注称 2K/4K 为超分；另一个分组宣称原生 4K。均为站点自述。

计费文案存在不一致：普通 GPT 模型表 4K 为 ¥0.05/次，生图页默认显示 $0.06；adobe 分组备注与各模型表也不完全一致。不可硬编码这些价格或直接按美元汇率换算平台余额。需要以选定分组、模型和实际扣费确认。

## 从客户端代码读取到的接口

### 网页异步生图提交

客户端按请求类型选择：

| 方法 | 路径 | 客户端用途 |
|---|---|---|
| POST | /v1/image-playground/generations | 文生图异步提交 |
| POST | /v1/image-playground/edits | 图生图异步提交 |
| POST | /v1/image-playground/gemini | Gemini 异步提交 |

请求头包含 Authorization: Bearer（选定 API Key）、Content-Type: application/json、Idempotency-Key、X-Client-Request-Id。两个请求标识来自同一 clientRequestId。服务端幂等范围、期限和去重效果仍需验证。

任务列表与详情分别由另一套客户端 Ne.get 调用 /image-playground/tasks 和 /image-playground/tasks/{id}；列表带 cursor、limit（客户端限制 1～100）。未追踪该客户端的最终 URL 前缀或认证实现，不能认为这两个查询接口直接支持生图 API Key。

### 批量图片接口线索

batchImage 客户端统一构造 Authorization: Bearer，并包含以下路径：

| 方法 | 路径 | 用途 |
|---|---|---|
| POST | /v1/images/batches | 提交批量任务；JSON body，带 Idempotency-Key |
| GET | /v1/images/batches/{id} | 查询批次 |
| GET | /v1/images/batches | 列表与筛选 |
| GET | /v1/images/batches/models | 模型列表 |
| GET | /v1/images/batches/{id}/items | 批次子任务 |
| POST | /v1/images/batches/{id}/cancel | 请求取消 |
| GET | /v1/images/batches/{id}/download | 下载结果文件 |
| GET | /v1/images/batches/{id}/items/{itemId}/content?image_index={index} | 下载某张图片 |
| DELETE | /v1/images/batches/{id} | 删除批次（仅看到代码，未执行） |

列表客户端支持 limit、cursor、status、task_name、downloaded、from、to。完整提交 schema、返回 schema、权限及可用性尚未确认。

### 基础模型查询与参数映射

- 客户端用 Bearer 认证请求 GET /v1/models。
- 通用构造器包含 model、prompt、n（n>1 时显式传）、size、quality、background、output_format。
- response_format 的设置受模型判断控制，不能对所有模型一律传 url。
- 参考图构造为 images: [{image_url: ...}]；非 Grok 模型按需添加 input_fidelity。
- Grok 分支另用 aspect_ratio、resolution（小写）和受能力限制的 quality。
- Gemini 分支将 data URL 转换为 inlineData: {mimeType, data} 并组织文本内容。完整结构需要继续读取与实测。
- 错误处理读取 error.message / message、error.code、HTTP status、X-Request-Id。
- 公共客户端文案还出现 /v1/images/generations 与 /images/edits，但本次没有读取到其完整直接调用实现。

## 对产品架构的影响

1. 保留 React + TypeScript + Vite、Go、SQLite、本地图片目录的 portable 方案。
2. 连接配置以供应商、分组、模型和密钥引用为维度，不能只保存一个模型字符串。
3. 按模型能力展示参数，区分请求的分辨率、实际输出尺寸及站点宣称的超分/原生模式。
4. 后端保存本地任务 ID、上游任务 ID、稳定的幂等键、完整参数和状态。结果未知时先核对，避免换新请求标识直接重发。
5. 生成成功后及时下载并归档原图、参考图和元数据；本地历史不依赖站点 24 小时任务窗口。
6. 优先验证 API Key 可用的基础生成接口和批量接口，再决定使用哪条上游任务链路。
7. 网页任务查询如依赖登录会话，portable 产品应自行管理任务，避免依赖浏览器 Cookie。

## 2026-10-19 补充：批量接口不可用，改用异步任务接口

背景：同步 POST /v1/images/edits 位于 Cloudflare 之后，单个请求超过约 2 分钟即被 524 或断开（实测 116–130 秒），中转站可能仍在生成并计费，本应用只能标为“结果未知”。

证据（均为免费、未使用 API Key）：

- 中转站基于开源 Sub2API（github.com/Wei-Shaw/sub2api）。前端批量页 BatchImageGuideView 只列出 `group.platform === "gemini"` 且 `allow_batch_image_generation` 的 Key，批量提交固定 `image_size: "1K"`；上游源码 service/batch_image.go 的批量供应商只有 `gemini_api` 与 `vertex`（Vertex 批量预测，排队可能以小时计）。用户本机全部生图任务使用 gpt-image-2，因此**批量接口不能用于当前连接**。
- 上游 docs/ASYNC_IMAGE_TASKS.md 与 handler/image_task_handler.go 提供 OpenAI/Grok 分组的异步任务接口，明确用于避免 Cloudflare 524：
  - POST /v1/images/edits/async、POST /v1/images/generations/async：请求体与同步接口完全相同（JSON 或 multipart），返回 202 `{id, task_id, status:"processing", expires_at, poll_url}`，`Retry-After: 3`。
  - GET /v1/images/tasks/{task_id}：必须用提交时的同一个 Key；`processing` / `completed`（`result` 同同步响应，`data[].url` 为对象存储链接，b64 被移除）/ `failed`（`http_status` 与 `error`）。轮询不计费。
  - 任务最长执行 30 分钟，状态与结果在最后一次更新后保留 24 小时；未知或他人任务返回 404。没有服务端幂等去重。
  - 该功能默认关闭，需站长配置对象存储；未开启时提交返回 404 `async image tasks are not enabled`。
- 无 Key 探测：三个异步路径均返回 401 `API_KEY_REQUIRED`，不存在的路径返回 404，说明路由已部署。
- 带 Key 免费探测（用户 2026-10-19 批准；连接 test3，Base URL 无 /v1 前缀）：POST /images/edits/async 空请求体返回 404 `async image tasks are not enabled`；GET /images/tasks/<不存在的ID> 返回 404 `IMAGE_TASK_NOT_FOUND`。即任务存储可用但站长未开启该功能。空请求体在创建任务前即被拒绝，未生成、未计费。
- 网页生图页的异步提交 /v1/image-playground/edits 虽用 API Key 提交，但查询 /api/v1/image-playground/tasks 要求网站登录会话（无 Key 返回 401 `UNAUTHORIZED: Authorization header is required`，与网关的 `API_KEY_REQUIRED` 不同），portable 应用无法只凭 API Key 取回结果。

**结论：当前两条路都不能用于本应用。** 批量接口只支持 Gemini；异步任务接口已部署但未开启。可请幻灵站长在后台开启“异步图片对象存储”（Admin → Backup → Async image object storage）。开启后，用同样的空请求体探测会返回 400 `Request body is empty`，届时按下面方案实现。在此之前继续使用 0.3.10 的同步行为：说明超时原因，不自动重发。

适配方案（每个生图连接单独开关，默认关闭，同步路径不变）：

1. 提交：同一 multipart/JSON 请求体改发到 `…/async`，只要等到 202。拿到 task_id 立即持久化到任务记录，状态从 submitting 变为 running。提交本身仍只发一次：断线、5xx 或拿不到 task_id 都按“结果未知”处理，不重发。
2. 轮询：遵循 Retry-After（3–10 秒），轮询 GET 可在网络错误、429、5xx 时继续，不产生新的生成。累计超过 35 分钟仍为 processing，或返回 404（过期、换 Key），标为结果未知并写明 task_id 以便核对。
3. 结果：completed 的 `result.data[].url` 交给现有下载流程；下载失败保留链接可补下载（链接一般 24 小时有效）。failed 映射为失败并显示中转站错误，用户可明确重新生成。提交返回 404 未开启时提示关闭此选项。401/403 沿用现有的暂停同批逻辑。
4. 并行：轮询期间保持占用“同时生成张数”的名额，使并行数仍代表中转站同时生成的张数。
5. 重启：已持有 task_id 的 running 任务在重启后继续轮询、不重新提交；尚未拿到 task_id 的 submitting 任务仍标为结果未知。
6. 诊断：记录 task_id、轮询次数与最近状态，不记录密钥与响应正文。

## 开发前还需实测

- 选定分组对应的 Key 与模型权限。
- 完整文生图 / 图生图请求、返回 schema，参考图 JSON 与 multipart 等协议差异。
- 批量接口是否对该 Key 开放、单次上限、并发限额、取消语义。
- 异步任务查询能否仅使用 API Key，以及最终路径前缀。
- 实际耗时、超时策略、429/5xx 处理、幂等重试与重复扣费行为。
- 图片 URL 有效期、下载认证与输出格式。
- 选定分组和分辨率的实际扣费。
