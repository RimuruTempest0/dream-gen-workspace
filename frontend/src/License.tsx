import { useState } from "react";
import { Link } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import {
  App as AntApp,
  Alert,
  Button,
  Input,
  Progress,
  Tag,
  Tooltip,
} from "antd";
import { SafetyCertificateOutlined } from "@ant-design/icons";
import { call } from "./api";
import { dateTime, notImages, useActions, useData, time } from "./ui";

export type LicenseStatus = {
  mode: "developer" | "trial" | "active" | "expired" | "invalid" | "mismatch";
  softwareID?: string;
  customer?: string;
  trialLimit: number;
  trialUsed: number;
  trialPending: number;
  trialRemaining: number;
  upstreamRemaining?: string;
  activatedAt?: string;
  message?: string;
  /** When the licence ends (none: never); daysLeft only in its last 7 days. */
  expiresAt?: string;
  daysLeft?: number;
  activated?: boolean;
  /** The mass-market program running as the hosted edition (托管版). */
  hosted?: boolean;
  /** The mass-market program in the open edition: a licence file makes it hosted. */
  canImport?: boolean;
};

export function useLicense() {
  return useData<LicenseStatus>(["license"], "LicenseStatus", [], 5000);
}

// Sidebar line: trial images left, activated, or a licence problem.
export function LicenseBadge({ tooltip }: { tooltip?: boolean }) {
  const l = useLicense().data;
  if (!l || l.mode === "developer") return null;
  const text =
    l.mode === "trial"
      ? `试用剩余 ${l.trialRemaining} / ${l.trialLimit} 张`
      : l.mode === "active"
        ? l.daysLeft
          ? `${l.daysLeft} 天后到期`
          : "已激活"
        : l.mode === "expired"
          ? "授权已到期"
          : "授权异常";
  return (
    <Tooltip title={tooltip ? text : undefined} placement="right">
      <Link
        to="/settings#license"
        className={`license-badge license-${l.mode}`}
        aria-label={`授权状态：${text}`}
      >
        <SafetyCertificateOutlined aria-hidden="true" />
        <span className="rail-label">{text}</span>
      </Link>
    </Tooltip>
  );
}

// Shown above the generate forms when the trial is used up or the licence blocks.
export function LicenseNotice() {
  const l = useLicense().data;
  if (!l) return null;
  if (l.mode === "invalid" || l.mode === "mismatch" || l.mode === "expired")
    return (
      <Alert
        type="error"
        showIcon
        className="license-notice"
        message={l.mode === "expired" ? "软件授权已到期" : "暂时不能生图"}
        description={l.message}
      />
    );
  if (l.daysLeft)
    return (
      <Alert
        type="warning"
        showIcon
        className="license-notice"
        message={`软件授权 ${l.daysLeft} 天后到期`}
        description={
          <>
            到期时间 {dateTime(l.expiresAt || "")}
            。到期后不能生成新图片，已有图片不受影响。请联系软件提供方续费，收到新的授权文件后在
            <Link to="/settings#license">系统 → 连接 → 授权</Link>导入。
          </>
        }
      />
    );
  if (l.mode === "trial" && l.trialRemaining <= 0)
    return (
      <Alert
        type="warning"
        showIcon
        className="license-notice"
        message="试用额度已用完"
        description={
          <>
            {l.trialLimit} 张试用图片已用完，之前生成的图片都保留在作品与素材。
            <Link to="/settings#license">输入激活码</Link>后继续使用。
          </>
        }
      />
    );
  return null;
}

