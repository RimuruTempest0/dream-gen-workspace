import { useEffect, useState, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import {
  App as AntApp,
  Button,
  Input,
  InputNumber,
  Select as UISelect,
  Alert,
  Space,
  Switch,
} from "antd";
import {
  PictureOutlined,
  SettingOutlined,
  ArrowRightOutlined,
  UploadOutlined,
  DownloadOutlined,
  CloseOutlined,
  EditOutlined,
} from "@ant-design/icons";
import {
  call,
  blankDraft,
  errorText,
  type Connection,
  type GenerateInput,
  type Asset,
  type Template,
  type RecentBatch,
  type ConceptResult,
  type ConceptChoice,
  type ConceptLibrary,
  type MoodChoice,
  type MoodOption,
  type Task,
} from "../api";
import {
  useData,
  useActions,
  useImageDownloads,
  DataState,
  Header,
  Status,
  TaskTiming,
  autoHandledNote,
  AssetImage,
  ModelSelect,
  presetQualities,
  presetSizes,
  time,
  withCurrent,
  ImageViewer,
  type ViewerItem,
} from "../ui";
import { TemplateBar, type TemplateMode } from "../Templates";
import { LicenseNotice } from "../License";
import { gatewayLabel, useGateways } from "../Gateways";
import { ConceptBar, ConceptStage } from "../Concepts";
import { MoodBar, MoodGuide, MoodNudges, MoodTags } from "../Mood";

// What the prompt box held before a concept rewrite, and what the rewrite put there.
type ConceptUndo = {
  prompt: string;
  batch: boolean;
  labels?: string[];
  after: string;
};

export function Workbench() {
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
  const { modal, notification } = AntApp.useApp();
  const [stageOpen, setStageOpen] = useState(false);
  const [conceptUndo, setConceptUndo] = useState<ConceptUndo | null>(null);
  const conceptEntry = useRef<HTMLButtonElement>(null);
  const [dragging, setDragging] = useState(false);
  const [viewerIndex, setViewerIndex] = useState<number | null>(null);
  // Mood guide (0.7.26): the guide mode is remembered; with it off the page is
  // exactly as before and a saved mood simply does not apply.
  const guideMode = useData<boolean>(["guide-mode"], "GetGuideMode");
  const guideOn = !!guideMode.data;
  const [guideOpen, setGuideOpen] = useState(false);
  const moodEntry = useRef<HTMLButtonElement>(null);
  const conceptLib = useData<ConceptLibrary>(["concepts"], "ListConcepts");
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
  const moodActive = guideOn && !!input.mood;
  const compare = moodActive && !!input.compare;
  // With a mood the prompt may be empty; a comparison is one image per rendering.
  const total = compare
    ? 2
    : (moodActive ? Math.max(prompts.length, 1) : prompts.length) * input.count;
  const pickedConcept = (() => {
    const c = input.concept;
    const f = conceptLib.data?.forms.find((x) => x.id === c?.form);
    const st = conceptLib.data?.stances.find((x) => x.id === c?.stance);
    return f && st ? `${f.name} × ${st.name}` : "";
  })();
  function setGuide(on: boolean) {
    qc.setQueryData(["guide-mode"], on);
    call("SetGuideMode", on).catch((e) => {
      qc.invalidateQueries({ queryKey: ["guide-mode"] });
      notification.error({
        message: "引导模式未保存",
        description: errorText(e),
      });
    });
  }
  function applyMood(mood: MoodChoice) {
    change({ mood });
    setGuideOpen(false);
  }
  function clearMood() {
    change({ mood: undefined, concept: undefined, compare: false });
  }
  function pickConcept(concept: ConceptChoice) {
    change({ concept });
    setStageOpen(false);
  }
  function setCompare(on: boolean) {
    if (on) setBatch(false);
    change({ compare: on });
  }
  // 再靠近一点: one more image from this one, one direction changed.
  function nudge(t: Task, assetID: string, n: MoodOption) {
    const requestID = crypto.randomUUID();
    actions.confirm(
      `再出 1 张（${n.name}）？`,
      <>
        以这张图为底图，只往“{n.name}
        ”改一点，其余保持。这是新的生图请求，费用以图片服务实际计费为准。
      </>,
      async () => {
        await call("NudgeMoodImage", {
          requestID,
          taskID: t.id,
          assetID,
          direction: n.id,
        });
        setViewerIndex(null);
        await qc.invalidateQueries({ queryKey: ["tasks"] });
      },
      false,
      "再出 1 张",
    );
  }
  // Labels count per prompt line; once the lines no longer match they are dropped.
  const conceptLabels =
    input.conceptLabels?.length === prompts.length
      ? input.conceptLabels
      : undefined;
  // The text a concept rewrite starts from. Several batch lines are replaced, so
  // the user confirms first; the first line is the base.
  function prepareConcept(n: number) {
    const lines = prompt
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean);
    if (!batch || lines.length < 2) return Promise.resolve(prompt.trim());
    return new Promise<string | null>((resolve) => {
      modal.confirm({
        title: `替换当前 ${lines.length} 条批量提示词？`,
        content:
          (n > 1
            ? `将以第一条为底稿生成 ${n} 条对比提示词，替换现有内容。`
            : "将以第一条为底稿融入概念，替换现有内容。") +
          "改写后可点“撤销改写”恢复。",
        okText: "替换",
        cancelText: "取消",
        autoFocusButton: "cancel",
        onOk: () => resolve(lines[0]),
        onCancel: () => resolve(null),
      });
    });
  }
  function applyConcept(r: ConceptResult) {
    const single = r.prompts.length === 1;
    const next = single
      ? r.prompts[0]
      : r.prompts.map((s) => s.replace(/\s*\n\s*/g, " ").trim()).join("\n");
    setConceptUndo({ prompt, batch, labels: input.conceptLabels, after: next });
    setPrompt(next);
    // Several prompts are one line each in batch mode; one prompt may keep its lines.
    if (!single) setBatch(true);
    else if (next.includes("\n")) setBatch(false);
    change({ conceptLabels: r.labels });
    setStageOpen(false);
    if (r.usedLLM)
      notification.success({
        message: single
          ? "已按概念改写提示词"
          : `已生成 ${r.prompts.length} 条对比提示词`,
      });
    else notification.info({ message: "未配置 AI 模型，已直接附加概念要点" });
  }
  function undoConcept() {
    if (!conceptUndo) return;
    setPrompt(conceptUndo.prompt);
    setBatch(conceptUndo.batch);
    change({ conceptLabels: conceptUndo.labels });
    setConceptUndo(null);
    promptRef.current?.focus();
  }
  function change(p: Partial<GenerateInput>) {
    setInput((v) => ({ ...v, ...p }));
    setFormError("");
  }
  // Every image of the recent batches, in display order, for the large viewer.
  const viewerItems: ViewerItem[] = [];
  const viewerTasks = new Map<string, Task>();
  tasks.data?.forEach((b) =>
    b.tasks.forEach((t) =>
      t.assetIDs.forEach((assetID) => {
        viewerItems.push({
          assetID,
          label: t.prompt.length > 28 ? t.prompt.slice(0, 28) + "…" : t.prompt,
        });
        viewerTasks.set(assetID, t);
      }),
    ),
  );
  function openViewer(assetID: string) {
    const i = viewerItems.findIndex((v) => v.assetID === assetID);
    if (i >= 0) setViewerIndex(i);
  }
  function addReference(assetID: string) {
    if (input.references.includes(assetID)) {
      notification.info({ message: "这张图已经是参考图" });
      return;
    }
    if (input.references.length >= 4) {
      notification.warning({ message: "参考图最多 4 张，请先移除一张" });
      return;
    }
    change({ references: [...input.references, assetID] });
    notification.success({ message: "已添加为参考图" });
  }
  // Puts a past prompt and its size and quality back in the form. The current text
  // is replaced only after a confirm, so nothing typed is lost by a stray click.
  function reusePrompt(t: Task) {
    // A mood image brings back the user's own prompt, its mood and concept.
    const text = t.moodChoice ? t.basePrompt || "" : t.prompt;
    const apply = () => {
      setPrompt(text);
      setBatch(false);
      setConceptUndo(null);
      change({
        conceptLabels: undefined,
        ...(t.moodChoice
          ? { mood: t.moodChoice, concept: t.conceptChoice, compare: false }
          : {}),
        ...(t.size ? { size: t.size } : {}),
        ...(t.quality ? { quality: t.quality } : {}),
      });
      if (t.moodChoice && !guideOn) setGuide(true);
      setViewerIndex(null);
      requestAnimationFrame(() => {
        promptRef.current?.focus();
        document
          .getElementById("prompt")
          ?.scrollIntoView({ block: "center", behavior: "smooth" });
      });
    };
    const current = prompt.trim();
    if (!current || current === text.trim()) return apply();
    modal.confirm({
      title: "用这条提示词替换当前内容？",
      content: t.moodChoice
        ? "同时套用它的心境、尺寸和质量。"
        : "同时套用它的尺寸和质量。",
      okText: "替换",
      cancelText: "取消",
      autoFocusButton: "cancel",
      onOk: apply,
    });
  }
  // Why the generate button is off, said next to it instead of a silent grey button.
  const blocked = !ready
    ? ""
    : !connections.data?.length
      ? "先添加图片服务"
      : !input.connectionID
        ? "请选择图片服务"
        : !selected?.hasKey
          ? "所选图片服务未保存 API Key"
          : total > 200
            ? `一次最多 200 张，当前 ${total} 张`
            : "";
  async function generate() {
    if (!selected?.hasKey) {
      setFormError("请先选择已保存密钥的连接。");
      return;
    }
    if (!(input.model || selected.model)) {
      setFormError("请选择模型。");
      return;
    }
    if (!prompts.length && !moodActive) {
      setFormError("请填写提示词。");
      promptRef.current?.focus();
      return;
    }
    setFormError("");
    const requestID = crypto.randomUUID();
    actions.confirm(
      `生成 ${total} 张图片？`,
      moodActive ? (
        <>
          将使用“{selected.name}”按心境改写提示词后生成 {total} 张
          {compare ? "（写实、写意各 1 张，会产生双倍费用）" : ""}
          。改写由 AI
          模型完成，改写没有成功的图片不发送、不计费。费用以图片服务实际计费为准。
        </>
      ) : (
        <>
          将使用“{selected.name}”提交 {total}{" "}
          个子任务。费用以图片服务实际计费为准。
        </>
      ),
      async () => {
        await call("Submit", {
          ...input,
          prompts,
          conceptLabels: moodActive ? undefined : conceptLabels,
          // A mood (and the concept picked with it) only applies in the guide mode.
          mood: moodActive ? input.mood : undefined,
          concept: moodActive ? input.concept : undefined,
          compare: compare || undefined,
          count: compare ? 1 : input.count,
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
      change({ conceptLabels: undefined });
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
        title="专业模式"
        sub="自己写提示词，选择连接、模型和参数。结果自动保存在本机。"
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
            <Space size={18}>
              <Space>
                <span>引导模式</span>
                <Switch
                  checked={guideOn}
                  onChange={setGuide}
                  disabled={guideMode.isPending}
                  aria-label="引导模式"
                />
              </Space>
              <Space>
                <span>批量提示词</span>
                <Switch
                  checked={batch}
                  onChange={setBatch}
                  disabled={compare}
                  aria-label="批量提示词"
                />
              </Space>
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
            onChange={(e) => {
              setPrompt(e.target.value);
              setFormError("");
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" && e.ctrlKey && !e.nativeEvent.isComposing)
                generate();
            }}
            autoSize={{ minRows: 7, maxRows: 16 }}
            style={{ resize: "none" }}
            placeholder={
              moodActive
                ? "写下想画什么，气氛交给心境；也可以留空，按心境自定画面"
                : "例如：白色背景上的陶瓷咖啡杯，柔和自然光，突出釉面质感，电商产品摄影"
            }
            aria-invalid={!!formError}
            aria-describedby="generate-error"
          />
          <div className="prompt-meta">
            <span>
              {prompt.length} 字 ·{" "}
              {moodActive && !prompts.length
                ? "提示词留空：按心境自定画面"
                : batch
                  ? `${prompts.length} 条提示词，每行独立生成`
                  : input.references.length
                    ? `图生图 · ${input.references.length} 张参考图`
                    : "文生图 / 添加参考图后转为图生图"}
            </span>
          </div>
          {guideOn && (
            <>
              <MoodBar
                mood={input.mood}
                disabled={!ready}
                onOpen={() => setGuideOpen(true)}
                onClear={clearMood}
                openRef={moodEntry}
              />
              <MoodGuide
                open={guideOpen}
                value={input.mood}
                onClose={() => setGuideOpen(false)}
                onAfterClose={(applied) =>
                  applied
                    ? promptRef.current?.focus()
                    : moodEntry.current?.focus()
                }
                onApply={applyMood}
              />
            </>
          )}
          <ConceptBar
            labels={
              moodActive
                ? pickedConcept
                  ? [pickedConcept]
                  : []
                : conceptLabels || []
            }
            canUndo={
              !moodActive && !!conceptUndo && conceptUndo.after === prompt
            }
            disabled={!ready}
            onOpen={() => setStageOpen(true)}
            onClear={() => {
              if (moodActive) return change({ concept: undefined });
              change({ conceptLabels: undefined });
              setConceptUndo(null);
            }}
            onUndo={undoConcept}
            openRef={conceptEntry}
          />
          <ConceptStage
            open={stageOpen}
            hasReferences={input.references.length > 0}
            onClose={() => setStageOpen(false)}
            onAfterClose={(applied) =>
              applied
                ? promptRef.current?.focus()
                : conceptEntry.current?.focus()
            }
            prepare={prepareConcept}
            onComposed={applyConcept}
            onPick={moodActive ? pickConcept : undefined}
          />
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
            className={"reference-area" + (dragging ? " dragging" : "")}
            onDragOver={(e) => {
              e.preventDefault();
              if (input.references.length < 4) setDragging(true);
            }}
            onDragLeave={(e) => {
              if (!e.currentTarget.contains(e.relatedTarget as Node))
                setDragging(false);
            }}
            onDrop={(e) => {
              e.preventDefault();
              setDragging(false);
              if (e.dataTransfer.files.length)
                browserDrop(e.dataTransfer.files);
            }}
          >
            {input.references.length ? (
              input.references.map((id, i) => (
                <div className="reference" key={id}>
                  <AssetImage id={id} />
                  <Button
                    size="small"
                    icon={<CloseOutlined aria-hidden="true" />}
                    aria-label={`移除参考图 ${i + 1}`}
                    onClick={() =>
                      change({
                        references: input.references.filter((x) => x !== id),
                      })
                    }
                  />
                </div>
              ))
            ) : (
              <button
                type="button"
                className="reference-empty"
                onClick={addFiles}
                disabled={actions.busy}
              >
                <UploadOutlined aria-hidden="true" />
                <span>
                  {dragging
                    ? "松开即可添加"
                    : "拖入 PNG、JPEG、WebP，或点击添加图片"}
                </span>
                <span className="muted">
                  也可在“最近生成”里把结果设为参考图
                </span>
              </button>
            )}
          </div>
          <div className="divider" />
          <div className="parameter-grid">
            <div>
              <label className="field-label">图片服务</label>
              <UISelect
                aria-label="图片服务"
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
                value={compare ? 1 : input.count}
                min={1}
                max={20}
                disabled={compare}
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
          {guideOn && (
            <div className="mood-compare" data-on={compare}>
              <Switch
                id="mood-compare"
                checked={compare}
                disabled={!input.mood}
                onChange={setCompare}
                aria-describedby="mood-compare-note"
              />
              <label htmlFor="mood-compare" className="mood-compare-label">
                对比出图
              </label>
              <span id="mood-compare-note" className="mood-compare-note">
                {input.mood
                  ? "写实、写意各出 1 张，挑更像的一张 · 会产生双倍费用"
                  : "先选好心境才能对比出图"}
              </span>
            </div>
          )}
          <div className="generate-footer">
            <span
              className={blocked ? "footer-blocked" : "muted"}
              role="status"
            >
              {blocked
                ? blocked
                : draftError
                  ? "草稿未保存：" + draftError
                  : ready
                    ? compare
                      ? "对比出图：写实、写意各 1 张 · 会产生双倍费用"
                      : batch && prompts.length > 1
                        ? `${prompts.length} 条 × 每条 ${input.count} 张 · Ctrl + Enter 生成`
                        : "草稿保存在本机 · Ctrl + Enter 生成"
                    : "正在读取草稿"}
            </span>
            <Button
              type="primary"
              size="large"
              icon={<ArrowRightOutlined aria-hidden="true" />}
              onClick={generate}
              disabled={!ready || !!blocked}
              loading={actions.busy}
            >
              生成{total > 1 ? ` ${total} 张图片` : "图片"}
            </Button>
          </div>
          {!connections.isPending && !connections.data?.length && (
            <Alert
              showIcon
              type="info"
              message="先添加一个图片服务"
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
                            {t.assetIDs.map((id, n) => (
                              <div key={id}>
                                <button
                                  type="button"
                                  className="image-open"
                                  aria-label={`查看大图：${t.prompt.slice(0, 24)}${t.assetIDs.length > 1 ? `（第 ${n + 1} 张）` : ""}`}
                                  onClick={() => openViewer(id)}
                                >
                                  <AssetImage id={id} />
                                </button>
                                <div className="result-image-actions">
                                  <Button
                                    size="small"
                                    icon={
                                      <DownloadOutlined aria-hidden="true" />
                                    }
                                    disabled={downloads.busy}
                                    onClick={() => downloads.downloadAsset(id)}
                                  >
                                    下载图片
                                  </Button>
                                  <Button
                                    size="small"
                                    disabled={
                                      input.references.length >= 4 ||
                                      input.references.includes(id)
                                    }
                                    onClick={() => addReference(id)}
                                  >
                                    {input.references.includes(id)
                                      ? "已是参考图"
                                      : "作为参考图"}
                                  </Button>
                                </div>
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
                          <div className="result-status">
                            <Status state={t.status} task={t} />
                            {t.concept && (
                              <span className="concept-tag">{t.concept}</span>
                            )}
                            <MoodTags task={t} />
                          </div>
                          <p className="result-prompt" title={t.prompt}>
                            {t.prompt ||
                              (t.rewording
                                ? "按心境自定画面，正在写提示词"
                                : "")}
                          </p>
                          {t.mood &&
                            t.status === "succeeded" &&
                            t.assetIDs[0] && (
                              <MoodNudges
                                onNudge={(n) => nudge(t, t.assetIDs[0], n)}
                              />
                            )}
                          <Button
                            type="link"
                            size="small"
                            className="reuse-prompt"
                            icon={<EditOutlined aria-hidden="true" />}
                            onClick={() => reusePrompt(t)}
                          >
                            复用提示词
                          </Button>
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
      <ImageViewer
        items={viewerItems}
        index={viewerIndex}
        onIndex={setViewerIndex}
        onClose={() => setViewerIndex(null)}
        actions={(item) => {
          const t = viewerTasks.get(item.assetID);
          return (
            <>
              <Button
                icon={<DownloadOutlined aria-hidden="true" />}
                disabled={downloads.busy}
                onClick={() => downloads.downloadAsset(item.assetID)}
              >
                下载图片
              </Button>
              <Button
                disabled={
                  input.references.length >= 4 ||
                  input.references.includes(item.assetID)
                }
                onClick={() => addReference(item.assetID)}
              >
                {input.references.includes(item.assetID)
                  ? "已是参考图"
                  : "作为参考图"}
              </Button>
              {t && (
                <Button
                  icon={<EditOutlined aria-hidden="true" />}
                  onClick={() => reusePrompt(t)}
                >
                  复用提示词
                </Button>
              )}
              {t?.mood && t.status === "succeeded" && (
                <MoodNudges onNudge={(n) => nudge(t, item.assetID, n)} />
              )}
            </>
          );
        }}
      />
    </>
  );
}
