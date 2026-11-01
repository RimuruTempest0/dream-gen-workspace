# 上游能力调查（客户交付版 · 阶段 0）

调查日期：2026-10-24。目的：为"激活前试用 20 张 + 激活后启用客户专属 Key"的客户模式确认各上游能做什么，回答四个问题：

1. 能否给单个 API Key 设硬额度，用完即拒绝？拒绝时返回什么？
2. 能否限制单个 Key 只能用某些模型？
3. 能否用 Key 本身免费查询剩余额度？返回什么格式？
4. 有没有管理 API（创建 Key、改额度、查用量、禁用），需要什么鉴权？

## 调查范围与方法

本机数据目录（`release/ImageWorkbench-0.3.0/data`）里共配置了 3 个上游：

| 名称 | 类型 | 地址 | 模型 |
| --- | --- | --- | --- |
| 幻灵 | 生图中转站 | `https://ai.huanling.icu` | gpt-image-2 |
| 1 route | 生图中转站 | `https://image-api.1route.dev` | gpt-image-2 |
| deepseek | LLM | `https://api.deepseek.com/anthropic` | deepseek-flash |

除这 3 个外没有其他已配置的图片中转站。

依据分三类，每条结论后会注明：

- **实测**：用本机已保存的 Key 发起的免费 GET 请求（`/v1/models`、额度查询接口、公开站点配置）。没有调用任何生图或 LLM 生成接口，也没有修改任何上游设置。Key 只在内存中解密使用，没有写入任何文件。
- **源码**：幻灵和 1 route 都运行开源的 Sub2API（[Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api)）。我按两站实际报告的版本号 v0.2.17、v0.2.13 对应的 tag 读了源码，下文行为在这两个 tag 中一致。
- **官方文档**：DeepSeek 用 [api-docs.deepseek.com](https://api-docs.deepseek.com/) 和[官方 FAQ](https://static.deepseek.com/faq/index.html?lang=zh#/category/4)。

> 注意：Sub2API 是开源程序，站长可以在后台关闭功能，或在前面加代理屏蔽路径（1 route 就屏蔽了一部分，见下文）。源码能证明"这个版本具备该能力"，不能证明"站长没关掉它"。所以凡是只有源码依据、没有实测的结论，文末都列了需要找客服确认的问题。

---

## 一、幻灵 Huanling（`https://ai.huanling.icu`）

**身份确认（实测）**：`GET /api/v1/settings/public`（公开接口，无需 Key）返回 `site_name: "幻灵AI"`、`site_subtitle: "Subscription to API Conversion Platform"`、`version: "0.2.17"`，确认是 Sub2API v0.2.17。`/v1/models` 返回 gpt-image-2、gpt-image-2-w、gpt-image-2.5、gpt-image-2.5-flare 四个模型。

### 1. 单 Key 硬额度 —— 支持（按美元金额，不按张数）

- 依据（源码）：`backend/internal/handler/api_key_handler.go`。创建 Key 时可传 `quota`（USD，0 表示不限），还可以传 `expires_in_days`，以及按 5 小时、1 天、7 天滚动窗口的 `rate_limit_5h/1d/7d`（USD）。修改时还能传 `reset_quota: true` 把已用额度清零。
- 额度在服务端记账，字段是 `quota_used`，客户端没办法重置。
- 拒绝判定（源码，`server/middleware/api_key_auth.go`）：每次请求**之前**检查 `quota_used >= quota`，满足就拒绝。生图接口 `/v1/images/generations` 被拒时返回：
  - HTTP **429**
  - `{"code":"API_KEY_QUOTA_EXHAUSTED","message":"API key 额度已用完"}`
  - 只有 `/v1/responses` 这类 Responses 接口返回 OpenAI 格式：`{"error":{"type":"insufficient_quota","code":"insufficient_quota","message":"API key 额度已用完",...}}`，同样是 429。
- 其他相关拒绝（源码）：
  - Key 已停用：401 `API_KEY_DISABLED`
  - Key 已过期：403 `API_KEY_EXPIRED`
  - 账户钱包余额 ≤ 0：403 `INSUFFICIENT_BALANCE`
  - 超出滚动窗口限额：429
- **对"试用 20 张"的影响**：
  - 额度按美元计，不是按张数。生图按 token 计价（实测 `/v1/sub2api/billing` 返回 `billing_scope: "token"`，倍率 1），所以每张的价格会随尺寸、质量、参考图浮动。实测历史均价约 $0.02 每张：gpt-image-2 共 42 次请求花了 $0.90；2.5-flare 每次约 $0.03。20 张大约是 $0.40–0.45，但不是精确值。
  - 检查发生在请求之前，所以最后一张会把用量推到超过额度（`quota_used` 可能略大于 `quota`）。如果并发发起 N 张，最多可能超出 N 张的费用。试用模式建议在应用里把并发限制为 1，并以上游返回的剩余金额为准，换算成"约 N 张"展示。
  - Key 额度只限制这一把 Key 的花费，实际扣的是站长账户（也就是你）的钱包。钱包余额耗尽时所有 Key 一起失效（403 `INSUFFICIENT_BALANCE`），所以钱包要留足余量。

### 2. 限制 Key 只能用某些模型 —— 部分支持（需站长配合）

- 依据（源码，`server/middleware/group_model_allowlist.go`）：模型白名单是**分组级**的。分组由站点管理员配置；普通用户创建或修改 Key 时，只能用 `group_id` 选择站长开放的分组（`GET /api/v1/groups/available`）。请求了白名单以外的模型时，返回 404：`Model "xxx" is not available for this group`，生图接口用 OpenAI 错误格式。
- Key 上没有单独的"允许模型"字段。要做到"只允许 gpt-image-2"，需要幻灵站长提供一个白名单只含 gpt-image-2 的分组。**需问客服。**
- 不限制模型时，试用 Key 的最大损失也已经被 USD 额度封顶，最多是被用来调用更贵的模型、提前把试用额度花完。所以这项是加分项，不是试用防重置的前提。

### 3. 用 Key 免费查询剩余额度 —— 支持（实测）

- `GET https://ai.huanling.icu/v1/usage`，请求头 `Authorization: Bearer <Key>`。
- 实测：200，返回 JSON。new-api 风格的 `/api/usage/token`、OpenAI 风格的 `/v1/dashboard/billing/subscription` 和 `/v1/dashboard/billing/usage` 都返回 404，不可用。
- 不计费（源码）：鉴权中间件对 `/v1/usage` 设置了 `skipBilling`，不做额度、余额检查，也不记账。**额度用完或已过期的 Key 依然能查询**，便于应用显示"试用已用完"。停用的 Key 会在更早的步骤被拒（401 `API_KEY_DISABLED`）。
- 返回格式分两种模式，由服务端决定：
  - **未设额度的 Key**（当前本机 Key 就是这种，实测）：`mode: "unrestricted"`，返回的是账户钱包余额：
    ```json
    {"mode":"unrestricted","isValid":true,"planName":"钱包余额","balance":…,"remaining":…,"unit":"USD",
     "usage":{"today":{"requests":0,"cost":0,...},"total":{"requests":44,"cost":0.96,...},"rpm":0,"tpm":0},
     "daily_usage":[{"date":"2026-10-18","requests":36,"cost":0.72,...}],
     "model_stats":[{"model":"gpt-image-2","requests":42,"cost":0.9,...}]}
    ```
  - **设了额度或限速的 Key**（源码，`handler/gateway_handler.go` 的 `usageQuotaLimited`）：
    ```json
    {"mode":"quota_limited","isValid":true,"status":"active",
     "quota":{"limit":0.45,"used":0.12,"remaining":0.33,"unit":"USD"},
     "remaining":0.33,"unit":"USD",
     "rate_limits":[{"window":"1d","limit":..,"used":..,"remaining":..,"reset_at":"..."}],
     "expires_at":"...","days_until_expiry":..,
     "usage":{...},"daily_usage":[...],"model_stats":[...]}
    ```
    `status` 的取值有 `active`、`quota_exhausted`、`expired`。`usage.total.requests` 是这把 Key 的累计请求数，可以作为"已生成张数"的参考。注意它也会计入失败的请求，以实际表现为准。
- 附带免费接口：`GET /v1/sub2api/billing`（实测 200）返回这把 Key 的计费倍率，可用于换算价格。

### 4. 管理 API —— 有（用户级，需登录 JWT）

- 依据（源码，`server/routes/user.go`、`handler/api_key_handler.go`、`handler/auth_handler.go`）。实测 `GET https://ai.huanling.icu/api/v1/keys` 不带凭证返回 401 `{"code":"UNAUTHORIZED","message":"Authorization header is required"}`，说明接口在线。
- 鉴权：**不能用 API Key**，要用账户登录拿到的 JWT。
  - `POST /api/v1/auth/login`，body 为 `{"email","password"}`，返回 `access_token`、`refresh_token`、`expires_in`。
  - 开了两步验证时会返回 `requires_2fa` 和 `temp_token`，需要再调 `POST /api/v1/auth/login/2fa`。
  - access token 默认 24 小时；`POST /api/v1/auth/refresh` 用 refresh token 续期，refresh token 默认 30 天。站长可以改这些时长。
  - 登录接口限速 20 次/分钟。幻灵当前未开人机验证（`turnstile_enabled: false`），TOTP 两步验证是用户可选的。
- 接口（统一包在 `{"code":0,"message":"success","data":...}` 里）：

  | 操作 | 接口 |
  | --- | --- |
  | 列出 Key | `GET /api/v1/keys?page=&page_size=&search=&status=` |
  | 查看单个 | `GET /api/v1/keys/:id`（含 `key`、`quota`、`quota_used`、`status`、`expires_at`、`usage_5h/1d/7d` 等） |
  | 创建 | `POST /api/v1/keys`，`{"name","group_id","quota","expires_in_days","rate_limit_5h/1d/7d","custom_key","ip_whitelist"}`，返回的记录里含完整 `key` |
  | 改额度、续期、停用 | `PUT /api/v1/keys/:id`，`{"quota":..,"reset_quota":true,"status":"inactive"/"active","expires_at":"RFC3339","group_id":..}` |
  | 删除 | `DELETE /api/v1/keys/:id` |
  | 按日用量 | `GET /api/v1/user/api-keys/:id/usage/daily` |
  | 用量明细、统计 | `GET /api/v1/usage`、`/api/v1/usage/stats`、`POST /api/v1/usage/dashboard/api-keys-usage` |
  | 可选分组 | `GET /api/v1/groups/available` |

- 结论：阶段 2/3 的管理员工具**可以**自动创建试用 Key（带 USD 额度）、改额度、停用和删除 Key、查用量。前提是工具保存幻灵账户的邮箱和密码，或长期有效的 refresh token，按本项目规则应使用 DPAPI 加密保存。站长若开启"后端模式"（`backend_mode_enabled`，当前为 false）会拦截用户面板接口。

---

## 二、1 Route（`https://image-api.1route.dev`）

**身份确认（实测）**：

- 主站 `https://www.1route.dev/api/v1/settings/public` 返回 `site_name: "1 Route"`、`version: "0.2.13"`、`api_base_url: "https://api.1route.dev"`。`custom_endpoints` 把 `https://image-api.1route.dev` 标注为"图片生成"专用入口（备注"如遇 CF 的 524 超时错误可尝试"）。
- `image-api.1route.dev/v1/models` 不带 Key 时返回 Sub2API 原版报错 `{"code":"API_KEY_REQUIRED",...}`，带 Key 时返回的模型列表格式与幻灵完全一致。
- 结论：1 Route 是 Sub2API v0.2.13，`image-api` 子域是同一实例的图片专用入口，前面多了一层 openresty 代理，只放行部分路径。

### 1. 单 Key 硬额度 —— 支持（按美元金额；源码）

- 与幻灵同一套程序，v0.2.13 中额度字段、拒绝逻辑和返回值与上文一致：额度用完时生图返回 429 `{"code":"API_KEY_QUOTA_EXHAUSTED","message":"API key 额度已用完"}`。
- 计价同为 token 计费、倍率 1（实测 `api.1route.dev/v1/sub2api/billing`）。实测 gpt-image-2-1k 22 次花了 $0.44，正好 $0.02 每张。
- 未实测"额度用完"的真实返回，因为要花钱把额度用完。另外 `image-api` 子域前面的代理是否原样透传 429，**需问客服或用小额度 Key 实测**。

### 2. 限制 Key 只能用某些模型 —— 部分支持（需站长配合；源码）

- 同幻灵：只能通过站长配置的、带模型白名单的分组实现。1 Route 模型很多（gpt-image-2、-1k/-2k/-4k、2.5 系列、grok-imagine 等），价格不同。**需问客服**有没有只含 gpt-image-2 的分组。

### 3. 用 Key 免费查询剩余额度 —— 支持，但要走主 API 域名（实测）

- `GET https://image-api.1route.dev/v1/usage` 实测 **404**（`not found`，由代理返回，不是 Sub2API 的 404）。new-api、OpenAI 风格的额度接口同样 404。
- `GET https://api.1route.dev/v1/usage` 用**同一把 Key** 实测 **200**，返回结构与幻灵相同（当前为 `mode: "unrestricted"`、`planName: "钱包余额"`、`unit: "USD"`，并附带当天 gpt-image-2-1k 的用量），说明是同一账户、同一实例。
- 因此应用里查询 1 Route 剩余额度时，要把查询地址设为 `https://api.1route.dev/v1/usage`，不能沿用生图地址。不计费的依据同幻灵（源码中有 `skipBilling`）。

### 4. 管理 API —— 有（用户级，需登录 JWT；源码 + 实测在线）

- 面板 API 在 `https://www.1route.dev/api/v1/...`（`1route.dev` 会 301 跳到 `www`）。实测 `GET https://www.1route.dev/api/v1/keys` 不带凭证返回 401 `UNAUTHORIZED`，接口在线。
- 接口、鉴权方式同幻灵。1 Route 当前关闭了新用户注册（`registration_enabled: false`）、未开人机验证和 TOTP，`backend_mode_enabled: false`。
- 未实测带 JWT 的调用，因为这需要用户的面板账户密码，本机只保存了 API Key。

---

## 三、DeepSeek（LLM，`https://api.deepseek.com/anthropic`，模型 deepseek-flash）

**身份确认（实测）**：`GET https://api.deepseek.com/models` 返回 `deepseek-flash`（DeepSeek-V4.1-Flash）和 `deepseek-v4-pro`，是 DeepSeek 官方平台，不是中转。

### 1. 单 Key 硬额度 —— 不支持

- 依据（官方文档）：
  - [Error Codes](https://api-docs.deepseek.com/quick_start/error_codes) 只有账户级的 `402 Insufficient Balance`："You have run out of balance"。
  - [Rate Limit & Isolation](https://api-docs.deepseek.com/quick_start/rate_limit) 写明并发限制按账户计算，与用哪个 Key 无关。
  - 文档和 [FAQ](https://static.deepseek.com/faq/index.html?lang=zh#/category/4) 里都没有单 Key 额度、单 Key 预算的说明。
- 余额耗尽时返回 HTTP **402**，所有 Key 一起失效。
- 如果想给每个客户单独封顶，只能每个客户用一个独立的 DeepSeek 账户（各自充值），或者把 LLM 放到一个支持单 Key 额度的中转后面。**需问客服**是否有子账户或单 Key 预算功能。

### 2. 限制 Key 只能用某些模型 —— 不支持（官方文档未提供）

- 文档中 Key 没有模型范围设置。账户下所有 Key 都能调用全部模型。**需问客服**确认。

### 3. 用 Key 免费查询剩余额度 —— 仅账户级余额（实测 + 官方文档）

- `GET https://api.deepseek.com/user/balance`，请求头 `Authorization: Bearer <Key>`。文档见 [Get User Balance](https://api-docs.deepseek.com/api/get-user-balance)。
- 实测 200：
  ```json
  {"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"…","granted_balance":"0.00","topped_up_balance":"…"}]}
  ```
- 返回的是**整个账户**的余额，不是这把 Key 的剩余额度。这是只读查询，文档未提到计费。`/v1/dashboard/billing/subscription` 实测 404。
- 按 Key 查看用量只能在网页控制台做：FAQ "如何分 key 查看用量"指引登录开放平台，到"用量信息"页按 API Key 筛选或导出 CSV。没有 API。

### 4. 管理 API —— 不支持（官方文档未提供）

- API 文档只有模型调用、`/models`、`/user/balance` 等接口，没有创建、禁用 Key 或查询分 Key 用量的接口。Key 只能在 [platform.deepseek.com](https://platform.deepseek.com/) 网页上手动创建和删除。**需问客服**有没有企业或管理 API。

---

## 汇总表

| 上游 | 1. 单 Key 硬额度 | 2. Key 限模型 | 3. Key 免费查剩余 | 4. 管理 API |
| --- | --- | --- | --- | --- |
| 幻灵（Sub2API v0.2.17） | **支持**（USD 额度，满额后 429 `API_KEY_QUOTA_EXHAUSTED`；源码） | **待客服确认**（需站长提供仅含 gpt-image-2 的分组） | **支持**（`/v1/usage`，实测；设额度后返回 `quota.remaining`） | **支持**（`/api/v1/keys`，需登录 JWT；源码 + 实测在线） |
| 1 Route（Sub2API v0.2.13） | **支持**（同上；源码，代理透传待确认） | **待客服确认**（同上） | **支持**，但要用 `https://api.1route.dev/v1/usage`（实测；`image-api` 子域 404） | **支持**（`www.1route.dev/api/v1/keys`，需登录 JWT；源码 + 实测在线） |
| DeepSeek 官方 | **不支持**（只有账户余额，余额耗尽 402；文档） | **不支持**（文档无此功能，待客服确认） | **仅账户级**（`/user/balance`，实测） | **不支持**（文档无此功能，待客服确认） |

### 对阶段 1–3 的结论

- **试用 20 张能否靠上游保证**：生图部分可以。在幻灵或 1 Route 为每份软件创建一把带 USD 额度的试用 Key（约 20 × 单价，再留一点余量），额度在服务端记账，客户无法重置。代价是只能"约 20 张"，不是精确张数。
- **试用剩余数能否读上游**：可以。应用调用 `/v1/usage`，读取 `quota.remaining`（USD），配合单价换算成张数，或者读 `usage.total.requests` 显示已用次数。1 Route 必须查 `api.1route.dev`。
- **管理员工具能否自动化**：两家 Sub2API 都可以，用面板账户登录拿 JWT 后创建、修改、停用、删除 Key 并查用量。DeepSeek 不行，客户专属 LLM Key 只能在网页手工创建；试用期的 LLM 费用也无法按 Key 封顶，需要由应用侧限制，或另选支持单 Key 额度的 LLM 中转。

---

## 请向客服确认的问题

以下按上游分组，可以直接复制发送。

### 幻灵 Huanling

1. 我在用户后台创建 API Key 时设置的"额度（USD）"是否已在站点上开放？额度用完后，`/v1/images/generations` 是否返回 HTTP 429，body 为 `{"code":"API_KEY_QUOTA_EXHAUSTED","message":"API key 额度已用完"}`？
2. 额度是在请求前判断的，最后一次请求可能超额。能否改成"预扣"，或者有没有办法避免超额？如果不能，超额部分按实际费用从我的钱包扣，对吗？
3. 能否为我的账户提供一个只允许 `gpt-image-2` 的分组（模型白名单），让我把试用 Key 绑定到这个分组？
4. gpt-image-2 每张图的计费规则是什么（按 token 还是按张？1024×1024、2K、4K、不同 quality、带参考图时分别多少钱）？我需要按"约 20 张"换算 Key 额度。
5. 我计划用程序调用用户面板接口（`/api/v1/auth/login`、`/api/v1/keys` 的增删改查），自动管理十几把 Key。这样是否允许？有没有频率限制？会不会因此触发风控或封号？
6. `GET /v1/usage` 查询剩余额度是否完全免费、不计入任何用量？有没有频率限制？
7. 站点后续是否计划开启"后端模式"或关闭用户面板 API？如果开启，有没有替代的 Key 管理方式？

### 1 Route

1. 我账户的 API Key 能否设置"额度（USD）"？额度用完后，通过 `https://image-api.1route.dev` 调用生图是否返回 HTTP 429 `API_KEY_QUOTA_EXHAUSTED`，还是会被前置代理改写成别的状态码或内容？
2. `https://image-api.1route.dev/v1/usage` 返回 404，`https://api.1route.dev/v1/usage` 正常。请确认后者可以长期用来查询 Key 剩余额度，且不计费。能否在 `image-api` 子域也放行 `/v1/usage`？
3. 能否提供一个只允许 `gpt-image-2`（或指定分辨率版本，例如 `gpt-image-2-1k`）的分组，让我把试用 Key 绑定到这个分组？
4. gpt-image-2 和 gpt-image-2-1k/2k/4k 每张的价格分别是多少？是按 token 还是按张计费？
5. 我计划用程序调用 `https://www.1route.dev/api/v1/auth/login` 和 `/api/v1/keys` 自动创建、修改、停用 Key。这样是否允许？有没有频率限制或风控？
6. 额度判断在请求前进行，最后一次请求可能超额。是否有预扣机制？超额部分如何计费？

### DeepSeek

1. 能否给单个 API Key 设置消费上限（金额或 token 数），用完后只有这把 Key 被拒绝、不影响账户里其他 Key？
2. 能否限制某个 API Key 只能调用指定模型（例如只允许 `deepseek-flash`）？
3. 有没有查询单个 API Key 用量或剩余额度的 API？目前 `/user/balance` 只返回账户总余额，按 Key 的用量只能在网页"用量信息"里看或导出。
4. 有没有管理 API（或企业版、子账户功能）可以用程序创建、禁用 API Key 并查询其用量？
5. 如果为每位客户注册独立的 DeepSeek 账户，各自充值、互不影响，是否符合使用条款？企业实名下能否开多个子账户？
