import {
  useEffect,
  useState,
  useRef,
  type ReactNode,
  type ComponentProps,
} from "react";
import {
  NavLink,
  Routes,
  Route,
  useLocation,
  useNavigate,
} from "react-router-dom";
import {
  useQuery,
  useInfiniteQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  App as AntApp,
  Button,
  Input,
  InputNumber,
  Select as UISelect,
  Form as UIForm,
  Modal,
  Alert,
  Empty,
  Spin,
  Tag,
  Table,
  Space,
  Switch,
  Tooltip,
  Progress,
} from "antd";
import {
  PictureOutlined,
  AppstoreOutlined,
  ClockCircleOutlined,
  FolderOutlined,
  SettingOutlined,
  PlusOutlined,
  ArrowRightOutlined,
  UploadOutlined,
  DownloadOutlined,
  DeleteOutlined,
  FileTextOutlined,
  ReloadOutlined,
  CloseOutlined,
  ApiOutlined,
  CheckCircleOutlined,
  EyeOutlined,
  EyeInvisibleOutlined,
  LoadingOutlined,
  CheckSquareOutlined,
  CheckOutlined,
  DashboardOutlined,
  RobotOutlined,
} from "@ant-design/icons";
import type { SmartProgress } from "./agent-api";

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
      ? `智能生图正在分析${p.step ? "：" + runStepNames[p.step] : ""}`
      : p.state === "needs_input"
        ? "智能生图等待补充资料"
        : p.state === "paused"
          ? `智能生图已暂停，已完成 ${p.done} / ${p.total} 张`
          : `智能生图生成中，已完成 ${p.done} / ${p.total} 张`;
  // The link keeps its plain name; this text is its description (aria-describedby).
  return (
    <Tooltip title={label}>
      <span className="nav-progress" id="smart-progress-status">
        <span className="sr-only">{label}</span>
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
      </span>
    </Tooltip>
  );
}
import {
  call,
  blankDraft,
  errorText,
  stateNames,
  type Connection,
  type ConnectionInput,
  type GenerateInput,
  type Task,
  type Asset,
  type Template,
  type Summary,
  type TestResult,
  type Page,
  type RecentBatch,
} from "./api";

import {
  useData,
  useActions,
  SecretInput,
  useImageDownloads,
  DataState,
  Header,
  Status,
  TaskTiming,
  TaskFailureAlert,
  autoHandledNote,
  AssetImage,
  CancelTaskButton,
  ModelSelect,
  assetLabel,
  coalesce,
  presetQualities,
  presetSizes,
  taskTitle,
  time,
  withCurrent,
} from "./ui";
import { TemplateBar, TemplatesPage, type TemplateMode } from "./Templates";
import { SmartImages } from "./SmartImages";
import { runStepNames } from "./RunSteps";
import {
  LicenseBadge,
  LicenseNotice,
  LicensePanel,
  useLicense,
} from "./License";
import {
  GatewayFailover,
  GatewayRow,
  gatewayLabel,
  useGateways,
} from "./Gateways";
import { Logs } from "./Logs";
import { GatewayMonitorPage, LLMMonitorPage } from "./Monitor";
import {
  LLMConnections,
  AgentSettings,
  ConnectionFields,
} from "./AgentSettings";

