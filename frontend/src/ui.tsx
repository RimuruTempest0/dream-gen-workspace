import {
  useEffect,
  useState,
  useRef,
  type ReactNode,
  type ComponentProps,
} from "react";
import { flushSync } from "react-dom";
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
import {
  call,
  errorText,
  stateNames,
  type Asset,
  type Connection,
  type Task,
} from "./api";
import { formatDateTime, formatTime, t, type MessageKey } from "./i18n";

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

// The admin tool writes each hosted line's name into the license file as
// 图片线路 N / LLM 线路 N. Such a default name on a hosted line is shown in the
// display language; any other name is the provider's or customer's data and is
// shown as written. These are for display only: never put the result into
// data that is edited or saved.
const defaultLineName = /^(?:(图片线路)|LLM 线路) (\d+)$/;
function lineName(name: string): string {
  const m = defaultLineName.exec(name);
  if (!m) return name;
  return t(m[1] ? "ui.line.image" : "ui.line.llm", { n: m[2] });
}
// A connection's name for display: localized only for a hosted line.
export function hostedLineName(c: { name: string; managed?: string }): string {
  return c.managed ? lineName(c.name) : c.name;
}
// For a name stored without its connection (a task's earlier gateway, monitor
// rows): localized only when a hosted line currently carries that name.
export function useLineNames(): (name: string) => string {
  const connections = useData<Connection[]>(["connections"], "ListConnections");
  const hosted = new Set(
    (connections.data || []).filter((c) => c.managed).map((c) => c.name),
  );
  return (name) => (hosted.has(name) ? lineName(name) : name);
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
// The labels are getters: t() runs when a page reads them, never on import.
function preset(value: string, key: MessageKey) {
  return {
    value,
    get label() {
      return t(key);
    },
  };
}
export const presetSizes = [
  preset("auto", "ui.size.auto"),
  preset("1024x1024", "ui.size.square"),
  preset("1536x1024", "ui.size.landscape"),
  preset("1024x1536", "ui.size.portrait"),
  preset("1792x1024", "ui.size.wide"),
  preset("1024x1792", "ui.size.tall"),
];
export const presetQualities = [
  preset("auto", "ui.quality.auto"),
  preset("low", "ui.quality.low"),
  preset("medium", "ui.quality.medium"),
  preset("high", "ui.quality.high"),
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
export function taskTitle(task: Task) {
  if (task.refineInstruction)
    return t("ui.taskTitle.refine", { text: task.refineInstruction });
  const lines = task.prompt
    .split("\n")
    .map((s) => s.trim())
    .filter(
      (s) =>
        s &&
        !s.startsWith("样板图：") &&
        !s.startsWith("Samples: reference image"),
    );
  return (lines[0] || task.prompt).replace(/^用途[：:]\s*/, "");
}

// Generated images are saved under random file names, which say nothing.
export function assetLabel(a: Asset) {
  return a.kind === "reference"
    ? a.name
    : t("ui.asset.generated", { time: time(a.createdAt) });
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
    {
      value: "",
      label: t("ui.model.default", {
        model: connection?.model || t("ui.unset"),
      }),
    },
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
      {models.isError && <p className="hint">{t("ui.model.listFailed")}</p>}
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
        message: t("ui.action.failed"),
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
    cancelText?: string,
  ) {
    if (dialogOpen.current) return;
    dialogOpen.current = true;
    modal.confirm({
      title,
      content,
      okText: okText || t(danger ? "ui.confirm.danger" : "ui.confirm.ok"),
      cancelText: cancelText || t("ui.confirm.cancel"),
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
            message: t("ui.action.failed"),
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
          t("ui.cancelTask.title"),
          t(sent ? "ui.cancelTask.sent" : "ui.cancelTask.unsent"),
          () => call("CancelTask", task.id),
          true,
          t("ui.cancelTask.button"),
          t("ui.cancelTask.keep"),
        )
      }
    >
      {t("ui.cancelTask.button")}
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
          aria-label={t(show ? "ui.secret.hide" : "ui.secret.show")}
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
      notification.success({ message: t("ui.download.saved") });
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
      notification.success({ message: t("ui.download.zipSaved", { count }) });
    });
  }
  return { downloadAsset, downloadBatch, busy: actions.busy };
}
export function DataState({
  query,
  children,
  empty = false,
  emptyText,
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
        <Spin tip={t("ui.data.loading")} />
      </div>
    );
  if (query.error)
    return (
      <Alert
        type="error"
        showIcon
        message={t("ui.data.failed")}
        description={errorText(query.error)}
        action={
          <Button onClick={() => query.refetch()}>{t("ui.data.retry")}</Button>
        }
      />
    );
  if (empty)
    return (
      <div className="state-panel">
        <Empty description={emptyText ?? t("ui.data.empty")}>{action}</Empty>
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
  const phaseKey =
    state === "submitting"
      ? (
          {
            preparing: "ui.phase.preparing",
            connecting: "ui.phase.connecting",
            connected: "ui.phase.connected",
            waiting: "ui.phase.waiting",
            receiving: "ui.phase.receiving",
          } as Record<string, MessageKey>
        )[task?.diagnostic?.stage || ""]
      : undefined;
  const phase = phaseKey && t(phaseKey);
  const waitingForSample = state === "queued" && task?.samplePending;
  const rewording = state === "queued" && task?.rewording;
  // An automatic re-generation is held (unsent) until its image service answers a free check.
  const waitingForService = state === "queued" && !!task?.autoRetryOf;
  return (
    <Tag color={color}>
      {rewording
        ? task?.rewordFor === "mood"
          ? t("ui.status.rewordMood")
          : t("ui.status.reword")
        : waitingForSample
          ? t("ui.status.waitSample")
          : waitingForService
            ? t("ui.status.waitService")
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
          {t("ui.timing.retryDownload", {
            seconds: Math.ceil((retryAt - now) / 1000),
          })}
        </small>
        {route}
      </div>
    );
  if (task.status === "queued" || !Number.isFinite(seconds))
    return task.autoRetryOf || task.routedFrom || task.policyRetryOf ? (
      <div className="task-timing">{route}</div>
    ) : null;
  const span = durationText;
  // Since 0.3.12 the download runs in its own pool after generation, so the
  // card shows both phases; older tasks only have the total time.
  const downloadStart = Date.parse(d?.downloadStartedAt || "");
  let label = t(active ? "ui.timing.waited" : "ui.timing.took", {
    time: span(seconds),
  });
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
    label = t(downloading ? "ui.timing.downloading" : "ui.timing.downloaded", {
      generated: span(generated),
      fetched: span(fetched),
    });
  }
  return (
    <div className="task-timing">
      <small>{label}</small>
      {route}
      {active && seconds >= 120 && (
        <p>
          {t(
            task.status === "downloading"
              ? "ui.timing.slowDownload"
              : "ui.timing.slowService",
          )}
        </p>
      )}
    </div>
  );
}
// Which gateway produced the image when it was moved or re-generated automatically.
function TaskRoute({ task }: { task: Task }) {
  const lineNames = useLineNames();
  const name = task.connection?.name
    ? hostedLineName(task.connection)
    : t("ui.route.backup");
  if (task.policyRetryOf)
    return (
      <small className="task-route">
        {t(
          task.rewording
            ? "ui.route.policyRewording"
            : task.policyStep === 2
              ? "ui.route.policyReworded"
              : "ui.route.policyResent",
        )}
      </small>
    );
  if (task.autoRetryOf)
    return (
      <small className="task-route">
        {t(
          task.status === "queued"
            ? "ui.route.autoRetryWaiting"
            : "ui.route.autoRetried",
          { name },
        )}
      </small>
    );
  if (task.routedFrom)
    return (
      <small className="task-route" title={task.routeReason}>
        {t("ui.route.moved", {
          from: lineNames(task.routedFromName || ""),
          to: name,
        })}
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
  // The backend names the refusal (0.8.0); the text checks below are for tasks
  // a version before 0.8.0 saved, in Chinese.
  if (task.refusal) return task.refusal === "policy" ? "" : task.refusal;
  if (
    task.error.startsWith("图片服务账户余额不足") ||
    // Tasks saved before 0.7.24 use the old wording.
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
    return t("ui.note.unknownRetried");
  if (task.status === "failed" && task.policyRetriedAs)
    return t("ui.note.policyResent");
  return "";
}
// A failure sentence from the backend carries its technical cause in full-width
// brackets (an address, a Go network error). On a result card the sentence is
// what the user reads; the cause folds away under it.
const technicalCause = /（[^（）]*[\x21-\x7e]{12,}[^（）]*）/;
export function TaskErrorText({ error }: { error: string }) {
  const plain = error.replace(technicalCause, "");
  if (plain === error || !plain.trim())
    return <p className="error-text">{error}</p>;
  return (
    <div className="error-text">
      <p>{plain}</p>
      <details className="task-error-details">
        <summary>{t("ui.error.details")}</summary>
        <p>{error}</p>
      </details>
    </div>
  );
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
  let key: MessageKey = "ui.failure.default";
  if (
    task.status === "failed" &&
    (task.refusal === "policy" || task.error.startsWith(policyRefused))
  ) {
    key = "ui.failure.policy";
  } else if (task.status === "download_failed") {
    key =
      (task.autoDownloads || 0) > 0
        ? "ui.failure.downloadRetried"
        : "ui.failure.download";
  } else if (task.status === "unknown") {
    key = task.autoRetryOf ? "ui.failure.unknownAuto" : "ui.failure.unknown";
  } else if (task.status === "blocked") {
    key = "ui.failure.blocked";
  } else if (rejectionOf(task) === "balance") {
    key = "ui.failure.balance";
  } else if (rejectionOf(task) === "auth") {
    key = "ui.failure.auth";
  }
  const message = t(key);
  return (
    <Alert
      type="warning"
      showIcon
      message={message}
      description={
        <details className="task-error-details">
          <summary>{t("ui.error.details")}</summary>
          <p>{task.error}</p>
        </details>
      }
    />
  );
}
// The asset ids that appeared while this page was open: the images that have
// just arrived, as opposed to the ones that were already there. `scope` is
// whatever the list belongs to (a run, a page); a new scope starts over.
export function useArrivals(scope: string, ids: string[] | undefined) {
  const state = useRef({
    scope,
    seen: null as Set<string> | null,
    fresh: new Set<string>(),
  });
  if (state.current.scope !== scope)
    state.current = { scope, seen: null, fresh: new Set() };
  const s = state.current;
  if (ids) {
    if (!s.seen) s.seen = new Set(ids);
    else
      for (const id of ids)
        if (!s.seen.has(id)) {
          s.seen.add(id);
          s.fresh.add(id);
        }
  }
  return s.fresh;
}
// True while the app is doing something for this image right now: sending,
// waiting for the image service, saving the result, or rewriting the prompt.
// Waiting in line, for a retry or for the service to come back is not.
export function taskWorking(task: Task) {
  if (task.status === "queued") return !!task.rewording;
  if (task.status === "downloading")
    return !(Date.parse(task.downloadRetryAt || "") > Date.now());
  return ["submitting", "running"].includes(task.status);
}
export function AssetImage({
  id,
  thumbnail = true,
  arrived = false,
}: {
  id: string;
  thumbnail?: boolean;
  // This image has just been generated: it is revealed once, like a light
  // coming on. Images that were already there appear without animation.
  arrived?: boolean;
}) {
  // A saved image never changes: read it once and keep it for a while, so going back
  // to a page shows its images at once.
  const q = useQuery<string>({
    queryKey: ["image", id, thumbnail],
    queryFn: () => call<string>("ReadAsset", id, thumbnail),
    staleTime: Infinity,
    gcTime: 10 * 60 * 1000,
  });
  // Only an image the user actually waited for fades in; one that is already
  // in memory is simply there.
  const waited = useRef(q.isPending);
  return (
    <div className="image-frame">
      {q.isPending ? (
        <Spin />
      ) : q.error ? (
        <span>{t("ui.image.unreadable")}</span>
      ) : (
        <img
          src={q.data}
          alt={t("ui.image.alt")}
          decoding="async"
          className={
            arrived
              ? "image-arrived"
              : waited.current
                ? "image-waited"
                : undefined
          }
        />
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
// Runs a state change that rearranges the page as a view transition, so an
// element that is in both layouts moves to its new place instead of jumping.
// With "reduce motion" on, or where the runtime has no view transitions, the
// change simply happens.
export function withViewTransition(update: () => void) {
  const doc = document as Document & {
    startViewTransition?: (callback: () => void) => unknown;
  };
  if (
    !doc.startViewTransition ||
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  ) {
    update();
    return;
  }
  doc.startViewTransition(() => flushSync(update));
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
      title={
        item
          ? t("ui.viewer.title", {
              label: item.label,
              n: index! + 1,
              total: items.length,
            })
          : ""
      }
      open={open}
      onCancel={onClose}
      width="min(1200px, 94vw)"
      centered
      destroyOnHidden
      footer={
        item && (
          <div className="viewer-footer">
            {/* Nothing to move between when there is one image. */}
            {items.length > 1 && (
              <Space>
                <Button
                  disabled={index === 0}
                  onClick={() => onIndex(index! - 1)}
                >
                  {t("ui.viewer.prev")}
                </Button>
                <Button
                  disabled={index === items.length - 1}
                  onClick={() => onIndex(index! + 1)}
                >
                  {t("ui.viewer.next")}
                </Button>
              </Space>
            )}
            <Space wrap className="viewer-actions">
              {actions?.(item)}
            </Space>
          </div>
        )
      }
      className="image-viewer-modal"
    >
      <div className={actual ? "image-viewer actual" : "image-viewer"}>
        {q.isPending ? (
          <Spin />
        ) : q.error ? (
          <span>{t("ui.image.unreadable")}</span>
        ) : (
          <img
            key={item?.assetID}
            src={q.data}
            alt={item?.label || t("ui.viewer.image")}
            title={t(actual ? "ui.viewer.fit" : "ui.viewer.actual")}
            onClick={() => setActual((v) => !v)}
          />
        )}
      </div>
      <p className="hint">
        {t(items.length > 1 ? "ui.viewer.hintMany" : "ui.viewer.hint")}
      </p>
    </Modal>
  );
}
/** A span of seconds as "42 秒" or "3 分 5 秒" in the interface language. */
export function durationText(seconds: number) {
  return seconds >= 60
    ? t("ui.duration.minutes", { m: Math.floor(seconds / 60), s: seconds % 60 })
    : t("ui.duration.seconds", { s: seconds });
}

/** Date and time with the year, for dates that matter beyond this year (expiry). */
export function dateTime(value: string) {
  return formatDateTime(value);
}

/** Month, day and time in the interface language. */
export function time(value: string) {
  return formatTime(value);
}
