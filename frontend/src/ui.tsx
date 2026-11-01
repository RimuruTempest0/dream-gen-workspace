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
      await qc.invalidateQueries();
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
          await qc.invalidateQueries();
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
  return (
    <Tag color={color}>
      {waitingForSample ? "等待样板图" : phase || stateNames[state] || state}
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
  if (task.status === "queued" || !Number.isFinite(seconds))
    return task.autoRetryOf || task.routedFrom ? (
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
  if (task.autoRetryOf)
    return (
      <small className="task-route">
        {task.status === "queued"
          ? `524 后自动重新生成：等待「${name}」或健康的备用中转站（新的计费请求）`
          : `524 后自动重新生成 · 「${name}」（可能重复计费）`}
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
export function TaskFailureAlert({ task }: { task: Task }) {
  if (!task.error) return null;
  let message = "这张图片尚未完成，请查看详细错误。";
  if (task.status === "download_failed") {
    message = /deadline exceeded|timeout|超时|10 分钟/i.test(task.error)
      ? "已收到图片结果，但下载超时。可点“补下载”，不会重新生图。"
      : "已收到图片结果，但尚未保存完成。可点“补下载”，不会重新生图。";
  } else if (task.status === "unknown") {
    const status = task.diagnostic?.httpStatus;
    message = [504, 522, 524].includes(status || 0)
      ? task.autoRetriedAs
        ? `中转站等待超时（HTTP ${status}），结果未知。已按设置自动重新生成一次，可能产生两次费用。`
        : `中转站等待超时（HTTP ${status}），结果未知。请先核对中转站记录，系统不会自动重发生图。`
      : "结果未知，请先核对供应商记录，系统不会自动重发。";
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
  const q = useData<string>(["image", id, thumbnail], "ReadAsset", [
    id,
    thumbnail,
  ]);
  return (
    <div className="image-frame">
      {q.isPending ? (
        <Spin />
      ) : q.error ? (
        <span>图片无法读取</span>
      ) : (
        <img src={q.data} alt="本地图片预览" />
      )}
    </div>
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