export default function App() {
  const loc = useLocation();
  const qc = useQueryClient();
  const { modal, notification } = AntApp.useApp();
  const summary = useData<Summary>(["summary"], "Dashboard", [], 4000);
  const links = [
    ["/", "生图工作台", <PictureOutlined aria-hidden="true" />],
    ["/smart", "智能生图", <ApiOutlined aria-hidden="true" />],
    ["/tasks", "任务中心", <ClockCircleOutlined aria-hidden="true" />],
    ["/assets", "素材库", <AppstoreOutlined aria-hidden="true" />],
    [
      "/monitor/gateways",
      "中转站监控",
      <DashboardOutlined aria-hidden="true" />,
    ],
    ["/monitor/llm", "LLM 监控", <RobotOutlined aria-hidden="true" />],
    ["/templates", "提示词模板", <FileTextOutlined aria-hidden="true" />],
    ["/settings", "连接与设置", <SettingOutlined aria-hidden="true" />],
    ["/logs", "日志", <FileTextOutlined aria-hidden="true" />],
  ] as const;
  useEffect(() => {
    window.scrollTo(0, 0);
  }, [loc.pathname]);
  useEffect(() => {
    document.title = `${links.find((l) => l[0] === loc.pathname)?.[1] || "生图工作台"} · ImageWorkbench`;
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
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-mark">
            <PictureOutlined aria-hidden="true" />
          </div>
          <div>
            生图工作台<small>IMAGE WORKBENCH</small>
          </div>
        </div>
        <nav aria-label="主导航">
          {links.map(([path, label, icon]) => (
            <NavLink
              key={path}
              to={path}
              end
              aria-label={path === "/smart" ? label : undefined}
              aria-describedby={
                path === "/smart" ? "smart-progress-status" : undefined
              }
            >
              {icon}
              <span>{label}</span>
              {path === "/tasks" && !!summary.data?.active && (
                <span className="count">{summary.data.active}</span>
              )}
              {path === "/smart" && <SmartProgressRing />}
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <LicenseBadge />
          <div className="local-status">
            <CheckCircleOutlined aria-hidden="true" /> 本地数据工作区
          </div>
          <small>Portable · {summary.data?.version || "0.7.1"}</small>
        </div>
      </aside>
      <main className="main">
        <div className="topbar">
          <span>
            图片制作 / {links.find((l) => l[0] === loc.pathname)?.[1]}
          </span>
          <span className="privacy-note">图片与任务保存在本机</span>
        </div>
        <div className="page">
          <Routes>
            <Route path="/" element={<Workbench />} />
            <Route path="/smart" element={<SmartImages />} />
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
                  <NavLink to="/">返回工作台</NavLink>
                </Empty>
              }
            />
          </Routes>
        </div>
      </main>
    </div>
  );
}

function Workbench() {
  const navigate = useNavigate();
  const actions = useActions();
  const qc = useQueryClient();
  const downloads = useImageDownloads();
  const allConnections = useData<Connection[]>(
    ["connections"],
    "ListConnections",
  );
  const connections = {
    ...allConnections,
    data: allConnections.data?.filter((c) => c.kind !== "llm"),
  };
  const tasks = useData<RecentBatch[]>(
    ["tasks", "recent"],
    "ListRecentBatches",
    [6],
    2500,
  );
  const draft = useData<GenerateInput>(["draft"], "GetDraft");
  const [input, setInput] = useState<GenerateInput>(blankDraft);
  const [prompt, setPrompt] = useState("");
  const [batch, setBatch] = useState(false);
  const [ready, setReady] = useState(false);
  const [draftError, setDraftError] = useState("");
  const [formError, setFormError] = useState("");
  const promptRef = useRef<null | any>(null);
  const latestDraft = useRef<GenerateInput | null>(null);
  const [composing, setComposing] = useState(false);
  useEffect(() => {
    if (ready)
      latestDraft.current = {
        ...input,
        prompts: batch ? prompt.split("\n").filter((s) => s.trim()) : [prompt],
      };
  }, [input, prompt, batch, ready]);
  useEffect(
    () => () => {
      if (latestDraft.current)
        call("SetDraft", latestDraft.current).catch(() => {});
    },
    [],
  );
  useEffect(() => {
    if (draft.data && !ready) {
      setInput({ ...blankDraft, ...draft.data, projectID: "" });
      setPrompt((draft.data.prompts || []).join("\n"));
      setBatch((draft.data.prompts?.length || 0) > 1);
      setReady(true);
    }
  }, [draft.data]);
  useEffect(() => {
    if (!ready || composing) return;
    const timer = setTimeout(() => {
      call("SetDraft", {
        ...input,
        prompts: batch ? prompt.split("\n").filter((s) => s.trim()) : [prompt],
      })
        .then(() => setDraftError(""))
        .catch((e) => setDraftError(errorText(e)));
    }, 500);
    return () => clearTimeout(timer);
  }, [input, prompt, batch, ready, composing]);
  useEffect(() => {
    const off = window.runtime?.EventsOn("files:imported", (as: Asset[]) =>
      setInput((v) => ({
        ...v,
        references: [
          ...new Set([...v.references, ...as.map((a) => a.id)]),
        ].slice(0, 4),
      })),
    );
    return () => off?.();
  }, []);
  const gateways = useGateways();
  // A new draft starts on the primary gateway when one is set up.
  useEffect(() => {
    const primary = connections.data?.find(
      (c) => c.id === gateways.settings.data?.primaryID && c.hasKey,
    );
    if (ready && !input.connectionID && primary)
      setInput((v) =>
        v.connectionID ? v : { ...v, connectionID: primary.id, model: "" },
      );
  }, [ready, connections.data, gateways.settings.data]);
  const selected = connections.data?.find((c) => c.id === input.connectionID);
  const prompts = (batch ? prompt.split("\n") : [prompt])
    .map((s) => s.trim())
    .filter(Boolean);
  const total = prompts.length * input.count;
  function change(p: Partial<GenerateInput>) {
    setInput((v) => ({ ...v, ...p }));
  }
  async function generate() {
    if (!selected?.hasKey) {
      setFormError("请先选择已保存密钥的连接。");
      return;
    }
    if (!(input.model || selected.model)) {
      setFormError("请选择模型。");
      return;
    }
    if (!prompts.length) {
      setFormError("请填写提示词。");
      promptRef.current?.focus();
      return;
    }
    setFormError("");
    const requestID = crypto.randomUUID();
    actions.confirm(
      `生成 ${total} 张图片？`,
      <>
        将使用“{selected.name}”提交 {total} 个子任务。费用以中转站实际计费为准。
      </>,
      async () => {
        await call("Submit", {
          ...input,
          prompts,
          model: input.model || selected.model,
          requestID,
        });
        await qc.invalidateQueries({ queryKey: ["tasks"] });
      },
    );
  }
  function applyTemplate(t: Template, mode: TemplateMode) {
    if (mode === "replace") {
      setPrompt(t.prompt);
      // One template is one prompt: lines in it must not become separate images.
      if (t.prompt.includes("\n")) setBatch(false);
      change({
        ...(t.size ? { size: t.size } : {}),
        ...(t.quality ? { quality: t.quality } : {}),
      });
    } else {
      setPrompt((v) =>
        batch
          ? v.trimEnd() + "\n" + t.prompt.replace(/\s*\n\s*/g, " ")
          : v.trimEnd() + "\n\n" + t.prompt,
      );
    }
    promptRef.current?.focus();
  }
  async function addFiles() {
    const as = await actions.run(() => call<Asset[]>("ImportFiles"));
    if (as)
      change({
        references: [
          ...new Set([...input.references, ...as.map((a) => a.id)]),
        ].slice(0, 4),
      });
  }
  async function browserDrop(files: FileList) {
    for (const f of Array.from(files).slice(0, 4 - input.references.length)) {
      if (f.size > 32 * 1024 * 1024) {
        setFormError("图片超过 32 MB");
        continue;
      }
      const data = await f.arrayBuffer();
      let binary = "";
      for (const byte of new Uint8Array(data))
        binary += String.fromCharCode(byte);
      const asset = await actions.run(() =>
        call<Asset>("ImportImage", f.name, btoa(binary)),
      );
      if (asset)
        setInput((v) => ({
          ...v,
          references: [...v.references, asset.id].slice(0, 4),
        }));
    }
  }
  return (
    <>
      <LicenseNotice />
      <Header
        title="把想法变成图片"
        sub="输入提示词，选择连接与模型，结果自动归档到本地。"
        action={
          <Button
            icon={<SettingOutlined aria-hidden="true" />}
            onClick={() => navigate("/settings")}
          >
            管理连接
          </Button>
        }
      />
      {connections.error && (
        <Alert
          type="error"
          message="连接读取失败"
          description={errorText(connections.error)}
        />
      )}
      <div className="workbench">
        <section className="panel composer">
          <div className="panel-heading">
            <h2>生成内容</h2>
            <Space>
              <span>批量提示词</span>
              <Switch
                checked={batch}
                onChange={setBatch}
                aria-label="批量提示词"
              />
            </Space>
          </div>
          <div className="field-header prompt-header">
            <label className="field-label" htmlFor="prompt">
              {batch ? "提示词（每行一条）" : "提示词"}
            </label>
            <TemplateBar
              text={prompt}
              size={input.size}
              quality={input.quality}
              ready={ready}
              onApply={applyTemplate}
            />
          </div>
          <Input.TextArea
            ref={promptRef}
            id="prompt"
            onCompositionStart={() => setComposing(true)}
            onCompositionEnd={() => setComposing(false)}
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && e.ctrlKey && !e.nativeEvent.isComposing)
                generate();
            }}
            autoSize={{ minRows: 7, maxRows: 16 }}
            style={{ resize: "none" }}
            placeholder="例如：白色背景上的陶瓷咖啡杯，柔和自然光，突出釉面质感，电商产品摄影"
            aria-invalid={!!formError}
            aria-describedby="generate-error"
          />
          <div className="prompt-meta">
            <span>
              {prompt.length} 字 ·{" "}
              {batch ? "每行独立生成" : "文生图 / 添加参考图后转为图生图"}
            </span>
          </div>
          <div className="field-header">
            <label className="field-label">
              参考图 <span className="muted">可选 · 最多 4 张</span>
            </label>
            <Button
              size="small"
              icon={<UploadOutlined aria-hidden="true" />}
              onClick={addFiles}
              disabled={input.references.length >= 4}
              loading={actions.busy}
            >
              添加图片
            </Button>
          </div>
          <div
            className="reference-area"
            onDragOver={(e) => e.preventDefault()}
            onDrop={(e) => {
              e.preventDefault();
              if (e.dataTransfer.files.length)
                browserDrop(e.dataTransfer.files);
            }}
          >
            {input.references.length ? (
              input.references.map((id) => (
                <div className="reference" key={id}>
                  <AssetImage id={id} />
                  <Button
                    size="small"
                    icon={<CloseOutlined aria-hidden="true" />}
                    aria-label="移除参考图"
                    onClick={() =>
                      change({
                        references: input.references.filter((x) => x !== id),
                      })
                    }
                  />
                </div>
              ))
            ) : (
              <span className="muted">
                拖入 PNG、JPEG、WebP，或点击添加图片
              </span>
            )}
          </div>
          <div className="divider" />
          <div className="parameter-grid">
            <div>
              <label className="field-label">中转站连接</label>
              <UISelect
                aria-label="中转站连接"
                value={input.connectionID || undefined}
                placeholder="选择已配置连接"
                options={(connections.data || []).map((c) => ({
                  value: c.id,
                  label: gatewayLabel(
                    c,
                    gateways.settings.data,
                    gateways.byID.get(c.id),
                  ),
                }))}
                onChange={(id) => change({ connectionID: id, model: "" })}
              />
            </div>
            <div>
              <label className="field-label" htmlFor="model">
                模型
              </label>
              <ModelSelect
                id="model"
                connection={selected}
                value={input.model}
                onChange={(model) => change({ model })}
              />
            </div>
            <div>
              <label className="field-label" htmlFor="size">
                尺寸
              </label>
              <UISelect
                id="size"
                value={input.size}
                options={withCurrent(presetSizes, input.size)}
                onChange={(size) => change({ size })}
              />
            </div>
            <div>
              <label className="field-label" htmlFor="quality">
                质量
              </label>
              <UISelect
                id="quality"
                value={input.quality}
                options={withCurrent(presetQualities, input.quality)}
                onChange={(quality) => change({ quality })}
              />
            </div>
            <div>
              <label className="field-label" htmlFor="count">
                每条生成张数
              </label>
              <InputNumber
                id="count"
                value={input.count}
                min={1}
                max={20}
                onChange={(v) => change({ count: v || 1 })}
              />
            </div>
          </div>
          <p className="hint">选“自动”时不发送该参数；尺寸需由所选模型支持。</p>
          {formError && (
            <Alert
              id="generate-error"
              type="error"
              showIcon
              message={formError}
            />
          )}
          <div className="generate-footer">
            <span className="muted">
              {draftError
                ? "草稿未保存：" + draftError
                : ready
                  ? "草稿保存在本机 · Ctrl + Enter 生成"
                  : "正在读取草稿"}
            </span>
            <Button
              type="primary"
              size="large"
              icon={<ArrowRightOutlined aria-hidden="true" />}
              onClick={generate}
              disabled={!ready || !selected?.hasKey || total > 200}
              loading={actions.busy}
            >
              生成{total > 1 ? ` ${total} 张图片` : "图片"}
            </Button>
          </div>
          {!connections.isPending && !connections.data?.length && (
            <Alert
              showIcon
              type="info"
              message="先添加一个中转站连接"
              description="在应用设置里填写 Base URL 与 API Key。"
              action={
                <Button onClick={() => navigate("/settings")}>去配置</Button>
              }
            />
          )}
        </section>
        <section className="panel result-panel">
          <div className="panel-heading">
            <h2>最近生成</h2>
            <Button type="link" onClick={() => navigate("/tasks")}>
              全部任务
            </Button>
          </div>
          <DataState
            query={tasks}
            empty={!tasks.data?.length}
            emptyText="第一张图片，从这里开始"
          >
            <div className="recent-batches">
              {tasks.data?.map((batch) => (
                <section
                  className="recent-batch"
                  key={batch.id}
                  aria-label={`生成批次 ${batch.id.slice(0, 8)}`}
                >
                  <div className="batch-heading">
                    <div>
                      <strong>{time(batch.createdAt)}</strong>
                      <p className="hint">
                        已完成 {batch.completed} / {batch.total} 个任务 ·{" "}
                        {batch.imageCount} 张图片
                        {batch.active ? " · 仍在执行" : ""}
                      </p>
                    </div>
                    <Button
                      icon={<DownloadOutlined aria-hidden="true" />}
                      disabled={!batch.imageCount || downloads.busy}
                      onClick={() => downloads.downloadBatch(batch.id)}
                    >
                      {batch.completed === batch.total
                        ? `下载套图（${batch.imageCount} 张）`
                        : `下载已完成图片（${batch.imageCount} 张）`}
                    </Button>
                  </div>
                  <div className="recent-results">
                    {batch.tasks.map((t) => (
                      <div key={t.id} className="result-item">
                        {t.assetIDs.length ? (
                          <div>
                            {t.assetIDs.map((id) => (
                              <div key={id}>
                                <AssetImage id={id} />
                                <Button
                                  className="image-download"
                                  size="small"
                                  icon={<DownloadOutlined aria-hidden="true" />}
                                  disabled={downloads.busy}
                                  onClick={() => downloads.downloadAsset(id)}
                                >
                                  下载图片
                                </Button>
                              </div>
                            ))}
                          </div>
                        ) : (
                          <div className="result-placeholder">
                            <PictureOutlined aria-hidden="true" />
                            <Status state={t.status} task={t} />
                          </div>
                        )}
                        <div className="result-copy">
                          <Status state={t.status} task={t} />
                          <p>{t.prompt}</p>
                          <small>
                            {t.model} · {time(t.createdAt)}
                          </small>
                          <TaskTiming task={t} />
                          {t.error &&
                            (autoHandledNote(t) ? (
                              <p className="hint" title={t.error}>
                                {autoHandledNote(t)}
                              </p>
                            ) : (
                              <p className="error-text">{t.error}</p>
                            ))}
                        </div>
                      </div>
                    ))}
                  </div>
                  {batch.total > batch.tasks.length && (
                    <p className="hint">
                      预览 {batch.tasks.length}{" "}
                      个任务，套图下载包含此批次所有已保存的原图。
                    </p>
                  )}
                </section>
              ))}
            </div>
          </DataState>
        </section>
      </div>
    </>
  );
}

