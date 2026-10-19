import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  Button,
  Collapse,
  Empty,
  Input,
  InputNumber,
  Modal,
  Pagination,
  Select as UISelect,
  Space,
  Tag,
} from "antd";
import {
  DownloadOutlined,
  PlusOutlined,
  UploadOutlined,
  DeleteOutlined,
} from "@ant-design/icons";
import { Link } from "react-router-dom";
import {
  call,
  errorText,
  type Asset,
  type Connection,
  type Project,
  type Task,
} from "./api";
import {
  blankAgentDraft,
  type AgentInput,
  type AgentPage,
  type AgentRun,
  type AgentRuntimeInfo,
  type SmartProgress,
} from "./agent-api";
import {
  AssetImage,
  DataState,
  Header,
  ImageViewer,
  Status,
  TaskTiming,
  TaskFailureAlert,
  time,
  useActions,
  useData,
  useImageDownloads,
  type ViewerItem,
} from "./ui";

export function extractProductLinks(text: string): string[] {
  const values = text.match(/https?:\/\/[^\s\]\)\\<>"'，。；]+/g) || [];
  return [
    ...new Set(
      values.filter((value) => {
        try {
          const u = new URL(value);
          return !!u.hostname && !u.username && !u.password;
        } catch {
          return false;
        }
      }),
    ),
  ];
}

function ReferenceStrip({
  ids,
  remove,
}: {
  ids: string[];
  remove?: (id: string) => void;
}) {
  return (
    <div className="agent-references">
      {ids.map((id) => (
        <div className="agent-reference" key={id}>
          <AssetImage id={id} />
          <small className="mono">{id.slice(0, 8)}</small>
          {remove && (
            <Button
              size="small"
              type="text"
              aria-label={`移除参考图 ${id.slice(0, 8)}`}
              icon={<DeleteOutlined aria-hidden="true" />}
              onClick={() => remove(id)}
            />
          )}
        </div>
      ))}
    </div>
  );
}
function AgentTiming({ run, tasks }: { run: AgentRun; tasks: Task[] }) {
  const [now, setNow] = useState(Date.now());
  const busy =
    run.status === "analyzing" ||
    (!!run.batchID &&
      (tasks.length === 0 ||
        tasks.some((t) =>
          ["queued", "submitting", "running", "downloading"].includes(t.status),
        )));
  useEffect(() => {
    if (!busy) return;
    const interval = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(interval);
  }, [busy, run.id]);
  const start = Date.parse(run.createdAt);
  const savedEnd = Math.max(
    Date.parse(run.updatedAt || run.createdAt),
    ...tasks.map((t) => Date.parse(t.updatedAt || t.createdAt)),
  );
  const elapsed = Math.max(
    0,
    Math.floor(((busy ? now : savedEnd) - start) / 1000),
  );
  return (
    <span className="muted">
      {busy ? "已用时" : "累计用时"} {Math.floor(elapsed / 60)} 分{" "}
      {elapsed % 60} 秒
    </span>
  );
}

// Stage texts written by the Go product browser (internal/core/product_browser.go).
const browserReadingStage = "正在用浏览器读取商品页";
const browserVerifyStage = "需要你在弹出的窗口里完成验证";

// Counts from the saved verification event, outside the live region so screen
// readers hear the stage once instead of every second.
function VerificationWait({ run }: { run: AgentRun }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const interval = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(interval);
  }, []);
  const event = [...run.events]
    .reverse()
    .find((e) => e.text === browserVerifyStage);
  const since = Date.parse(event?.at || run.updatedAt);
  const seconds = Math.max(0, Math.floor((now - since) / 1000));
  return (
    <p className="muted">
      请在弹出的“完成验证后将自动继续”窗口中完成网站验证，完成后自动继续；关闭窗口将改为上传图片或粘贴描述。等待验证中（已{" "}
      {seconds} 秒）
    </p>
  );
}

// Common OpenAI-compatible sizes; a previously saved custom size stays selectable.
const presetSizes = [
  { value: "auto", label: "自动（由模型决定）" },
  { value: "1024x1024", label: "1024×1024（1:1 方图）" },
  { value: "1536x1024", label: "1536×1024（3:2 横图）" },
  { value: "1024x1536", label: "1024×1536（2:3 竖图）" },
  { value: "1792x1024", label: "1792×1024（16:9 横图）" },
  { value: "1024x1792", label: "1024×1792（9:16 竖图）" },
];
const ratioSizes: Record<string, string> = {
  "1:1": "1024x1024",
  "3:2": "1536x1024",
  "2:3": "1024x1536",
  "16:9": "1792x1024",
};

