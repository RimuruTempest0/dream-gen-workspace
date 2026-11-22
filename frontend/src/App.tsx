import { useEffect, useState, type ReactNode } from "react";
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
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  SlidersOutlined,
  StarOutlined,
} from "@ant-design/icons";
import type { SmartProgress } from "./agent-api";
import { call, type Summary } from "./api";
import { useData, coalesce } from "./ui";
import { TemplatesPage } from "./Templates";
import { SmartImages } from "./SmartImages";
import { runStepName } from "./RunSteps";
import { LicenseBadge } from "./License";
import { UpdateBadge } from "./Updates";
import { Logs } from "./Logs";
import { GatewayMonitorPage, LLMMonitorPage } from "./Monitor";
import { Workbench } from "./pages/Workbench";
import { Tasks } from "./pages/Tasks";
import { Assets } from "./pages/Assets";
import { Settings } from "./pages/Settings";
import { appName, t, useI18n, type MessageKey } from "./i18n";
import { RailLanguageSwitch } from "./i18n/LanguageSwitch";

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
      ? p.step
        ? t("shell.progress.analyzingStep", { step: runStepName(p.step) })
        : t("shell.progress.analyzing")
      : p.state === "needs_input"
        ? t("shell.progress.needsInput")
        : t(
            p.state === "paused"
              ? "shell.progress.paused"
              : "shell.progress.generating",
            { done: p.done, total: p.total },
          );
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
// Labels are text keys: t() runs at render, in the current language.
const systemTabs: readonly (readonly [string, MessageKey])[] = [
  ["/settings", "shell.system.connections"],
  ["/monitor/gateways", "shell.system.gateways"],
  ["/monitor/llm", "shell.system.llm"],
  ["/logs", "shell.system.logs"],
];
function SystemTabs() {
  return (
    <nav className="system-tabs" aria-label={t("shell.nav.system")}>
      {systemTabs.map(([path, label]) => (
        <NavLink key={path} to={path} end>
          {t(label)}
        </NavLink>
      ))}
    </nav>
  );
}
// Three groups: make, keep, run. A null entry draws the divider between groups.
type RailLink = readonly [string, MessageKey, ReactNode];
const links: readonly (RailLink | null)[] = [
  ["/", "shell.nav.create", <StarOutlined aria-hidden="true" />],
  ["/pro", "shell.nav.pro", <SlidersOutlined aria-hidden="true" />],
  [
    "/templates",
    "shell.nav.templates",
    <FileTextOutlined aria-hidden="true" />,
  ],
  null,
  ["/tasks", "shell.nav.tasks", <ClockCircleOutlined aria-hidden="true" />],
  ["/assets", "shell.nav.assets", <AppstoreOutlined aria-hidden="true" />],
  null,
  ["/settings", "shell.nav.system", <SettingOutlined aria-hidden="true" />],
];
function pageName(path: string) {
  const tab = systemTabs.find((s) => s[0] === path);
  if (tab) return t(tab[1]);
  return t(links.find((l) => l && l[0] === path)?.[1] || "shell.nav.create");
}
export default function App() {
  const loc = useLocation();
  const qc = useQueryClient();
  const { modal, notification } = AntApp.useApp();
  const summary = useData<Summary>(["summary"], "Dashboard", [], 4000);
  // The rail stays open; the user collapses it with the button beside the
  // wordmark, and the choice is kept for the next start.
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem("rail-collapsed") === "1";
    } catch {
      return false;
    }
  });
  const toggleRail = () =>
    setCollapsed((c) => {
      try {
        localStorage.setItem("rail-collapsed", c ? "0" : "1");
      } catch {}
      return !c;
    });
  const inSystem = systemTabs.some((s) => s[0] === loc.pathname);
  useEffect(() => {
    window.scrollTo(0, 0);
  }, [loc.pathname]);
  const { lang } = useI18n();
  useEffect(() => {
    document.title = `${pageName(loc.pathname)} · ${appName()}`;
  }, [loc.pathname, lang]);
  useEffect(() => {
    const refresh = coalesce(() => {
      qc.invalidateQueries({ queryKey: ["tasks"] });
      qc.invalidateQueries({ queryKey: ["assets"] });
      qc.invalidateQueries({ queryKey: ["summary"] });
    });
    const off = window.runtime?.EventsOn("tasks:changed", refresh);
    const close = window.runtime?.EventsOn("app:close-requested", () =>
      modal.confirm({
        title: t("shell.close.title"),
        content: t("shell.close.content"),
        okText: t("shell.close.ok"),
        cancelText: t("shell.close.cancel"),
        autoFocusButton: "cancel",
        onOk: () => call("ForceQuit"),
      }),
    );
    const err = window.runtime?.EventsOn("files:error", (message: string) =>
      notification.error({
        message: t("shell.drop.failed"),
        description: message,
      }),
    );
    return () => {
      off?.();
      close?.();
      err?.();
    };
  }, []);
  return (
    <div className={collapsed ? "shell is-collapsed" : "shell"}>
      <aside className="rail">
        <div className="rail-brand">
          <svg className="rail-mark" viewBox="0 0 28 28" aria-hidden="true">
            <circle cx="14" cy="14" r="12.5" />
            <path d="M14 1.5a12.5 12.5 0 0 1 0 25c4-3.5 6-7.7 6-12.5s-2-9-6-12.5z" />
          </svg>
          <span className="rail-wordmark">{appName()}</span>
          <Tooltip
            title={t(collapsed ? "shell.rail.expand" : "shell.rail.collapse")}
            placement="right"
          >
            <button
              type="button"
              className="rail-toggle"
              aria-label={t(
                collapsed ? "shell.rail.expand" : "shell.rail.collapse",
              )}
              aria-expanded={!collapsed}
              onClick={toggleRail}
            >
              {collapsed ? (
                <MenuUnfoldOutlined aria-hidden="true" />
              ) : (
                <MenuFoldOutlined aria-hidden="true" />
              )}
            </button>
          </Tooltip>
        </div>
        <nav aria-label={t("shell.nav.label")}>
          {links.map((l, i) =>
            l ? (
              <Tooltip
                key={l[0]}
                title={collapsed ? t(l[1]) : undefined}
                placement="right"
              >
                <NavLink
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
                  <span className="rail-label">{t(l[1])}</span>
                  {l[0] === "/tasks" && !!summary.data?.active && (
                    <span className="count">{summary.data.active}</span>
                  )}
                </NavLink>
              </Tooltip>
            ) : (
              <div key={i} className="rail-gap" />
            ),
          )}
        </nav>
        <div className="rail-foot">
          <SmartProgressRing />
          <LicenseBadge tooltip={collapsed} />
          <UpdateBadge tooltip={collapsed} />
          <RailLanguageSwitch tooltip={collapsed} />
          <small className="rail-version">
            {t("shell.rail.version", {
              version: summary.data?.version || "0.8.2",
            })}
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
                <Empty description={t("shell.notFound")}>
                  <NavLink to="/">{t("shell.notFound.back")}</NavLink>
                </Empty>
              }
            />
          </Routes>
        </div>
      </main>
    </div>
  );
}
