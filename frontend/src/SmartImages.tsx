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
import { Link, useLocation } from "react-router-dom";
import {
  call,
  errorText,
  type Asset,
  type Connection,
  type Template,
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
import { TemplateBar, type TemplateMode } from "./Templates";
import { RunActivity, RunSteps, runStepLabel } from "./RunSteps";
import {
  ModelSelect,
  assetLabel,
  coalesce,
  presetQualities,
  presetSizes,
  withCurrent,
  AssetImage,
  AssetOption,
  CancelTaskButton,
  DataState,
  Header,
  ImageViewer,
  Status,
  TaskTiming,
  TaskFailureAlert,
  rejectionOf,
  time,
  useActions,
  useData,
  useImageDownloads,
  type ViewerItem,
} from "./ui";
import { gatewayLabel, useGateways } from "./Gateways";
import { LicenseNotice } from "./License";

export function extractProductLinks(text: string): string[] {
  // Full-width brackets and punctuation end a link pasted in Chinese text ("…th=1】").
  const values =
    text.match(/https?:\/\/[^\s\]\)\\<>"'【】「」『』（）《》，。；！、]+/g) ||
    [];
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
  // Waiting for the user's answers does not count; the timer stands still then.
  const elapsed = Math.max(
    0,
    Math.floor(((busy ? now : savedEnd) - start - (run.waitedMS || 0)) / 1000),
  );
  const waiting = run.status === "needs_input";
  return (
    <span className="muted">
      {busy ? "已用时" : "累计用时"} {Math.floor(elapsed / 60)} 分{" "}
      {elapsed % 60} 秒{waiting && "（等待补充资料，计时已暂停）"}
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

const ratioSizes: Record<string, string> = {
  "1:1": "1024x1024",
  "3:2": "1536x1024",
  "2:3": "1024x1536",
  "16:9": "1792x1024",
};

// Images the relay refused before generating: a refused key or an empty account.
const isKeyRejected = (task: Task) => rejectionOf(task) === "auth";
const isBalanceRejected = (task: Task) => rejectionOf(task) === "balance";

// An image drawn from the suite's detail sample says which image it waits for or
// copied, so a pause before it starts is not mistaken for a stuck queue.
function SampleNote({ task, tasks }: { task: Task; tasks: Task[] }) {
  if (!task.sampleItem) return null;
  const n = tasks.findIndex((t) => t.planItemID === task.sampleItem) + 1;
  const which = n > 0 ? `第 ${n} 张样板图` : "样板图";
  if (task.samplePending)
    return (
      <p className="hint">
        等{which}完成后开始，照它画袖口等细节；等待期间未提交、不计费。
      </p>
    );
  if (task.sampleAssetID)
    return <p className="hint">已照{which}画袖口等细节。</p>;
  if (task.status !== "queued")
    return <p className="hint">{which}没有成功，这张按文字说明生成。</p>;
  return null;
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
        placeholder="例如：把袖口的狗掌和 Amelia 摆正成水平的一行，狗掌在左"
      />
      <p className="hint" style={{ marginTop: 28 }}>
        写清要去掉什么、换成什么，并点名是谁或哪个位置。例如：右侧女性脱掉米白色针织衫，改穿和左侧同款的卫衣，颜色
        Maroon，胸前图案和袖口刺绣与左侧完全相同。
      </p>
      <p className="hint">
        将以这张图片为底图发起一次新的生图请求，可能产生费用。原图保留在“之前的版本”中。
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
  const gateways = useGateways();
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
  const incomingTemplate = (
    useLocation().state as { templateID?: string } | null
  )?.templateID;
  const initialized = useRef(""),
    answersSynced = useRef(""),
    autoOpen = useRef(!!incomingTemplate),
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
        projectID: "",
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
    if (!run) return;
    const stop = run.status === "needs_input" ? run.updatedAt : "";
    const opened = initialized.current !== run.id;
    if (opened) {
      initialized.current = run.id;
      setInput(run.input);
      setProductText(run.input.productText);
    }
    // Each new stop shows the saved answers, so a question asked again starts empty
    // instead of showing the answer that did not settle it.
    if (opened || (stop && answersSynced.current !== stop)) {
      setAnswers(
        Object.fromEntries(run.questions.map((q) => [q.id, q.answer || ""])),
      );
    }
    answersSynced.current = stop;
  }, [run]);
  useEffect(() => {
    const refresh = coalesce(() => {
      qc.invalidateQueries({ queryKey: ["agent-run"] });
      qc.invalidateQueries({ queryKey: ["agent-runs"] });
      qc.invalidateQueries({ queryKey: ["agent-tasks"] });
    });
    const off = window.runtime?.EventsOn("agent:changed", refresh);
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
      // New requests start on the primary gateway when one is set up.
      imageConnectionID:
        v.imageConnectionID ||
        connections.data?.find(
          (c) => c.id === gateways.settings.data?.primaryID && c.hasKey,
        )?.id ||
        connections.data?.find((c) => c.kind !== "llm" && c.hasKey)?.id ||
        "",
    }));
  }, [ready, runID, connections.data, gateways.settings.data]);
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
  const sizeOptions = withCurrent(presetSizes, input.size);
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
      ? run.stage === browserVerifyStage
        ? run.stage
        : run.progress
          ? runStepLabel(run.progress)
          : run.stage === "准备素材" || run.stage === browserReadingStage
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
  function applyTemplate(t: Template, mode: TemplateMode) {
    const current = input.instruction.trimEnd();
    changeInstruction(
      mode === "append" ? current + "\n\n" + t.prompt : t.prompt,
    );
    if (mode === "replace")
      change({
        ...(t.size ? { size: t.size } : {}),
        ...(t.quality ? { quality: t.quality } : {}),
      });
    document.getElementById("agent-instruction")?.focus();
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
      // The steps are at the top of the page; the button can be far below them.
      window.scrollTo(0, 0);
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
    window.scrollTo(0, 0);
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
        optionFilterProp="label"
        listItemHeight={44}
        listHeight={308}
        options={
          assets.data?.items.map((a) => ({
            value: a.id,
            label: assetLabel(a),
          })) || []
        }
        optionRender={(option) => (
          <AssetOption id={String(option.value)} name={String(option.label)} />
        )}
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
      <LicenseNotice />
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
          <div className="field-header prompt-header">
            <label htmlFor="agent-instruction" className="field-label">
              完整制作指令
            </label>
            <TemplateBar
              text={input.instruction}
              size={input.size}
              quality={input.quality}
              ready={ready && !run}
              disabled={!!run || actions.busy}
              onApply={applyTemplate}
            />
          </div>
          <Input.TextArea
            id="agent-instruction"
            value={input.instruction}
            disabled={!!run || actions.busy}
            autoSize={{ minRows: 6, maxRows: 12 }}
            style={{ resize: "none" }}
            placeholder="粘贴商品链接和完整套图需求，上传参考图后开始制作。"
            onChange={(e) => changeInstruction(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && e.ctrlKey && !e.nativeEvent.isComposing)
                start();
            }}
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
                        label: gatewayLabel(
                          c,
                          gateways.settings.data,
                          gateways.byID.get(c.id),
                        ),
                      }))}
                      onChange={(imageConnectionID) =>
                        change({ imageConnectionID, imageModel: "" })
                      }
                    />
                    <label htmlFor="agent-image-model" className="field-label">
                      生图模型
                    </label>
                    <ModelSelect
                      id="agent-image-model"
                      connection={imageConnection}
                      value={input.imageModel}
                      disabled={!!run || actions.busy}
                      load={advanced.includes("settings") && !run}
                      onChange={(imageModel) => change({ imageModel })}
                    />
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
                          options={withCurrent(presetQualities, input.quality)}
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
          <div className="sticky-actions">
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
          </div>
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
                <p
                  role="status"
                  aria-live="polite"
                  className={analyzing && run.progress ? "sr-only" : undefined}
                >
                  {stage}
                </p>
                {analyzing && run.progress && <RunSteps run={run} />}
                <RunActivity run={run} />
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
                {run.error &&
                  (run.status === "failed" || run.status === "interrupted") && (
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
                {!!run.llmReconnects &&
                  run.status !== "failed" &&
                  run.status !== "interrupted" && (
                    <p
                      className="hint llm-reconnect-note"
                      title={run.llmReconnectError}
                    >
                      LLM 连接中断
                      {run.llmReconnects > 1
                        ? ` ${run.llmReconnects} 次`
                        : "一次"}
                      ，已自动重试
                      {analyzing ? "，分析继续进行。" : "并继续完成。"}
                      不影响生图，也不会额外计费。
                    </p>
                  )}
                {!analyzing && !submitted && (
                  <div className="agent-supplement">
                    {run.product.error ? (
                      <Alert
                        type="warning"
                        showIcon
                        message="暂时无法读取商品链接，请上传商品参考图或粘贴商品描述。"
                      />
                    ) : (
                      run.product.imageNote && (
                        <p className="hint">
                          已读取商品页，{run.product.imageNote}。
                        </p>
                      )
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
              {tasks.some(isBalanceRejected) && (
                <Alert
                  type="error"
                  showIcon
                  message="生图中转站账户余额不足"
                  description={
                    <>
                      这些图片没有生成，也不计费；同批未提交的图片已暂停（设置了备用中转站时会改用备用）。请到该中转站后台充值，再点“重新生成全部失败图片”。API
                      Key 不需要重新输入。
                    </>
                  }
                />
              )}
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
                    <SampleNote task={task} tasks={tasks} />
                    {task.sample && (
                      <p className="hint">
                        细节样板：先生成这张，其他看得见袖口等细节的图片照它画
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
                    <CancelTaskButton task={task} />
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
                        ? r.progress
                          ? runStepLabel(r.progress)
                          : "处理中"
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
