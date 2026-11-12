import { useEffect, useState, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import {
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
} from "../ui";
import { TemplateBar, type TemplateMode } from "../Templates";
import { LicenseNotice } from "../License";
import { gatewayLabel, useGateways } from "../Gateways";

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
