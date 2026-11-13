import { useEffect } from "react";
import { NavLink, Routes, Route, useLocation } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { App as AntApp, Empty, Tooltip, Progress } from "antd";
import {
  PictureOutlined,
  AppstoreOutlined,
  ClockCircleOutlined,
  SettingOutlined,
  FileTextOutlined,
  ApiOutlined,
  CheckCircleOutlined,
  LoadingOutlined,
  DashboardOutlined,
  RobotOutlined,
} from "@ant-design/icons";
import type { SmartProgress } from "./agent-api";
import { call, type Summary } from "./api";
import { useData, coalesce } from "./ui";
import { TemplatesPage } from "./Templates";
import { SmartImages } from "./SmartImages";
import { runStepNames } from "./RunSteps";
import { LicenseBadge } from "./License";
import { UpdateBadge } from "./Updates";
import { Logs } from "./Logs";
import { GatewayMonitorPage, LLMMonitorPage } from "./Monitor";
import { Workbench } from "./pages/Workbench";
import { Tasks } from "./pages/Tasks";
import { Assets } from "./pages/Assets";
import { Settings } from "./pages/Settings";

// Shows the newest smart-generation run while it is unfinished: a ring filled by
// finished images, a spinner while analyzing, a dot while waiting for input.
function SmartProgressRing() {
  const q = useData<SmartProgress>(
    ["smart-progress"],
    "CurrentSmartProgress",
    [],
    3000,
  );
  const p = q.data;
  if (!p?.runID) return null;
  const label =
    p.state === "analyzing"
      ? `智能生图正在分析${p.step ? "：" + runStepNames[p.step] : ""}`
      : p.state === "needs_input"
        ? "智能生图等待补充资料"
        : p.state === "paused"
          ? `智能生图已暂停，已完成 ${p.done} / ${p.total} 张`
          : `智能生图生成中，已完成 ${p.done} / ${p.total} 张`;
  // The link keeps its plain name; this text is its description (aria-describedby).
  return (
    <Tooltip title={label}>
      <span className="nav-progress" id="smart-progress-status">
        <span className="sr-only">{label}</span>
        <span className="nav-progress-visual" aria-hidden="true">
          {p.state === "analyzing" ? (
            <LoadingOutlined spin />
          ) : p.state === "needs_input" ? (
            <span className="nav-progress-dot" />
          ) : (
            <Progress
              type="circle"
              size={18}
              strokeWidth={14}
              showInfo={false}
              percent={p.total ? Math.round((p.done / p.total) * 100) : 0}
              status={p.state === "paused" ? "exception" : "active"}
            />
          )}
        </span>
      </span>
    </Tooltip>
  );
}
export default function App() {
  const loc = useLocation();
  const qc = useQueryClient();
  const { modal, notification } = AntApp.useApp();
  const summary = useData<Summary>(["summary"], "Dashboard", [], 4000);
  const links = [
    ["/", "生图工作台", <PictureOutlined aria-hidden="true" />],
    ["/smart", "智能生图", <ApiOutlined aria-hidden="true" />],
    ["/tasks", "任务中心", <ClockCircleOutlined aria-hidden="true" />],
    ["/assets", "素材库", <AppstoreOutlined aria-hidden="true" />],
    [
      "/monitor/gateways",
      "中转站监控",
      <DashboardOutlined aria-hidden="true" />,
    ],
    ["/monitor/llm", "LLM 监控", <RobotOutlined aria-hidden="true" />],
    ["/templates", "提示词模板", <FileTextOutlined aria-hidden="true" />],
    ["/settings", "连接与设置", <SettingOutlined aria-hidden="true" />],
    ["/logs", "日志", <FileTextOutlined aria-hidden="true" />],
  ] as const;
  useEffect(() => {
    window.scrollTo(0, 0);
  }, [loc.pathname]);
  useEffect(() => {
    document.title = `${links.find((l) => l[0] === loc.pathname)?.[1] || "生图工作台"} · ImageWorkbench`;
  }, [loc.pathname]);
  useEffect(() => {
    const refresh = coalesce(() => {
      qc.invalidateQueries({ queryKey: ["tasks"] });
      qc.invalidateQueries({ queryKey: ["assets"] });
      qc.invalidateQueries({ queryKey: ["summary"] });
    });
    const off = window.runtime?.EventsOn("tasks:changed", refresh);
    const close = window.runtime?.EventsOn("app:close-requested", () =>
      modal.confirm({
        title: "退出并暂停本地执行？",
        content:
          "未提交任务会暂停。平台已收到的任务可能继续生成或扣费，重启后请核对结果。最小化窗口可继续执行。",
        okText: "退出应用",
        cancelText: "继续运行",
        autoFocusButton: "cancel",
        onOk: () => call("ForceQuit"),
      }),
    );
    const err = window.runtime?.EventsOn("files:error", (message: string) =>
      notification.error({ message: "参考图未导入", description: message }),
    );
    return () => {
      off?.();
      close?.();
      err?.();
    };
  }, []);
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-mark">
            <PictureOutlined aria-hidden="true" />
          </div>
          <div>
            生图工作台<small>IMAGE WORKBENCH</small>
          </div>
        </div>
        <nav aria-label="主导航">
          {links.map(([path, label, icon]) => (
            <NavLink
              key={path}
              to={path}
              end
              aria-label={path === "/smart" ? label : undefined}
              aria-describedby={
                path === "/smart" ? "smart-progress-status" : undefined
              }
            >
              {icon}
              <span>{label}</span>
              {path === "/tasks" && !!summary.data?.active && (
                <span className="count">{summary.data.active}</span>
              )}
              {path === "/smart" && <SmartProgressRing />}
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <LicenseBadge />
          <UpdateBadge />
          <div className="local-status">
            <CheckCircleOutlined aria-hidden="true" /> 本地数据工作区
          </div>
          <small>Portable · {summary.data?.version || "0.7.18"}</small>
        </div>
      </aside>
      <main className="main">
        <div className="topbar">
          <span>
            图片制作 / {links.find((l) => l[0] === loc.pathname)?.[1]}
          </span>
          <span className="privacy-note">图片与任务保存在本机</span>
        </div>
        <div className="page">
          <Routes>
            <Route path="/" element={<Workbench />} />
            <Route path="/smart" element={<SmartImages />} />
            <Route path="/tasks" element={<Tasks />} />
            <Route path="/assets" element={<Assets />} />
            <Route path="/monitor/gateways" element={<GatewayMonitorPage />} />
            <Route path="/monitor/llm" element={<LLMMonitorPage />} />
            <Route path="/templates" element={<TemplatesPage />} />
            <Route path="/settings" element={<Settings />} />
            <Route path="/logs" element={<Logs />} />
            <Route
              path="*"
              element={
                <Empty description="页面不存在">
                  <NavLink to="/">返回工作台</NavLink>
                </Empty>
              }
            />
          </Routes>
        </div>
      </main>
    </div>
  );
}
