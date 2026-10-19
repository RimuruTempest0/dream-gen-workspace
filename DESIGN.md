---
version: alpha
colors:
  primary: "#245BDB"
  surface: "#FFFFFF"
  background: "#F5F6F8"
  text: "#182230"
  success: "#18794E"
  danger: "#B42318"
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

同步生图显示实际请求阶段与已等待时长，超过两分钟提示中转站响应较慢；不能推断平台正在生成。断线或超时保留“结果未知”，不自动重试。任务详情持续刷新，并展示可用于核对平台记录的请求标识和诊断；旧任务缺少的诊断不得补造。

下载阶段单独说明正在取回已返回的图片链接；临时错误只重试图片 GET，不重发生图。智能生图卡片与任务详情使用共享 TaskFailureAlert，直接区分下载失败和生成结果未知，详细错误通过键盘可操作的折叠区查看。套图可一键补下载全部失败图片，无需计费确认；重新生成仍沿用费用确认。

最近生成按一次提交的批次归组，展示已完成任务数和可下载图片数。单图下载与套图 ZIP 下载使用相同保存路径选择、取消和错误反馈。预览上限不限制套图导出范围；部分完成时明确标注“下载已完成图片”。

0.2.0 智能生图沿用上述组件与色彩：需求和逐张方案双栏，窄窗口改为单栏；编号只表示套图顺序。已提交方案作为可读的记录保留，禁用控件通过背景、边框和不可操作状态区分，文字仍使用 text 色，保证方案内容可读。空态与说明文字统一使用 muted 色。

0.2.1：所有 LLM 配置与智能生图选择器统一称为“LLM 供应商”，生图连接仍称“生图中转站”。沿用共享组件和布局。

0.2.2 日志页沿用共享 Header、Ant Design Table、Select、Input.Search、Switch 和 Modal。桌面筛选横排、窄窗口换行，表格在面板内横向滚动；级别用文字和语义色，详情支持长错误换行。

0.3.0 智能生图采用制作需求与制作进度/结果双栏，隐藏商品分析、规则、Skills执行与方案编辑。张数和比例常显，供应商等参数折叠在“更多设置”；缺失资料用集中简短表单，右侧优先呈现图片和下载。沿用共享组件与语义色，窄窗口改单栏，所有反馈使用真实状态和时间。