function Tasks() {
  const [status, setStatus] = useState("");
  const [page, setPage] = useState(1);
  const [selected, setDetail] = useState<Task | null>(null);
  const detailQuery = useQuery<Task>({
    queryKey: ["task", selected?.id],
    queryFn: () => call<Task>("GetTask", selected!.id),
    enabled: !!selected,
    refetchInterval: 2500,
  });
  const detail = selected ? detailQuery.data || selected : null;
  const q = useData<Page<Task>>(
    ["tasks", status, page],
    "ListTasks",
    [status, (page - 1) * 10, 10],
    2500,
  );
  const actions = useActions();
  useEffect(() => {
    if (page > 1 && q.data && q.data.total <= (page - 1) * 10)
      setPage(Math.max(1, Math.ceil(q.data.total / 10)));
  }, [q.data]);
  function retry(t: Task) {
    actions.confirm(
      "重新生成这张图片？",
      "这会提交新的计费请求。结果未知的任务请先核对平台记录。",
      () => call("Retry", t.id),
    );
  }
  return (
    <>
      <Header
        title="任务中心"
        sub="查看持久化队列、生成参数和结果。离开页面不会中断任务。"
      />
      <section className="panel">
        <div className="panel-heading">
          <h2>生成任务</h2>
          <UISelect
            aria-label="任务状态"
            value={status}
            onChange={(v) => {
              setStatus(v);
              setPage(1);
            }}
            style={{ width: 160 }}
            options={[
              { value: "", label: "全部状态" },
              ...Object.entries(stateNames).map(([value, label]) => ({
                value,
                label,
              })),
            ]}
          />
        </div>
        <DataState query={q}>
          <Table<Task>
            rowKey="id"
            loading={q.isFetching && q.isPending}
            dataSource={q.data?.items || []}
            scroll={{ x: 1000 }}
            pagination={{
              current: page,
              pageSize: 10,
              total: q.data?.total || 0,
              onChange: setPage,
              showSizeChanger: false,
              showTotal: (n) => `共 ${n} 个任务`,
            }}
            columns={[
              {
                title: "图片",
                width: 76,
                render: (_, t) =>
                  t.assetIDs[0] ? (
                    <div className="task-thumb">
                      <AssetImage id={t.assetIDs[0]} />
                    </div>
                  ) : (
                    <div className="task-thumb task-thumb-empty">
                      <PictureOutlined aria-hidden="true" />
                    </div>
                  ),
              },
              {
                title: "提示词",
                dataIndex: "prompt",
                ellipsis: { showTitle: false },
                width: 290,
                render: (_, t) => <span title={t.prompt}>{taskTitle(t)}</span>,
              },
              { title: "模型", dataIndex: "model", width: 130 },
              {
                title: "状态",
                dataIndex: "status",
                width: 190,
                render: (v, task) => (
                  <>
                    <Status state={v} task={task} />
                    <TaskTiming task={task} />
                  </>
                ),
              },
              {
                title: "创建时间",
                dataIndex: "createdAt",
                width: 120,
                render: time,
              },
              {
                title: "操作",
                width: 250,
                render: (_, t) => (
                  <Space wrap>
                    <Button size="small" onClick={() => setDetail(t)}>
                      详情
                    </Button>
                    <CancelTaskButton task={t} size="small" />
                    {[
                      "queued",
                      "submitting",
                      "downloading",
                      "blocked",
                    ].includes(t.status) ? (
                      <Button
                        size="small"
                        onClick={() =>
                          actions.run(
                            () => call("StopBatch", t.batchID),
                            "已停止待执行项",
                          )
                        }
                      >
                        停止队列
                      </Button>
                    ) : (
                      <Button size="small" onClick={() => retry(t)}>
                        重新生成
                      </Button>
                    )}
                    {t.status === "download_failed" && (
                      <Button
                        size="small"
                        onClick={() =>
                          actions.run(
                            () => call("RetryDownload", t.id),
                            "已加入补下载队列",
                          )
                        }
                      >
                        补下载
                      </Button>
                    )}
                    {t.status === "blocked" && (
                      <Button
                        size="small"
                        onClick={() =>
                          actions.run(
                            () => call("ResumeBlocked", t.id),
                            "未提交任务已恢复",
                          )
                        }
                      >
                        恢复
                      </Button>
                    )}
                  </Space>
                ),
              },
            ]}
            locale={{ emptyText: <Empty description="还没有生成任务" /> }}
          />
        </DataState>
      </section>
      <Modal
        title="任务详情"
        open={!!detail}
        footer={<Button onClick={() => setDetail(null)}>关闭</Button>}
        onCancel={() => setDetail(null)}
        width={740}
      >
        {detail && (
          <>
            <Status state={detail.status} task={detail} />
            <TaskTiming task={detail} />
            <dl className="details">
              <dt>连接</dt>
              <dd>
                {detail.connection.name} · {detail.connection.baseURL}
              </dd>
              <dt>模型</dt>
              <dd>{detail.model}</dd>
              <dt>参数</dt>
              <dd>
                尺寸 {detail.size || "auto"} · 质量 {detail.quality || "auto"}
              </dd>
              <dt>提示词</dt>
              <dd className="prewrap">{detail.prompt}</dd>
              <dt>任务标识</dt>
              <dd className="mono">{detail.id}</dd>
              <dt>提交时间</dt>
              <dd>{time(detail.createdAt)}</dd>
              {!!detail.diagnostic?.startedAt && (
                <>
                  <dt>请求地址</dt>
                  <dd className="mono">{detail.diagnostic.requestURL}</dd>
                  <dt>响应状态</dt>
                  <dd>
                    {detail.diagnostic.httpStatus
                      ? `HTTP ${detail.diagnostic.httpStatus}`
                      : "尚未收到响应状态"}
                  </dd>
                  {!!detail.diagnostic.contentType && (
                    <>
                      <dt>响应类型</dt>
                      <dd>{detail.diagnostic.contentType}</dd>
                    </>
                  )}
                  {!!detail.diagnostic.httpProtocol && (
                    <>
                      <dt>传输协议</dt>
                      <dd>
                        {detail.diagnostic.httpProtocol}
                        {detail.diagnostic.transferEncoding
                          ? ` · ${detail.diagnostic.transferEncoding}`
                          : ""}
                      </dd>
                      <dt>声明长度</dt>
                      <dd>
                        {detail.diagnostic.contentLength! >= 0
                          ? `${detail.diagnostic.contentLength?.toLocaleString()} 字节`
                          : "平台未声明响应长度"}
                      </dd>
                    </>
                  )}
                  {!!detail.diagnostic.networkRoute && (
                    <>
                      <dt>网络连接</dt>
                      <dd>
                        {detail.diagnostic.networkRoute === "proxy"
                          ? "通过代理连接"
                          : "直接连接"}
                      </dd>
                    </>
                  )}
                  {!!detail.diagnostic.upstreamRequestID && (
                    <>
                      <dt>平台请求标识</dt>
                      <dd className="mono">
                        {detail.diagnostic.upstreamRequestID}
                      </dd>
                    </>
                  )}
                  <dt>已接收</dt>
                  <dd>
                    {detail.diagnostic.bytesReceived.toLocaleString()} 字节
                  </dd>
                  {!!detail.diagnostic.networkError && (
                    <>
                      <dt>中断原因</dt>
                      <dd className="mono">{detail.diagnostic.networkError}</dd>
                    </>
                  )}
                </>
              )}
            </dl>
            {detailQuery.isError && (
              <Alert
                type="warning"
                message="详情刷新失败，当前显示最近记录。"
              />
            )}
            <TaskFailureAlert task={detail} />
            <div className="asset-grid">
              {detail.assetIDs.map((id) => (
                <AssetImage key={id} id={id} thumbnail={false} />
              ))}
            </div>
          </>
        )}
      </Modal>
    </>
  );
}

