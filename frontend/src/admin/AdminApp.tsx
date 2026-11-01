import { NavLink, Route, Routes, useLocation } from "react-router-dom";
import {
  KeyOutlined,
  SettingOutlined,
  TeamOutlined,
  SafetyCertificateOutlined,
} from "@ant-design/icons";
import { useEffect } from "react";
import { useData } from "../ui";
import type { AdminState } from "./types";
import { CustomerDetail, Customers } from "./Customers";
import { KeyPool } from "./Keys";
import { AdminSettings } from "./AdminSettings";

export function useAdminState() {
  return useData<AdminState>(["admin-state"], "State");
}

const links = [
  ["/", "客户", <TeamOutlined aria-hidden="true" />],
  ["/keys", "Key 池", <KeyOutlined aria-hidden="true" />],
  ["/settings", "设置与签名密钥", <SettingOutlined aria-hidden="true" />],
] as const;

export default function AdminApp() {
  const loc = useLocation();
  const state = useAdminState();
  const version = useData<string>(["admin-version"], "ToolVersion");
  useEffect(() => {
    document.title = "ImageWorkbench 管理工具";
  }, []);
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-mark">
            <SafetyCertificateOutlined aria-hidden="true" />
          </div>
          <div>
            管理工具<small>IMAGEWORKBENCH ADMIN</small>
          </div>
        </div>
        <nav aria-label="主导航">
          {links.map(([path, label, icon]) => (
            <NavLink key={path} to={path} end={path === "/"}>
              {icon}
              <span>{label}</span>
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <small>
            {state.data?.customers.length ?? 0} 位客户 ·{" "}
            {state.data?.keys.filter((k) => k.status === "available").length ??
              0}{" "}
            个可用 Key
            <br />
            管理工具 {version.data}
          </small>
        </div>
      </aside>
      <main className="main">
        <div className="topbar">
          <span>
            管理工具 /{" "}
            {loc.pathname.startsWith("/customer")
              ? "客户详情"
              : links.find((l) => l[0] === loc.pathname)?.[1]}
          </span>
          <span className="privacy-note">
            数据只保存在本机，Key 使用 Windows 加密
          </span>
        </div>
        <div className="page">
          <Routes>
            <Route path="/" element={<Customers />} />
            <Route path="/customer/:id" element={<CustomerDetail />} />
            <Route path="/keys" element={<KeyPool />} />
            <Route path="/settings" element={<AdminSettings />} />
          </Routes>
        </div>
      </main>
    </div>
  );
}
