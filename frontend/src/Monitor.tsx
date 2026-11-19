import { useState, type ReactNode } from "react";
import {
  App as AntApp,
  Button,
  Select as UISelect,
  Table,
  Tag,
  Tooltip,
} from "antd";
import { DownloadOutlined } from "@ant-design/icons";
import { call, type Connection } from "./api";
import {
  DataState,
  Header,
  hostedLineName,
  useActions,
  useData,
  useLineNames,
} from "./ui";
import {
  currentLanguage,
  formatNumber,
  languageInfo,
  t,
  type MessageKey,
} from "./i18n";

// Both monitors read the app's own records; nothing here calls a provider.
type Query = {
  days: number;
  connectionID: string;
  source: string;
  result: string;
  offset: number;
  limit: number;
};
type GatewayStat = {
  connectionID: string;
  name: string;
  role: "primary" | "backup" | "";
  backupOrder: number;
  deleted: boolean;
  state: "healthy" | "slow" | "down" | "unknown";
  checkedAt?: string;
  probeMS: number;
  requests: number;
  succeeded: number;
  unknown: number;
  failed: number;
  running: number;
  medianMS: number;
  p90MS: number;
  medianDownloadMS: number;
  routedIn: number;
  routedOut: number;
  autoRegenerated: number;
};
type GatewayCall = {
  taskID: string;
  at: string;
  connectionID: string;
  name: string;
  sourceLabel: string;
  model: string;
  result: "succeeded" | "unknown" | "failed" | "running";
  downloadFailed: boolean;
  failReason?: "balance" | "auth";
  httpStatus: number;
  generationMS: number;
  downloadMS: number;
  requestID: string;
  routedFromName?: string;
  autoRetry: boolean;
};
type GatewayMonitor = {
  total: number;
  succeeded: number;
  unknown: number;
  failed: number;
  failedAuth: number;
  failedBalance: number;
  running: number;
  rerouted: number;
  autoRegenerated: number;
  gateways: GatewayStat[];
  days: { day: string; counts: Record<string, number> }[];
  calls: GatewayCall[];
  callTotal: number;
};
type LLMCall = {
  id: string;
  at: string;
  kind: "analysis" | "test" | "rewrite" | "check";
  runID?: string;
  connectionName: string;
  model: string;
  stage: string;
  retry: boolean;
  firstByteMS: number;
  totalMS: number;
  inputTokens: number;
  outputTokens: number;
  status: "completed" | "dropped" | "error";
  httpStatus: number;
  error?: string;
  requestID?: string;
};
type LLMMonitor = {
  requests: number;
  analysisRuns: number;
  testRequests: number;
  inputTokens: number;
  outputTokens: number;
  dropped: number;
  recovered: number;
  errors: number;
  errorNote: string;
  medianRunMS: number;
  providers: {
    connectionID: string;
    name: string;
    model: string;
    deleted: boolean;
    capabilityOK: boolean;
    requests: number;
    analysisRuns: number;
    tests: number;
    completed: number;
    dropped: number;
    errors: number;
    medianFirstMS: number;
    medianTotalMS: number;
    maxTotalMS: number;
    inputTokens: number;
    outputTokens: number;
  }[];
  drops: {
    at: string;
    runName: string;
    stage: string;
    waitedMS: number;
    reason: string;
    detail: string;
    outcome: string;
    recovered: boolean;
  }[];
  runs: {
    runID: string;
    startedAt: string;
    name: string;
    connectionName: string;
    requests: number;
    inputTokens: number;
    outputTokens: number;
    dropped: number;
    errors: number;
    durationMS: number;
    result: string;
    resultTone: string;
  }[];
  calls: LLMCall[];
  callTotal: number;
  runNames: Record<string, string>;
};

const pageSize = 20;
const gatewayColors = ["#EFE8DA", "#E08A5B", "#8FA3BA", "#86B98F", "#B79AE8"];
const stateKeys: Record<GatewayStat["state"], MessageKey> = {
  healthy: "monitor.state.healthy",
  slow: "monitor.state.slow",
  down: "monitor.state.down",
  unknown: "monitor.state.unknown",
};
const stateTone = {
  healthy: "success",
  slow: "warning",
  down: "error",
  unknown: "default",
} as const;

