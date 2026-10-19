import React from "react";
import ReactDOM from "react-dom/client";
import { ConfigProvider, App as AntApp } from "antd";
import zhCN from "antd/locale/zh_CN";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { HashRouter } from "react-router-dom";
import { colors, font, installTokens } from "./tokens";
import App from "./App";
import "./styles.css";
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
        theme={{
          token: {
            colorPrimary: colors.primary,
            colorPrimaryHover: colors.primary,
            colorLinkHover: colors.primary,
            colorSuccess: colors.success,
            colorSuccessBg: colors.successBg,
            colorSuccessBorder: colors.border,
            colorError: colors.danger,
            colorErrorBg: colors.dangerBg,
            colorErrorBorder: colors.border,
            colorWarning: colors.warning,
            colorWarningBg: colors.warningBg,
            colorInfo: colors.primary,
            colorInfoBg: colors.soft,
            colorPrimaryBg: colors.soft,
            colorText: colors.text,
            colorTextDescription: colors.muted,
            colorTextSecondary: colors.muted,
            colorTextDisabled: colors.text,
            colorBgLayout: colors.background,
            fontFamily: font,
            fontSize: 14,
            borderRadius: 8,
            controlHeight: 38,
          },
        }}
      >
        <AntApp>
          <HashRouter>
            <App />
          </HashRouter>
        </AntApp>
      </ConfigProvider>
    </QueryClientProvider>
  </React.StrictMode>,
);