function Assets() {
  const downloads = useImageDownloads();
  const [detail, setDetail] = useState<Asset | null>(null);
  const pages = useInfiniteQuery({
    queryKey: ["assets"],
    initialPageParam: 0,
    queryFn: ({ pageParam }) =>
      call<Page<Asset>>("ListAssets", "", pageParam, 24),
    getNextPageParam: (last, all) => {
      const count = all.reduce((sum, p) => sum + p.items.length, 0);
      return count < last.total ? count : undefined;
    },
  });
  const q = {
    ...pages,
    data: pages.data
      ? {
          items: pages.data.pages.flatMap((p) => p.items),
          total: pages.data.pages[0].total,
        }
      : undefined,
  };
  const actions = useActions();
  const { notification } = AntApp.useApp();
  const nav = useNavigate();
  // Batch mode: clicking a card selects it. Only loaded, still-listed images count,
  // in list order, so removed images drop out by themselves.
  const [selecting, setSelecting] = useState(false);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const items = q.data?.items || [];
  const chosen = items.filter((a) => picked.has(a.id));
  const ids = chosen.map((a) => a.id);
  const allPicked = items.length > 0 && chosen.length === items.length;
  function toggle(id: string) {
    setPicked((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }
  function stopSelecting() {
    setSelecting(false);
    setPicked(new Set());
  }
  async function exportChosen() {
    await actions.run(async () => {
      const path = await call<string>(
        "ChooseExportPath",
        `素材-${ids.length}张.zip`,
      );
      if (!path) return;
      const count = await call<number>("ExportAssetsToPath", ids, path);
      notification.success({ message: `已导出素材 ZIP（${count} 张）` });
    });
  }
  async function useReference(a: Asset) {
    await actions.run(async () => {
      const draft = await call<GenerateInput>("GetDraft");
      await call("SetDraft", {
        ...blankDraft,
        ...draft,
        references: [...new Set([...(draft.references || []), a.id])].slice(
          0,
          4,
        ),
      });
      nav("/");
    });
  }
  return (
    <>
      <Header
        title="素材库"
        sub="生成原图与参考图保存在本机，随时预览、导出或再次使用。"
      />
      <section className="panel">
        <div className="panel-heading">
          <h2>{q.data?.total || 0} 张本地图片</h2>
          <Space wrap>
            {!selecting && (
              <Button
                icon={<CheckSquareOutlined aria-hidden="true" />}
                disabled={!items.length}
                onClick={() => setSelecting(true)}
              >
                批量管理
              </Button>
            )}
          </Space>
        </div>
        {selecting && (
          <div
            className="asset-batch-bar"
            role="toolbar"
            aria-label="批量管理素材"
          >
            <strong aria-live="polite">已选 {chosen.length} 张</strong>
            <Button
              size="small"
              onClick={() =>
                setPicked(
                  allPicked ? new Set() : new Set(items.map((a) => a.id)),
                )
              }
            >
              {allPicked ? "取消全选" : `全选已加载（${items.length} 张）`}
            </Button>
            <Button
              size="small"
              icon={<DownloadOutlined aria-hidden="true" />}
              disabled={!chosen.length || actions.busy}
              onClick={exportChosen}
            >
              导出 ZIP
            </Button>
            <Button
              size="small"
              danger
              icon={<DeleteOutlined aria-hidden="true" />}
              disabled={!chosen.length || actions.busy}
              onClick={() =>
                actions.confirm(
                  `从素材库移除 ${ids.length} 张图片？`,
                  "图片会从列表隐藏，文件和任务引用仍保留。",
                  async () => {
                    await call("UpdateAssets", ids, "", true);
                    setPicked(new Set());
                  },
                  true,
                )
              }
            >
              移除
            </Button>
            <Button size="small" type="text" onClick={stopSelecting}>
              完成
            </Button>
          </div>
        )}
        <DataState
          query={q}
          empty={!q.data?.items.length}
          emptyText="还没有本地图片"
          action={
            <Button type="primary" onClick={() => nav("/")}>
              去生成图片
            </Button>
          }
        >
          <div className="asset-grid">
            {q.data?.items.map((a) => (
              <article
                className={
                  "asset-card" + (picked.has(a.id) ? " asset-card-picked" : "")
                }
                key={a.id}
              >
                {selecting ? (
                  <button
                    className="image-button asset-pick"
                    onClick={() => toggle(a.id)}
                    aria-label={`选择 ${assetLabel(a)}`}
                    aria-pressed={picked.has(a.id)}
                  >
                    <AssetImage id={a.id} />
                    <span className="asset-pick-box" aria-hidden="true">
                      {picked.has(a.id) && <CheckOutlined />}
                    </span>
                  </button>
                ) : (
                  <button
                    className="image-button"
                    onClick={() => setDetail(a)}
                    aria-label={`预览 ${assetLabel(a)}`}
                  >
                    <AssetImage id={a.id} />
                  </button>
                )}
                <div className="asset-card-copy">
                  <strong title={a.name}>
                    {a.kind === "reference" ? a.name : time(a.createdAt)}
                  </strong>
                  <small>
                    {a.width} × {a.height} ·{" "}
                    {a.kind === "reference" ? "参考图" : "生成图片"}
                  </small>
                  <Space>
                    <Button size="small" onClick={() => useReference(a)}>
                      作为参考图
                    </Button>
                    <Tooltip title="导出原图">
                      <Button
                        size="small"
                        icon={<DownloadOutlined aria-hidden="true" />}
                        aria-label="导出原图"
                        onClick={() =>
                          actions.run(
                            () =>
                              call<boolean>(
                                "ExportAsset",
                                a.id,
                                a.path.split("/").pop(),
                              ),
                            undefined,
                          )
                        }
                      />
                    </Tooltip>
                  </Space>
                </div>
              </article>
            ))}
          </div>
          {q.data && q.data.items.length < q.data.total && (
            <div className="load-more">
              <Button
                onClick={() => pages.fetchNextPage()}
                loading={q.isFetching}
              >
                加载更多
              </Button>
            </div>
          )}
        </DataState>
      </section>
      <Modal
        title="图片详情"
        open={!!detail}
        onCancel={() => setDetail(null)}
        footer={null}
        width={800}
      >
        {detail && (
          <>
            <AssetImage id={detail.id} thumbnail={false} />
            <p>
              {assetLabel(detail)} · {detail.width} × {detail.height}
            </p>
            <Space wrap>
              <Button
                icon={<DownloadOutlined aria-hidden="true" />}
                disabled={downloads.busy}
                onClick={() => downloads.downloadAsset(detail.id)}
              >
                导出原图
              </Button>
              <Button
                danger
                icon={<DeleteOutlined aria-hidden="true" />}
                onClick={() =>
                  actions.confirm(
                    "从素材库移除？",
                    "图片会从列表隐藏，文件和任务引用仍保留。",
                    async () => {
                      await call(
                        "UpdateAsset",
                        detail.id,
                        detail.projectID,
                        true,
                      );
                      setDetail(null);
                    },
                    true,
                  )
                }
              >
                移除
              </Button>
            </Space>
          </>
        )}
      </Modal>
    </>
  );
}

function Settings() {
  const q = useData<Connection[]>(["connections"], "ListConnections");
  const summary = useData<Summary>(["summary"], "Dashboard");
  const license = useLicense();
  const customer = !!license.data && license.data.mode !== "developer";
  const actions = useActions();
  const { notification } = AntApp.useApp();
  const [moving, setMoving] = useState(false);
  async function chooseLocation() {
    const destination = await actions.run(() =>
      call<string>("ChooseDataDirectory"),
    );
    if (!destination) return;
    actions.confirm(
      "更改保存位置？",
      <div>
        <p>
          现有图片、任务和配置会一起迁移，原目录保留。新目录必须是空文件夹。
        </p>
        <p className="hint">当前目录</p>
        <code className="data-path">{summary.data?.dataDir}</code>
        <p className="hint">新目录</p>
        <code className="data-path">{destination}</code>
      </div>,
      async () => {
        setMoving(true);
        try {
          await call("ChangeDataDirectory", destination);
        } finally {
          setMoving(false);
        }
      },
      false,
      "迁移并使用",
    );
  }
  const [editing, setEditing] = useState<Connection | null | undefined>(
    undefined,
  );
  const [form] = UIForm.useForm();
  const [result, setResult] = useState<TestResult | null>(null);
  const [testing, setTesting] = useState(false);
  const [testError, setTestError] = useState("");
  const testRun = useRef<string | null>(null);
  function cancelTest() {
    const id = testRun.current;
    testRun.current = null;
    setTesting(false);
    if (id) call("CancelConnectionTest", id).catch(() => {});
  }
  useEffect(
    () => () => {
      if (testRun.current)
        call("CancelConnectionTest", testRun.current).catch(() => {});
    },
    [],
  );
  function open(c: Connection | null) {
    setEditing(c);
    form.resetFields();
    form.setFieldsValue(
      c ? { ...c, key: "" } : { name: "", baseURL: "", model: "", key: "" },
    );
    setResult(null);
    setTestError("");
  }
  async function save() {
    try {
      const values = await form.validateFields();
      const saved = await actions.run(
        () =>
          call<Connection>("SaveConnection", {
            ...values,
            id: editing?.id || "",
            clearKey: false,
          }),
        "连接已保存",
      );
      if (saved) {
        setEditing(undefined);
        form.resetFields();
      }
    } catch {
      form.scrollToField("baseURL");
    }
  }
  async function test() {
    if (testRun.current) return;
    const requestID = crypto.randomUUID();
    testRun.current = requestID;
    setTesting(true);
    setResult(null);
    setTestError("");
    try {
      const v = await form.validateFields(["baseURL", "key"]);
      if (testRun.current !== requestID) return;
      const out = await call<TestResult>("TestConnection", {
        ...form.getFieldsValue(),
        ...v,
        id: editing?.id || "",
        testRequestID: requestID,
      });
      if (testRun.current === requestID) setResult(out);
    } catch (e) {
      if (testRun.current !== requestID) return;
      if (e && typeof e === "object" && "errorFields" in e) {
        const fields = (e as { errorFields: { name: string[] }[] }).errorFields;
        if (fields.length) form.scrollToField(fields[0].name, { focus: true });
      } else setTestError(errorText(e));
    } finally {
      if (testRun.current === requestID) {
        testRun.current = null;
        setTesting(false);
      }
    }
  }
  function close() {
    if (testing) return;
    if (form.isFieldsTouched())
      actions.confirm(
        "放弃未保存的连接修改？",
        "API Key 仅在点击保存后持久化。",
        async () => {
          setEditing(undefined);
          form.resetFields();
        },
      );
    else {
      setEditing(undefined);
      form.resetFields();
    }
  }
  return (
    <>
      <Header
        title="连接与设置"
        sub={
          customer
            ? "授权、线路状态和本地数据。线路由软件提供方配置，无需填写地址或 Key。"
            : "在应用里配置 Base URL 和 API Key，自由切换兼容的中转站。"
        }
        action={
          customer ? undefined : (
            <Button
              type="primary"
              icon={<PlusOutlined aria-hidden="true" />}
              onClick={() => open(null)}
            >
              添加连接
            </Button>
          )
        }
      />
      <LicensePanel />
      <section className="panel">
        <div className="panel-heading">
          <h2>
            <ApiOutlined aria-hidden="true" /> 生图中转站
          </h2>
          <span className="muted">Images 兼容协议</span>
        </div>
        <DataState
          query={q}
          empty={!q.data?.some((c) => c.kind !== "llm")}
          emptyText="添加你的第一个中转站"
          action={
            <Button type="primary" onClick={() => open(null)}>
              填写 Base URL 与 API Key
            </Button>
          }
        >
          <div className="connection-list">
            {q.data
              ?.filter((c) => c.kind !== "llm")
              .map((c) => (
                <div className="connection-row" key={c.id}>
                  <div className="connection-icon">
                    <ApiOutlined aria-hidden="true" />
                  </div>
                  <div className="connection-info">
                    <strong>
                      {c.name}{" "}
                      <Tag color={c.hasKey ? "success" : "warning"}>
                        {c.hasKey ? "密钥已保存" : "需要密钥"}
                      </Tag>
                    </strong>
                    {c.managed ? (
                      <p className="hint">由软件授权提供</p>
                    ) : (
                      <p className="mono">{c.baseURL}</p>
                    )}
                    <small>
                      默认模型：{c.model || "未设置，可在工作台填写"}
                    </small>
                    <GatewayRow
                      connection={c}
                      images={(q.data || []).filter((x) => x.kind !== "llm")}
                    />
                  </div>
                  {c.managed ? (
                    <Tag>由软件授权管理</Tag>
                  ) : (
                    <Space wrap>
                      <Button onClick={() => open(c)}>编辑 / 测试</Button>
                      {c.hasKey && (
                        <Button
                          onClick={() =>
                            actions.confirm(
                              "清除此连接的密钥？",
                              "新任务将无法提交。已提交任务保存的加密凭证版本仍用于恢复；密钥不会从任务中返回到界面。",
                              () =>
                                call("SaveConnection", {
                                  ...c,
                                  key: "",
                                  clearKey: true,
                                }),
                            )
                          }
                        >
                          清除密钥
                        </Button>
                      )}
                      <Button
                        danger
                        aria-label={`删除连接 ${c.name}`}
                        icon={<DeleteOutlined aria-hidden="true" />}
                        onClick={() =>
                          actions.confirm(
                            `删除连接“${c.name}”？`,
                            "需要先停止队列。历史任务与图片保留，重新生成需要重新配置连接。",
                            () => call("DeleteConnection", c.id),
                            true,
                          )
                        }
                      />
                    </Space>
                  )}
                </div>
              ))}
          </div>
        </DataState>
      </section>
      <GatewayFailover />
      <LLMConnections />
      <AgentSettings />
      <section className="panel storage-panel">
        <div className="panel-heading">
          <h2>本地数据与备份</h2>
          <Tag>本机存储</Tag>
        </div>
        <p>数据位置</p>
        <code className="data-path">{summary.data?.dataDir || "正在读取"}</code>
        <Space wrap className="location-actions">
          <Button
            icon={<FolderOutlined aria-hidden="true" />}
            disabled={!summary.data || moving}
            loading={actions.busy}
            onClick={() =>
              actions.run(() => call("OpenDataDirectory"), "已打开数据目录")
            }
          >
            打开数据位置
          </Button>
          <Button
            icon={<SettingOutlined aria-hidden="true" />}
            disabled={
              !summary.data || !!summary.data.active || moving || actions.busy
            }
            loading={moving}
            onClick={chooseLocation}
          >
            更改保存位置
          </Button>
        </Space>
        <p className="hint">
          可以自选空文件夹作为保存位置，现有数据会一起迁移。新位置立即生效，重启后继续使用。
        </p>
        <p className="hint">
          备份包含模板、任务与图片，不包含 API Key
          或界面缓存。换电脑后重新输入密钥。
        </p>
        <Space wrap>
          <Button
            icon={<DownloadOutlined aria-hidden="true" />}
            loading={actions.busy}
            disabled={moving}
            onClick={() =>
              actions.run(async () => {
                const saved = await call<boolean>("Backup");
                if (saved) return "saved";
                return undefined;
              }, undefined)
            }
          >
            创建备份
          </Button>
          <Button
            icon={<ReloadOutlined aria-hidden="true" />}
            disabled={!!summary.data?.active || moving}
            onClick={() =>
              actions.confirm(
                "从备份恢复数据？",
                "当前数据会保留在恢复前目录。任务和图片将以备份为准，密钥需要重新输入。请先停止并结束活动任务。",
                () => call("Restore"),
                true,
              )
            }
          >
            恢复备份
          </Button>
        </Space>
        <p className="hint">
          商品链接被网站拦截时，智能生图会用应用内浏览器读取页面，Cookie
          保存在当前数据目录，不进入备份。清除后，下次读取可能需要重新完成验证。
        </p>
        <Space wrap>
          <Button
            icon={<DeleteOutlined aria-hidden="true" />}
            disabled={moving}
            onClick={() =>
              actions.confirm(
                "清除商品页浏览器数据？",
                "将删除应用内浏览器保存的 Cookie 与缓存，不影响图片、任务、连接和密钥。",
                async () => {
                  await call("ClearProductBrowserData");
                  notification.success({ message: "已清除商品页浏览器数据" });
                },
                true,
                "清除",
              )
            }
          >
            清除商品页浏览器数据
          </Button>
        </Space>
        {!!summary.data?.active && (
          <p className="hint">
            当前有活动或等待任务，完成或停止后才能更改保存位置或恢复备份。
          </p>
        )}
      </section>
      <Modal
        title={editing ? "编辑中转站连接" : "添加中转站连接"}
        open={editing !== undefined}
        onCancel={close}
        onOk={save}
        okText="保存连接"
        cancelText="取消"
        confirmLoading={actions.busy}
        okButtonProps={{ disabled: testing }}
        maskClosable={!testing}
        width={640}
        destroyOnHidden
      >
        <UIForm form={form} layout="vertical" noValidate disabled={testing}>
          <ConnectionFields editing={editing} />
          <div className="test-row">
            <Button
              icon={<ApiOutlined aria-hidden="true" />}
              onClick={test}
              loading={testing}
            >
              测试连接 / 获取模型
            </Button>
            {testing && (
              <Button disabled={false} onClick={cancelTest}>
                取消测试
              </Button>
            )}
            <span className="muted">不会提交生图任务</span>
          </div>
          {testing && (
            <p role="status" className="hint">
              正在连接并获取模型，最多等待 30 秒。可以取消测试。
            </p>
          )}
          {testError && <Alert type="error" showIcon message={testError} />}{" "}
          {result && (
            <div className="test-result">
              <Alert
                type={result.ids.length ? "success" : "warning"}
                showIcon
                message={result.auth}
                description={
                  <>
                    {result.models}
                    <br />
                    {result.generation}
                  </>
                }
              />
              {!!result.ids.length && (
                <UISelect
                  className="full-width"
                  aria-label="查询到的模型"
                  showSearch
                  optionFilterProp="label"
                  placeholder="选择一个默认模型"
                  options={result.ids.map((id) => ({ value: id, label: id }))}
                  onChange={(id) => form.setFieldValue("model", id)}
                />
              )}
            </div>
          )}
        </UIForm>
      </Modal>
    </>
  );
}
