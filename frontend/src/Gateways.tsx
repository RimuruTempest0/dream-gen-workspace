import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button, Radio, Space, Switch, Tag, Tooltip } from "antd";
import {
  ArrowDownOutlined,
  ArrowUpOutlined,
  SafetyCertificateOutlined,
} from "@ant-design/icons";
import { call, type Connection } from "./api";
import { useActions, useData, time } from "./ui";
import { t, type MessageKey } from "./i18n";

export type GatewaySettings = {
  primaryID: string;
  backupIDs: string[];
  autoReroute: boolean;
};
export type GatewayHealth = {
  connectionID: string;
  name: string;
  model: string;
  state: "healthy" | "slow" | "down" | "unknown";
  reason: string;
  checkedAt?: string;
  probeMS: number;
  modelsOK: boolean;
  modelCount: number;
  medianMS: number;
  recentTimeouts: number;
  inFlight: number;
};

const stateKeys: Record<GatewayHealth["state"], MessageKey> = {
  healthy: "gateways.state.healthy",
  slow: "gateways.state.slow",
  down: "gateways.state.down",
  unknown: "gateways.state.unknown",
};
/** Health state name in the interface language. */
export function gatewayStateText(state: GatewayHealth["state"]) {
  return t(stateKeys[state]);
}
const stateColor = {
  healthy: "success",
  slow: "warning",
  down: "error",
  unknown: "default",
} as const;

export function useGateways() {
  const settings = useData<GatewaySettings>(
    ["gateway-settings"],
    "GetGatewaySettings",
  );
  const health = useData<GatewayHealth[]>(
    ["gateway-health"],
    "GatewayHealth",
    [],
    5000,
  );
  const byID = new Map((health.data || []).map((h) => [h.connectionID, h]));
  return { settings, health, byID };
}

export function GatewayTag({ health }: { health?: GatewayHealth }) {
  const state = health?.state || "unknown";
  const tag = (
    <Tag color={stateColor[state]} className="gateway-tag">
      {gatewayStateText(state)}
    </Tag>
  );
  return health?.reason ? <Tooltip title={health.reason}>{tag}</Tooltip> : tag;
}

// Option text for a connection select: name, role and health.
export function gatewayLabel(
  c: Connection,
  g?: GatewaySettings,
  h?: GatewayHealth,
) {
  const role =
    g?.primaryID === c.id
      ? ` · ${t("gateways.primary")}`
      : g?.backupIDs.includes(c.id)
        ? ` · ${t("gateways.backup")}`
        : "";
  const state =
    h && h.state !== "unknown" ? ` · ${gatewayStateText(h.state)}` : "";
  return c.name + role + state + (c.hasKey ? "" : ` · ${t("gateways.noKey")}`);
}

function seconds(ms: number) {
  return t("gateways.seconds", { n: Math.round(ms / 1000) });
}

