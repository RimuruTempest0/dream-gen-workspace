import React, { useMemo } from "react";
import ReactDOM from "react-dom/client";
import { ConfigProvider, App as AntApp } from "antd";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { HashRouter } from "react-router-dom";
import { installTokens, useAntdTheme } from "./tokens";
import { LanguageProvider, loadLanguage, type LanguageInfo } from "./i18n";
import App from "./App";
import "./styles.css";
installTokens();
const client = new QueryClient({
  defaultOptions: { queries: { retry: false, refetchOnWindowFocus: true } },
});
function Themed({ info }: { info: LanguageInfo }) {
  const base = useAntdTheme();
  const theme = useMemo(
    () => ({ ...base, token: { ...base.token, fontFamily: info.font } }),
    [base, info],
  );
  return (
    <ConfigProvider
      button={{ autoInsertSpace: false }}
      locale={info.antd}
      theme={theme}
    >
      <AntApp>
        <HashRouter>
          {/* Keyed on the language: the whole page re-renders in it at once. */}
          <App key={info.code} />
        </HashRouter>
      </AntApp>
    </ConfigProvider>
  );
}
loadLanguage().then((initial) =>
  ReactDOM.createRoot(document.getElementById("root")!).render(
    <React.StrictMode>
      <QueryClientProvider client={client}>
        <LanguageProvider initial={initial}>
          {(_, info) => <Themed info={info} />}
        </LanguageProvider>
      </QueryClientProvider>
    </React.StrictMode>,
  ),
);
