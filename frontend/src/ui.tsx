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
  ) {
    if (dialogOpen.current) return;
    dialogOpen.current = true;
    modal.confirm({
      title,
      content,
      okText: okText || (danger ? "确认执行" : "继续"),
      cancelText: "取消",
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
  return <Tag color={color}>{phase || stateNames[state] || state}</Tag>;
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
  const start = Date.parse(task.diagnostic?.startedAt || task.createdAt);
  const end = active ? now : Date.parse(task.updatedAt || task.createdAt);
  const seconds = Math.max(0, Math.floor((end - start) / 1000));
  if (task.status === "queued" || !Number.isFinite(seconds)) return null;
  return (
    <div className="task-timing">
      <small>
        {active ? "已等待" : "用时"} {Math.floor(seconds / 60)} 分{" "}
        {seconds % 60} 秒
      </small>
      {active && seconds >= 120 && (
        <p>中转站响应较慢，请保持应用运行。再次生成会提交新的计费请求。</p>
      )}
    </div>
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
