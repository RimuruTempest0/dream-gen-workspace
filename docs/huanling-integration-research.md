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

## 开发前还需实测

- 选定分组对应的 Key 与模型权限。
- 完整文生图 / 图生图请求、返回 schema，参考图 JSON 与 multipart 等协议差异。
- 批量接口是否对该 Key 开放、单次上限、并发限额、取消语义。
- 异步任务查询能否仅使用 API Key，以及最终路径前缀。
- 实际耗时、超时策略、429/5xx 处理、幂等重试与重复扣费行为。
- 图片 URL 有效期、下载认证与输出格式。
- 选定分组和分辨率的实际扣费。