// The backend marks images the relay refused for the API key (HTTP 401/403).
function isKeyRejected(task: Task) {
  return task.status === "failed" && !!task.error?.startsWith("API Key 无效");
}

type RefineTarget = { task: Task; assetID: string };

// Refinement is a new, possibly billable edit request based on one finished
// image; the dialog states the cost and the request ID makes retries idempotent.
function RefineDialog({
  target,
  onClose,
}: {
  target: RefineTarget | null;
  onClose: () => void;
}) {
  const actions = useActions();
  const [text, setText] = useState("");
  const requestID = useRef("");
  useEffect(() => {
    if (target) {
      setText("");
      requestID.current = crypto.randomUUID();
    }
  }, [target]);
  async function submit() {
    if (!target || !text.trim()) return;
    const batch = await actions.run(
      () =>
        call<string>("RefineAgentImage", {
          requestID: requestID.current,
          taskID: target.task.id,
          assetID: target.assetID,
          instruction: text.trim(),
        }),
      "已提交优化，完成后显示为这张图片的新版本",
    );
    if (batch !== undefined) onClose();
  }
  return (
    <Modal
      title="优化这张图片"
      open={!!target}
      onCancel={onClose}
      onOk={submit}
      okText="开始优化"
      cancelText="取消"
      okButtonProps={{ disabled: !text.trim() }}
      confirmLoading={actions.busy}
      destroyOnHidden
    >
      {target && (
        <div className="refine-preview">
          <AssetImage id={target.assetID} />
        </div>
      )}
      <label htmlFor="refine-instruction" className="field-label">
        想怎么调整？
      </label>
      <Input.TextArea
        id="refine-instruction"
        value={text}
        onChange={(e) => setText(e.target.value)}
        maxLength={1000}
        showCount
        autoSize={{ minRows: 3, maxRows: 8 }}
        placeholder="例如：袖口名字改成横排，与其他图片一致；其余保持不变"
      />
      <p className="hint">
        将以这张图片为底图发起一次新的生图请求，可能产生费用。只修改你写的内容，原图保留在“之前的版本”中。
      </p>
    </Modal>
  );
}

