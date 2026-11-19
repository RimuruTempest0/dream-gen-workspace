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
  TaskErrorText,
  TaskTiming,
  autoHandledNote,
  AssetImage,
  taskWorking,
  useArrivals,
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
import { t as tr } from "../i18n";

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
  const arrivals = useArrivals(
    "recent",
    tasks.data?.flatMap((b) => b.tasks.flatMap((t) => t.assetIDs)),
  );
  const draft = useData<GenerateInput>(["draft"], "GetDraft");
  const [input, setInput] = useState<GenerateInput>(blankDraft);
  const [prompt, setPrompt] = useState("");
  const [batch, setBatch] = useState(false);
  const [ready, setReady] = useState(false);
  const [draftError, setDraftError] = useState("");
  const [formError, setFormError] = useState("");
  const promptRef = useRef<null | any>(null);
  // Brief marks for a change made from the other column: the reference image
  // that was just added, and the prompt that was just filled in.
  const [newReference, setNewReference] = useState("");
  const [promptFilled, setPromptFilled] = useState(false);
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
        message: tr("workbench.guide.notSaved"),
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
      tr("workbench.nudge.title", { name: n.name }),
      tr("workbench.nudge.body", { name: n.name }),
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
      tr("workbench.nudge.ok"),
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
        title: tr("workbench.concept.replaceTitle", { count: lines.length }),
        content:
          n > 1
            ? tr("workbench.concept.replaceCompare", { count: n })
            : tr("workbench.concept.replaceFuse"),
        okText: tr("workbench.replace"),
        cancelText: tr("common.cancel"),
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
          ? tr("workbench.concept.rewritten")
          : tr("workbench.concept.compared", { count: r.prompts.length }),
      });
    else notification.info({ message: tr("workbench.concept.noLLM") });
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
      notification.info({ message: tr("workbench.ref.already") });
      return;
    }
    if (input.references.length >= 4) {
      notification.warning({ message: tr("workbench.ref.max") });
      return;
    }
    change({ references: [...input.references, assetID] });
    setNewReference(assetID);
    notification.success({ message: tr("workbench.ref.added") });
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
      setPromptFilled(true);
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
      title: tr("workbench.reuse.title"),
      content: t.moodChoice
        ? tr("workbench.reuse.withMood")
        : tr("workbench.reuse.plain"),
      okText: tr("workbench.replace"),
      cancelText: tr("common.cancel"),
      autoFocusButton: "cancel",
      onOk: apply,
    });
  }
  // Why the generate button is off, said next to it instead of a silent grey button.
  const blocked = !ready
    ? ""
    : !connections.data?.length
      ? tr("workbench.blocked.noService")
      : !input.connectionID
        ? tr("workbench.blocked.pickService")
        : !selected?.hasKey
          ? tr("workbench.blocked.noKey")
          : total > 200
            ? tr("workbench.blocked.tooMany", { max: 200, total })
            : "";
  async function generate() {
    if (!selected?.hasKey) {
      setFormError(tr("workbench.error.noKey"));
      return;
    }
    if (!(input.model || selected.model)) {
      setFormError(tr("workbench.error.noModel"));
      return;
    }
    if (!prompts.length && !moodActive) {
      setFormError(tr("workbench.error.noPrompt"));
      promptRef.current?.focus();
      return;
    }
    setFormError("");
    const requestID = crypto.randomUUID();
    actions.confirm(
      tr("workbench.confirm.title", { total }),
      tr(
        moodActive
          ? compare
            ? "workbench.confirm.moodCompare"
            : "workbench.confirm.mood"
          : "workbench.confirm.plain",
        { name: selected.name, total },
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
        setFormError(tr("workbench.error.tooLarge"));
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
        title={tr("shell.nav.pro")}
        sub={tr("workbench.sub")}
        action={
          <Button
            icon={<SettingOutlined aria-hidden="true" />}
            onClick={() => navigate("/settings")}
          >
            {tr("workbench.manage")}
          </Button>
        }
      />
      {connections.error && (
        <Alert
          type="error"
          message={tr("workbench.connError")}
          description={errorText(connections.error)}
        />
      )}
      <div className="workbench">
        <section className="panel composer">
          <div className="panel-heading">
            <h2>{tr("workbench.compose")}</h2>
            <Space size={18}>
              <Space>
                <span>{tr("workbench.guide")}</span>
                <Switch
                  checked={guideOn}
                  onChange={setGuide}
                  disabled={guideMode.isPending}
                  aria-label={tr("workbench.guide")}
                />
              </Space>
              <Space>
                <span>{tr("workbench.batch")}</span>
                <Switch
                  checked={batch}
                  onChange={setBatch}
                  disabled={compare}
                  aria-label={tr("workbench.batch")}
                />
              </Space>
            </Space>
          </div>
          <div className="field-header prompt-header">
            <label className="field-label" htmlFor="prompt">
              {batch ? tr("workbench.promptLines") : tr("workbench.prompt")}
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
            className={promptFilled ? "is-filled" : undefined}
            onAnimationEnd={(e) => {
              if (e.animationName === "mark-filled") setPromptFilled(false);
            }}
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
                ? tr("workbench.placeholder.mood")
                : tr("workbench.placeholder.plain")
            }
            aria-invalid={!!formError}
            aria-describedby="generate-error"
          />
          <div className="prompt-meta">
            <span>
              {tr("workbench.meta.chars", { count: prompt.length })} ·{" "}
              {moodActive && !prompts.length
                ? tr("workbench.meta.moodEmpty")
                : batch
                  ? tr("workbench.meta.batch", { count: prompts.length })
                  : input.references.length
                    ? tr("workbench.meta.img2img", {
                        count: input.references.length,
                      })
                    : tr("workbench.meta.txt2img")}
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
              {tr("workbench.refs.label")}{" "}
              <span className="muted">{tr("workbench.refs.hint")}</span>
            </label>
            <Button
              size="small"
              icon={<UploadOutlined aria-hidden="true" />}
              onClick={addFiles}
              disabled={input.references.length >= 4}
              loading={actions.busy}
            >
              {tr("workbench.refs.add")}
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
                <div
                  className={
                    id === newReference ? "reference is-new" : "reference"
                  }
                  key={id}
                  onAnimationEnd={(e) => {
                    if (e.animationName === "mark-new") setNewReference("");
                  }}
                >
                  <AssetImage id={id} />
                  <Button
                    size="small"
                    icon={<CloseOutlined aria-hidden="true" />}
                    aria-label={tr("workbench.refs.remove", { n: i + 1 })}
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
                    ? tr("workbench.refs.drop")
                    : tr("workbench.refs.empty")}
                </span>
                <span className="muted">{tr("workbench.refs.fromRecent")}</span>
              </button>
            )}
          </div>
          <div className="divider" />
          <div className="parameter-grid">
            <div>
              <label className="field-label">{tr("workbench.service")}</label>
              <UISelect
                aria-label={tr("workbench.service")}
                value={input.connectionID || undefined}
                placeholder={tr("workbench.servicePlaceholder")}
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
                {tr("workbench.model")}
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
                {tr("workbench.size")}
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
                {tr("workbench.quality")}
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
                {tr("workbench.count")}
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
          <p className="hint">{tr("workbench.autoHint")}</p>
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
                {tr("workbench.compare")}
              </label>
              <span id="mood-compare-note" className="mood-compare-note">
                {input.mood
                  ? tr("workbench.compare.note")
                  : tr("workbench.compare.needMood")}
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
                  ? tr("workbench.footer.draftError", { error: draftError })
                  : ready
                    ? compare
                      ? tr("workbench.footer.compare")
                      : batch && prompts.length > 1
                        ? tr("workbench.footer.batch", {
                            lines: prompts.length,
                            count: input.count,
                          })
                        : tr("workbench.footer.saved")
                    : tr("workbench.footer.loading")}
            </span>
            <Button
              type="primary"
              size="large"
              icon={<ArrowRightOutlined aria-hidden="true" />}
              onClick={generate}
              disabled={!ready || !!blocked}
              loading={actions.busy}
            >
              {total > 1
                ? tr("workbench.generate.many", { total })
                : tr("workbench.generate.one")}
            </Button>
          </div>
          {!connections.isPending && !connections.data?.length && (
            <Alert
              showIcon
              type="info"
              message={tr("workbench.noService.title")}
              description={tr("workbench.noService.desc")}
              action={
                <Button onClick={() => navigate("/settings")}>
                  {tr("workbench.noService.action")}
                </Button>
              }
            />
          )}
        </section>
        <section className="panel result-panel">
          <div className="panel-heading">
            <h2>{tr("workbench.recent")}</h2>
            <Button type="link" onClick={() => navigate("/tasks")}>
              {tr("workbench.allRecords")}
            </Button>
          </div>
          <DataState
            query={tasks}
            empty={!tasks.data?.length}
            emptyText={tr("workbench.empty")}
          >
            <div className="recent-batches">
              {tasks.data?.map((batch) => (
                <section
                  className="recent-batch"
                  key={batch.id}
                  aria-label={tr("workbench.batchLabel", {
                    id: batch.id.slice(0, 8),
                  })}
                >
                  <div className="batch-heading">
                    <div>
                      <strong>{time(batch.createdAt)}</strong>
                      <p className="hint">
                        {tr(
                          batch.active
                            ? "workbench.batch.progressActive"
                            : "workbench.batch.progress",
                          {
                            done: batch.completed,
                            total: batch.total,
                            images: batch.imageCount,
                          },
                        )}
                      </p>
                    </div>
                    <Button
                      icon={<DownloadOutlined aria-hidden="true" />}
                      disabled={!batch.imageCount || downloads.busy}
                      onClick={() => downloads.downloadBatch(batch.id)}
                    >
                      {batch.completed === batch.total
                        ? tr("workbench.batch.download", {
                            count: batch.imageCount,
                          })
                        : tr("workbench.batch.downloadDone", {
                            count: batch.imageCount,
                          })}
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
                                  aria-label={
                                    t.assetIDs.length > 1
                                      ? tr("workbench.viewNth", {
                                          prompt: t.prompt.slice(0, 24),
                                          n: n + 1,
                                        })
                                      : tr("workbench.view", {
                                          prompt: t.prompt.slice(0, 24),
                                        })
                                  }
                                  onClick={() => openViewer(id)}
                                >
                                  <AssetImage
                                    id={id}
                                    arrived={arrivals.has(id)}
                                  />
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
                                    {tr("workbench.download")}
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
                                      ? tr("workbench.isRef")
                                      : tr("workbench.asRef")}
                                  </Button>
                                </div>
                              </div>
                            ))}
                          </div>
                        ) : (
                          <div
                            className={
                              taskWorking(t)
                                ? "result-placeholder is-working"
                                : "result-placeholder"
                            }
                          >
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
                              (t.rewording ? tr("workbench.rewording") : "")}
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
                            {tr("workbench.reuse")}
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
                              <TaskErrorText error={t.error} />
                            ))}
                        </div>
                      </div>
                    ))}
                  </div>
                  {batch.total > batch.tasks.length && (
                    <p className="hint">
                      {tr("workbench.batch.preview", {
                        count: batch.tasks.length,
                      })}
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
                {tr("workbench.download")}
              </Button>
              <Button
                disabled={
                  input.references.length >= 4 ||
                  input.references.includes(item.assetID)
                }
                onClick={() => addReference(item.assetID)}
              >
                {input.references.includes(item.assetID)
                  ? tr("workbench.isRef")
                  : tr("workbench.asRef")}
              </Button>
              {t && (
                <Button
                  icon={<EditOutlined aria-hidden="true" />}
                  onClick={() => reusePrompt(t)}
                >
                  {tr("workbench.reuse")}
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