export function LicensePanel() {
  const license = useLicense();
  const actions = useActions();
  const qc = useQueryClient();
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const { notification } = AntApp.useApp();
  const l = license.data;
  async function importFile() {
    const path = await call<string>("ChooseCustomerFile");
    if (!path) return;
    await call("ImportCustomerFile", path);
    // New lines or a new expiry: refresh every list, not just the licence.
    await qc.invalidateQueries(notImages);
    notification.success({ message: "已导入新的授权文件" });
  }
  if (!l || (l.mode === "developer" && !l.canImport)) return null;
  // The open edition of the mass-market line: a licence file from the provider
  // turns this same program into the hosted edition.
  if (l.mode === "developer")
    return (
      <section
        className="panel license-panel"
        id="license"
        aria-labelledby="license-title"
      >
        <div className="panel-heading">
          <h2 id="license-title">
            <SafetyCertificateOutlined aria-hidden="true" /> 托管版授权
          </h2>
          <Tag>未使用</Tag>
        </div>
        <p className="hint">
          你现在用的是自己配置的图片服务和 AI
          模型。如果软件提供方给了你授权文件（customer.dat），在这里导入，就改用提供方的线路，不用自己填地址和
          Key；你已经配置的连接会保留，授权期间不使用。
        </p>
        <div className="license-import">
          <Button
            onClick={() => actions.run(importFile)}
            disabled={actions.busy}
          >
            导入授权文件
          </Button>
        </div>
      </section>
    );
  async function activate() {
    setError("");
    try {
      await call("Activate", code);
      setCode("");
      // Activation adds the backup lines: refresh every list, not just the licence.
      await qc.invalidateQueries(notImages);
    } catch (e) {
      setError(String((e as Error)?.message || e));
    }
  }
  return (
    <section
      className="panel license-panel"
      id="license"
      aria-labelledby="license-title"
    >
      <div className="panel-heading">
        <h2 id="license-title">
          <SafetyCertificateOutlined aria-hidden="true" /> 授权
        </h2>
        <Tag
          color={
            l.mode === "active"
              ? "success"
              : l.mode === "trial"
                ? "processing"
                : "error"
          }
        >
          {l.mode === "active"
            ? "已激活"
            : l.mode === "trial"
              ? "试用中"
              : l.mode === "expired"
                ? "已到期"
                : "授权异常"}
        </Tag>
      </div>
      <dl className="license-facts">
        {l.customer && (
          <>
            <dt>{l.hosted ? "顾客" : "客户"}</dt>
            <dd>{l.customer}</dd>
          </>
        )}
        <dt>软件标识</dt>
        <dd className="mono">{l.softwareID || "—"}</dd>
        {(l.mode === "active" || l.activated) && l.activatedAt && (
          <>
            <dt>激活时间</dt>
            <dd>{time(l.activatedAt)}</dd>
          </>
        )}
        {l.expiresAt && (
          <>
            <dt>有效期至</dt>
            <dd>{dateTime(l.expiresAt)}</dd>
          </>
        )}
      </dl>
      {(l.mode === "invalid" ||
        l.mode === "mismatch" ||
        l.mode === "expired") && (
        <Alert type="error" showIcon message={l.message} />
      )}
      {!!l.daysLeft && (
        <Alert
          type="warning"
          showIcon
          message={`${l.daysLeft} 天后到期，到期后不能生成新图片。请联系软件提供方续费。`}
        />
      )}
      <div className="license-import">
        <Button onClick={() => actions.run(importFile)} disabled={actions.busy}>
          导入授权文件
        </Button>
        <small className="hint">
          续费或更换线路后，软件提供方会发给你新的
          customer.dat。导入后立即生效，图片、任务和激活状态都保留。
        </small>
      </div>
      {l.hosted && (
        <p className="hint">
          想改用自己的图片服务和 AI 模型：关闭软件，删除程序目录里的
          customer.dat，再打开。提供方的线路会随之移除。
        </p>
      )}
      {l.mode === "active" && (
        <p className="hint">
          已启用全部线路（含备用线路）。额度和有效期以软件提供方的设置为准，升级软件后无需重新激活。
        </p>
      )}
      {l.mode === "active" && l.upstreamRemaining && (
        <p className="hint">
          图片线路 1 剩余额度：{l.upstreamRemaining}（以图片服务为准）
        </p>
      )}
      {l.mode === "trial" && (
        <>
          <div className="license-trial">
            <span>
              试用剩余 <strong>{l.trialRemaining}</strong> / {l.trialLimit} 张
              {l.trialPending ? `（${l.trialPending} 张生成中）` : ""}
            </span>
            <Progress
              percent={
                l.trialLimit
                  ? Math.round(
                      ((l.trialUsed + l.trialPending) / l.trialLimit) * 100,
                    )
                  : 0
              }
              showInfo={false}
              status={l.trialRemaining ? "active" : "exception"}
              aria-label={`已用 ${l.trialUsed + l.trialPending} / ${l.trialLimit} 张`}
            />
            <small className="hint">
              成功生成的图片计入试用；失败、取消和被拒绝的不计入，结果未知的计入
              1 张。重新安装软件或清除数据不会重置试用。
            </small>
            {l.upstreamRemaining && (
              <small className="hint">
                试用线路剩余额度：{l.upstreamRemaining}（以图片服务为准）
              </small>
            )}
          </div>
          <form
            className="license-form"
            onSubmit={(e) => {
              e.preventDefault();
              actions.run(activate);
            }}
          >
            <label htmlFor="activation-code" className="field-label">
              激活码
            </label>
            <div className="license-form-row">
              <Input
                id="activation-code"
                value={code}
                autoComplete="off"
                spellCheck={false}
                placeholder="XXXXX-XXXXX-XXXXX-XXXXX-XXXXX"
                status={error ? "error" : undefined}
                aria-describedby={error ? "activation-error" : undefined}
                onChange={(e) => {
                  setCode(e.target.value);
                  setError("");
                }}
              />
              <Button
                type="primary"
                htmlType="submit"
                loading={actions.busy}
                disabled={!code.trim()}
              >
                激活
              </Button>
            </div>
            {error && (
              <p id="activation-error" className="field-error" role="alert">
                {error}
              </p>
            )}
            <p className="hint">
              激活后启用全部线路（含备用线路），试用期间的图片和任务都会保留。不需要填写任何地址或
              Key。
            </p>
          </form>
        </>
      )}
    </section>
  );
}
