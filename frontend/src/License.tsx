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
import { t } from "./i18n";
import { fill } from "./i18n/fill";

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
      ? t("license.badge.trial", {
          left: l.trialRemaining,
          limit: l.trialLimit,
        })
      : l.mode === "active"
        ? l.daysLeft
          ? t("license.badge.daysLeft", { days: l.daysLeft })
          : t("license.state.active")
        : l.mode === "expired"
          ? t("license.badge.expired")
          : t("license.state.problem");
  return (
    <Tooltip title={tooltip ? text : undefined} placement="right">
      <Link
        to="/settings#license"
        className={`license-badge license-${l.mode}`}
        aria-label={t("license.badge.aria", { status: text })}
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
        message={
          l.mode === "expired"
            ? t("license.notice.expired")
            : t("license.notice.blocked")
        }
        description={l.message}
      />
    );
  if (l.daysLeft)
    return (
      <Alert
        type="warning"
        showIcon
        className="license-notice"
        message={t("license.notice.expiring", { days: l.daysLeft })}
        description={fill(
          t("license.notice.expiringBody", {
            date: dateTime(l.expiresAt || ""),
          }),
          {
            link: (
              <Link to="/settings#license">
                {t("license.notice.expiringLink")}
              </Link>
            ),
          },
        )}
      />
    );
  if (l.mode === "trial" && l.trialRemaining <= 0)
    return (
      <Alert
        type="warning"
        showIcon
        className="license-notice"
        message={t("license.notice.trialUsed")}
        description={fill(
          t("license.notice.trialUsedBody", { limit: l.trialLimit }),
          {
            link: (
              <Link to="/settings#license">
                {t("license.notice.trialUsedLink")}
              </Link>
            ),
          },
        )}
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
    notification.success({ message: t("license.imported") });
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
            <SafetyCertificateOutlined aria-hidden="true" />{" "}
            {t("license.hosted.title")}
          </h2>
          <Tag>{t("license.hosted.unused")}</Tag>
        </div>
        <p className="hint">{t("license.hosted.hint")}</p>
        <div className="license-import">
          <Button
            onClick={() => actions.run(importFile)}
            disabled={actions.busy}
          >
            {t("license.import")}
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
          <SafetyCertificateOutlined aria-hidden="true" /> {t("license.title")}
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
            ? t("license.state.active")
            : l.mode === "trial"
              ? t("license.state.trial")
              : l.mode === "expired"
                ? t("license.state.expired")
                : t("license.state.problem")}
        </Tag>
      </div>
      <dl className="license-facts">
        {l.customer && (
          <>
            <dt>
              {l.hosted ? t("license.fact.customer") : t("license.fact.client")}
            </dt>
            <dd>{l.customer}</dd>
          </>
        )}
        <dt>{t("license.fact.softwareID")}</dt>
        <dd className="mono">{l.softwareID || "—"}</dd>
        {(l.mode === "active" || l.activated) && l.activatedAt && (
          <>
            <dt>{t("license.fact.activatedAt")}</dt>
            <dd>{time(l.activatedAt)}</dd>
          </>
        )}
        {l.expiresAt && (
          <>
            <dt>{t("license.fact.expiresAt")}</dt>
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
          message={t("license.expiring", { days: l.daysLeft })}
        />
      )}
      <div className="license-import">
        <Button onClick={() => actions.run(importFile)} disabled={actions.busy}>
          {t("license.import")}
        </Button>
        <small className="hint">{t("license.import.hint")}</small>
      </div>
      {l.hosted && <p className="hint">{t("license.leaveHosted")}</p>}
      {l.mode === "active" && (
        <p className="hint">{t("license.active.hint")}</p>
      )}
      {l.mode === "active" && l.upstreamRemaining && (
        <p className="hint">
          {t("license.active.upstream", { amount: l.upstreamRemaining })}
        </p>
      )}
      {l.mode === "trial" && (
        <>
          <div className="license-trial">
            <span>
              {fill(t("license.trial.left", { limit: l.trialLimit }), {
                left: <strong>{l.trialRemaining}</strong>,
              })}
              {l.trialPending
                ? t("license.trial.pending", { count: l.trialPending })
                : ""}
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
              aria-label={t("license.trial.used", {
                used: l.trialUsed + l.trialPending,
                limit: l.trialLimit,
              })}
            />
            <small className="hint">{t("license.trial.rules")}</small>
            {l.upstreamRemaining && (
              <small className="hint">
                {t("license.trial.upstream", { amount: l.upstreamRemaining })}
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
              {t("license.code")}
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
                {t("license.activate")}
              </Button>
            </div>
            {error && (
              <p id="activation-error" className="field-error" role="alert">
                {error}
              </p>
            )}
            <p className="hint">{t("license.activate.hint")}</p>
          </form>
        </>
      )}
    </section>
  );
}