export function duration(ms: number) {
  if (!ms) return "—";
  if (ms < 10_000)
    return t("monitor.seconds", {
      n: formatNumber(ms / 1000, {
        minimumFractionDigits: 1,
        maximumFractionDigits: 1,
      }),
    });
  const s = Math.round(ms / 1000);
  return s >= 60
    ? t("monitor.minutes", { m: Math.floor(s / 60), s: s % 60 })
    : t("monitor.seconds", { n: s });
}
export function tokens(n: number) {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 1000) return `${(n / 1000).toFixed(n >= 100_000 ? 0 : 1)}K`;
  return String(n);
}
// Month, day and time in the local zone. Chinese keeps the compact
// 11/19 08:05:09 form; other languages use their own order.
function clock(value: string, seconds = true) {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  if (currentLanguage() === "zh-CN") {
    const p = (n: number) => String(n).padStart(2, "0");
    const s = `${p(d.getMonth() + 1)}/${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
    return seconds ? `${s}:${p(d.getSeconds())}` : s;
  }
  return new Intl.DateTimeFormat(languageInfo(currentLanguage()).intl, {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    ...(seconds ? { second: "2-digit" } : {}),
  }).format(d);
}
const clockCell = (v: string) => clock(v);
function percent(part: number, whole: number) {
  return whole ? Math.round((part / whole) * 100) : 0;
}
const joinNotes = (parts: string[]) => parts.filter(Boolean).join(" · ");

function Stat({
  label,
  value,
  note,
  tone,
}: {
  label: string;
  value: ReactNode;
  note?: ReactNode;
  tone?: "success" | "warning" | "danger";
}) {
  return (
    <div className="monitor-stat">
      <span>{label}</span>
      <strong className={tone ? `tone-${tone}` : undefined}>{value}</strong>
      {note && <small>{note}</small>}
    </div>
  );
}

// A stacked bar of outcomes; the label carries the numbers for screen readers.
function OutcomeBar({
  parts,
  label,
}: {
  parts: [number, string][];
  label: string;
}) {
  const total = parts.reduce((s, [n]) => s + n, 0);
  return (
    <div className="outcome-bar" role="img" aria-label={label}>
      {total > 0 &&
        parts.map(([n, cls], i) =>
          n ? (
            <span
              key={i}
              className={cls}
              style={{ width: `${(n / total) * 100}%` }}
            />
          ) : null,
        )}
    </div>
  );
}

function Figure({
  label,
  value,
  tone,
}: {
  label: string;
  value: ReactNode;
  tone?: string;
}) {
  return (
    <div>
      {label}
      <strong className={tone ? `tone-${tone}` : undefined}>{value}</strong>
    </div>
  );
}

// Wide tables scroll sideways in a box keyboard users can focus and scroll.
function ScrollTable({ children }: { children: ReactNode }) {
  return (
    <div
      className="table-scroll"
      tabIndex={0}
      role="region"
      aria-label={t("monitor.tableScroll")}
    >
      {children}
    </div>
  );
}

function useMonitorQuery() {
  const [q, setQ] = useState<Query>({
    days: 7,
    connectionID: "",
    source: "",
    result: "",
    offset: 0,
    limit: pageSize,
  });
  const change = (p: Partial<Query>) =>
    setQ((v) => ({ ...v, offset: 0, ...p }));
  return { q, setQ, change };
}

function useCSVExport(method: string, prefix: string) {
  const actions = useActions();
  const { notification } = AntApp.useApp();
  return {
    busy: actions.busy,
    run: (q: Query) =>
      actions.run(async () => {
        const day = new Date().toISOString().slice(0, 10);
        const path = await call<string>(
          "ChooseExportPath",
          `${prefix}-${day}.csv`,
        );
        if (!path) return;
        const n = await call<number>(
          method,
          { ...q, offset: 0, limit: 0 },
          path,
        );
        notification.success({ message: t("monitor.exported", { count: n }) });
      }),
  };
}

const dayOptions = () => [
  { value: 1, label: t("monitor.days.today") },
  { value: 7, label: t("monitor.days.7") },
  { value: 30, label: t("monitor.days.30") },
  { value: 0, label: t("monitor.days.all") },
];

function FilterBar({
  q,
  change,
  connections,
  connectionLabel,
  allLabel,
  sourceLabel,
  sources,
  onExport,
  exporting,
}: {
  q: Query;
  change: (p: Partial<Query>) => void;
  connections: Connection[];
  connectionLabel: string;
  allLabel: string;
  sourceLabel: string;
  sources: { value: string; label: string }[];
  onExport: () => void;
  exporting: boolean;
}) {
  return (
    <div className="monitor-filters" role="search">
      <label>
        <span>{t("monitor.filter.days")}</span>
        <UISelect
          aria-label={t("monitor.filter.days")}
          value={q.days}
          style={{ width: 140 }}
          options={dayOptions()}
          onChange={(days) => change({ days })}
        />
      </label>
      <label>
        <span>{connectionLabel}</span>
        <UISelect
          aria-label={connectionLabel}
          value={q.connectionID}
          style={{ width: 200 }}
          options={[
            { value: "", label: allLabel },
            ...connections.map((c) => ({
              value: c.id,
              label: hostedLineName(c),
            })),
          ]}
          onChange={(connectionID) => change({ connectionID })}
        />
      </label>
      <label>
        <span>{sourceLabel}</span>
        <UISelect
          aria-label={sourceLabel}
          value={q.source}
          style={{ width: 160 }}
          options={sources}
          onChange={(source) => change({ source })}
        />
      </label>
      <span className="monitor-filters-gap" />
      <Button
        icon={<DownloadOutlined aria-hidden="true" />}
        loading={exporting}
        onClick={onExport}
      >
        {t("monitor.export")}
      </Button>
    </div>
  );
}

const gatewayResult = {
  succeeded: ["monitor.result.succeeded", "success"],
  unknown: ["monitor.result.unknown", "warning"],
  failed: ["monitor.result.failed", "danger"],
  running: ["monitor.result.running", ""],
} as const satisfies Record<string, readonly [MessageKey, string]>;

export function GatewayMonitorPage() {
  const { q, setQ, change } = useMonitorQuery();
  const connections = useData<Connection[]>(["connections"], "ListConnections");
  const lineNames = useLineNames();
  const data = useData<GatewayMonitor>(
    ["gateway-monitor", q],
    "GatewayMonitor",
    [q],
    5000,
  );
  const exporter = useCSVExport(
    "ExportGatewayCallsToPath",
    t("monitor.gw.exportName"),
  );
  const m = data.data;
  const colorOf = (id: string) =>
    gatewayColors[
      Math.max(0, m?.gateways.findIndex((g) => g.connectionID === id) ?? 0) %
        gatewayColors.length
    ];
  const charted = (m?.gateways || []).filter((g) => g.requests > 0);
  const peak = Math.max(
    1,
    ...(m?.days || []).map((d) =>
      Object.values(d.counts).reduce((s, n) => s + n, 0),
    ),
  );
  return (
    <>
      <Header title={t("monitor.gw.title")} sub={t("monitor.gw.sub")} />
      <FilterBar
        q={q}
        change={change}
        connections={(connections.data || []).filter((c) => c.kind !== "llm")}
        connectionLabel={t("monitor.gw.connection")}
        allLabel={t("monitor.gw.allConnections")}
        sourceLabel={t("monitor.gw.source")}
        sources={[
          { value: "", label: t("monitor.gw.allSources") },
          { value: "smart", label: t("monitor.gw.source.smart") },
          { value: "workbench", label: t("monitor.gw.source.workbench") },
        ]}
        onExport={() => exporter.run(q)}
        exporting={exporter.busy}
      />
      <DataState query={data}>
        {m && (
          <>
            <section
              className="monitor-stats"
              aria-label={t("monitor.overview")}
            >
              <Stat
                label={t("monitor.gw.total")}
                value={m.total}
                note={joinNotes([
                  t("monitor.gw.total.services", {
                    count: m.gateways.filter((g) => g.requests).length,
                  }),
                  m.running
                    ? t("monitor.gw.total.running", { count: m.running })
                    : "",
                ])}
              />
              <Stat
                label={t("monitor.result.succeeded")}
                value={m.succeeded}
                tone="success"
                note={t("monitor.gw.successRate", {
                  rate: percent(m.succeeded, m.total - m.running),
                })}
              />
              <Stat
                label={t("monitor.gw.unknown")}
                value={m.unknown}
                tone="warning"
                note={t("monitor.gw.unknown.note")}
              />
              <Stat
                label={t("monitor.gw.failed")}
                value={m.failed}
                tone="danger"
                note={
                  joinNotes([
                    m.failedBalance
                      ? t("monitor.gw.failed.balance", {
                          count: m.failedBalance,
                        })
                      : "",
                    m.failedAuth
                      ? t("monitor.gw.failed.auth", { count: m.failedAuth })
                      : "",
                  ]) || t("monitor.gw.failed.note")
                }
              />
              <Stat
                label={t("monitor.gw.rerouted")}
                value={`${m.rerouted} · ${m.autoRegenerated}`}
                note={t("monitor.gw.rerouted.note")}
              />
            </section>
            <h2 className="monitor-heading">{t("monitor.gw.byService")}</h2>
            <div className="monitor-cards">
              {m.gateways.map((g) => (
                <article className="monitor-card" key={g.connectionID}>
                  <div className="monitor-card-head">
                    <strong>
                      <span
                        className="swatch"
                        style={{ background: colorOf(g.connectionID) }}
                        aria-hidden="true"
                      />
                      {lineNames(g.name)}
                    </strong>
                    {g.deleted ? (
                      <Tag>{t("monitor.deleted")}</Tag>
                    ) : (
                      <Tag color={stateTone[g.state]}>
                        {g.role === "primary"
                          ? `${t("monitor.gw.primary")} · `
                          : g.role === "backup"
                            ? `${t("monitor.gw.backup", { n: g.backupOrder })} · `
                            : ""}
                        {t(stateKeys[g.state])}
                      </Tag>
                    )}
                  </div>
                  <div className="monitor-big">
                    <strong>{g.requests}</strong>
                    <span>
                      {t("monitor.requests")}
                      {m.total
                        ? ` · ${t("monitor.gw.share", {
                            rate: percent(g.requests, m.total),
                          })}`
                        : ""}
                    </span>
                  </div>
                  <OutcomeBar
                    label={t("monitor.gw.bar", {
                      succeeded: g.succeeded,
                      unknown: g.unknown,
                      failed: g.failed,
                    })}
                    parts={[
                      [g.succeeded, "ok"],
                      [g.unknown, "warn"],
                      [g.failed, "bad"],
                    ]}
                  />
                  {g.requests ? (
                    <div className="monitor-figures">
                      <Figure
                        label={t("monitor.result.succeeded")}
                        value={g.succeeded}
                      />
                      <Figure
                        label={t("monitor.result.unknown")}
                        value={g.unknown}
                        tone={g.unknown ? "warning" : undefined}
                      />
                      <Figure
                        label={t("monitor.result.failed")}
                        value={g.failed}
                        tone={g.failed ? "danger" : undefined}
                      />
                      <Figure
                        label={t("monitor.gw.median")}
                        value={duration(g.medianMS)}
                      />
                      <Figure
                        label={t("monitor.gw.p90")}
                        value={duration(g.p90MS)}
                      />
                      <Figure
                        label={t("monitor.gw.downloadMedian")}
                        value={duration(g.medianDownloadMS)}
                      />
                    </div>
                  ) : (
                    <p className="hint">{t("monitor.gw.noRequests")}</p>
                  )}
                  <small className="monitor-card-foot">
                    {joinNotes([
                      g.routedOut
                        ? t("monitor.gw.routedOut", { count: g.routedOut })
                        : "",
                      g.routedIn
                        ? t("monitor.gw.routedIn", { count: g.routedIn })
                        : "",
                      g.autoRegenerated
                        ? t("monitor.gw.regenerated", {
                            count: g.autoRegenerated,
                          })
                        : "",
                      g.checkedAt
                        ? t("monitor.gw.checked", {
                            time: clock(g.checkedAt),
                            ms: String(g.probeMS),
                          })
                        : "",
                    ]) || t("monitor.gw.unchecked")}
                  </small>
                </article>
              ))}
            </div>
            <section className="panel monitor-chart" aria-labelledby="gw-daily">
              <div className="panel-heading">
                <h2 id="gw-daily">{t("monitor.gw.daily")}</h2>
                <div className="monitor-legend">
                  {charted.map((g) => (
                    <span key={g.connectionID}>
                      <span
                        className="swatch"
                        style={{ background: colorOf(g.connectionID) }}
                        aria-hidden="true"
                      />
                      {lineNames(g.name)}
                    </span>
                  ))}
                </div>
              </div>
              <div
                className="day-bars"
                role="img"
                aria-label={t("monitor.gw.daily.aria", {
                  days: m.days
                    .map((d) =>
                      t("monitor.gw.daily.day", {
                        day: d.day,
                        count: Object.values(d.counts).reduce(
                          (s, n) => s + n,
                          0,
                        ),
                      }),
                    )
                    .join(t("monitor.listSeparator")),
                })}
                style={{
                  gridTemplateColumns: `repeat(${m.days.length}, minmax(0, 1fr))`,
                }}
              >
                {m.days.map((d) => (
                  <div key={d.day} className="day-bar">
                    {charted
                      .slice()
                      .reverse()
                      .map((g) =>
                        d.counts[g.connectionID] ? (
                          <span
                            key={g.connectionID}
                            title={t("monitor.gw.daily.bar", {
                              name: lineNames(g.name),
                              count: d.counts[g.connectionID],
                            })}
                            style={{
                              height: `${(d.counts[g.connectionID] / peak) * 160}px`,
                              background: colorOf(g.connectionID),
                            }}
                          />
                        ) : null,
                      )}
                  </div>
                ))}
              </div>
              <div
                className="day-labels"
                style={{
                  gridTemplateColumns: `repeat(${m.days.length}, minmax(0, 1fr))`,
                }}
              >
                {m.days.map((d, i) => (
                  <span key={d.day}>
                    {m.days.length > 10 && i % 5 && i !== m.days.length - 1
                      ? ""
                      : d.day}
                  </span>
                ))}
              </div>
            </section>
            <section className="panel" aria-labelledby="gw-log">
              <div className="panel-heading">
                <div>
                  <h2 id="gw-log">{t("monitor.gw.log")}</h2>
                  <p className="hint">{t("monitor.gw.log.hint")}</p>
                </div>
                <UISelect
                  aria-label={t("monitor.gw.log.result")}
                  value={q.result}
                  style={{ width: 140 }}
                  options={[
                    { value: "", label: t("monitor.allResults") },
                    {
                      value: "succeeded",
                      label: t("monitor.result.succeeded"),
                    },
                    { value: "unknown", label: t("monitor.result.unknown") },
                    { value: "failed", label: t("monitor.result.failed") },
                  ]}
                  onChange={(result) => change({ result })}
                />
              </div>
              <ScrollTable>
                <Table<GatewayCall>
                  rowKey="taskID"
                  size="small"
                  style={{ minWidth: 1180 }}
                  dataSource={m.calls}
                  locale={{ emptyText: t("monitor.gw.log.empty") }}
                  pagination={{
                    current: Math.floor(q.offset / pageSize) + 1,
                    pageSize,
                    total: m.callTotal,
                    showSizeChanger: false,
                    onChange: (page) =>
                      setQ((v) => ({ ...v, offset: (page - 1) * pageSize })),
                  }}
                  columns={[
                    {
                      title: t("monitor.col.time"),
                      dataIndex: "at",
                      render: clockCell,
                      width: 140,
                    },
                    {
                      title: t("monitor.gw.connection"),
                      dataIndex: "name",
                      width: 150,
                      render: (v: string) => lineNames(v),
                    },
                    {
                      title: t("monitor.gw.source"),
                      width: 240,
                      render: (_, c) => (
                        <span className="monitor-source">
                          {c.sourceLabel}
                          {c.routedFromName && (
                            <Tag color="blue">
                              {t("monitor.gw.routedFrom", {
                                name: lineNames(c.routedFromName),
                              })}
                            </Tag>
                          )}
                          {c.autoRetry && (
                            <Tag color="warning">
                              {t("monitor.gw.autoRetry")}
                            </Tag>
                          )}
                        </span>
                      ),
                    },
                    {
                      title: t("monitor.col.model"),
                      dataIndex: "model",
                      width: 130,
                    },
                    {
                      title: t("monitor.col.result"),
                      width: 170,
                      render: (_, c) => {
                        const [key, tone] = gatewayResult[c.result];
                        return (
                          <span className={tone ? `tone-${tone}` : undefined}>
                            {t(key)}
                            {c.httpStatus ? ` · HTTP ${c.httpStatus}` : ""}
                            {c.downloadFailed
                              ? t("monitor.gw.downloadFailed")
                              : ""}
                            {c.failReason === "balance"
                              ? ` · ${t("monitor.gw.balance")}`
                              : c.failReason === "auth"
                                ? ` · ${t("monitor.gw.authRejected")}`
                                : ""}
                          </span>
                        );
                      },
                    },
                    {
                      title: t("monitor.col.generation"),
                      width: 90,
                      render: (_, c) => duration(c.generationMS),
                    },
                    {
                      title: t("monitor.col.download"),
                      width: 90,
                      render: (_, c) => duration(c.downloadMS),
                    },
                    {
                      title: t("monitor.gw.requestID"),
                      width: 150,
                      render: (_, c) =>
                        c.requestID ? (
                          <Tooltip title={c.requestID}>
                            <code className="mono">
                              {c.requestID.slice(0, 12)}
                              {c.requestID.length > 12 ? "…" : ""}
                            </code>
                          </Tooltip>
                        ) : (
                          <span className="muted">
                            {c.result === "failed"
                              ? t("monitor.gw.notCharged")
                              : t("monitor.gw.noRequestID")}
                          </span>
                        ),
                    },
                  ]}
                />
              </ScrollTable>
            </section>
          </>
        )}
      </DataState>
    </>
  );
}

const llmResult = {
  completed: ["monitor.llm.result.completed", "success"],
  dropped: ["monitor.llm.result.dropped", "warning"],
  error: ["monitor.llm.result.error", "danger"],
} as const satisfies Record<string, readonly [MessageKey, string]>;

export function LLMMonitorPage() {
  const { q, setQ, change } = useMonitorQuery();
  const connections = useData<Connection[]>(["connections"], "ListConnections");
  const lineNames = useLineNames();
  const data = useData<LLMMonitor>(["llm-monitor", q], "LLMMonitor", [q], 5000);
  const exporter = useCSVExport(
    "ExportLLMCallsToPath",
    t("monitor.llm.exportName"),
  );
  const m = data.data;
  return (
    <>
      <Header title={t("monitor.llm.title")} sub={t("monitor.llm.sub")} />
      <FilterBar
        q={q}
        change={change}
        connections={(connections.data || []).filter((c) => c.kind === "llm")}
        connectionLabel={t("monitor.llm.connection")}
        allLabel={t("monitor.llm.allConnections")}
        sourceLabel={t("monitor.llm.kind")}
        sources={[
          { value: "", label: t("monitor.llm.allKinds") },
          { value: "analysis", label: t("monitor.llm.kind.analysis") },
          { value: "test", label: t("monitor.llm.kind.test") },
          { value: "rewrite", label: t("monitor.llm.kind.rewrite") },
          { value: "check", label: t("monitor.llm.kind.check") },
        ]}
        onExport={() => exporter.run(q)}
        exporting={exporter.busy}
      />
      <DataState query={data}>
        {m && (
          <>
            <section
              className="monitor-stats"
              aria-label={t("monitor.overview")}
            >
              <Stat
                label={t("monitor.llm.requests")}
                value={m.requests}
                note={t("monitor.llm.requests.note", {
                  runs: m.analysisRuns,
                  tests: m.testRequests,
                })}
              />
              <Stat
                label={t("monitor.llm.tokens")}
                value={`${tokens(m.inputTokens)} / ${tokens(m.outputTokens)}`}
                note={t("monitor.llm.tokens.note")}
              />
              <Stat
                label={t("monitor.llm.result.dropped")}
                value={m.dropped}
                tone={m.dropped ? "warning" : undefined}
                note={
                  m.dropped
                    ? m.recovered === m.dropped
                      ? t("monitor.llm.dropped.allRetried")
                      : t("monitor.llm.dropped.retried", {
                          count: m.recovered,
                        })
                    : t("monitor.llm.dropped.none")
                }
              />
              <Stat
                label={t("monitor.llm.errors")}
                value={m.errors}
                tone={m.errors ? "danger" : undefined}
                note={m.errorNote || t("monitor.llm.errors.none")}
              />
              <Stat
                label={t("monitor.llm.runTime")}
                value={duration(m.medianRunMS)}
                note={t("monitor.llm.runTime.note")}
              />
            </section>
            <h2 className="monitor-heading">{t("monitor.llm.byModel")}</h2>
            <div className="monitor-cards">
              {m.providers.map((p) => (
                <article className="monitor-card" key={p.connectionID}>
                  <div className="monitor-card-head">
                    <div>
                      <strong>{lineNames(p.name)}</strong>
                      <small className="monitor-sub">
                        {t("monitor.llm.model", {
                          model: p.model || t("monitor.llm.model.unset"),
                        })}
                        {p.analysisRuns
                          ? ` · ${t("monitor.llm.runs", { count: p.analysisRuns })}`
                          : p.requests
                            ? ` · ${t("monitor.llm.testsOnly")}`
                            : ""}
                      </small>
                    </div>
                    {p.deleted ? (
                      <Tag>{t("monitor.deleted")}</Tag>
                    ) : (
                      <Tag color={p.capabilityOK ? "success" : "default"}>
                        {p.capabilityOK
                          ? t("monitor.llm.capOK")
                          : t("monitor.llm.capPending")}
                      </Tag>
                    )}
                  </div>
                  <div className="monitor-big">
                    <strong>{p.requests}</strong>
                    <span>
                      {t("monitor.requests")}
                      {p.tests
                        ? ` · ${t("monitor.llm.tests", { count: p.tests })}`
                        : ""}
                    </span>
                  </div>
                  <OutcomeBar
                    label={t("monitor.llm.bar", {
                      completed: p.completed,
                      dropped: p.dropped,
                      errors: p.errors,
                    })}
                    parts={[
                      [p.completed, "ok"],
                      [p.dropped, "warn"],
                      [p.errors, "bad"],
                    ]}
                  />
                  {p.requests ? (
                    <div className="monitor-figures">
                      <Figure
                        label={t("monitor.llm.result.completed")}
                        value={p.completed}
                      />
                      <Figure
                        label={t("monitor.llm.result.dropped")}
                        value={p.dropped}
                        tone={p.dropped ? "warning" : undefined}
                      />
                      <Figure
                        label={t("monitor.llm.result.error")}
                        value={p.errors}
                        tone={p.errors ? "danger" : undefined}
                      />
                      <Figure
                        label={t("monitor.llm.firstMedian")}
                        value={duration(p.medianFirstMS)}
                      />
                      <Figure
                        label={t("monitor.llm.totalMedian")}
                        value={duration(p.medianTotalMS)}
                      />
                      <Figure
                        label={t("monitor.llm.longest")}
                        value={duration(p.maxTotalMS)}
                      />
                    </div>
                  ) : (
                    <p className="hint">{t("monitor.llm.noRequests")}</p>
                  )}
                  <small className="monitor-card-foot">
                    {t("monitor.llm.tokenFoot", {
                      input: tokens(p.inputTokens),
                      output: tokens(p.outputTokens),
                    })}
                  </small>
                </article>
              ))}
            </div>
            <section className="panel" aria-labelledby="llm-drops">
              <div className="panel-heading">
                <div>
                  <h2 id="llm-drops">{t("monitor.llm.result.dropped")}</h2>
                  <p className="hint">{t("monitor.llm.drops.hint")}</p>
                </div>
              </div>
              {m.drops.length ? (
                <div className="drop-list">
                  {m.drops.map((d) => (
                    <div className="drop-item" key={d.at}>
                      <div>
                        <strong>{clock(d.at, false)}</strong>
                        <span
                          className={
                            d.recovered ? "tone-success" : "tone-danger"
                          }
                        >
                          {d.outcome}
                        </span>
                      </div>
                      <small>
                        {d.runName || t("monitor.llm.kind.analysisShort")}
                        {d.stage ? ` · ${d.stage}` : ""} ·{" "}
                        {t("monitor.llm.drop.waited", {
                          time: duration(d.waitedMS),
                        })}
                      </small>
                      <Tooltip title={d.detail}>
                        <small>
                          {t("monitor.llm.drop.reason", { reason: d.reason })}
                        </small>
                      </Tooltip>
                    </div>
                  ))}
                </div>
              ) : (
                <p className="hint">{t("monitor.llm.drops.none")}</p>
              )}
            </section>
            <section className="panel" aria-labelledby="llm-runs">
              <div className="panel-heading">
                <div>
                  <h2 id="llm-runs">{t("monitor.llm.byRun")}</h2>
                  <p className="hint">{t("monitor.llm.byRun.hint")}</p>
                </div>
              </div>
              <ScrollTable>
                <Table
                  rowKey="runID"
                  size="small"
                  style={{ minWidth: 1080 }}
                  dataSource={m.runs}
                  pagination={
                    m.runs.length > 10
                      ? { pageSize: 10, showSizeChanger: false }
                      : false
                  }
                  locale={{ emptyText: t("monitor.llm.byRun.empty") }}
                  columns={[
                    {
                      title: t("monitor.col.started"),
                      dataIndex: "startedAt",
                      render: clockCell,
                      width: 140,
                    },
                    {
                      title: t("monitor.col.run"),
                      dataIndex: "name",
                      width: 240,
                    },
                    {
                      title: t("monitor.col.provider"),
                      dataIndex: "connectionName",
                      width: 150,
                      render: (v: string) => lineNames(v),
                    },
                    {
                      title: t("monitor.col.requests"),
                      dataIndex: "requests",
                      width: 70,
                    },
                    {
                      title: t("monitor.llm.tokens"),
                      width: 150,
                      render: (_, r) =>
                        `${tokens(r.inputTokens)} / ${tokens(r.outputTokens)}`,
                    },
                    {
                      title: t("monitor.col.drops"),
                      width: 110,
                      render: (_, r) =>
                        r.dropped ? (
                          <span className="tone-warning">
                            {t("monitor.times", { count: r.dropped })}
                          </span>
                        ) : (
                          "0"
                        ),
                    },
                    {
                      title: t("monitor.col.runTime"),
                      width: 110,
                      render: (_, r) => duration(r.durationMS),
                    },
                    {
                      title: t("monitor.col.result"),
                      width: 130,
                      render: (_, r) => (
                        <span
                          className={
                            r.resultTone ? `tone-${r.resultTone}` : undefined
                          }
                        >
                          {r.result}
                        </span>
                      ),
                    },
                  ]}
                />
              </ScrollTable>
            </section>
            <section className="panel" aria-labelledby="llm-log">
              <div className="panel-heading">
                <div>
                  <h2 id="llm-log">{t("monitor.llm.log")}</h2>
                  <p className="hint">{t("monitor.llm.log.hint")}</p>
                </div>
                <UISelect
                  aria-label={t("monitor.llm.log.result")}
                  value={q.result}
                  style={{ width: 140 }}
                  options={[
                    { value: "", label: t("monitor.allResults") },
                    {
                      value: "completed",
                      label: t("monitor.llm.result.completed"),
                    },
                    {
                      value: "dropped",
                      label: t("monitor.llm.result.dropped"),
                    },
                    { value: "error", label: t("monitor.llm.result.error") },
                  ]}
                  onChange={(result) => change({ result })}
                />
              </div>
              <ScrollTable>
                <Table<LLMCall>
                  rowKey="id"
                  size="small"
                  style={{ minWidth: 1120 }}
                  dataSource={m.calls}
                  locale={{ emptyText: t("monitor.llm.log.empty") }}
                  pagination={{
                    current: Math.floor(q.offset / pageSize) + 1,
                    pageSize,
                    total: m.callTotal,
                    showSizeChanger: false,
                    onChange: (page) =>
                      setQ((v) => ({ ...v, offset: (page - 1) * pageSize })),
                  }}
                  columns={[
                    {
                      title: t("monitor.col.time"),
                      dataIndex: "at",
                      render: clockCell,
                      width: 140,
                    },
                    {
                      title: t("monitor.col.run"),
                      width: 170,
                      render: (_, c) =>
                        c.kind === "test"
                          ? t("monitor.llm.kind.test")
                          : c.kind === "rewrite"
                            ? t("monitor.llm.kind.rewrite")
                            : c.kind === "check"
                              ? t("monitor.llm.kind.check")
                              : m.runNames[c.runID || ""] ||
                                t("monitor.llm.kind.analysisShort"),
                    },
                    {
                      title: t("monitor.col.stage"),
                      width: 170,
                      render: (_, c) => (
                        <span className="monitor-source">
                          {c.stage || "—"}
                          {c.retry && (
                            <Tag color="blue">{t("monitor.llm.retry")}</Tag>
                          )}
                        </span>
                      ),
                    },
                    {
                      title: t("monitor.col.firstByte"),
                      width: 100,
                      render: (_, c) => duration(c.firstByteMS),
                    },
                    {
                      title: t("monitor.col.total"),
                      width: 100,
                      render: (_, c) => duration(c.totalMS),
                    },
                    {
                      title: t("monitor.col.tokens"),
                      width: 120,
                      render: (_, c) =>
                        c.inputTokens || c.outputTokens
                          ? `${tokens(c.inputTokens)} / ${tokens(c.outputTokens)}`
                          : "—",
                    },
                    {
                      title: t("monitor.col.result"),
                      width: 170,
                      render: (_, c) => {
                        const [key, tone] = llmResult[c.status];
                        const body = (
                          <span className={`tone-${tone}`}>
                            {t(key)}
                            {c.status === "error" && c.httpStatus
                              ? ` · HTTP ${c.httpStatus}`
                              : ""}
                          </span>
                        );
                        return c.error ? (
                          <Tooltip title={c.error}>{body}</Tooltip>
                        ) : (
                          body
                        );
                      },
                    },
                    {
                      title: t("monitor.llm.requestID"),
                      width: 150,
                      render: (_, c) =>
                        c.requestID ? (
                          <Tooltip title={c.requestID}>
                            <code className="mono">
                              {c.requestID.slice(0, 12)}
                              {c.requestID.length > 12 ? "…" : ""}
                            </code>
                          </Tooltip>
                        ) : (
                          <span className="muted">
                            {t("monitor.llm.noRequestID")}
                          </span>
                        ),
                    },
                  ]}
                />
              </ScrollTable>
            </section>
          </>
        )}
      </DataState>
    </>
  );
}
