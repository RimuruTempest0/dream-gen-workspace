import { useState } from "react";
import {
  App as AntApp,
  Button,
  Input,
  Modal,
  Select as UISelect,
  Space,
  Switch,
  Table,
  Tag,
} from "antd";
import { DownloadOutlined, ReloadOutlined } from "@ant-design/icons";
import { call } from "./api";
import { DataState, Header, time, useActions, useData } from "./ui";
import { t, type MessageKey } from "./i18n";
import { fill } from "./i18n/fill";

type LogEntry = {
  id: number;
  time: string;
  level: string;
  source: string;
  relatedID: string;
  message: string;
};
type LogPage = { items: LogEntry[]; total: number };
const levels: Record<string, MessageKey> = {
  info: "logs.level.info",
  warn: "logs.level.warn",
  error: "logs.level.error",
};
const sources: Record<string, MessageKey> = {
  app: "logs.source.app",
  connection: "logs.source.connection",
  agent: "logs.source.agent",
  image: "logs.source.image",
};
const levelName = (v: string) => (levels[v] ? t(levels[v]) : v);
const sourceName = (v: string) => (sources[v] ? t(sources[v]) : v);

export function Logs() {
  const [level, setLevel] = useState("");
  const [source, setSource] = useState("");
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [auto, setAuto] = useState(true);
  const [detail, setDetail] = useState<LogEntry | null>(null);
  const actions = useActions();
  const { notification } = AntApp.useApp();
  async function exportDiagnostics() {
    const path = await actions.run(async () => {
      const path = await call<string>("ChooseDiagnosticsPath");
      if (path) await call("ExportDiagnosticsToPath", path);
      return path;
    });
    if (path)
      notification.success({
        message: t("logs.exported"),
        description: t("logs.exported.body"),
      });
  }
  const q = useData<LogPage>(
    ["logs", level, source, keyword, page],
    "ListLogs",
    [level, source, keyword, (page - 1) * 50, 50],
    auto ? 3000 : undefined,
  );
  return (
    <>
      <Header
        title={t("logs.title")}
        sub={t("logs.sub")}
        action={
          <Space wrap>
            <Button
              icon={<DownloadOutlined aria-hidden="true" />}
              loading={actions.busy}
              onClick={() => void exportDiagnostics()}
            >
              {t("logs.export")}
            </Button>
            <Button
              icon={<ReloadOutlined aria-hidden="true" />}
              loading={q.isFetching}
              onClick={() => void q.refetch()}
            >
              {t("logs.refresh")}
            </Button>
          </Space>
        }
      />
      <section className="panel" aria-label={t("logs.records")}>
        <div className="log-filters">
          <div className="log-filter">
            <label htmlFor="log-level">{t("logs.filter.level")}</label>
            <UISelect
              id="log-level"
              value={level}
              onChange={(v) => {
                setLevel(v);
                setPage(1);
              }}
              options={[
                { value: "", label: t("logs.filter.allLevels") },
                ...Object.keys(levels).map((value) => ({
                  value,
                  label: levelName(value),
                })),
              ]}
            />
          </div>
          <div className="log-filter">
            <label htmlFor="log-source">{t("logs.filter.source")}</label>
            <UISelect
              id="log-source"
              value={source}
              onChange={(v) => {
                setSource(v);
                setPage(1);
              }}
              options={[
                { value: "", label: t("logs.filter.allSources") },
                ...Object.keys(sources).map((value) => ({
                  value,
                  label: sourceName(value),
                })),
              ]}
            />
          </div>
          <div className="log-filter log-search">
            <label htmlFor="log-keyword">{t("logs.filter.keyword")}</label>
            <Input.Search
              id="log-keyword"
              placeholder={t("logs.filter.keyword.placeholder")}
              maxLength={200}
              allowClear
              enterButton={t("logs.filter.search")}
              onSearch={(v) => {
                setKeyword(v.trim());
                setPage(1);
              }}
            />
          </div>
          <Space>
            <Switch id="log-auto" checked={auto} onChange={setAuto} />
            <label htmlFor="log-auto">{t("logs.auto")}</label>
          </Space>
        </div>
        <p className="hint">{t("logs.privacy")}</p>
        <div role="status" aria-live="polite" className="hint">
          {q.data
            ? t("logs.total", { count: q.data.total })
            : t("logs.loading")}
        </div>
        <DataState query={q}>
          <Table<LogEntry>
            rowKey="id"
            dataSource={q.data?.items || []}
            scroll={{ x: 760 }}
            locale={{
              emptyText: t("logs.empty"),
            }}
            pagination={{
              current: page,
              pageSize: 50,
              total: q.data?.total || 0,
              showSizeChanger: false,
              onChange: setPage,
              hideOnSinglePage: true,
            }}
            columns={[
              {
                title: t("logs.col.time"),
                dataIndex: "time",
                width: 170,
                render: (v: string) => time(v),
              },
              {
                title: t("logs.col.level"),
                dataIndex: "level",
                width: 80,
                render: (v: string) => (
                  <Tag
                    color={
                      v === "error"
                        ? "error"
                        : v === "warn"
                          ? "warning"
                          : "default"
                    }
                  >
                    {levelName(v)}
                  </Tag>
                ),
              },
              {
                title: t("logs.col.source"),
                dataIndex: "source",
                width: 110,
                render: (v: string) => sourceName(v),
              },
              {
                title: t("logs.col.message"),
                dataIndex: "message",
                render: (v: string) => (
                  <span className="log-message">
                    {Array.from(v).slice(0, 140).join("")}
                    {Array.from(v).length > 140 ? "…" : ""}
                  </span>
                ),
              },
              {
                title: t("logs.col.actions"),
                width: 95,
                render: (_, entry) => (
                  <Button
                    size="small"
                    aria-label={t("logs.detail.aria", { id: String(entry.id) })}
                    onClick={() => setDetail(entry)}
                  >
                    {t("logs.detail.open")}
                  </Button>
                ),
              },
            ]}
          />
        </DataState>
      </section>
      <Modal
        title={t("logs.detail.title")}
        open={!!detail}
        onCancel={() => setDetail(null)}
        footer={
          <Button onClick={() => setDetail(null)}>{t("logs.detail.close")}</Button>
        }
      >
        {detail && (
          <div className="log-detail">
            <p>{t("logs.detail.time", { time: time(detail.time) })}</p>
            <p>
              {t("logs.detail.levelSource", {
                level: levelName(detail.level),
                source: sourceName(detail.source),
              })}
            </p>
            {detail.relatedID && (
              <p>
                {fill(t("logs.detail.related"), {
                  id: <span className="mono">{detail.relatedID}</span>,
                })}
              </p>
            )}
            <pre className="log-message">{detail.message}</pre>
          </div>
        )}
      </Modal>
    </>
  );
}