// Per-connection controls shown inside the image relay list.
export function GatewayRow({
  connection,
  images,
}: {
  connection: Connection;
  images: Connection[];
}) {
  const { settings, byID } = useGateways();
  const qc = useQueryClient();
  const actions = useActions();
  const g = settings.data;
  const h = byID.get(connection.id);
  if (!g) return null;
  const isPrimary = g.primaryID === connection.id;
  const backupIndex = g.backupIDs.indexOf(connection.id);
  // Show the new role at once; a failed save is undone by the refetch.
  const save = (next: Partial<GatewaySettings>) => {
    qc.setQueryData(["gateway-settings"], { ...g, ...next });
    return actions.run(() => call("SetGatewaySettings", { ...g, ...next }));
  };
  function move(step: number) {
    const ids = [...g!.backupIDs];
    const to = backupIndex + step;
    if (to < 0 || to >= ids.length) return;
    [ids[backupIndex], ids[to]] = [ids[to], ids[backupIndex]];
    save({ backupIDs: ids });
  }
  return (
    <div className="gateway-row">
      <Space wrap size={[12, 6]}>
        <span>
          {t("gateways.health")} <GatewayTag health={h} />
        </span>
        <Radio
          checked={isPrimary}
          disabled={actions.busy}
          onChange={() =>
            save({
              primaryID: connection.id,
              backupIDs: g.backupIDs.filter((id) => id !== connection.id),
            })
          }
        >
          {t("gateways.primary")}
        </Radio>
        {!isPrimary && (
          <Space size={4}>
            <Switch
              size="small"
              checked={backupIndex >= 0}
              disabled={actions.busy}
              aria-label={t("gateways.setBackup", { name: connection.name })}
              onChange={(on) =>
                save({
                  backupIDs: on
                    ? [...g.backupIDs, connection.id]
                    : g.backupIDs.filter((id) => id !== connection.id),
                })
              }
            />
            <span>
              {backupIndex >= 0
                ? t("gateways.backupN", { n: backupIndex + 1 })
                : t("gateways.backup")}
            </span>
            {backupIndex >= 0 && g.backupIDs.length > 1 && (
              <>
                <Button
                  size="small"
                  type="text"
                  icon={<ArrowUpOutlined aria-hidden="true" />}
                  aria-label={t("gateways.moveUp", { name: connection.name })}
                  disabled={backupIndex === 0 || actions.busy}
                  onClick={() => move(-1)}
                />
                <Button
                  size="small"
                  type="text"
                  icon={<ArrowDownOutlined aria-hidden="true" />}
                  aria-label={t("gateways.moveDown", { name: connection.name })}
                  disabled={
                    backupIndex === g.backupIDs.length - 1 || actions.busy
                  }
                  onClick={() => move(1)}
                />
              </>
            )}
          </Space>
        )}
      </Space>
      <small className="gateway-detail">
        {h?.checkedAt
          ? t("gateways.detail.checked", {
              time: time(h.checkedAt),
              ms: String(h.probeMS),
              models: h.modelsOK
                ? t("gateways.detail.models", { count: h.modelCount })
                : t("gateways.detail.noModels"),
            })
          : t("gateways.detail.unchecked")}
        {h?.medianMS
          ? ` · ${t("gateways.detail.median", { time: seconds(h.medianMS) })}`
          : ""}
        {h?.recentTimeouts
          ? ` · ${t("gateways.detail.timeouts", { count: h.recentTimeouts })}`
          : ""}
        {images.length > 1 &&
          !isPrimary &&
          backupIndex < 0 &&
          ` · ${t("gateways.detail.notBackup")}`}
      </small>
    </div>
  );
}

export function GatewayFailover() {
  const { settings, health } = useGateways();
  const actions = useActions();
  const [checking, setChecking] = useState(false);
  const g = settings.data;
  if (!g) return null;
  const configured = !!g.primaryID || g.backupIDs.length > 0;
  const save = (next: Partial<GatewaySettings>, success?: string) =>
    actions.run(() => call("SetGatewaySettings", { ...g, ...next }), success);
  return (
    <section className="panel" aria-labelledby="gateway-failover-title">
      <div className="panel-heading">
        <h2 id="gateway-failover-title">
          <SafetyCertificateOutlined aria-hidden="true" />{" "}
          {t("gateways.failover.title")}
        </h2>
        <Button
          loading={checking}
          disabled={!health.data?.length}
          onClick={async () => {
            setChecking(true);
            try {
              await actions.run(
                () => call("CheckGateways"),
                t("gateways.failover.checked"),
              );
            } finally {
              setChecking(false);
            }
          }}
        >
          {t("gateways.failover.check")}
        </Button>
      </div>
      {!configured && <p className="hint">{t("gateways.failover.unset")}</p>}
      <p className="hint">{t("gateways.failover.probe")}</p>
      <div className="gateway-switch">
        <Switch
          checked={g.autoReroute}
          disabled={actions.busy}
          aria-labelledby="auto-reroute-label"
          onChange={(autoReroute) => save({ autoReroute })}
        />
        <div>
          <strong id="auto-reroute-label">
            {t("gateways.failover.reroute")}
          </strong>
          <p className="hint">{t("gateways.failover.reroute.hint")}</p>
        </div>
      </div>
      <p className="hint">{t("gateways.failover.unknown")}</p>
    </section>
  );
}
