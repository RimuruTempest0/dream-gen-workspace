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

export type GatewaySettings = {
  primaryID: string;
  backupIDs: string[];
  autoReroute: boolean;
  autoRegenerate524: boolean;
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

const stateText = {
  healthy: "正常",
  slow: "变慢",
  down: "不可用",
  unknown: "未检查",
} as const;
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
      {stateText[state]}
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
      ? " · 主用"
      : g?.backupIDs.includes(c.id)
        ? " · 备用"
        : "";
  const state = h && h.state !== "unknown" ? ` · ${stateText[h.state]}` : "";
  return c.name + role + state + (c.hasKey ? "" : " · 未配置密钥");
}

function seconds(ms: number) {
  return `${Math.round(ms / 1000)} 秒`;
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
          健康 <GatewayTag health={h} />
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
          主用
        </Radio>
        {!isPrimary && (
          <Space size={4}>
            <Switch
              size="small"
              checked={backupIndex >= 0}
              disabled={actions.busy}
              aria-label={`将 ${connection.name} 设为备用中转站`}
              onChange={(on) =>
                save({
                  backupIDs: on
                    ? [...g.backupIDs, connection.id]
                    : g.backupIDs.filter((id) => id !== connection.id),
                })
              }
            />
            <span>{backupIndex >= 0 ? `备用 ${backupIndex + 1}` : "备用"}</span>
            {backupIndex >= 0 && g.backupIDs.length > 1 && (
              <>
                <Button
                  size="small"
                  type="text"
                  icon={<ArrowUpOutlined aria-hidden="true" />}
                  aria-label={`${connection.name} 备用顺序上移`}
                  disabled={backupIndex === 0 || actions.busy}
                  onClick={() => move(-1)}
                />
                <Button
                  size="small"
                  type="text"
                  icon={<ArrowDownOutlined aria-hidden="true" />}
                  aria-label={`${connection.name} 备用顺序下移`}
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
          ? `免费检查 ${time(h.checkedAt)} · 响应 ${h.probeMS} ms · ${
              h.modelsOK ? `${h.modelCount} 个模型` : "未读取到模型列表"
            }`
          : "尚未检查"}
        {h?.medianMS ? ` · 最近生成中位 ${seconds(h.medianMS)}` : ""}
        {h?.recentTimeouts ? ` · 15 分钟内超时 ${h.recentTimeouts} 次` : ""}
        {images.length > 1 && !isPrimary && backupIndex < 0 && " · 未加入备用"}
      </small>
    </div>
  );
}

const costNotice =
  "开启后，遇到中转站超时（HTTP 524 等）的图片会自动在另一家健康、模型完全相同的中转站重新生成；没有可用备用时，等原中转站恢复正常后再重新生成。每张最多 1 次、每批最多 3 张。原请求可能已在中转站生成并计费，因此这些图片可能产生两次费用。";

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
          <SafetyCertificateOutlined aria-hidden="true" /> 备用切换
        </h2>
        <Button
          loading={checking}
          disabled={!health.data?.length}
          onClick={async () => {
            setChecking(true);
            try {
              await actions.run(
                () => call("CheckGateways"),
                "免费检查完成（只读取模型列表，不产生费用）",
              );
            } finally {
              setChecking(false);
            }
          }}
        >
          立即检查（免费）
        </Button>
      </div>
      {!configured && (
        <p className="hint">
          在上方生图中转站中选择一个“主用”，并把其他中转站设为“备用”后，以下功能生效。未设置时生图行为与之前相同。
        </p>
      )}
      <p className="hint">
        生图期间每 3
        分钟免费检查一次主用和备用中转站（只读取模型列表，不生成图片、不计费），并根据最近的生成耗时和超时判断是否变慢。只有模型列表中包含完全相同模型
        ID 的中转站才会被用作备用。
      </p>
      <div className="gateway-switch">
        <Switch
          checked={g.autoReroute}
          disabled={actions.busy}
          aria-labelledby="auto-reroute-label"
          onChange={(autoReroute) => save({ autoReroute })}
        />
        <div>
          <strong id="auto-reroute-label">
            自动把未发送的图片转到备用中转站
          </strong>
          <p className="hint">
            主用中转站变慢或不可用时，还没发送的图片改用健康的备用中转站。这些图片从未发送过，不会额外计费。
          </p>
        </div>
      </div>
      <div className="gateway-switch">
        <Switch
          checked={g.autoRegenerate524}
          disabled={actions.busy}
          aria-labelledby="auto-regenerate-label"
          onChange={(on) => {
            if (!on) {
              save({ autoRegenerate524: false }, "已关闭 524 自动重新生成");
              return;
            }
            actions.confirm(
              "开启 524 自动重新生成？",
              <div>
                <p>{costNotice}</p>
                <p className="hint">可随时在这里关闭。</p>
              </div>,
              () =>
                call("SetGatewaySettings", { ...g, autoRegenerate524: true }),
              true,
              "确认开启（可能重复计费）",
            );
          }}
        />
        <div>
          <strong id="auto-regenerate-label">524 后自动换中转站重新生成</strong>{" "}
          <Tag color="warning">可能产生额外费用</Tag>
          <p className="hint">
            默认关闭。每张最多自动重新生成 1 次、每批最多 3
            张；原请求可能已计费，开启前会再次提示。
          </p>
        </div>
      </div>
    </section>
  );
}
