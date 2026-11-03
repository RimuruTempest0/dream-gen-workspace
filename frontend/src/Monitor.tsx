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
import { DataState, Header, useActions, useData } from "./ui";

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
  kind: "analysis" | "test" | "rewrite";
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
const gatewayColors = ["#245BDB", "#C2410C", "#5B6B80", "#18794E", "#7C3AED"];
const stateText = {
  healthy: "正常",
  slow: "变慢",
  down: "不可用",
  unknown: "未检查",
};
const stateTone = {
  healthy: "success",
  slow: "warning",
  down: "error",
  unknown: "default",
} as const;

export function duration(ms: number) {
  if (!ms) return "—";
  if (ms < 10_000) return `${(ms / 1000).toFixed(1)} 秒`;
  const s = Math.round(ms / 1000);
  return s >= 60 ? `${Math.floor(s / 60)} 分 ${s % 60} 秒` : `${s} 秒`;
}
export function tokens(n: number) {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 1000) return `${(n / 1000).toFixed(n >= 100_000 ? 0 : 1)}K`;
  return String(n);
}
function clock(value: string) {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  const p = (n: number) => String(n).padStart(2, "0");
  return `${p(d.getMonth() + 1)}/${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}
function percent(part: number, whole: number) {
  return whole ? Math.round((part / whole) * 100) : 0;
}

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
      aria-label="表格，可左右滚动"
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
        notification.success({ message: `已导出 ${n} 条调用记录` });
      }),
  };
}

const dayOptions = [
  { value: 1, label: "今天" },
  { value: 7, label: "最近 7 天" },
  { value: 30, label: "最近 30 天" },
  { value: 0, label: "全部" },
];

function FilterBar({
  q,
  change,
  connections,
  connectionLabel,
  allLabel,
  sources,
  onExport,
  exporting,
}: {
  q: Query;
  change: (p: Partial<Query>) => void;
  connections: Connection[];
  connectionLabel: string;
  allLabel: string;
  sources: { value: string; label: string }[];
  onExport: () => void;
  exporting: boolean;
}) {
  return (
    <div className="monitor-filters" role="search">
      <label>
        <span>时间范围</span>
        <UISelect
          aria-label="时间范围"
          value={q.days}
          style={{ width: 140 }}
          options={dayOptions}
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
            ...connections.map((c) => ({ value: c.id, label: c.name })),
          ]}
          onChange={(connectionID) => change({ connectionID })}
        />
      </label>
      <label>
        <span>{sources[0].label === "全部来源" ? "来源" : "请求类型"}</span>
        <UISelect
          aria-label={sources[0].label === "全部来源" ? "来源" : "请求类型"}
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
        导出 CSV
      </Button>
    </div>
  );
}

const gatewayResult = {
  succeeded: ["成功", "success"],
  unknown: ["结果未知", "warning"],
  failed: ["失败", "danger"],
  running: ["进行中", ""],
} as const;

export function GatewayMonitorPage() {
  const { q, setQ, change } = useMonitorQuery();
  const connections = useData<Connection[]>(["connections"], "ListConnections");
  const data = useData<GatewayMonitor>(
    ["gateway-monitor", q],
    "GatewayMonitor",
    [q],
    5000,
  );
  const exporter = useCSVExport("ExportGatewayCallsToPath", "中转站调用记录");
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
      <Header
        title="中转站监控"
        sub="每一次实际发出的生图请求都记在发往的中转站下。免费健康检查、未发送就暂停或取消的图片不计入。"
      />
      <FilterBar
        q={q}
        change={change}
        connections={(connections.data || []).filter((c) => c.kind !== "llm")}
        connectionLabel="中转站"
        allLabel="全部中转站"
        sources={[
          { value: "", label: "全部来源" },
          { value: "smart", label: "智能生图" },
          { value: "workbench", label: "生图工作台" },
        ]}
        onExport={() => exporter.run(q)}
        exporting={exporter.busy}
      />
      <DataState query={data}>
        {m && (
          <>
            <section className="monitor-stats" aria-label="总览">
              <Stat
                label="实际发出的生图请求"
                value={m.total}
                note={`${m.gateways.filter((g) => g.requests).length} 个中转站${m.running ? ` · 进行中 ${m.running}` : ""}`}
              />
              <Stat
                label="成功"
                value={m.succeeded}
                tone="success"
                note={`成功率 ${percent(m.succeeded, m.total - m.running)}%`}
              />
              <Stat
                label="结果未知（超时）"
                value={m.unknown}
                tone="warning"
                note="可能已计费，请核对"
              />
              <Stat
                label="失败（未计费）"
                value={m.failed}
                tone="danger"
                note={
                  [
                    m.failedBalance ? `余额不足 ${m.failedBalance}` : "",
                    m.failedAuth ? `API Key 被拒绝 ${m.failedAuth}` : "",
                  ]
                    .filter(Boolean)
                    .join(" · ") || "中转站拒绝或参数错误"
                }
              />
              <Stat
                label="转到备用 · 自动重新生成"
                value={`${m.rerouted} · ${m.autoRegenerated}`}
                note="重新生成可能重复计费"
              />
            </section>
            <h2 className="monitor-heading">按中转站</h2>
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
                      {g.name}
                    </strong>
                    {g.deleted ? (
                      <Tag>已删除</Tag>
                    ) : (
                      <Tag color={stateTone[g.state]}>
                        {g.role === "primary"
                          ? "主用 · "
                          : g.role === "backup"
                            ? `备用 ${g.backupOrder} · `
                            : ""}
                        {stateText[g.state]}
                      </Tag>
                    )}
                  </div>
                  <div className="monitor-big">
                    <strong>{g.requests}</strong>
                    <span>
                      次请求
                      {m.total ? ` · 占 ${percent(g.requests, m.total)}%` : ""}
                    </span>
                  </div>
                  <OutcomeBar
                    label={`成功 ${g.succeeded}，结果未知 ${g.unknown}，失败 ${g.failed}`}
                    parts={[
                      [g.succeeded, "ok"],
                      [g.unknown, "warn"],
                      [g.failed, "bad"],
                    ]}
                  />
                  {g.requests ? (
                    <div className="monitor-figures">
                      <Figure label="成功" value={g.succeeded} />
                      <Figure
                        label="结果未知"
                        value={g.unknown}
                        tone={g.unknown ? "warning" : undefined}
                      />
                      <Figure
                        label="失败"
                        value={g.failed}
                        tone={g.failed ? "danger" : undefined}
                      />
                      <Figure label="生成中位" value={duration(g.medianMS)} />
                      <Figure label="慢的 10%" value={duration(g.p90MS)} />
                      <Figure
                        label="下载中位"
                        value={duration(g.medianDownloadMS)}
                      />
                    </div>
                  ) : (
                    <p className="hint">这段时间没有图片发往这里。</p>
                  )}
                  <small className="monitor-card-foot">
                    {[
                      g.routedOut ? `转出 ${g.routedOut} 张到备用` : "",
                      g.routedIn ? `转入 ${g.routedIn} 张` : "",
                      g.autoRegenerated
                        ? `自动重新生成 ${g.autoRegenerated} 张（可能重复计费）`
                        : "",
                      g.checkedAt
                        ? `免费检查 ${clock(g.checkedAt)}，响应 ${g.probeMS} ms`
                        : "",
                    ]
                      .filter(Boolean)
                      .join(" · ") || "尚未进行免费检查"}
                  </small>
                </article>
              ))}
            </div>
            <section className="panel monitor-chart" aria-labelledby="gw-daily">
              <div className="panel-heading">
                <h2 id="gw-daily">每日请求数</h2>
                <div className="monitor-legend">
                  {charted.map((g) => (
                    <span key={g.connectionID}>
                      <span
                        className="swatch"
                        style={{ background: colorOf(g.connectionID) }}
                        aria-hidden="true"
                      />
                      {g.name}
                    </span>
                  ))}
                </div>
              </div>
              <div
                className="day-bars"
                role="img"
                aria-label={`每日请求数：${m.days.map((d) => `${d.day} ${Object.values(d.counts).reduce((s, n) => s + n, 0)} 次`).join("，")}`}
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
                            title={`${g.name}：${d.counts[g.connectionID]} 次`}
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
                  <h2 id="gw-log">调用记录</h2>
                  <p className="hint">
                    一行是一次实际发出的生图请求。请求 ID
                    可用于在中转站后台核对计费。
                  </p>
                </div>
                <UISelect
                  aria-label="调用结果"
                  value={q.result}
                  style={{ width: 140 }}
                  options={[
                    { value: "", label: "全部结果" },
                    { value: "succeeded", label: "成功" },
                    { value: "unknown", label: "结果未知" },
                    { value: "failed", label: "失败" },
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
                  locale={{ emptyText: "这段时间没有生图请求" }}
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
                      title: "时间",
                      dataIndex: "at",
                      render: clock,
                      width: 140,
                    },
                    { title: "中转站", dataIndex: "name", width: 150 },
                    {
                      title: "来源",
                      width: 240,
                      render: (_, c) => (
                        <span className="monitor-source">
                          {c.sourceLabel}
                          {c.routedFromName && (
                            <Tag color="blue">从 {c.routedFromName} 转入</Tag>
                          )}
                          {c.autoRetry && (
                            <Tag color="warning">自动重新生成</Tag>
                          )}
                        </span>
                      ),
                    },
                    { title: "模型", dataIndex: "model", width: 130 },
                    {
                      title: "结果",
                      width: 170,
                      render: (_, c) => {
                        const [text, tone] = gatewayResult[c.result];
                        return (
                          <span className={tone ? `tone-${tone}` : undefined}>
                            {text}
                            {c.httpStatus ? ` · HTTP ${c.httpStatus}` : ""}
                            {c.downloadFailed ? "（下载未完成）" : ""}
                            {c.failReason === "balance"
                              ? " · 余额不足"
                              : c.failReason === "auth"
                                ? " · API Key 被拒绝"
                                : ""}
                          </span>
                        );
                      },
                    },
                    {
                      title: "生成",
                      width: 90,
                      render: (_, c) => duration(c.generationMS),
                    },
                    {
                      title: "下载",
                      width: 90,
                      render: (_, c) => duration(c.downloadMS),
                    },
                    {
                      title: "中转站请求 ID",
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
                            {c.result === "failed" ? "未计费" : "中转站未返回"}
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
  completed: ["完成", "success"],
  dropped: ["连接中断", "warning"],
  error: ["错误", "danger"],
} as const;

export function LLMMonitorPage() {
  const { q, setQ, change } = useMonitorQuery();
  const connections = useData<Connection[]>(["connections"], "ListConnections");
  const data = useData<LLMMonitor>(["llm-monitor", q], "LLMMonitor", [q], 5000);
  const exporter = useCSVExport("ExportLLMCallsToPath", "LLM调用记录");
  const m = data.data;
  return (
    <>
      <Header
        title="LLM 监控"
        sub="智能生图分析时发给 LLM 供应商的每一次请求：用量、耗时和连接中断。能力验证单独标注。"
      />
      <FilterBar
        q={q}
        change={change}
        connections={(connections.data || []).filter((c) => c.kind === "llm")}
        connectionLabel="LLM 供应商"
        allLabel="全部供应商"
        sources={[
          { value: "", label: "全部" },
          { value: "analysis", label: "智能生图分析" },
          { value: "test", label: "能力验证" },
          { value: "rewrite", label: "改写被拒提示词" },
        ]}
        onExport={() => exporter.run(q)}
        exporting={exporter.busy}
      />
      <DataState query={data}>
        {m && (
          <>
            <section className="monitor-stats" aria-label="总览">
              <Stat
                label="LLM 请求"
                value={m.requests}
                note={`${m.analysisRuns} 次智能生图 · ${m.testRequests} 次验证`}
              />
              <Stat
                label="输入 / 输出 tokens"
                value={`${tokens(m.inputTokens)} / ${tokens(m.outputTokens)}`}
                note="按供应商返回的用量"
              />
              <Stat
                label="连接中断"
                value={m.dropped}
                tone={m.dropped ? "warning" : undefined}
                note={
                  m.dropped
                    ? m.recovered === m.dropped
                      ? "均已自动重试"
                      : `${m.recovered} 次已自动重试`
                    : "没有中断"
                }
              />
              <Stat
                label="请求错误"
                value={m.errors}
                tone={m.errors ? "danger" : undefined}
                note={m.errorNote || "没有错误"}
              />
              <Stat
                label="分析用时（中位）"
                value={duration(m.medianRunMS)}
                note="一次需求从首个请求到最后回复"
              />
            </section>
            <h2 className="monitor-heading">按 LLM 供应商</h2>
            <div className="monitor-cards">
              {m.providers.map((p) => (
                <article className="monitor-card" key={p.connectionID}>
                  <div className="monitor-card-head">
                    <div>
                      <strong>{p.name}</strong>
                      <small className="monitor-sub">
                        模型 {p.model || "未设置"}
                        {p.analysisRuns
                          ? ` · ${p.analysisRuns} 次智能生图`
                          : p.requests
                            ? " · 仅验证"
                            : ""}
                      </small>
                    </div>
                    {p.deleted ? (
                      <Tag>已删除</Tag>
                    ) : (
                      <Tag color={p.capabilityOK ? "success" : "default"}>
                        {p.capabilityOK ? "能力验证通过" : "未完成能力验证"}
                      </Tag>
                    )}
                  </div>
                  <div className="monitor-big">
                    <strong>{p.requests}</strong>
                    <span>次请求{p.tests ? ` · 验证 ${p.tests}` : ""}</span>
                  </div>
                  <OutcomeBar
                    label={`完成 ${p.completed}，连接中断 ${p.dropped}，错误 ${p.errors}`}
                    parts={[
                      [p.completed, "ok"],
                      [p.dropped, "warn"],
                      [p.errors, "bad"],
                    ]}
                  />
                  {p.requests ? (
                    <div className="monitor-figures">
                      <Figure label="完成" value={p.completed} />
                      <Figure
                        label="连接中断"
                        value={p.dropped}
                        tone={p.dropped ? "warning" : undefined}
                      />
                      <Figure
                        label="错误"
                        value={p.errors}
                        tone={p.errors ? "danger" : undefined}
                      />
                      <Figure
                        label="首次响应中位"
                        value={duration(p.medianFirstMS)}
                      />
                      <Figure
                        label="完整回复中位"
                        value={duration(p.medianTotalMS)}
                      />
                      <Figure label="最长一次" value={duration(p.maxTotalMS)} />
                    </div>
                  ) : (
                    <p className="hint">这段时间没有请求。</p>
                  )}
                  <small className="monitor-card-foot">
                    输入 {tokens(p.inputTokens)} tokens · 输出{" "}
                    {tokens(p.outputTokens)} tokens
                  </small>
                </article>
              ))}
            </div>
            <section className="panel" aria-labelledby="llm-drops">
              <div className="panel-heading">
                <div>
                  <h2 id="llm-drops">连接中断</h2>
                  <p className="hint">
                    LLM
                    回复途中连接被断开的记录。等待时长接近的多次中断，通常是代理或网络的超时设置所致。
                  </p>
                </div>
              </div>
              {m.drops.length ? (
                <div className="drop-list">
                  {m.drops.map((d) => (
                    <div className="drop-item" key={d.at}>
                      <div>
                        <strong>{clock(d.at).slice(0, 11)}</strong>
                        <span
                          className={
                            d.recovered ? "tone-success" : "tone-danger"
                          }
                        >
                          {d.outcome}
                        </span>
                      </div>
                      <small>
                        {d.runName || "智能生图"}
                        {d.stage ? ` · ${d.stage}` : ""} · 等待约{" "}
                        {duration(d.waitedMS)} 后断开
                      </small>
                      <Tooltip title={d.detail}>
                        <small>原因：{d.reason}</small>
                      </Tooltip>
                    </div>
                  ))}
                </div>
              ) : (
                <p className="hint">这段时间没有连接中断。</p>
              )}
            </section>
            <section className="panel" aria-labelledby="llm-runs">
              <div className="panel-heading">
                <div>
                  <h2 id="llm-runs">按智能生图需求</h2>
                  <p className="hint">每个需求用了多少 LLM 请求和 tokens。</p>
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
                  locale={{ emptyText: "这段时间没有智能生图分析" }}
                  columns={[
                    {
                      title: "开始时间",
                      dataIndex: "startedAt",
                      render: clock,
                      width: 140,
                    },
                    { title: "需求", dataIndex: "name", width: 240 },
                    {
                      title: "供应商",
                      dataIndex: "connectionName",
                      width: 150,
                    },
                    { title: "请求", dataIndex: "requests", width: 70 },
                    {
                      title: "输入 / 输出 tokens",
                      width: 150,
                      render: (_, r) =>
                        `${tokens(r.inputTokens)} / ${tokens(r.outputTokens)}`,
                    },
                    {
                      title: "中断",
                      width: 110,
                      render: (_, r) =>
                        r.dropped ? (
                          <span className="tone-warning">{r.dropped} 次</span>
                        ) : (
                          "0"
                        ),
                    },
                    {
                      title: "分析用时",
                      width: 110,
                      render: (_, r) => duration(r.durationMS),
                    },
                    {
                      title: "结果",
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
                  <h2 id="llm-log">请求记录</h2>
                  <p className="hint">
                    一行是一次发给 LLM 供应商的请求，不保存请求内容。供应商请求
                    ID 可用于核对用量。
                  </p>
                </div>
                <UISelect
                  aria-label="请求结果"
                  value={q.result}
                  style={{ width: 140 }}
                  options={[
                    { value: "", label: "全部结果" },
                    { value: "completed", label: "完成" },
                    { value: "dropped", label: "连接中断" },
                    { value: "error", label: "错误" },
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
                  locale={{ emptyText: "这段时间没有 LLM 请求" }}
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
                      title: "时间",
                      dataIndex: "at",
                      render: clock,
                      width: 140,
                    },
                    {
                      title: "需求",
                      width: 170,
                      render: (_, c) =>
                        c.kind === "test"
                          ? "能力验证"
                          : c.kind === "rewrite"
                            ? "改写被拒提示词"
                            : m.runNames[c.runID || ""] || "智能生图",
                    },
                    {
                      title: "阶段",
                      width: 170,
                      render: (_, c) => (
                        <span className="monitor-source">
                          {c.stage || "—"}
                          {c.retry && <Tag color="blue">重试</Tag>}
                        </span>
                      ),
                    },
                    {
                      title: "首次响应",
                      width: 100,
                      render: (_, c) => duration(c.firstByteMS),
                    },
                    {
                      title: "总用时",
                      width: 100,
                      render: (_, c) => duration(c.totalMS),
                    },
                    {
                      title: "输入 / 输出",
                      width: 120,
                      render: (_, c) =>
                        c.inputTokens || c.outputTokens
                          ? `${tokens(c.inputTokens)} / ${tokens(c.outputTokens)}`
                          : "—",
                    },
                    {
                      title: "结果",
                      width: 170,
                      render: (_, c) => {
                        const [text, tone] = llmResult[c.status];
                        const body = (
                          <span className={`tone-${tone}`}>
                            {text}
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
                      title: "供应商请求 ID",
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
                          <span className="muted">未返回</span>
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
