import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Alert, Button, Modal, Progress, Space, Switch } from "antd";
import { CloudDownloadOutlined } from "@ant-design/icons";
import { call } from "./api";
import { time, useActions, useData } from "./ui";

export type UpdateStatus = {
  supported: boolean;
  autoCheck: boolean;
  current: string;
  checkedAt?: string;
  checking?: boolean;
  available: boolean;
  version?: string;
  notes?: string;
  size?: number;
  stage?: "" | "downloading" | "ready";
  downloaded?: number;
  /** The one-click update is under way (downloading, or waiting to restart). */
  installing?: boolean;
  /** Why the restart is put off; it follows by itself once this is over. */
  waiting?: string;
  error?: string;
};

export function useUpdateStatus() {
  const q = useData<UpdateStatus>(["update-status"], "UpdateStatus", [], 2000);
  return q;
}

const mb = (n?: number) => `${((n || 0) / 1048576).toFixed(1)} MB`;

// What a waiting version offers: its notes and the one button. One click
// downloads, checks and restarts; while images are being generated the restart
// waits for them.
function UpdateOffer({ u }: { u: UpdateStatus }) {
  const actions = useActions();
  const qc = useQueryClient();
  const refresh = () => qc.invalidateQueries({ queryKey: ["update-status"] });
  // Between the click and the first progress report the stage is still empty.
  const downloading =
    u.stage === "downloading" || (!!u.installing && u.stage !== "ready");
  return (
    <div className="update-available">
      <strong>
        新版本 {u.version}（{mb(u.size)}）
      </strong>
      {u.notes && <p className="update-notes">{u.notes}</p>}
      {u.stage === "downloading" && (
        <Progress
          percent={Math.min(
            99,
            Math.round(((u.downloaded || 0) / (u.size || 1)) * 100),
          )}
          aria-label="更新下载进度"
        />
      )}
      {u.installing && u.stage === "ready" ? (
        <Alert
          type="info"
          showIcon
          role="status"
          message={
            u.waiting
              ? "已下载并校验，等当前图片完成后自动重启"
              : "已下载并校验，正在重启…"
          }
          description={u.waiting || undefined}
          action={
            u.waiting ? (
              <Button
                size="small"
                onClick={() =>
                  actions.run(async () => {
                    await call("CancelUpdateWait");
                    await refresh();
                  })
                }
              >
                暂不重启
              </Button>
            ) : undefined
          }
        />
      ) : (
        <Button
          type="primary"
          icon={<CloudDownloadOutlined aria-hidden="true" />}
          loading={downloading}
          disabled={actions.busy}
          onClick={() =>
            actions.run(async () => {
              await call("InstallUpdate");
              await refresh();
            })
          }
        >
          {downloading ? "正在下载" : "立即更新"}
        </Button>
      )}
      <p className="hint">
        点一次即可：下载、校验文件完整性，然后自动重启。图片、任务和设置都保留，更新失败会自动恢复原来的程序。
      </p>
    </div>
  );
}

// Rail line when a newer version is waiting; opens the offer.
export function UpdateBadge() {
  const u = useUpdateStatus().data;
  const [open, setOpen] = useState(false);
  if (!u?.available || !u.version) return null;
  return (
    <>
      <button
        type="button"
        className="update-badge"
        onClick={() => setOpen(true)}
      >
        <CloudDownloadOutlined aria-hidden="true" /> 有新版本 {u.version}
      </button>
      <Modal
        open={open}
        title="软件更新"
        footer={null}
        onCancel={() => setOpen(false)}
        destroyOnHidden
      >
        <p className="hint">当前版本 {u.current}</p>
        {u.error && <Alert type="warning" showIcon message={u.error} />}
        <UpdateOffer u={u} />
      </Modal>
    </>
  );
}

export function UpdatePanel() {
  const q = useUpdateStatus();
  const actions = useActions();
  const qc = useQueryClient();
  const u = q.data;
  if (!u) return null;
  const refresh = () => qc.invalidateQueries({ queryKey: ["update-status"] });
  return (
    <section
      className="panel update-panel"
      id="update"
      aria-labelledby="update-title"
    >
      <div className="panel-heading">
        <h2 id="update-title">软件更新</h2>
        <span className="hint">当前版本 {u.current}</span>
      </div>
      {!u.supported ? (
        <p className="hint">
          此版本没有设置在线更新。升级时请向软件提供方获取完整包，解压覆盖程序目录并保留
          data 和 data-location.txt。
        </p>
      ) : (
        <>
          <Space wrap align="center">
            <Switch
              checked={u.autoCheck}
              aria-label="自动检查更新"
              onChange={(on) =>
                actions.run(async () => {
                  await call("SetUpdateAutoCheck", on);
                  await refresh();
                })
              }
            />
            <span>自动检查更新（启动后和每 24 小时，只读取更新信息）</span>
            <Button
              loading={u.checking}
              disabled={actions.busy || u.installing}
              onClick={() =>
                actions.run(async () => {
                  await call("CheckForUpdate");
                  await refresh();
                })
              }
            >
              检查更新
            </Button>
          </Space>
          {u.checkedAt && <p className="hint">上次检查：{time(u.checkedAt)}</p>}
          {u.error && <Alert type="warning" showIcon message={u.error} />}
          {u.available && u.version ? (
            <UpdateOffer u={u} />
          ) : (
            u.checkedAt && !u.error && <p className="hint">已是最新版本。</p>
          )}
        </>
      )}
    </section>
  );
}
