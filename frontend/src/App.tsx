import { useEffect, useState } from "react";
import {
  NavLink,
  Navigate,
  Routes,
  Route,
  useLocation,
} from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { App as AntApp, Empty, Tooltip, Progress } from "antd";
import {
  AppstoreOutlined,
  ClockCircleOutlined,
  SettingOutlined,
  FileTextOutlined,
  LoadingOutlined,
  SlidersOutlined,
  StarOutlined,
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
      ? `正在分析${p.step ? "：" + runStepNames[p.step] : ""}`
      : p.state === "needs_input"
        ? "等待补充资料"
        : p.state === "paused"
          ? `已暂停，已完成 ${p.done} / ${p.total} 张`
          : `生成中，已完成 ${p.done} / ${p.total} 张`;
  // The link keeps its plain name; this text is its description (aria-describedby).
  return (
    <Tooltip title={label} placement="right">
      <NavLink to="/" className="rail-item rail-progress">
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
        <span className="rail-label" id="smart-progress-status">
          {label}
        </span>
      </NavLink>
    </Tooltip>
  );
}
// The four system pages share one entry in the rail and this row of tabs.
const systemTabs = [
  ["/settings", "连接"],
  ["/monitor/gateways", "服务状态"],
  ["/monitor/llm", "模型状态"],
  ["/logs", "日志"],
] as const;
function SystemTabs() {
  return (
    <nav className="system-tabs" aria-label="系统">
      {systemTabs.map(([path, label]) => (
        <NavLink key={path} to={path} end>
          {label}
        </NavLink>
      ))}
    </nav>
  );
}
// Three groups: make, keep, run. A null entry draws the divider between groups.
const links = [
  ["/", "创作", <StarOutlined aria-hidden="true" />],
  ["/pro", "专业模式", <SlidersOutlined aria-hidden="true" />],
  ["/templates", "模板", <FileTextOutlined aria-hidden="true" />],
  null,
  ["/tasks", "生成记录", <ClockCircleOutlined aria-hidden="true" />],
  ["/assets", "作品与素材", <AppstoreOutlined aria-hidden="true" />],
  null,
  ["/settings", "系统", <SettingOutlined aria-hidden="true" />],
] as const;
function pageName(path: string) {
  const tab = systemTabs.find((t) => t[0] === path);
  if (tab) return tab[1];
  return links.find((l) => l && l[0] === path)?.[1] || "创作";
}
export default function App() {
  const loc = useLocation();
  const qc = useQueryClient();
  const { modal, notification } = AntApp.useApp();
  const summary = useData<Summary>(["summary"], "Dashboard", [], 4000);
  const [railClosed, setRailClosed] = useState(false);
  const inSystem = systemTabs.some((t) => t[0] === loc.pathname);
  useEffect(() => {
    window.scrollTo(0, 0);
  }, [loc.pathname]);
  useEffect(() => {
    document.title = `${pageName(loc.pathname)} · ImageWorkbench`;
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
    <div className="shell">
      <aside
        className={railClosed ? "rail rail-closed" : "rail"}
        onClick={() => setRailClosed(true)}
        onMouseLeave={() => setRailClosed(false)}
      >
        <div className="rail-brand">
          <svg className="rail-mark" viewBox="0 0 28 28" aria-hidden="true">
            <circle cx="14" cy="14" r="12.5" />
            <path d="M14 1.5a12.5 12.5 0 0 1 0 25c4-3.5 6-7.7 6-12.5s-2-9-6-12.5z" />
          </svg>
          <span className="rail-wordmark">生图工作台</span>
        </div>
        <nav aria-label="主导航">
          {links.map((l, i) =>
            l ? (
              <NavLink
                key={l[0]}
                to={l[0]}
                end
                className={({ isActive }) =>
                  "rail-item" +
                  (isActive || (l[0] === "/settings" && inSystem)
                    ? " active"
                    : "")
                }
                aria-describedby={
                  l[0] === "/" ? "smart-progress-status" : undefined
                }
              >
                {l[2]}
                <span className="rail-label">{l[1]}</span>
                {l[0] === "/tasks" && !!summary.data?.active && (
                  <span className="count">{summary.data.active}</span>
                )}
              </NavLink>
            ) : (
              <div key={i} className="rail-gap" />
            ),
          )}
        </nav>
        <div className="rail-foot">
          <SmartProgressRing />
          <LicenseBadge />
          <UpdateBadge />
          <small className="rail-version">
            版本 {summary.data?.version || "0.7.38"}，数据保存在本机
          </small>
        </div>
      </aside>
      <main className="shell-main">
        <div className="page">
          {inSystem && <SystemTabs />}
          <Routes>
            <Route path="/" element={<SmartImages />} />
            <Route path="/smart" element={<Navigate to="/" replace />} />
            <Route path="/pro" element={<Workbench />} />
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
                  <NavLink to="/">返回创作</NavLink>
                </Empty>
              }
            />
          </Routes>
        </div>
      </main>
    </div>
  );
}
