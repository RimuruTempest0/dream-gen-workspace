import {
  useEffect,
  useState,
  useRef,
  type ReactNode,
  type ComponentProps,
} from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  App as AntApp,
  Button,
  Input,
  Alert,
  Empty,
  Modal,
  Popover,
  Select,
  Space,
  Spin,
  Tag,
} from "antd";
import { EyeOutlined, EyeInvisibleOutlined } from "@ant-design/icons";
import { call, errorText, stateNames, type Asset, type Task } from "./api";

export function useData<T>(
  key: unknown[],
  method: string,
  args: unknown[] = [],
  interval?: number,
) {
  return useQuery<T>({
    queryKey: key,
    queryFn: () => call<T>(method, ...args),
    refetchInterval: interval,
  });
}
// A saved image never changes, so refreshing after an action skips the images.
export const notImages = {
  predicate: (q: { queryKey: readonly unknown[] }) => q.queryKey[0] !== "image",
};

// Runs at most once per wait, after a burst ends: progress events arrive in bursts
// while images generate.
export function coalesce(fn: () => void, wait = 400) {
  let timer: number | undefined;
  return () => {
    if (timer === undefined)
      timer = window.setTimeout(() => {
        timer = undefined;
        fn();
      }, wait);
  };
}

// Common OpenAI-compatible sizes; a previously saved custom size stays selectable.
export const presetSizes = [
  { value: "auto", label: "自动（由模型决定）" },
  { value: "1024x1024", label: "1024×1024（1:1 方图）" },
  { value: "1536x1024", label: "1536×1024（3:2 横图）" },
  { value: "1024x1536", label: "1024×1536（2:3 竖图）" },
  { value: "1792x1024", label: "1792×1024（16:9 横图）" },
  { value: "1024x1792", label: "1024×1792（9:16 竖图）" },
];
export const presetQualities = [
  { value: "auto", label: "自动" },
  { value: "low", label: "低" },
  { value: "medium", label: "中" },
  { value: "high", label: "高" },
];
// The presets plus a saved value that is not one of them.
export function withCurrent(
  options: { value: string; label: string }[],
  current?: string,
) {
  return !current || options.some((o) => o.value === current)
    ? options
    : [...options, { value: current, label: current }];
}

// What a person recognises a task by. Prompts open with fixed lines (the sample note,
// the refine wrapper), which made every row of a set look the same.
export function taskTitle(t: Task) {
  if (t.refineInstruction) return "优化：" + t.refineInstruction;
  const lines = t.prompt
    .split("\n")
    .map((s) => s.trim())
    .filter((s) => s && !s.startsWith("样板图："));
  return (lines[0] || t.prompt).replace(/^用途[：:]\s*/, "");
}

// Generated images are saved under random file names, which say nothing.
export function assetLabel(a: Asset) {
  return a.kind === "reference" ? a.name : `生成图片 · ${time(a.createdAt)}`;
}

// Model picker for an image connection. The list is a GET /models of the saved
// connection (no generation, no cost); "" means the connection's default model.
export function ModelSelect({
  connection,
  value,
  onChange,
  id,
  disabled,
  load = true,
}: {
  connection?: { id?: string; model?: string };
  value: string;
  onChange: (model: string) => void;
  id: string;
  disabled?: boolean;
  load?: boolean;
}) {
  const models = useQuery({
    queryKey: ["connection-models", connection?.id],
    queryFn: () => call<string[]>("ListConnectionModels", connection!.id),
    enabled: !!connection?.id && load,
    staleTime: 5 * 60 * 1000,
    retry: false,
  });
  const options = [
    { value: "", label: `使用连接默认（${connection?.model || "未设置"}）` },
    ...[...new Set([...(models.data || []), ...(value ? [value] : [])])].map(
      (model) => ({ value: model, label: model }),
    ),
  ];
  return (
    <>
      <Select
        id={id}
        className="full-width"
        showSearch
        optionFilterProp="label"
        value={value}
        disabled={disabled || !connection}
        loading={models.isFetching}
        options={options}
        onChange={onChange}
      />
      {models.isError && (
        <p className="hint">暂时无法获取模型列表，可使用连接默认模型。</p>
      )}
    </>
  );
}