export function SmartImages() {
  const actions = useActions(),
    downloads = useImageDownloads(),
    qc = useQueryClient();
  const [refining, setRefining] = useState<RefineTarget | null>(null);
  const concurrency = useData<number>(
    ["image-concurrency"],
    "ImageConcurrency",
  );
  const connections = useData<Connection[]>(["connections"], "ListConnections");
  const projects = useData<Project[]>(["projects"], "ListProjects");
  const runtime = useData<AgentRuntimeInfo>(["agent-runtime"], "AgentRuntime");
  const assets = useData<{ items: Asset[] }>(
    ["agent-assets"],
    "ListAssets",
    ["", 0, 100],
    4000,
  );
  const draft = useData<AgentInput>(["agent-draft"], "GetAgentDraft");
  const [input, setInput] = useState<AgentInput>(blankAgentDraft);
  const [ready, setReady] = useState(false),
    [error, setError] = useState(""),
    [draftError, setDraftError] = useState("");
  const [page, setPage] = useState(1),
    [runID, setRunID] = useState("");
  const [answers, setAnswers] = useState<Record<string, string>>({}),
    [productText, setProductText] = useState("");
  const [advanced, setAdvanced] = useState<string[]>([]);
  const history = useData<AgentPage>(
    ["agent-runs", page],
    "ListAgentRuns",
    [(page - 1) * 10, 10],
    3000,
  );
  const runQuery = useQuery({
    queryKey: ["agent-run", runID],
    queryFn: () => call<AgentRun>("GetAgentRun", runID),
    enabled: !!runID,
    refetchInterval: 2000,
  });
  const taskQuery = useQuery({
    queryKey: ["agent-tasks", runID],
    queryFn: () => call<Task[]>("GetAgentTasks", runID),
    enabled: !!runID,
    refetchInterval: 2500,
  });
  const run = runQuery.data,
    tasks = taskQuery.data || [];
  const initialized = useRef(""),
    autoOpen = useRef(false),
    autoURL = useRef(""),
    startRequest = useRef<string | null>(null),
    continuationRequest = useRef<string | null>(null);
  const latestDraft = useRef<AgentInput | null>(null);
  useEffect(() => {
    if (!ready && draft.data) {
      setInput({
        ...blankAgentDraft,
        ...draft.data,
        references: draft.data.references || [],
        skillIDs: draft.data.skillIDs || [],
      });
      setReady(true);
    }
  }, [draft.data, ready]);
  // Returning to this page reopens the newest run while it is unfinished
  // (analyzing, waiting for input, generating or paused); otherwise a new request.
  useEffect(() => {
    if (autoOpen.current) return;
    autoOpen.current = true;
    call<SmartProgress>("CurrentSmartProgress")
      .then((p) => {
        if (p.runID) setRunID((current) => current || p.runID);
      })
      .catch(() => {});
  }, []);
  useEffect(() => {
    if (run && initialized.current !== run.id) {
      initialized.current = run.id;
      setInput(run.input);
      setProductText(run.input.productText);
      setAnswers(
        Object.fromEntries(run.questions.map((q) => [q.id, q.answer || ""])),
      );
    }
  }, [run]);
  useEffect(() => {
    const off = window.runtime?.EventsOn("agent:changed", () => {
      qc.invalidateQueries({ queryKey: ["agent-run"] });
      qc.invalidateQueries({ queryKey: ["agent-runs"] });
      qc.invalidateQueries({ queryKey: ["agent-tasks"] });
    });
    return () => off?.();
  }, [qc]);
  useEffect(() => {
    if (!ready || runID) return;
    latestDraft.current = input;
    const t = setTimeout(() => {
      call("SetAgentDraft", input)
        .then(() => setDraftError(""))
        .catch((e) => setDraftError(errorText(e)));
    }, 500);
    return () => clearTimeout(t);
  }, [input, ready, runID]);
  useEffect(
    () => () => {
      if (latestDraft.current)
        call("SetAgentDraft", latestDraft.current).catch(() => {});
    },
    [],
  );
  useEffect(() => {
    if (!ready || runID || !connections.data) return;
    setInput((v) => ({
      ...v,
      llmConnectionID:
        v.llmConnectionID ||
        connections.data?.find((c) => c.kind === "llm" && c.hasKey)?.id ||
        "",
      imageConnectionID:
        v.imageConnectionID ||
        connections.data?.find((c) => c.kind !== "llm" && c.hasKey)?.id ||
        "",
    }));
  }, [ready, runID, connections.data]);
  const llms = connections.data?.filter((c) => c.kind === "llm") || [],
    images = connections.data?.filter((c) => c.kind !== "llm") || [];
  const imageConnection = run?.imageConnection?.id
    ? run.imageConnection
    : images.find((c) => c.id === input.imageConnectionID);
  const llmConnection =
    run?.autoGeneration && run?.llmConnection?.id
      ? run.llmConnection
      : llms.find((c) => c.id === input.llmConnectionID);
  const model = input.imageModel || imageConnection?.model || "未设置";
  // Model list is a GET /models of the saved connection (no generation, no cost),
  // fetched only while the settings panel is open.
  const imageModels = useQuery({
    queryKey: ["connection-models", imageConnection?.id],
    queryFn: () => call<string[]>("ListConnectionModels", imageConnection!.id),
    enabled: !!imageConnection?.id && advanced.includes("settings") && !run,
    staleTime: 5 * 60 * 1000,
    retry: false,
  });
  const modelOptions = [
    {
      value: "",
      label: `使用连接默认（${imageConnection?.model || "未设置"}）`,
    },
    ...[
      ...new Set([
        ...(imageModels.data || []),
        ...(input.imageModel ? [input.imageModel] : []),
      ]),
    ].map((value) => ({ value, label: value })),
  ];
  // Viewer walks the set in slot order: each slot's current image, then its earlier versions.
  const [viewerIndex, setViewerIndex] = useState<number | null>(null);
  const viewerItems: ViewerItem[] = [];
  const viewerTargets = new Map<string, { task: Task; current: boolean }>();
  tasks.forEach((task, slot) => {
    task.assetIDs.forEach((assetID) => {
      viewerItems.push({ assetID, label: `第 ${slot + 1} 张` });
      viewerTargets.set(assetID, { task, current: true });
    });
    (task.previousAssetIDs || []).forEach((assetID, version) => {
      viewerItems.push({
        assetID,
        label: `第 ${slot + 1} 张 · 之前的版本 ${version + 1}`,
      });
      viewerTargets.set(assetID, { task, current: false });
    });
  });
  function openViewer(assetID: string) {
    const at = viewerItems.findIndex((item) => item.assetID === assetID);
    if (at >= 0) setViewerIndex(at);
  }
  // Failed and paused images can be sent again in one confirmed action; unknown ones cannot.
  const retryable = tasks.filter(
    (t) => t.status === "failed" || t.status === "blocked",
  ).length;
  const pendingDownloads = tasks.filter((t) => t.status === "download_failed");
  const sizeOptions = [
    ...presetSizes,
    ...(presetSizes.some((s) => s.value === input.size)
      ? []
      : [{ value: input.size, label: input.size }]),
  ];
  const analyzing = run?.status === "analyzing",
    submitted = !!run?.batchID;
  const completed = tasks.filter((t) => t.status === "succeeded").length,
    available = tasks.reduce((n, t) => n + t.assetIDs.length, 0);
  const failed = tasks.some((t) =>
    ["failed", "unknown", "download_failed", "canceled", "blocked"].includes(
      t.status,
    ),
  );
  const stage = !run
    ? "等待开始"
    : analyzing
      ? run.stage === "准备素材" ||
        run.stage === browserReadingStage ||
        run.stage === browserVerifyStage
        ? run.stage
        : "处理中"
      : submitted
        ? tasks.length > 0 && completed === tasks.length
          ? "已完成"
          : tasks.some((t) =>
                ["queued", "submitting", "running", "downloading"].includes(
                  t.status,
                ),
              )
            ? "正在生成"
            : failed
              ? "制作未完成"
              : "正在生成"
        : run.status === "needs_input"
          ? "等待补充"
          : run.status === "interrupted"
            ? "已中断"
            : run.status === "failed"
              ? "制作未完成"
              : "等待继续制作";
  const candidates = extractProductLinks(input.instruction);
  const pendingQuestions =
    run?.questions.filter((q) => q.required && !q.answer.trim()) || [];
  function change(p: Partial<AgentInput>) {
    setInput((v) => ({ ...v, ...p }));
    setError("");
    startRequest.current = null;
    continuationRequest.current = null;
  }
  function changeInstruction(value: string) {
    const links = extractProductLinks(value);
    const canExtract =
      !input.productURL ||
      (autoURL.current !== "" && autoURL.current === input.productURL);
    const p: Partial<AgentInput> = { instruction: value };
    if (canExtract) {
      p.productURL = links.length === 1 ? links[0] : "";
      autoURL.current = p.productURL;
    }
    change(p);
  }
  async function addFiles() {
    const out = await actions.run(() => call<Asset[]>("ImportAgentFiles"));
    if (out?.length) {
      const refs = [...new Set([...input.references, ...out.map((a) => a.id)])];
      if (refs.length > 20) {
        setError("参考图最多20张，请移除多余图片");
        return;
      }
      change({ references: refs });
    }
  }
  async function start() {
    if (!input.instruction.trim()) {
      setError("请填写完整制作指令");
      return;
    }
    const id = startRequest.current || crypto.randomUUID();
    startRequest.current = id;
    const out = await actions.run(() =>
      call<string>("StartSmartGeneration", input, id),
    );
    if (out) {
      setRunID(out);
      latestDraft.current = null;
      setError("");
    }
  }
  async function continueRun() {
    if (!run) return;
    const id = continuationRequest.current || crypto.randomUUID();
    continuationRequest.current = id;
    await actions.run(() =>
      call<string>(
        "ContinueSmartGeneration",
        {
          runID: run.id,
          instruction: "",
          productText,
          references: input.references,
          answers,
        },
        id,
      ),
    );
    continuationRequest.current = null;
  }
  function newRun() {
    setRunID("");
    initialized.current = "";
    startRequest.current = null;
    continuationRequest.current = null;
    setAnswers({});
    setProductText("");
    setError("");
  }
  const references = (
    <>
      <Space wrap>
        <Button
          icon={<UploadOutlined aria-hidden="true" />}
          disabled={analyzing || submitted}
          onClick={addFiles}
          loading={actions.busy}
        >
          上传参考图
        </Button>
      </Space>
      <UISelect
        aria-label="从素材库选择参考图"
        className="full-width"
        mode="multiple"
        disabled={analyzing || submitted}
        value={input.references}
        placeholder="也可选择已有素材"
        options={
          assets.data?.items.map((a) => ({ value: a.id, label: a.name })) || []
        }
        onChange={(v) => change({ references: v.slice(0, 20) })}
      />
      <ReferenceStrip
        ids={input.references}
        remove={
          analyzing || submitted
            ? undefined
            : (id) =>
                change({ references: input.references.filter((v) => v !== id) })
        }
      />
    </>
  );
  return (
    <>
      <Header
        title="智能生图"
        sub="输入制作需求，自动完成套图。需要补充资料时会集中提示。"
        action={
          <Button
            icon={<PlusOutlined aria-hidden="true" />}
            disabled={actions.busy}
            onClick={newRun}
          >
            新建需求
          </Button>
        }
      />
      {runtime.data && !runtime.data.available && (
        <Alert
          type="warning"
          showIcon
          message="制作组件不可用，请完整解压便携包或在设置中检查。"
        />
      )}
      {draftError && (
        <Alert
          type="error"
          showIcon
          message="草稿未保存"
          description={draftError}
        />
      )}
      {error && <Alert type="error" showIcon message={error} />}
      <div className="agent-layout">
        <section className="panel agent-input-panel">
          <div className="panel-heading">
            <h2>制作需求</h2>
            <Tag>{run ? "已保存需求" : "草稿"}</Tag>
          </div>
          <label htmlFor="agent-instruction" className="field-label">
            完整制作指令
          </label>
          <Input.TextArea
            id="agent-instruction"
            value={input.instruction}
            disabled={!!run || actions.busy}
            autoSize={{ minRows: 8, maxRows: 18 }}
            style={{ resize: "none" }}
            placeholder="粘贴商品链接和完整套图需求，上传参考图后开始制作。"
            onChange={(e) => changeInstruction(e.target.value)}
          />
          <label htmlFor="agent-url" className="field-label">
            商品链接
          </label>
          <Input
            id="agent-url"
            value={input.productURL}
            disabled={!!run || actions.busy}
            placeholder="可从指令自动提取，也可手动填写"
            onChange={(e) => {
              autoURL.current = "";
              change({ productURL: e.target.value });
            }}
          />
          {!run && candidates.length > 1 && (
            <>
              <label htmlFor="agent-link-choice" className="field-label">
                选择本次商品链接
              </label>
              <UISelect
                id="agent-link-choice"
                className="full-width"
                value={input.productURL || undefined}
                options={candidates.map((value) => ({ value, label: value }))}
                onChange={(v) => {
                  autoURL.current = "";
                  change({ productURL: v });
                }}
              />
            </>
          )}
          <div className="parameter-grid">
            <div>
              <label htmlFor="agent-count" className="field-label">
                目标张数
              </label>
              <InputNumber
                id="agent-count"
                className="full-width"
                min={1}
                max={200}
                value={input.targetCount}
                disabled={!!run || actions.busy}
                onChange={(v) => change({ targetCount: v || 12 })}
              />
            </div>
            <div>
              <label htmlFor="agent-ratio" className="field-label">
                画面比例
              </label>
              <UISelect
                id="agent-ratio"
                className="full-width"
                disabled={!!run || actions.busy}
                value={input.ratio}
                options={["1:1", "3:2", "2:3", "16:9"].map((value) => ({
                  value,
                  label: value,
                }))}
                onChange={(ratio) =>
                  change({
                    ratio,
                    // Keep "auto"; otherwise follow the ratio with its matching size.
                    size:
                      input.size === "auto"
                        ? "auto"
                        : ratioSizes[ratio] || input.size,
                  })
                }
              />
            </div>
          </div>
          <div className="agent-reference-heading">
            <h3>参考素材</h3>
            <p className="hint">
              上传商品图、字体或色卡。每次需求最多20张参考图。
            </p>
            {references}
          </div>
          <Collapse
            ghost
            activeKey={advanced}
            onChange={(keys) =>
              setAdvanced(Array.isArray(keys) ? keys : [keys])
            }
            items={[
              {
                key: "settings",
                label: "更多设置",
                children: (
                  <>
                    <label htmlFor="agent-llm" className="field-label">
                      LLM 供应商
                    </label>
                    <UISelect
                      id="agent-llm"
                      className="full-width"
                      value={input.llmConnectionID || undefined}
                      disabled={!!run || actions.busy}
                      options={llms.map((c) => ({
                        value: c.id,
                        label: c.name,
                      }))}
                      onChange={(llmConnectionID) =>
                        change({ llmConnectionID })
                      }
                    />
                    <label htmlFor="agent-image" className="field-label">
                      生图中转站
                    </label>
                    <UISelect
                      id="agent-image"
                      className="full-width"
                      value={input.imageConnectionID || undefined}
                      disabled={!!run || actions.busy}
                      options={images.map((c) => ({
                        value: c.id,
                        label: c.name,
                      }))}
                      onChange={(imageConnectionID) =>
                        change({ imageConnectionID, imageModel: "" })
                      }
                    />
                    <label htmlFor="agent-image-model" className="field-label">
                      生图模型
                    </label>
                    <UISelect
                      id="agent-image-model"
                      className="full-width"
                      showSearch
                      value={input.imageModel}
                      disabled={!!run || actions.busy || !imageConnection}
                      loading={imageModels.isFetching}
                      options={modelOptions}
                      onChange={(imageModel) => change({ imageModel })}
                    />
                    {imageModels.isError && (
                      <p className="hint">
                        暂时无法获取模型列表，可使用连接默认模型。
                      </p>
                    )}
                    <div className="parameter-grid">
                      <div>
                        <label htmlFor="agent-size" className="field-label">
                          图片尺寸
                        </label>
                        <UISelect
                          id="agent-size"
                          className="full-width"
                          disabled={!!run || actions.busy}
                          value={input.size}
                          options={sizeOptions}
                          onChange={(size) => change({ size })}
                        />
                      </div>
                      <div>
                        <label htmlFor="agent-quality" className="field-label">
                          图片质量
                        </label>
                        <UISelect
                          id="agent-quality"
                          className="full-width"
                          disabled={!!run || actions.busy}
                          value={input.quality}
                          options={[
                            { value: "auto", label: "自动" },
                            { value: "low", label: "低" },
                            { value: "medium", label: "中" },
                            { value: "high", label: "高" },
                          ]}
                          onChange={(quality) => change({ quality })}
                        />
                      </div>
                    </div>
                    <p className="hint">
                      尺寸需由生图模型支持。比例同时写入图片制作要求。
                    </p>
                    <label htmlFor="agent-concurrency" className="field-label">
                      同时生成张数
                    </label>
                    <UISelect
                      id="agent-concurrency"
                      className="full-width"
                      value={concurrency.data}
                      loading={concurrency.isLoading}
                      options={[1, 2, 3, 4].map((value) => ({
                        value,
                        label: value === 1 ? "1 张（逐张生成）" : `${value} 张`,
                      }))}
                      onChange={(n) =>
                        actions.run(async () => {
                          await call("SetImageConcurrency", n);
                          qc.setQueryData(["image-concurrency"], n);
                        })
                      }
                    />
                    <p className="hint">
                      生成中也可调整，立即生效；调低时已在生成的图片会继续完成。对生图工作台同样生效。中转站可能限制并发，出现大量失败或
                      429 时请调低。
                    </p>
                    <label htmlFor="agent-project" className="field-label">
                      所属项目
                    </label>
                    <UISelect
                      id="agent-project"
                      className="full-width"
                      allowClear
                      value={input.projectID || undefined}
                      disabled={!!run || actions.busy}
                      options={
                        projects.data?.map((p) => ({
                          value: p.id,
                          label: p.name,
                        })) || []
                      }
                      onChange={(v) => change({ projectID: v || "" })}
                    />
                  </>
                ),
              },
            ]}
          />
          {(!input.llmConnectionID || !input.imageConnectionID) && (
            <p className="hint">
              请在更多设置中选择连接，或前往
              <Link to="/settings">连接与设置</Link>添加供应商。
            </p>
          )}
          <p className="hint">
            本次 {input.targetCount} 张 · 生图模型：{model}
            <br />
            LLM：{llmConnection?.model || "未设置"}
            。将调用LLM与生图服务，可能产生费用。
          </p>
          {!run && (
            <Button
              className="agent-primary"
              type="primary"
              block
              loading={actions.busy}
              disabled={!ready || runtime.data?.available === false}
              onClick={start}
            >
              开始生成套图
            </Button>
          )}
        </section>
        <div className="agent-content">
          <section className="panel">
            <div className="panel-heading">
              <h2>制作进度与结果</h2>
              <Space>
                <Tag
                  color={
                    analyzing
                      ? "processing"
                      : stage === "已完成"
                        ? "success"
                        : "default"
                  }
                >
                  {stage}
                </Tag>
                {run && <AgentTiming run={run} tasks={tasks} />}
              </Space>
            </div>
            {runQuery.error && (
              <Alert
                type="error"
                showIcon
                message="制作状态暂时无法读取，请刷新后重试。"
              />
            )}
            {!run && (
              <Empty description="填写需求并开始制作，图片会显示在这里。" />
            )}
            {run && (
              <>
                <p role="status" aria-live="polite">
                  {stage}
                </p>
                {analyzing && run.stage === browserVerifyStage && (
                  <VerificationWait run={run} />
                )}
                {analyzing && (
                  <Button
                    onClick={() =>
                      actions.run(() => call("CancelAgentAnalysis", run.id))
                    }
                  >
                    取消制作
                  </Button>
                )}
                {run.error && (
                  <Alert
                    type="warning"
                    showIcon
                    message={
                      run.status === "interrupted"
                        ? "制作已中断，资料已保留，可继续制作。"
                        : "制作未完成，资料已保留。可继续制作，详细错误请查看日志。"
                    }
                  />
                )}
                {!analyzing && !submitted && (
                  <div className="agent-supplement">
                    {run.product.error && (
                      <Alert
                        type="warning"
                        showIcon
                        message="暂时无法读取商品链接，请上传商品参考图或粘贴商品描述。"
                      />
                    )}
                    {pendingQuestions.length > 0 && <h3>补充必要资料</h3>}
                    {pendingQuestions
                      .filter((q) => q.id !== "product-material")
                      .map((q) => (
                        <div key={q.id}>
                          <label
                            htmlFor={`answer-${q.id}`}
                            className="field-label"
                          >
                            {q.label}
                          </label>
                          <Input.TextArea
                            id={`answer-${q.id}`}
                            value={answers[q.id] ?? ""}
                            onChange={(e) => {
                              setAnswers((v) => ({
                                ...v,
                                [q.id]: e.target.value,
                              }));
                              continuationRequest.current = null;
                            }}
                            autoSize={{ minRows: 2, maxRows: 5 }}
                            style={{ resize: "none" }}
                          />
                        </div>
                      ))}
                    {(pendingQuestions.length > 0 || run.product.error) && (
                      <>
                        <label
                          htmlFor="agent-supplement-text"
                          className="field-label"
                        >
                          商品描述或补充说明
                        </label>
                        <Input.TextArea
                          id="agent-supplement-text"
                          value={productText}
                          onChange={(e) => {
                            setProductText(e.target.value);
                            continuationRequest.current = null;
                          }}
                          autoSize={{ minRows: 3, maxRows: 8 }}
                          style={{ resize: "none" }}
                        />
                        <p className="hint">
                          需要图片时，可通过左侧“上传参考图”补充。
                        </p>
                      </>
                    )}
                    <Button
                      type="primary"
                      loading={actions.busy}
                      onClick={continueRun}
                    >
                      {pendingQuestions.length > 0
                        ? "补充并继续制作"
                        : "继续制作"}
                    </Button>
                  </div>
                )}
              </>
            )}
          </section>
          {submitted && (
            <section className="panel">
              <div className="panel-heading">
                <h2>套图结果</h2>
                <Space wrap>
                  {pendingDownloads.length > 0 && (
                    <Button
                      disabled={actions.busy}
                      onClick={() =>
                        actions.run(async () => {
                          for (const task of pendingDownloads) {
                            await call("RetryDownload", task.id);
                          }
                        }, "已加入补下载队列，不会重新生图")
                      }
                    >
                      补下载全部失败图片（{pendingDownloads.length} 张）
                    </Button>
                  )}
                  {retryable > 0 && (
                    <Button
                      onClick={() =>
                        actions.confirm(
                          `重新生成 ${retryable} 张图片？`,
                          `将为 ${retryable} 张失败或暂停的图片重新发起生图请求，可能产生费用。“结果未知”的图片不包含在内，请先核对平台记录。`,
                          () => call("RetryFailedAgentImages", run!.id),
                          false,
                          "重新生成",
                        )
                      }
                    >
                      重新生成全部失败图片（{retryable} 张）
                    </Button>
                  )}
                  <Button
                    icon={<DownloadOutlined aria-hidden="true" />}
                    disabled={!available || downloads.busy}
                    loading={downloads.busy}
                    onClick={() => downloads.downloadBatch(run!.batchID)}
                  >
                    {tasks.length > 0 && completed === tasks.length
                      ? "下载整套图片"
                      : "下载已完成图片"}
                  </Button>
                </Space>
              </div>
              {tasks.some(isKeyRejected) && (
                <Alert
                  type="error"
                  showIcon
                  message="生图中转站拒绝了 API Key"
                  description={
                    <>
                      这些图片没有生成，也不计费；同批未提交的图片已暂停。请到
                      <Link to="/settings">连接与设置</Link>
                      重新输入该生图中转站的 API
                      Key，再点“重新生成全部失败图片”。
                    </>
                  }
                />
              )}
              <p role="status">
                已完成 {completed} / {tasks.length || input.targetCount}{" "}
                个任务，可下载 {available} 张图片
              </p>
              {taskQuery.error && (
                <Alert
                  type="error"
                  showIcon
                  message="图片状态暂时无法读取，请稍后刷新。"
                />
              )}
              <div className="agent-results">
                {tasks.map((task, index) => (
                  <article className="agent-result" key={task.id}>
                    <Space>
                      <strong>第 {index + 1} 张</strong>
                      <Status state={task.status} task={task} />
                    </Space>
                    <TaskTiming task={task} />
                    <TaskFailureAlert task={task} />
                    {task.refineInstruction && (
                      <p className="hint refine-note">
                        优化：{task.refineInstruction}
                      </p>
                    )}
                    {task.assetIDs.map((id) => (
                      <div key={id}>
                        <button
                          type="button"
                          className="image-open"
                          aria-label={`查看第 ${index + 1} 张大图`}
                          onClick={() => openViewer(id)}
                        >
                          <AssetImage id={id} />
                        </button>
                        <div className="agent-result-actions">
                          <Button
                            disabled={downloads.busy}
                            onClick={() => downloads.downloadAsset(id)}
                          >
                            下载图片
                          </Button>
                          {task.status === "succeeded" && (
                            <Button
                              aria-label={`优化第 ${index + 1} 张图片`}
                              onClick={() => setRefining({ task, assetID: id })}
                            >
                              优化
                            </Button>
                          )}
                        </div>
                      </div>
                    ))}
                    {!!task.previousAssetIDs?.length && (
                      <details
                        className="agent-versions"
                        open={task.assetIDs.length === 0}
                      >
                        <summary>
                          之前的版本（{task.previousAssetIDs.length}）
                        </summary>
                        <div className="agent-version-grid">
                          {task.previousAssetIDs.map((id, version) => (
                            <div key={id}>
                              <button
                                type="button"
                                className="image-open"
                                aria-label={`查看第 ${index + 1} 张的之前版本 ${version + 1}`}
                                onClick={() => openViewer(id)}
                              >
                                <AssetImage id={id} />
                              </button>
                              <Button
                                size="small"
                                block
                                aria-label={`下载第 ${index + 1} 张的之前版本 ${version + 1}`}
                                disabled={downloads.busy}
                                onClick={() => downloads.downloadAsset(id)}
                              >
                                下载
                              </Button>
                            </div>
                          ))}
                        </div>
                      </details>
                    )}
                    {task.status === "blocked" && (
                      <Button
                        onClick={() =>
                          actions.confirm(
                            "恢复生成这张图片？",
                            "这张图片尚未提交。恢复后将调用生图服务，可能产生费用。",
                            () => call("ResumeBlocked", task.id),
                            false,
                            "恢复生成",
                          )
                        }
                      >
                        恢复生成
                      </Button>
                    )}
                    {task.status === "download_failed" && (
                      <Button
                        disabled={actions.busy}
                        onClick={() =>
                          actions.run(() => call("RetryDownload", task.id))
                        }
                      >
                        补下载
                      </Button>
                    )}
                    {["failed", "unknown", "canceled"].includes(
                      task.status,
                    ) && (
                      <Button
                        onClick={() =>
                          actions.confirm(
                            "重新生成这张图片？",
                            "将发起新的计费请求。结果未知时请先核对供应商记录。",
                            () => call("Retry", task.id),
                            false,
                            "重新生成",
                          )
                        }
                      >
                        重新生成
                      </Button>
                    )}
                  </article>
                ))}
              </div>
              {tasks.some((t) => ["queued", "blocked"].includes(t.status)) && (
                <Button
                  onClick={() =>
                    actions.run(() => call("StopBatch", run!.batchID))
                  }
                >
                  停止未提交图片
                </Button>
              )}
            </section>
          )}
        </div>
      </div>
      <RefineDialog target={refining} onClose={() => setRefining(null)} />
      <ImageViewer
        items={viewerItems}
        index={viewerIndex}
        onIndex={setViewerIndex}
        onClose={() => setViewerIndex(null)}
        actions={(item) => {
          const entry = viewerTargets.get(item.assetID);
          return (
            <>
              <Button
                disabled={downloads.busy}
                onClick={() => downloads.downloadAsset(item.assetID)}
              >
                下载原图
              </Button>
              {entry?.current && entry.task.status === "succeeded" && (
                <Button
                  onClick={() => {
                    setViewerIndex(null);
                    setRefining({ task: entry.task, assetID: item.assetID });
                  }}
                >
                  优化
                </Button>
              )}
            </>
          );
        }}
      />
      <section className="panel agent-history">
        <h2>制作历史</h2>
        <DataState
          query={history}
          empty={!history.data?.items.length}
          emptyText="暂无制作记录"
        >
          <div>
            {history.data?.items.map((r) => (
              <div className="agent-history-row" key={r.id}>
                <div>
                  <strong>{r.input.instruction.slice(0, 60)}</strong>
                  <p className="hint">
                    {time(r.createdAt)} · {r.input.targetCount} 张 ·{" "}
                    {r.batchID
                      ? "已提交生图"
                      : r.status === "analyzing"
                        ? "处理中"
                        : r.status === "needs_input"
                          ? "等待补充"
                          : "可继续制作"}
                  </p>
                </div>
                <Button
                  onClick={() => {
                    setRunID(r.id);
                    setError("");
                  }}
                >
                  打开记录
                </Button>
              </div>
            ))}
          </div>
        </DataState>
        <Pagination
          current={page}
          pageSize={10}
          total={history.data?.total || 0}
          showSizeChanger={false}
          onChange={setPage}
        />
      </section>
    </>
  );
}
