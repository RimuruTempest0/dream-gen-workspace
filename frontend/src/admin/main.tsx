import React from "react";
import ReactDOM from "react-dom/client";
import { ConfigProvider, App as AntApp } from "antd";
import zhCN from "antd/locale/zh_CN";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { HashRouter } from "react-router-dom";
import { antdTheme, installTokens } from "../tokens";
import AdminApp from "./AdminApp";
import "../styles.css";
import "./admin.css";
installTokens();
const client = new QueryClient({
  defaultOptions: { queries: { retry: false, refetchOnWindowFocus: true } },
});
ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={client}>
      <ConfigProvider
        button={{ autoInsertSpace: false }}
        locale={zhCN}
        theme={antdTheme}
      >
        <AntApp>
          <HashRouter>
            <AdminApp />
          </HashRouter>
        </AntApp>
      </ConfigProvider>
    </QueryClientProvider>
  </React.StrictMode>,
);
