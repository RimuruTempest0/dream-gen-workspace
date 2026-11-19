import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Alert, Button, Modal, Progress, Space, Switch, Tooltip } from "antd";
import { CloudDownloadOutlined } from "@ant-design/icons";
import { call } from "./api";
import { time, useActions, useData } from "./ui";
import { formatNumber, t } from "./i18n";

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

const mb = (n?: number) =>
  `${formatNumber((n || 0) / 1048576, { minimumFractionDigits: 1, maximumFractionDigits: 1 })} MB`;

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
        {t("updates.offer.version", { version: u.version || "", size: mb(u.size) })}
      </strong>
      {u.notes && <p className="update-notes">{u.notes}</p>}
      {u.stage === "downloading" && (
        <Progress
          percent={Math.min(
            99,
            Math.round(((u.downloaded || 0) / (u.size || 1)) * 100),
          )}
          aria-label={t("updates.offer.progress")}
        />
      )}
      {u.installing && u.stage === "ready" ? (
        <Alert
          type="info"
          showIcon
          role="status"
          message={
            u.waiting
              ? t("updates.offer.waiting")
              : t("updates.offer.restarting")
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
                {t("updates.offer.notNow")}
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
          {downloading ? t("updates.offer.downloading") : t("updates.offer.install")}
        </Button>
      )}
      <p className="hint">{t("updates.offer.hint")}</p>
    </div>
  );
}

// Rail line when a newer version is waiting; opens the offer.
export function UpdateBadge({ tooltip }: { tooltip?: boolean }) {
  const u = useUpdateStatus().data;
  const [open, setOpen] = useState(false);
  if (!u?.available || !u.version) return null;
  return (
    <>
      <Tooltip
        title={tooltip ? t("updates.badge", { version: u.version }) : undefined}
        placement="right"
      >
        <button
          type="button"
          className="update-badge"
          aria-label={t("updates.badge", { version: u.version })}
          onClick={() => setOpen(true)}
        >
          <CloudDownloadOutlined aria-hidden="true" />
          <span className="rail-label">
            {t("updates.badge", { version: u.version })}
          </span>
        </button>
      </Tooltip>
      <Modal
        open={open}
        title={t("updates.title")}
        footer={null}
        onCancel={() => setOpen(false)}
        destroyOnHidden
      >
        <p className="hint">
          {t("updates.current", { version: u.current })}
        </p>
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
        <h2 id="update-title">{t("updates.title")}</h2>
        <span className="hint">
          {t("updates.current", { version: u.current })}
        </span>
      </div>
      {!u.supported ? (
        <p className="hint">{t("updates.unsupported")}</p>
      ) : (
        <>
          <Space wrap align="center">
            <Switch
              checked={u.autoCheck}
              aria-label={t("updates.autoCheck")}
              onChange={(on) =>
                actions.run(async () => {
                  await call("SetUpdateAutoCheck", on);
                  await refresh();
                })
              }
            />
            <span>{t("updates.autoCheck.hint")}</span>
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
              {t("updates.check")}
            </Button>
          </Space>
          {u.checkedAt && (
            <p className="hint">
              {t("updates.checkedAt", { time: time(u.checkedAt) })}
            </p>
          )}
          {u.error && <Alert type="warning" showIcon message={u.error} />}
          {u.available && u.version ? (
            <UpdateOffer u={u} />
          ) : (
            u.checkedAt && !u.error && <p className="hint">{t("updates.upToDate")}</p>
          )}
        </>
      )}
    </section>
  );
}
