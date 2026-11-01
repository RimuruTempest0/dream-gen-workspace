import { useState } from "react";
import { Link } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { Alert, Button, Input, Progress, Tag } from "antd";
import { SafetyCertificateOutlined } from "@ant-design/icons";
import { call } from "./api";
import { useActions, useData, time } from "./ui";

export type LicenseStatus = {
  mode: "developer" | "trial" | "active" | "invalid" | "mismatch";
  softwareID?: string;
  customer?: string;
  trialLimit: number;
  trialUsed: number;
  trialPending: number;
  trialRemaining: number;
  upstreamRemaining?: string;
  activatedAt?: string;
  message?: string;
};

export function useLicense() {
  return useData<LicenseStatus>(["license"], "LicenseStatus", [], 5000);
}

// Sidebar line: trial images left, activated, or a licence problem.
export function LicenseBadge() {
  const l = useLicense().data;
  if (!l || l.mode === "developer") return null;
  const text =
    l.mode === "trial"
      ? `试用剩余 ${l.trialRemaining} / ${l.trialLimit} 张`
      : l.mode === "active"
        ? "已激活"
        : "授权异常";
  return (
    <Link
      to="/settings#license"
      className={`license-badge license-${l.mode}`}
      aria-label={`授权状态：${text}`}
    >
      <SafetyCertificateOutlined aria-hidden="true" /> {text}
    </Link>
  );
}

// Shown above the generate forms when the trial is used up or the licence blocks.
export function LicenseNotice() {
  const l = useLicense().data;
  if (!l) return null;
  if (l.mode === "invalid" || l.mode === "mismatch")
    return (
      <Alert
        type="error"
        showIcon
        className="license-notice"
        message="暂时不能生图"
        description={l.message}
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
            {l.trialLimit} 张试用图片已用完，之前生成的图片都保留在素材库。
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
  const l = license.data;
  if (!l || l.mode === "developer") return null;
  async function activate() {
    setError("");
    try {
      await call("Activate", code);
      setCode("");
      // Activation adds the backup lines: refresh every list, not just the licence.
      await qc.invalidateQueries();
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
              : "授权异常"}
        </Tag>
      </div>
      <dl className="license-facts">
        {l.customer && (
          <>
            <dt>客户</dt>
            <dd>{l.customer}</dd>
          </>
        )}
        <dt>软件标识</dt>
        <dd className="mono">{l.softwareID || "—"}</dd>
        {l.mode === "active" && l.activatedAt && (
          <>
            <dt>激活时间</dt>
            <dd>{time(l.activatedAt)}</dd>
          </>
        )}
      </dl>
      {(l.mode === "invalid" || l.mode === "mismatch") && (
        <Alert type="error" showIcon message={l.message} />
      )}
      {l.mode === "active" && (
        <p className="hint">
          已启用全部线路（含备用线路）。额度和有效期以软件提供方的设置为准，升级软件后无需重新激活。
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
                试用线路剩余额度：{l.upstreamRemaining}（以中转站为准）
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
