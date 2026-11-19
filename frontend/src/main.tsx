import React, { useEffect, useMemo, useRef } from "react";
import ReactDOM from "react-dom/client";
import { ConfigProvider, App as AntApp } from "antd";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { HashRouter } from "react-router-dom";
import { installTokens, useAntdTheme } from "./tokens";
import { LanguageProvider, loadLanguage, type LanguageInfo } from "./i18n";
import App from "./App";
import { notImages } from "./ui";
import "./styles.css";
installTokens();
const client = new QueryClient({
  defaultOptions: { queries: { retry: false, refetchOnWindowFocus: true } },
});
function Themed({ info }: { info: LanguageInfo }) {
  const base = useAntdTheme();
  // Text the backend wrote in the interface language (summaries, the licence
  // state) is fetched again in the new one; saved images are not.
  const first = useRef(true);
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    void client.invalidateQueries(notImages);
  }, [info.code]);
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
          {/* Not keyed on the language: a switch re-renders the page in the new
              language and keeps what is on it (typed text, open dialogs). */}
          <App />
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
