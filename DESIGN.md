---
version: alpha
colors:
  primary: "#245BDB"
  surface: "#FFFFFF"
  background: "#F5F6F8"
  text: "#182230"
  success: "#18794E"
  danger: "#B42318"
  ink: "#182230"
  inkRaised: "#1F2B3B"
  inkSelected: "#1E3054"
  inkLine: "#3A4A60"
  inkText: "#F2F4F7"
  inkMuted: "#A7B2C2"
  inkAccent: "#8FB0F5"
typography:
  body:
    fontFamily: '"Microsoft YaHei UI", "Segoe UI", sans-serif'
    fontSize: "14px"
    lineHeight: "1.6"
  title:
    fontFamily: '"Segoe UI", "Microsoft YaHei UI", sans-serif'
    fontSize: "20px"
    lineHeight: "1.4"
  utility:
    fontFamily: 'Consolas, "Microsoft YaHei UI", monospace'
    fontSize: "12px"
rounded:
  control: "8px"
  panel: "12px"
spacing:
  small: "8px"
  medium: "16px"
  large: "24px"
components:
  workspace:
    backgroundColor: "#FFFFFF"
    textColor: "{colors.text}"
  success-tag:
    textColor: "{colors.success}"
  danger-tag:
    textColor: "{colors.danger}"
---

## Overview

这是电商运营日常使用的图片制作工具。采用产品界面语气，以接触表式图片工作区为视觉重点：参考图与生成结果同场展示，参数保持清晰。避免营销首页、装饰性指标、虚构图片和虚构任务。

## Colors

上述六种语义色是规范。frontend/src/tokens.ts 是运行时源，CSS 变量与 Ant Design ConfigProvider 都由此映射；本文件镜像相同值。辅助边框、次要文字和状态底色同样只在 tokens.ts 定义。

## Typography

中文系统字体减少字体加载跳动；标题 20px，正文 14px，地址、模型与技术标识使用 utility 字体。

## Layout

左侧固定导航，主内容自然滚动；工作台为输入与结果双栏，参数置于输入下方。窗口小于 1100px 时工作台单栏。图片区预留比例，避免下载后推动操作区。任务列表分页，素材使用加载更多。

## Elevation & Depth

静态面板使用细边框；仅浮层采用组件库阴影。

## Shapes

控件 8px，面板 12px。状态用文本标签，色彩仅补充含义。

## Components

Ant Design 提供共享 Button/Form/Select/Table/Modal/notification，使用 zh-CN locale。全局滚动条、焦点、减少动画规则在 styles.css。业务页面不定义独立设计变量。

按钮与链接的悬停文字沿用 primary 语义色，避免组件库自动生成的浅色导致白底文字对比度不足；焦点、背景及边框仍遵守共享组件规则。

## Do's and Don'ts

每屏提供可操作空态与持久错误。密钥遮罩；不可将 API Key 放入客户端存储。未运行的任务不显示成功，未测量的生成过程不展示百分比。

同步生图显示实际请求阶段与已等待时长，超过两分钟提示中转站响应较慢；不能推断平台正在生成。断线或超时标为“结果未知”，并自动重新生成一次（每张最多一次）。内容审核拒绝的图片自动原样重发、再改写措辞重发；改写中状态标签为“改写提示词”，卡片线路说明写明是哪一步，被自动重发的原图只留一行灰字，最终仍被拒才用警告说明。任务详情持续刷新，并展示可用于核对平台记录的请求标识和诊断；旧任务缺少的诊断不得补造。

下载阶段单独说明正在取回已返回的图片链接；临时错误只重试图片 GET，不重发生图。智能生图卡片与任务详情使用共享 TaskFailureAlert，直接区分下载失败和生成结果未知，详细错误通过键盘可操作的折叠区查看。下载失败先自动补下载三次（30 秒、2 分钟、5 分钟），等待时只显示倒计时；仍失败时套图可一键补下载全部失败图片，无需计费确认；重新生成仍沿用费用确认。

最近生成按一次提交的批次归组，展示已完成任务数和可下载图片数。单图下载与套图 ZIP 下载使用相同保存路径选择、取消和错误反馈。预览上限不限制套图导出范围；部分完成时明确标注“下载已完成图片”。

0.2.0 智能生图沿用上述组件与色彩：需求和逐张方案双栏，窄窗口改为单栏；编号只表示套图顺序。已提交方案作为可读的记录保留，禁用控件通过背景、边框和不可操作状态区分，文字仍使用 text 色，保证方案内容可读。空态与说明文字统一使用 muted 色。

0.2.1：所有 LLM 配置与智能生图选择器统一称为“LLM 供应商”，生图连接仍称“生图中转站”。沿用共享组件和布局。

0.2.2 日志页沿用共享 Header、Ant Design Table、Select、Input.Search、Switch 和 Modal。桌面筛选横排、窄窗口换行，表格在面板内横向滚动；级别用文字和语义色，详情支持长错误换行。

0.3.0 智能生图采用制作需求与制作进度/结果双栏，隐藏商品分析、规则、Skills执行与方案编辑。张数和比例常显，供应商等参数折叠在“更多设置”；缺失资料用集中简短表单，右侧优先呈现图片和下载。沿用共享组件与语义色，窄窗口改单栏，所有反馈使用真实状态和时间。

0.7.18 概念台：全应用唯一的深色面板，像展墙一样陈列概念卡；工作台其余部分保持浅色。设计方向（经 ui-ux-pro-max 技能核对：Minimalism & Swiss Style，适合专业工具）：靠克制显得高级——细边框、网格对齐、清楚的字号层级，不用渐变、装饰阴影和外部字体，全页只有 primary 一个强调色。

- 颜色：墨色变量只用于概念台。`ink` 为底（与 text 同值），`inkRaised` 用于悬停底色，`inkSelected` 为选中卡片底，`inkLine` 为卡片和分隔细线，`inkText` 主文字、`inkMuted` 次文字（在 ink 上约 7.5:1），`inkAccent` 是 primary 的浅色版，只用于墨色上的选中边框、焦点环、“×”和立场序号；ink 上的焦点环改用 inkAccent（primary 在墨色上对比不足 3:1）。主按钮仍是 primary 实底白字。
- 字体：只用系统字体。概念名 16px、字重 300、字距 0.06em，配 11px 英文副标；底部“形体 × 立场”标题 28px、字重 300、字距 0.08em；分组名 11px、大字距。
- 卡片：1px inkLine 边框、8px 圆角、无填充；线稿 48×48 viewBox、currentColor、1.5 线宽、无填充。选中 = inkAccent 边框 + inkSelected 底，并用 `aria-pressed` 表达，不只靠颜色；多立场时右上角显示选择顺序。
- 动效：卡片状态 150ms 颜色过渡；底部画面描述在选择变化时 150ms 淡入；遵守减少动画（全局规则关闭动画）。
- 工作台入口：提示词下方一行细边框按钮，左侧是 36px 墨色小方块（概念台的唯一“预告”），已选时显示“形体 × 立场”与“撤销改写 / 更换 / 清除”。最近生成的概念标签为细边框小标签，与状态同行。