export function useActions() {
  const { notification, modal } = AntApp.useApp();
  const qc = useQueryClient();
  const [busy, setBusy] = useState(false);
  const working = useRef(false);
  const dialogOpen = useRef(false);
  const feedbackKey = useRef("action-" + crypto.randomUUID());
  async function run<T>(
    fn: () => Promise<T>,
    success?: string,
  ): Promise<T | undefined> {
    if (working.current) return undefined;
    working.current = true;
    setBusy(true);
    try {
      const out = await fn();
      await qc.invalidateQueries(notImages);
      notification.destroy(feedbackKey.current);
      if (success) notification.success({ message: success });
      return out;
    } catch (e) {
      notification.error({
        key: feedbackKey.current,
        message: "操作未完成",
        description: errorText(e),
        duration: 0,
      });
      return undefined;
    } finally {
      working.current = false;
      setBusy(false);
    }
  }
  function confirm(
    title: string,
    content: ReactNode,
    fn: () => Promise<unknown>,
    danger = false,
    okText?: string,
    cancelText = "取消",
  ) {
    if (dialogOpen.current) return;
    dialogOpen.current = true;
    modal.confirm({
      title,
      content,
      okText: okText || (danger ? "确认执行" : "继续"),
      cancelText,
      okButtonProps: { danger },
      autoFocusButton: "cancel",
      afterClose: () => {
        dialogOpen.current = false;
      },
      onOk: async () => {
        try {
          await fn();
          await qc.invalidateQueries(notImages);
          notification.destroy(feedbackKey.current);
        } catch (e) {
          notification.error({
            key: feedbackKey.current,
            message: "操作未完成",
            description: errorText(e),
            duration: 0,
          });
          throw e;
        }
      },
    });
  }
  return { run, busy, confirm };
}
// An image can be cancelled until its result has come back (downloading is kept).
const cancelableStates = ["queued", "submitting", "running", "blocked"];
export function CancelTaskButton({
  task,
  size,
}: {
  task: Task;
  size?: "small";
}) {
  const actions = useActions();
  if (!cancelableStates.includes(task.status)) return null;
  const sent = task.status === "submitting" || task.status === "running";
  return (
    <Button
      size={size}
      danger
      onClick={() =>
        actions.confirm(
          "是否确认取消生成？",
          sent
            ? "这张图已提交到中转站。取消后不再等待结果；中转站可能仍会生成并计费，请以中转站记录为准。"
            : "这张图尚未提交，取消后不会计费。",
          () => call("CancelTask", task.id),
          true,
          "取消生成",
          "继续生成",
        )
      }
    >
      取消生成
    </Button>
  );
}
export function SecretInput(props: ComponentProps<typeof Input>) {
  const [show, setShow] = useState(false);
  return (
    <Input
      {...props}
      type={show ? "text" : "password"}
      suffix={
        <Button
          type="text"
          size="small"
          aria-label={show ? "隐藏 API Key" : "显示 API Key"}
          aria-pressed={show}
          icon={
            show ? (
              <EyeInvisibleOutlined aria-hidden="true" />
            ) : (
              <EyeOutlined aria-hidden="true" />
            )
          }
          onClick={() => setShow((v) => !v)}
        />
      }
    />
  );
}
export function useImageDownloads() {
  const actions = useActions();
  const { notification } = AntApp.useApp();
  async function downloadAsset(id: string) {
    await actions.run(async () => {
      const asset = await call<Asset>("GetExportAsset", id);
      const path = await call<string>(
        "ChooseExportPath",
        asset.path.split("/").pop(),
      );
      if (!path) return;
      await call("ExportAssetToPath", id, path);
      notification.success({ message: "图片已保存" });
    });
  }
  async function downloadBatch(id: string) {
    await actions.run(async () => {
      const path = await call<string>(
        "ChooseExportPath",
        `ImageWorkbench-${id.slice(0, 8)}.zip`,
      );
      if (!path) return;
      const count = await call<number>("ExportBatchToPath", id, path);
      notification.success({ message: `已保存套图 ZIP（${count} 张）` });
    });
  }
  return { downloadAsset, downloadBatch, busy: actions.busy };
}
export function DataState({
  query,
  children,
  empty = false,
  emptyText = "暂无内容",
  action,
}: {
  query: { isPending: boolean; error: unknown; refetch: () => unknown };
  children: ReactNode;
  empty?: boolean;
  emptyText?: string;
  action?: ReactNode;
}) {
  if (query.isPending)
    return (
      <div className="state-panel">
        <Spin tip="正在读取本地数据" />
      </div>
    );
  if (query.error)
    return (
      <Alert
        type="error"
        showIcon
        message="数据读取失败"
        description={errorText(query.error)}
        action={<Button onClick={() => query.refetch()}>重试</Button>}
      />
    );
  if (empty)
    return (
      <div className="state-panel">
        <Empty description={emptyText}>{action}</Empty>
      </div>
    );
  return children;
}
export function Header({
  title,
  sub,
  action,
}: {
  title: string;
  sub: string;
  action?: ReactNode;
}) {
  return (
    <div className="page-header">
      <div>
        <h1>{title}</h1>
        <p>{sub}</p>
      </div>
      {action}
    </div>
  );
}
export function Status({ state, task }: { state: string; task?: Task }) {
  const color =
    state === "succeeded"
      ? "success"
      : ["failed", "download_failed"].includes(state)
        ? "error"
        : state === "unknown"
          ? "warning"
          : ["submitting", "downloading"].includes(state)
            ? "processing"
            : "default";
  const phase =
    state === "submitting"
      ? (
          {
            preparing: "准备请求",
            connecting: "连接中转站",
            connected: "提交请求",
            waiting: "等待中转站",
            receiving: "接收结果",
          } as Record<string, string>
        )[task?.diagnostic?.stage || ""]
      : undefined;
  const waitingForSample = state === "queued" && task?.samplePending;
  const rewording = state === "queued" && task?.rewording;
  return (
    <Tag color={color}>
      {rewording
        ? "改写提示词"
        : waitingForSample
          ? "等待样板图"
          : phase || stateNames[state] || state}
    </Tag>
  );
}
export function TaskTiming({ task }: { task: Task }) {
  const active = ["submitting", "running", "downloading"].includes(task.status);
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (!active) return;
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [active, task.id]);
  const d = task.diagnostic;
  const start = Date.parse(d?.startedAt || task.createdAt);
  const end = active ? now : Date.parse(task.updatedAt || task.createdAt);
  const seconds = Math.max(0, Math.floor((end - start) / 1000));
  const route = <TaskRoute task={task} />;
  // A result that could not be saved waits for its automatic re-download.
  const retryAt = Date.parse(task.downloadRetryAt || "");
  if (task.status === "downloading" && retryAt > now)
    return (
      <div className="task-timing">
        <small>
          下载未完成，{Math.ceil((retryAt - now) / 1000)}{" "}
          秒后自动补下载（不会重新生图）
        </small>
        {route}
      </div>
    );
  if (task.status === "queued" || !Number.isFinite(seconds))
    return task.autoRetryOf || task.routedFrom || task.policyRetryOf ? (
      <div className="task-timing">{route}</div>
    ) : null;
  const span = (s: number) =>
    s >= 60 ? `${Math.floor(s / 60)} 分 ${s % 60} 秒` : `${s} 秒`;
  // Since 0.3.12 the download runs in its own pool after generation, so the
  // card shows both phases; older tasks only have the total time.
  const downloadStart = Date.parse(d?.downloadStartedAt || "");
  let label = `${active ? "已等待" : "用时"} ${span(seconds)}`;
  if (Number.isFinite(downloadStart)) {
    const generated = Math.max(
      0,
      Math.floor((d?.elapsedMS || downloadStart - start) / 1000),
    );
    const downloading = task.status === "downloading";
    const fetched = Math.max(
      0,
      Math.floor(
        (downloading
          ? now - downloadStart
          : d?.downloadMS || end - downloadStart) / 1000,
      ),
    );
    label = `生成 ${span(generated)} · ${downloading ? "下载中" : "下载"} ${span(fetched)}`;
  }
  return (
    <div className="task-timing">
      <small>{label}</small>
      {route}
      {active && seconds >= 120 && (
        <p>
          {task.status === "downloading"
            ? "正在下载已返回的图片，临时网络错误会自动重试。请保持应用运行，不会重新生图。"
            : "中转站响应较慢，请保持应用运行。再次生成会提交新的计费请求。"}
        </p>
      )}
    </div>
  );
}
// Which gateway produced the image when it was moved or re-generated automatically.
function TaskRoute({ task }: { task: Task }) {
  const name = task.connection?.name || "备用中转站";
  if (task.policyRetryOf)
    return (
      <small className="task-route">
        {task.rewording
          ? "内容审核拒绝后正在改写措辞，改写完自动重发（尚未发送、不计费）"
          : task.policyStep === 2
            ? "内容审核拒绝后已改写措辞自动重发"
            : "内容审核拒绝后已自动原样重发"}
      </small>
    );
  if (task.autoRetryOf)
    return (
      <small className="task-route">
        {task.status === "queued"
          ? `结果未知后自动重新生成：等待「${name}」或健康的备用中转站（新的计费请求）`
          : `结果未知后自动重新生成 · 「${name}」（可能重复计费）`}
      </small>
    );
  if (task.routedFrom)
    return (
      <small className="task-route" title={task.routeReason}>
        已从「{task.routedFromName}」转到「{name}」 · 未发送过，不额外计费
      </small>
    );
  return null;
}
// Why the relay refused an image before generating it. Before 0.5.5 an empty
// account was reported as an invalid key; its original message still says so.
const balanceWords =
  /insufficient[ _-]?(balance|quota|credit|funds)|quota[ _-]?(exhausted|exceeded)|\bbalance\b|\bquota\b|credits?\b|payment required|billing|余额|额度|欠费|充值/i;
