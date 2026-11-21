import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  Button,
  Collapse,
  Empty,
  Input,
  InputNumber,
  Pagination,
  Select as UISelect,
  Space,
  Tag,
  Tooltip,
} from "antd";
import {
  DownloadOutlined,
  PaperClipOutlined,
  PlusOutlined,
  UploadOutlined,
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
  hostedLineName,
  assetLabel,
  coalesce,
  presetQualities,
  presetSizes,
  withCurrent,
  AssetImage,
  useArrivals,
  withViewTransition,
  AssetOption,
  CancelTaskButton,
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
import { gatewayLabel, useGateways } from "./Gateways";
import { t, type MessageKey } from "./i18n";
import { LicenseNotice } from "./License";
import {
  ReferenceStrip,
  AgentTiming,
  isVerifyStage,
  isKnownStage,
  stageText,
  VerificationWait,
  ratioSizes,
  isKeyRejected,
  isBalanceRejected,
  AgentJudgments,
  SampleNote,
  SampleCheckNote,
  type RefineTarget,
  RefineDialog,
  RefineModeNote,
  type SmartMode,
  ModeSwitch,
  SuiteEstimateHint,
} from "./SmartParts";
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
  const skillCategories = useData<string[]>(
    ["skill-categories"],
    "SkillCategories",
  );
  const gateways = useGateways();
  const runtime = useData<AgentRuntimeInfo>(["agent-runtime"], "AgentRuntime");
  const assets = useData<{ items: Asset[] }>(
    ["agent-assets"],
    "ListAssets",
    ["", 0, 100],
    4000,
  );
  const draft = useData<AgentInput>(["agent-draft"], "GetAgentDraft");
  // Manual mode asks for missing facts and waits; automatic mode never asks.
  const mode = useData<string>(["smart-mode"], "SmartMode");
  const autoMode = mode.data === "auto";
  const [input, setInput] = useState<AgentInput>(blankAgentDraft);
  const [ready, setReady] = useState(false),
    [error, setError] = useState(""),
    [draftError, setDraftError] = useState("");
  const [page, setPage] = useState(1),
    [runID, setRunID] = useState("");
  const [answers, setAnswers] = useState<Record<string, string>>({}),
    [productText, setProductText] = useState("");
  const [advanced, setAdvanced] = useState<string[]>([]);
  // null: follow what the request already holds; true/false: the user's choice.
  const [moreChoice, setMoreChoice] = useState<boolean | null>(null);
  const templates = useData<Template[]>(["templates"], "ListTemplates");
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
  const arrivals = useArrivals(
    runID || "",
    taskQuery.data?.flatMap((t) => t.assetIDs),
  );
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
  const model = input.imageModel || imageConnection?.model || t("ui.unset");
  // Viewer walks the set in slot order: each slot's current image, then its earlier versions.
  const [viewerIndex, setViewerIndex] = useState<number | null>(null);
  const viewerItems: ViewerItem[] = [];
  const viewerTargets = new Map<string, { task: Task; current: boolean }>();
  tasks.forEach((task, slot) => {
    task.assetIDs.forEach((assetID) => {
      viewerItems.push({
        assetID,
        label: t("smart.image.number", { n: slot + 1 }),
      });
      viewerTargets.set(assetID, { task, current: true });
    });
    (task.previousAssetIDs || []).forEach((assetID, version) => {
      viewerItems.push({
        assetID,
        label: t("smart.image.previous", { n: slot + 1, version: version + 1 }),
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
  const done = submitted && tasks.length > 0 && completed === tasks.length;
  const stageKey: MessageKey = !run
    ? "smart.stage.waiting"
    : analyzing
      ? "run.processing"
      : submitted
        ? done
          ? "smart.stage.done"
          : tasks.some((t) =>
                ["queued", "submitting", "running", "downloading"].includes(
                  t.status,
                ),
              )
            ? "smart.stage.generating"
            : failed
              ? "smart.stage.unfinished"
              : "smart.stage.generating"
        : run.status === "needs_input"
          ? "smart.stage.needsInput"
          : run.status === "interrupted"
            ? "smart.stage.interrupted"
            : run.status === "failed"
              ? "smart.stage.unfinished"
              : "smart.stage.resume";
  // The stage while analyzing is the step, or a stage the backend named.
  const stage =
    run && analyzing
      ? isVerifyStage(run)
        ? stageText(run)
        : run.progress
          ? runStepLabel(run.progress)
          : isKnownStage(run)
            ? stageText(run)
            : t(stageKey)
      : t(stageKey);
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
        setError(t("smart.error.tooManyReferences", { max: 20 }));
        return;
      }
      change({ references: refs });
    }
  }
  async function start() {
    if (!input.instruction.trim()) {
      setError(t("smart.error.noInstruction"));
      return;
    }
    const id = startRequest.current || crypto.randomUUID();
    startRequest.current = id;
    const out = await actions.run(() =>
      call<string>("StartSmartGeneration", { ...input, autoMode }, id),
    );
    if (out) {
      withViewTransition(() => setRunID(out));
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
    withViewTransition(() => setRunID(""));
    initialized.current = "";
    startRequest.current = null;
    continuationRequest.current = null;
    setAnswers({});
    setProductText("");
    setError("");
    setMoreChoice(null);
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
          {t("smart.upload")}
        </Button>
      </Space>
      <UISelect
        aria-label={t("smart.references.pick")}
        className="full-width"
        mode="multiple"
        disabled={analyzing || submitted}
        value={input.references}
        placeholder={t("smart.references.placeholder")}
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
  // The entry is one box: instruction, a row of controls, the rest under 更多选项.
  const home = !runID;
  const missingConnection = !input.llmConnectionID || !input.imageConnectionID;
  const moreOpen =
    !home ||
    (moreChoice ??
      (!!input.productURL ||
        input.references.length > 0 ||
        candidates.length > 1 ||
        (ready && !!connections.data && missingConnection)));
  const templateBar = (
    <TemplateBar
      text={input.instruction}
      size={input.size}
      quality={input.quality}
      ready={ready && !run}
      disabled={!!run || actions.busy}
      onApply={applyTemplate}
    />
  );
  const countRatio = (
    <div className="parameter-grid">
      <div>
        <label htmlFor="agent-count" className="field-label">
          {t("smart.count")}
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
          {t("smart.ratio")}
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
  );
  const startButton = (
    <Button
      className="agent-primary"
      type="primary"
      block={!home}
      loading={actions.busy}
      disabled={!ready || runtime.data?.available === false}
      onClick={start}
    >
      {t("smart.start")}
    </Button>
  );
  // Newest generated images; uploads used as references are not works.
  const recent = (assets.data?.items || []).filter((a) => a.taskID).slice(0, 8);
  return (
    <>
      <LicenseNotice />
      <Header
        title={home ? t("smart.title.home") : t("shell.nav.create")}
        sub={t(autoMode ? "smart.sub.auto" : "smart.sub.manual")}
        action={
          <div className="smart-header-actions">
            <ModeSwitch
              value={autoMode ? "auto" : "manual"}
              disabled={!mode.data}
              onChange={(v) => actions.run(() => call("SetSmartMode", v))}
            />
            <span className="smart-header-divider" aria-hidden="true" />
            <Button
              icon={<PlusOutlined aria-hidden="true" />}
              disabled={actions.busy}
              onClick={newRun}
            >
              {t("smart.newRun")}
            </Button>
          </div>
        }
      />
      {runtime.data && !runtime.data.available && (
        <Alert type="warning" showIcon message={t("smart.runtimeMissing")} />
      )}
      {draftError && (
        <Alert
          type="error"
          showIcon
          message={t("smart.draftFailed")}
          description={draftError}
        />
      )}
      {error && <Alert type="error" showIcon message={error} />}
      <div className={home ? "agent-layout is-home" : "agent-layout"}>
        <section
          className={
            home
              ? "panel agent-input-panel create-box"
              : "panel agent-input-panel"
          }
        >
          <div className="panel-heading">
            <h2>{t("smart.request.title")}</h2>
            <Tag>{t(run ? "smart.request.saved" : "smart.request.draft")}</Tag>
          </div>
          <div className="field-header prompt-header">
            <label htmlFor="agent-instruction" className="field-label">
              {t("smart.instruction")}
            </label>
            {!home && templateBar}
          </div>
          <Input.TextArea
            id="agent-instruction"
            value={input.instruction}
            disabled={!!run || actions.busy}
            autoSize={{ minRows: home ? 3 : 6, maxRows: 12 }}
            style={{ resize: "none" }}
            placeholder={t("smart.instruction.placeholder")}
            onChange={(e) => changeInstruction(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && e.ctrlKey && !e.nativeEvent.isComposing)
                start();
            }}
          />
          {home && (
            <div className="composer-row">
              <Tooltip title={t("smart.addReferences")}>
                <Button
                  type="text"
                  aria-label={t("smart.addReferences")}
                  icon={<PaperClipOutlined aria-hidden="true" />}
                  disabled={actions.busy}
                  onClick={addFiles}
                />
              </Tooltip>
              {countRatio}
              <Button
                type="text"
                aria-expanded={moreOpen}
                aria-controls="agent-more"
                onClick={() => setMoreChoice(!moreOpen)}
              >
                {t("smart.more")}
              </Button>
              {startButton}
            </div>
          )}
          <div id="agent-more" className="agent-more" hidden={!moreOpen}>
            {home && <div className="composer-templates">{templateBar}</div>}
            <label htmlFor="agent-url" className="field-label">
              {t("smart.url")}
            </label>
            <Input
              id="agent-url"
              value={input.productURL}
              disabled={!!run || actions.busy}
              placeholder={t("smart.url.placeholder")}
              onChange={(e) => {
                autoURL.current = "";
                change({ productURL: e.target.value });
              }}
            />
            {!run && candidates.length > 1 && (
              <>
                <label htmlFor="agent-link-choice" className="field-label">
                  {t("smart.url.choose")}
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
            {!home && countRatio}
            <div className="agent-reference-heading">
              <h3>{t("smart.references.title")}</h3>
              <p className="hint">{t("smart.references.hint", { max: 20 })}</p>
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
                  label: t("smart.settings"),
                  children: (
                    <>
                      {!!skillCategories.data?.length && (
                        <>
                          <label
                            htmlFor="agent-category"
                            className="field-label"
                          >
                            {t("smart.category")}
                          </label>
                          <UISelect
                            id="agent-category"
                            className="full-width"
                            value={input.category || ""}
                            disabled={!!run || actions.busy}
                            options={[
                              { value: "", label: t("smart.category.none") },
                              ...skillCategories.data.map((value) => ({
                                value,
                                label: value,
                              })),
                            ]}
                            onChange={(category) => change({ category })}
                          />
                          <p className="hint">{t("smart.category.hint")}</p>
                        </>
                      )}
                      <label htmlFor="agent-llm" className="field-label">
                        {t("smart.llm")}
                      </label>
                      <UISelect
                        id="agent-llm"
                        className="full-width"
                        value={input.llmConnectionID || undefined}
                        disabled={!!run || actions.busy}
                        options={llms.map((c) => ({
                          value: c.id,
                          label: hostedLineName(c),
                        }))}
                        onChange={(llmConnectionID) =>
                          change({ llmConnectionID })
                        }
                      />
                      <label htmlFor="agent-image" className="field-label">
                        {t("smart.imageService")}
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
                      <label
                        htmlFor="agent-image-model"
                        className="field-label"
                      >
                        {t("smart.imageModel")}
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
                            {t("smart.size")}
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
                          <label
                            htmlFor="agent-quality"
                            className="field-label"
                          >
                            {t("smart.quality")}
                          </label>
                          <UISelect
                            id="agent-quality"
                            className="full-width"
                            disabled={!!run || actions.busy}
                            value={input.quality}
                            options={withCurrent(
                              presetQualities,
                              input.quality,
                            )}
                            onChange={(quality) => change({ quality })}
                          />
                        </div>
                      </div>
                      <p className="hint">{t("smart.size.hint")}</p>
                      <label
                        htmlFor="agent-concurrency"
                        className="field-label"
                      >
                        {t("smart.concurrency")}
                      </label>
                      <UISelect
                        id="agent-concurrency"
                        className="full-width"
                        value={concurrency.data}
                        loading={concurrency.isLoading}
                        options={[1, 2, 3, 4].map((value) => ({
                          value,
                          label:
                            value === 1
                              ? t("smart.concurrency.one")
                              : t("smart.concurrency.many", { n: value }),
                        }))}
                        onChange={(n) =>
                          actions.run(async () => {
                            await call("SetImageConcurrency", n);
                            qc.setQueryData(["image-concurrency"], n);
                          })
                        }
                      />
                      <p className="hint">{t("smart.concurrency.hint")}</p>
                    </>
                  ),
                },
              ]}
            />
            {(!input.llmConnectionID || !input.imageConnectionID) && (
              <p className="hint">
                {t("smart.connection.before")}
                <Link to="/settings">{t("smart.connection.link")}</Link>
                {t("smart.connection.after")}
              </p>
            )}
          </div>
          <div className="sticky-actions">
            <p className="hint">
              {t("smart.summary.images", {
                count: input.targetCount,
                model,
              })}
              <br />
              {t("smart.summary.llm", {
                model: llmConnection?.model || t("ui.unset"),
              })}
            </p>
            <SuiteEstimateHint
              connectionID={input.imageConnectionID}
              model={input.imageModel || ""}
              size={input.size}
              count={input.targetCount}
            />
            {!run && !home && startButton}
          </div>
        </section>
        {home && (
          <section
            className="recent-works"
            aria-labelledby="recent-works-title"
          >
            {recent.length ? (
              <>
                <div className="recent-works-head">
                  <h2 id="recent-works-title">{t("smart.recent")}</h2>
                  <Link to="/assets">{t("smart.recent.all")}</Link>
                </div>
                <div className="recent-works-wall">
                  {recent.map((a) => (
                    <Link
                      key={a.id}
                      to="/assets"
                      aria-label={t("smart.recent.view", {
                        name: assetLabel(a),
                      })}
                    >
                      <AssetImage id={a.id} />
                    </Link>
                  ))}
                </div>
              </>
            ) : (
              !!templates.data?.length && (
                <>
                  <div className="recent-works-head">
                    <h2 id="recent-works-title">{t("smart.templates")}</h2>
                    <Link to="/templates">{t("smart.templates.manage")}</Link>
                  </div>
                  <div className="template-starters">
                    {templates.data.slice(0, 6).map((t) => (
                      <button
                        key={t.id}
                        type="button"
                        onClick={() => applyTemplate(t, "replace")}
                      >
                        <strong>{t.name}</strong>
                        <span>{t.prompt}</span>
                      </button>
                    ))}
                  </div>
                </>
              )
            )}
          </section>
        )}
        <div className="agent-content" hidden={home}>
          <section className="panel">
            <div className="panel-heading">
              <h2>{t("smart.progress.title")}</h2>
              <Space>
                <Tag
                  color={
                    analyzing ? "processing" : done ? "success" : "default"
                  }
                >
                  {stage}
                </Tag>
                {run && (
                  <Tag>
                    {t(
                      run.input.autoMode
                        ? "smart.mode.autoTag"
                        : "smart.mode.manualTag",
                    )}
                  </Tag>
                )}
                {run && <AgentTiming run={run} tasks={tasks} />}
              </Space>
            </div>
            {runQuery.error && (
              <Alert type="error" showIcon message={t("smart.runUnreadable")} />
            )}
            {!run && <Empty description={t("smart.empty")} />}
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
                <AgentJudgments run={run} />
                {analyzing && isVerifyStage(run) && (
                  <VerificationWait run={run} />
                )}
                {analyzing && (
                  <Button
                    onClick={() =>
                      actions.run(() => call("CancelAgentAnalysis", run.id))
                    }
                  >
                    {t("smart.cancel")}
                  </Button>
                )}
                {run.error &&
                  (run.status === "failed" || run.status === "interrupted") && (
                    <Alert
                      type="warning"
                      showIcon
                      message={
                        run.status === "interrupted"
                          ? t("smart.interrupted")
                          : t("smart.failed")
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
                      {t(
                        run.llmReconnects > 1
                          ? analyzing
                            ? "smart.reconnect.manyAnalyzing"
                            : "smart.reconnect.manyDone"
                          : analyzing
                            ? "smart.reconnect.onceAnalyzing"
                            : "smart.reconnect.onceDone",
                        { count: run.llmReconnects },
                      )}
                    </p>
                  )}
                {!analyzing && !submitted && (
                  <div className="agent-supplement">
                    {run.product.error ? (
                      <Alert
                        type="warning"
                        showIcon
                        message={t("smart.product.unreadable")}
                      />
                    ) : (
                      run.product.imageNote && (
                        <p className="hint">
                          {t("smart.product.read", {
                            note: run.product.imageNote,
                          })}
                        </p>
                      )
                    )}
                    {pendingQuestions.length > 0 && (
                      <h3>{t("smart.supplement.title")}</h3>
                    )}
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
                          {t("smart.supplement.text")}
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
                        <p className="hint">{t("smart.supplement.hint")}</p>
                      </>
                    )}
                    <Button
                      type="primary"
                      loading={actions.busy}
                      onClick={continueRun}
                    >
                      {t(
                        pendingQuestions.length > 0
                          ? "smart.continueWith"
                          : "smart.continue",
                      )}
                    </Button>
                  </div>
                )}
              </>
            )}
          </section>
          {submitted && (
            <section className="panel">
              <div className="panel-heading">
                <h2>{t("smart.results")}</h2>
                <Space wrap>
                  {pendingDownloads.length > 0 && (
                    <Button
                      disabled={actions.busy}
                      onClick={() =>
                        actions.run(async () => {
                          for (const task of pendingDownloads) {
                            await call("RetryDownload", task.id);
                          }
                        }, t("smart.download.queued"))
                      }
                    >
                      {t("smart.download.retryAll", {
                        count: pendingDownloads.length,
                      })}
                    </Button>
                  )}
                  {retryable > 0 && (
                    <Button
                      onClick={() =>
                        actions.confirm(
                          t("smart.retry.allTitle", { count: retryable }),
                          t("smart.retry.allContent", { count: retryable }),
                          () => call("RetryFailedAgentImages", run!.id),
                          false,
                          t("smart.retry.ok"),
                        )
                      }
                    >
                      {t("smart.retry.all", { count: retryable })}
                    </Button>
                  )}
                  <Button
                    icon={<DownloadOutlined aria-hidden="true" />}
                    disabled={!available || downloads.busy}
                    loading={downloads.busy}
                    onClick={() => downloads.downloadBatch(run!.batchID)}
                  >
                    {t(done ? "smart.download.set" : "smart.download.done")}
                  </Button>
                </Space>
              </div>
              {tasks.some(isBalanceRejected) && (
                <Alert
                  type="error"
                  showIcon
                  message={t("smart.balance.title")}
                  description={t("smart.balance.text")}
                />
              )}
              {tasks.some(isKeyRejected) && (
                <Alert
                  type="error"
                  showIcon
                  message={t("smart.key.title")}
                  description={
                    <>
                      {t("smart.key.before")}
                      <Link to="/settings">{t("smart.connection.link")}</Link>
                      {t("smart.key.after")}
                    </>
                  }
                />
              )}
              <p role="status">
                {t("smart.results.status", {
                  done: completed,
                  total: tasks.length || input.targetCount,
                  available,
                })}
              </p>
              {taskQuery.error && (
                <Alert
                  type="error"
                  showIcon
                  message={t("smart.tasksUnreadable")}
                />
              )}
              <div className="agent-results">
                {tasks.map((task, index) => (
                  <article className="agent-result" key={task.id}>
                    <Space>
                      <strong>
                        {t("smart.image.number", { n: index + 1 })}
                      </strong>
                      <Status state={task.status} task={task} />
                    </Space>
                    <TaskTiming task={task} />
                    <TaskFailureAlert task={task} />
                    {task.refineInstruction && (
                      <p className="hint refine-note">
                        {t("ui.taskTitle.refine", {
                          text: task.refineInstruction,
                        })}
                      </p>
                    )}
                    <RefineModeNote task={task} />
                    <SampleNote task={task} tasks={tasks} />
                    {task.sample && (
                      <p className="hint">
                        {task.sampleArea
                          ? t("smart.sample.areaNote", {
                              area: task.sampleArea,
                            })
                          : t("smart.sample.detailNote")}
                      </p>
                    )}
                    {task.sample && <SampleCheckNote task={task} />}
                    {task.assetIDs.map((id) => (
                      <div key={id}>
                        <button
                          type="button"
                          className="image-open"
                          aria-label={t("smart.image.view", { n: index + 1 })}
                          onClick={() => openViewer(id)}
                        >
                          <AssetImage id={id} arrived={arrivals.has(id)} />
                        </button>
                        <div className="agent-result-actions">
                          <Button
                            disabled={downloads.busy}
                            onClick={() => downloads.downloadAsset(id)}
                          >
                            {t("smart.image.download")}
                          </Button>
                          {task.status === "succeeded" && (
                            <Button
                              aria-label={t("smart.image.refineLabel", {
                                n: index + 1,
                              })}
                              onClick={() => setRefining({ task, assetID: id })}
                            >
                              {t("smart.image.refine")}
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
                          {t("smart.versions", {
                            count: task.previousAssetIDs.length,
                          })}
                        </summary>
                        <div className="agent-version-grid">
                          {task.previousAssetIDs.map((id, version) => (
                            <div key={id}>
                              <button
                                type="button"
                                className="image-open"
                                aria-label={t("smart.versions.view", {
                                  n: index + 1,
                                  version: version + 1,
                                })}
                                onClick={() => openViewer(id)}
                              >
                                <AssetImage id={id} />
                              </button>
                              <Button
                                size="small"
                                block
                                aria-label={t("smart.versions.downloadLabel", {
                                  n: index + 1,
                                  version: version + 1,
                                })}
                                disabled={downloads.busy}
                                onClick={() => downloads.downloadAsset(id)}
                              >
                                {t("smart.versions.download")}
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
                            t("smart.resume.title"),
                            t("smart.resume.content"),
                            () => call("ResumeBlocked", task.id),
                            false,
                            t("smart.resume"),
                          )
                        }
                      >
                        {t("smart.resume")}
                      </Button>
                    )}
                    {task.status === "download_failed" && (
                      <Button
                        disabled={actions.busy}
                        onClick={() =>
                          actions.run(() => call("RetryDownload", task.id))
                        }
                      >
                        {t("smart.redownload")}
                      </Button>
                    )}
                    {["failed", "unknown", "canceled"].includes(
                      task.status,
                    ) && (
                      <Button
                        onClick={() =>
                          actions.confirm(
                            t("smart.retry.oneTitle"),
                            t("smart.retry.oneContent"),
                            () => call("Retry", task.id),
                            false,
                            t("smart.retry.ok"),
                          )
                        }
                      >
                        {t("smart.retry.ok")}
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
                  {t("smart.stopQueued")}
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
                {t("smart.viewer.original")}
              </Button>
              {entry?.current && entry.task.status === "succeeded" && (
                <Button
                  onClick={() => {
                    setViewerIndex(null);
                    setRefining({ task: entry.task, assetID: item.assetID });
                  }}
                >
                  {t("smart.image.refine")}
                </Button>
              )}
            </>
          );
        }}
      />
      <section className="panel agent-history">
        <h2>{t("smart.history")}</h2>
        <DataState
          query={history}
          empty={!history.data?.items.length}
          emptyText={t("smart.history.empty")}
        >
          <div>
            {history.data?.items.map((r) => (
              <div className="agent-history-row" key={r.id}>
                <div>
                  <strong>{r.input.instruction.slice(0, 60)}</strong>
                  <p className="hint">
                    {time(r.createdAt)} ·{" "}
                    {t("smart.history.count", { count: r.input.targetCount })} ·{" "}
                    {r.batchID
                      ? t("smart.history.submitted")
                      : r.status === "analyzing"
                        ? r.progress
                          ? runStepLabel(r.progress)
                          : t("run.processing")
                        : r.status === "needs_input"
                          ? t("smart.stage.needsInput")
                          : t("smart.history.resume")}
                  </p>
                </div>
                <Button
                  onClick={() => {
                    withViewTransition(() => setRunID(r.id));
                    setError("");
                  }}
                >
                  {t("smart.history.open")}
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
          hideOnSinglePage
          onChange={setPage}
        />
      </section>
    </>
  );
}
