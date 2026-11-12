import { Link } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { Alert, App as AntApp, Button, Progress, Space, Switch } from "antd";
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
  error?: string;
};

export function useUpdateStatus() {
  const q = useData<UpdateStatus>(["update-status"], "UpdateStatus", [], 2000);
  return q;
}

const mb = (n?: number) => `${((n || 0) / 1048576).toFixed(1)} MB`;

// Sidebar line when a newer version is waiting.
export function UpdateBadge() {
  const u = useUpdateStatus().data;
  if (!u?.available || !u.version) return null;
  return (
    <Link to="/settings#update" className="update-badge">
      <CloudDownloadOutlined aria-hidden="true" /> 有新版本 {u.version}
    </Link>
  );
}

export function UpdatePanel() {
  const q = useUpdateStatus();
  const actions = useActions();
  const qc = useQueryClient();
  const { modal } = AntApp.useApp();
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
          data、data-location.txt 和 customer.dat。
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
              disabled={actions.busy || u.stage === "downloading"}
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
              {u.stage === "ready" ? (
                <Button
                  type="primary"
                  onClick={() =>
                    modal.confirm({
                      title: `重启并更新到 ${u.version}？`,
                      content:
                        "软件会关闭，替换程序文件后自动重新打开。图片、任务、设置和授权都保留；更新失败会自动恢复原来的程序。正在生成的图片需要先完成。",
                      okText: "重启并更新",
                      cancelText: "稍后",
                      onOk: () => actions.run(() => call("InstallUpdate")),
                    })
                  }
                >
                  重启并更新
                </Button>
              ) : (
                <Button
                  type="primary"
                  icon={<CloudDownloadOutlined aria-hidden="true" />}
                  loading={u.stage === "downloading"}
                  disabled={actions.busy}
                  onClick={() =>
                    actions.run(async () => {
                      await call("DownloadUpdate");
                      await refresh();
                    })
                  }
                >
                  下载更新
                </Button>
              )}
              <p className="hint">下载后会校验文件完整性，校验通过才能安装。</p>
            </div>
          ) : (
            u.checkedAt && !u.error && <p className="hint">已是最新版本。</p>
          )}
        </>
      )}
    </section>
  );
}