export function rejectionOf(task: Task): "balance" | "auth" | "" {
  if (task.status !== "failed" || !task.error) return "";
  if (
    task.error.startsWith("中转站账户余额不足") ||
    (task.error.startsWith("API Key 无效") && balanceWords.test(task.error))
  )
    return "balance";
  return task.error.startsWith("API Key 无效") ? "auth" : "";
}
const policyRefused = "生图模型的内容审核拒绝了这张图";
// An image the app already sent again by itself keeps only a short note instead
// of a warning: an unknown result (since 0.6.27) or a content refusal (0.6.28).
export function autoHandledNote(task: Task) {
  if (task.status === "unknown" && task.autoRetriedAs)
    return "结果未知，已自动重新生成（原请求可能已计费）。";
  if (task.status === "failed" && task.policyRetriedAs)
    return "内容审核拒绝了这张图，已自动重发（未生成，一般不计费）。";
  return "";
}
export function TaskFailureAlert({ task }: { task: Task }) {
  if (!task.error) return null;
  const note = autoHandledNote(task);
  if (note)
    return (
      <p className="hint" title={task.error}>
        {note}
      </p>
    );
  let message = "这张图片尚未完成，请查看详细错误。";
  if (task.status === "failed" && task.error.startsWith(policyRefused)) {
    message =
      "生图模型的内容审核拒绝了这张图，自动重发后仍未通过。图片没有生成，一般不计费。请修改提示词，或换掉可能被误判的参考图后重新生成。";
  } else if (task.status === "download_failed") {
    message =
      (task.autoDownloads || 0) > 0
        ? "已收到图片结果，自动补下载后仍未保存。可点“补下载”再试，不会重新生图。"
        : "已收到图片结果，但尚未保存完成。可点“补下载”，不会重新生图。";
  } else if (task.status === "unknown") {
    message = task.autoRetryOf
      ? "自动重新生成的结果仍未知，不会再次自动重发。请先核对中转站记录。"
      : "结果未知，未能自动重新生成，请查看详细错误并核对中转站记录。";
  } else if (task.status === "blocked") {
    message = "已暂停，尚未提交、未计费。修正后可恢复生成。";
  } else if (rejectionOf(task) === "balance") {
    message =
      "中转站账户余额不足，中转站拒绝了请求，未生成也不计费。请到中转站后台充值后重新生成。";
  } else if (rejectionOf(task) === "auth") {
    message = "API Key 无效，中转站拒绝了请求，未生成也不计费。";
  }
  return (
    <Alert
      type="warning"
      showIcon
      message={message}
      description={
        <details className="task-error-details">
          <summary>查看详细错误</summary>
          <p>{task.error}</p>
        </details>
      }
    />
  );
}
export function AssetImage({
  id,
  thumbnail = true,
}: {
  id: string;
  thumbnail?: boolean;
}) {
  // A saved image never changes: read it once and keep it for a while, so going back
  // to a page shows its images at once.
  const q = useQuery<string>({
    queryKey: ["image", id, thumbnail],
    queryFn: () => call<string>("ReadAsset", id, thumbnail),
    staleTime: Infinity,
    gcTime: 10 * 60 * 1000,
  });
  return (
    <div className="image-frame">
      {q.isPending ? (
        <Spin />
      ) : q.error ? (
        <span>图片无法读取</span>
      ) : (
        <img src={q.data} alt="本地图片预览" decoding="async" />
      )}
    </div>
  );
}
// One asset in a picker: a small thumbnail beside its name. Hovering the thumbnail
// shows the image larger; the original is read only then.
export function AssetOption({ id, name }: { id: string; name: string }) {
  const [preview, setPreview] = useState(false);
  // Anchored to the whole row, so the preview opens beside the list, not over it.
  return (
    <Popover
      open={preview}
      trigger={[]}
      placement="right"
      align={{ offset: [32, 0] }}
      zIndex={1100}
      destroyOnHidden
      content={
        <div className="asset-option-preview">
          <AssetImage id={id} thumbnail={false} />
        </div>
      }
    >
      <div className="asset-option">
        <span
          className="asset-option-thumb"
          onMouseEnter={() => setPreview(true)}
          onMouseLeave={() => setPreview(false)}
        >
          <AssetImage id={id} />
        </span>
        <span className="asset-option-name">{name}</span>
      </div>
    </Popover>
  );
}
export type ViewerItem = { assetID: string; label: string };

