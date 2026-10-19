# 0.2.1 · LLM 供应商与 DeepSeek 工具验证修复

连接与设置、智能生图选择器及后台错误统一使用“LLM 供应商”。验证过但未通过的能力明确标为“未通过”，尚未验证时显示“待验证”。

DeepSeek 截图中的 `Thinking mode does not support this tool_choice` 来自工作台的工具探测：旧请求强制选择 `capability_echo`，与供应商默认启用的思考模式冲突。工具验证改用 `auto`，仍要求收到正确工具名及随机 nonce 的真实 `tool_use`；文本声明、错误工具、错误参数都不能通过。工具请求给思考与调用留出最多2048 tokens，并遵守用户设置的更低输出上限。验证仍为4项请求，不增加自动重试。

同时修复兼容清理误删历史消息 `thinking` 内容的问题。只移除顶层思考配置，保留供应商返回的思考块、签名与工具结果，使多轮工具对话能继续。

## DeepSeek 配置

- Base URL：`https://api.deepseek.com/anthropic`
- 模型：`deepseek-flash`
- 模式：其他模型 · 兼容接入
- 认证：x-api-key（默认）

地址、模型及 x-api-key 认证符合 [DeepSeek Anthropic API 文档](https://api-docs.deepseek.com/guides/anthropic_api/)；[思考模式文档](https://api-docs.deepseek.com/guides/thinking_mode/)说明默认启用思考。

升级后原供应商连接与密钥保留。请重新点击“验证 Agent 能力”；工作台不会自动将旧失败记录改为通过。验证会发送 LLM 请求，可能产生费用。

## 验证

Go 测试与 vet、TypeScript/前端构建、11项真实 Wails 桌面测试及 UI 严格检查通过。模拟服务复现强制工具选择返回400，验证新版自动选择可通过；真实内置 Claude Code 对接模拟思考响应，完成 Skill、MCP、参考图读取及12张方案，且后续请求完整保留思考内容与签名。错误工具和错误参数的否定测试通过。

未读取或调用用户的真实 DeepSeek Key；付费 API 调用为0。模拟验证不能代表线上账号和全部模型的实际可用性。

## 升级

关闭旧版，完整解压新版并覆盖原应用目录中的程序与随包运行组件。保留 `data`、`data-location.txt` 和自选数据目录；完整便携包仍包含固定 WebView2 和 Claude Code 2.1.296。