// Large view of one original image with previous/next navigation (buttons and
// arrow keys). Clicking the image toggles fit-to-window and actual size.
export function ImageViewer({
  items,
  index,
  onIndex,
  onClose,
  actions,
}: {
  items: ViewerItem[];
  index: number | null;
  onIndex: (index: number) => void;
  onClose: () => void;
  actions?: (item: ViewerItem) => ReactNode;
}) {
  const open = index !== null && index >= 0 && index < items.length;
  const item = open ? items[index] : undefined;
  const [actual, setActual] = useState(false);
  const q = useQuery({
    queryKey: ["image", item?.assetID, false],
    queryFn: () => call<string>("ReadAsset", item!.assetID, false),
    enabled: !!item,
  });
  useEffect(() => setActual(false), [item?.assetID]);
  useEffect(() => {
    if (!open) return;
    const key = (e: KeyboardEvent) => {
      if (e.key === "ArrowLeft" && index! > 0) onIndex(index! - 1);
      if (e.key === "ArrowRight" && index! < items.length - 1)
        onIndex(index! + 1);
    };
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  }, [open, index, items.length, onIndex]);
  return (
    <Modal
      title={item ? `${item.label}（${index! + 1} / ${items.length}）` : ""}
      open={open}
      onCancel={onClose}
      width="min(1200px, 94vw)"
      centered
      destroyOnHidden
      footer={
        item && (
          <Space wrap className="viewer-footer">
            <Button disabled={index === 0} onClick={() => onIndex(index! - 1)}>
              上一张
            </Button>
            <Button
              disabled={index === items.length - 1}
              onClick={() => onIndex(index! + 1)}
            >
              下一张
            </Button>
            {actions?.(item)}
          </Space>
        )
      }
    >
      <div className={actual ? "image-viewer actual" : "image-viewer"}>
        {q.isPending ? (
          <Spin />
        ) : q.error ? (
          <span>图片无法读取</span>
        ) : (
          <img
            src={q.data}
            alt={item?.label || "图片"}
            title={actual ? "点击适应窗口" : "点击查看实际大小"}
            onClick={() => setActual((v) => !v)}
          />
        )}
      </div>
      <p className="hint">
        点击图片切换“适应窗口 / 实际大小”，← → 键切换上一张、下一张。
      </p>
    </Modal>
  );
}
export function time(value: string) {
  return new Intl.DateTimeFormat("zh-CN", {
    timeZone: "Asia/Shanghai",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
}
