// Pieces of the smart-generation page (SmartImages.tsx), kept apart to keep that file readable.
import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Alert, Button, Input, Modal, Tag } from "antd";
import { DeleteOutlined } from "@ant-design/icons";
import { call, type Task } from "./api";
import { type AgentRun } from "./agent-api";
import { AssetImage, durationText, rejectionOf, useActions } from "./ui";
import { formatNumber, t, type MessageKey } from "./i18n";

const fixed = (digits: number) => ({
  minimumFractionDigits: digits,
  maximumFractionDigits: digits,
});

export function ReferenceStrip({
  ids,
  remove,
}: {
  ids: string[];
  remove?: (id: string) => void;
}) {
  return (
    <div className="agent-references">
      {ids.map((id) => (
        <div className="agent-reference" key={id}>
          <AssetImage id={id} />
          <small className="mono">{id.slice(0, 8)}</small>
          {remove && (
            <Button
              size="small"
              type="text"
              aria-label={t("smart.reference.remove", { id: id.slice(0, 8) })}
              icon={<DeleteOutlined aria-hidden="true" />}
              onClick={() => remove(id)}
            />
          )}
        </div>
      ))}
    </div>
  );
}
export function AgentTiming({ run, tasks }: { run: AgentRun; tasks: Task[] }) {
  const [now, setNow] = useState(Date.now());
  const busy =
    run.status === "analyzing" ||
    (!!run.batchID &&
      (tasks.length === 0 ||
        tasks.some((t) =>
          ["queued", "submitting", "running", "downloading"].includes(t.status),
        )));
  useEffect(() => {
    if (!busy) return;
    const interval = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(interval);
  }, [busy, run.id]);
  const start = Date.parse(run.createdAt);
  const savedEnd = Math.max(
    Date.parse(run.updatedAt || run.createdAt),
    ...tasks.map((t) => Date.parse(t.updatedAt || t.createdAt)),
  );
  // Waiting for the user's answers does not count; the timer stands still then.
  const elapsed = Math.max(
    0,
    Math.floor(((busy ? now : savedEnd) - start - (run.waitedMS || 0)) / 1000),
  );
  const waiting = run.status === "needs_input";
  return (
    <span className="muted">
      {t(busy ? "smart.timing.elapsed" : "smart.timing.total", {
        time: durationText(elapsed),
      })}
      {waiting && t("smart.timing.paused")}
    </span>
  );
}

// Stages the page reacts to come with a code (AgentRun.stageCode, 0.8.0), so
// the stage text may be in any language; stageText shows them in the
// interface language.
const knownStages: Record<string, MessageKey> = {
  "browser-reading": "run.browserReading",
  "browser-verify": "smart.stage.verify",
  preparing: "run.step.prepare",
};
// Runs saved before 0.8.0 carry only the Chinese stage text.
const legacyStages: Record<string, string> = {
  正在用浏览器读取商品页: "browser-reading",
  需要你在弹出的窗口里完成验证: "browser-verify",
  准备素材: "preparing",
};
type Staged = {
  stage?: string;
  text?: string;
  stageCode?: string;
  code?: string;
};
export const stageCodeOf = (s: Staged) =>
  s.stageCode || s.code || legacyStages[s.stage ?? s.text ?? ""] || "";
export const isKnownStage = (s: Staged) => !!knownStages[stageCodeOf(s)];
export const isVerifyStage = (s: Staged) => stageCodeOf(s) === "browser-verify";
export function stageText(s: Staged) {
  const key = knownStages[stageCodeOf(s)];
  return key ? t(key) : s.stage || "";
}

// Counts from the saved verification event, outside the live region so screen
// readers hear the stage once instead of every second.
export function VerificationWait({ run }: { run: AgentRun }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const interval = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(interval);
  }, []);
  const event = [...run.events].reverse().find((e) => isVerifyStage(e));
  const since = Date.parse(event?.at || run.updatedAt);
  const seconds = Math.max(0, Math.floor((now - since) / 1000));
  return (
    <p className="muted">
      {t("smart.verify.wait", { time: durationText(seconds) })}
    </p>
  );
}

export const ratioSizes: Record<string, string> = {
  "1:1": "1024x1024",
  "3:2": "1536x1024",
  "2:3": "1024x1536",
  "16:9": "1792x1024",
};

// Images the relay refused before generating: a refused key or an empty account.
export const isKeyRejected = (task: Task) => rejectionOf(task) === "auth";
export const isBalanceRejected = (task: Task) =>
  rejectionOf(task) === "balance";

// An image drawn from the suite's detail sample says which image it waits for or
// copied, so a pause before it starts is not mistaken for a stuck queue.
export const basisNames: Record<string, { label: MessageKey; color?: string }> =
  {
    instruction: { label: "smart.basis.instruction", color: "blue" },
    source: { label: "smart.basis.source", color: "cyan" },
    choice: { label: "smart.basis.choice", color: "purple" },
    habit: { label: "smart.basis.habit", color: "geekblue" },
    verify: { label: "smart.basis.verify", color: "orange" },
  };

// An automatic run answers its own questions; this shows how it decided each one,
// so the values it had to pick (需核对) can be checked against the images.
export function AgentJudgments({ run }: { run: AgentRun }) {
  const answered = run.questions.filter((q) => q.answeredBy === "agent");
  if (!run.input.autoMode || !answered.length) return null;
  const verify = answered.filter((q) => q.basis === "verify").length;
  return (
    <section
      className="agent-judgments"
      aria-labelledby="agent-judgments-title"
    >
      <h3 id="agent-judgments-title">
        {t("smart.judgments.title")}
        {verify > 0 && (
          <small className="agent-judgments-verify">
            {t("smart.judgments.verify", { count: verify })}
          </small>
        )}
      </h3>
      <ul>
        {answered.map((q) => {
          const basis = basisNames[q.basis || ""] || {
            label: "smart.basis.own",
          };
          return (
            <li key={q.id}>
              <span className="agent-judgment-question">{q.label}</span>
              <span className="agent-judgment-answer">
                <Tag color={basis.color}>{t(basis.label)}</Tag>
                {q.answer}
              </span>
              {q.reason && <small className="hint">{q.reason}</small>}
            </li>
          );
        })}
      </ul>
    </section>
  );
}

export function SampleNote({ task, tasks }: { task: Task; tasks: Task[] }) {
  const items = task.sampleItems?.length
    ? task.sampleItems
    : task.sampleItem
      ? [task.sampleItem]
      : [];
  if (!items.length) return null;
  const which = items
    .map((id) => {
      const n = tasks.findIndex((t) => t.planItemID === id) + 1;
      const area = n > 0 ? tasks[n - 1].sampleArea : "";
      if (n > 0 && area) return t("smart.sample.numberArea", { n, area });
      if (n > 0) return t("smart.sample.number", { n });
      if (area) return t("smart.sample.area", { area });
      return t("smart.sample.plain");
    })
    .join(t("smart.listSeparator"));
  if (task.samplePending)
    return <p className="hint">{t("smart.sample.pending", { which })}</p>;
  if (task.sampleAssetIDs?.length || task.sampleAssetID)
    return <p className="hint">{t("smart.sample.copied", { which })}</p>;
  if (task.status !== "queued")
    return <p className="hint">{t("smart.sample.failed", { which })}</p>;
  return null;
}

// The sample is checked against its design references before the set copies it.
export function SampleCheckNote({ task }: { task: Task }) {
  const redraw = task.sampleRedraws || 0;
  switch (task.sampleCheck) {
    case "checking":
      return <p className="hint">{t("smart.check.checking")}</p>;
    case "passed":
      return (
        <p className="hint">
          {redraw > 0
            ? t("smart.check.passedRedrawn", { count: redraw })
            : t("smart.check.passed")}
        </p>
      );
    case "failed":
      return (
        <Alert
          type="warning"
          showIcon
          message={t("smart.check.failed", { count: redraw })}
          description={task.sampleProblems}
        />
      );
    case "skipped":
      return <p className="hint">{task.sampleProblems}</p>;
  }
  if (redraw > 0 && task.status !== "succeeded")
    return (
      <p className="hint" title={task.sampleProblems}>
        {t("smart.check.redrawing", {
          count: redraw,
          problems: task.sampleProblems || "",
        })}
      </p>
    );
  return null;
}

// How a refinement is being made: drawn again from the plan or changed on the
// old image; decided by the AI after the request is submitted.
export function RefineModeNote({ task }: { task: Task }) {
  if (!task.refinedFrom) return null;
  let key: MessageKey | null = null;
  if (task.refineMode === "regenerate") key = "smart.refine.mode.regenerate";
  else if (task.refineMode === "edit")
    key = task.refineUnjudged
      ? "smart.refine.mode.unjudged"
      : "smart.refine.mode.edit";
  else if (task.status === "queued" && task.rewording)
    key = "smart.refine.mode.deciding";
  if (!key) return null;
  return (
    <p className="hint refine-mode" data-testid="refine-mode">
      {t(key)}
    </p>
  );
}

export type RefineTarget = { task: Task; assetID: string };

// Refinement is a new, possibly billable edit request based on one finished
// image; the dialog states the cost and the request ID makes retries idempotent.
export function RefineDialog({
  target,
  onClose,
}: {
  target: RefineTarget | null;
  onClose: () => void;
}) {
  const actions = useActions();
  const [text, setText] = useState("");
  const requestID = useRef("");
  useEffect(() => {
    if (target) {
      setText("");
      requestID.current = crypto.randomUUID();
    }
  }, [target]);
  async function submit() {
    if (!target || !text.trim()) return;
    const batch = await actions.run(
      () =>
        call<string>("RefineAgentImage", {
          requestID: requestID.current,
          taskID: target.task.id,
          assetID: target.assetID,
          instruction: text.trim(),
        }),
      t("smart.refine.submitted"),
    );
    if (batch !== undefined) onClose();
  }
  return (
    <Modal
      title={t("smart.refine.title")}
      open={!!target}
      onCancel={onClose}
      onOk={submit}
      okText={t("smart.refine.ok")}
      cancelText={t("smart.refine.cancel")}
      okButtonProps={{ disabled: !text.trim() }}
      confirmLoading={actions.busy}
      destroyOnHidden
    >
      {target && (
        <div className="refine-preview">
          <AssetImage id={target.assetID} />
        </div>
      )}
      <label htmlFor="refine-instruction" className="field-label">
        {t("smart.refine.label")}
      </label>
      <Input.TextArea
        id="refine-instruction"
        value={text}
        onChange={(e) => setText(e.target.value)}
        maxLength={1000}
        showCount
        autoSize={{ minRows: 3, maxRows: 8 }}
        placeholder={t("smart.refine.placeholder")}
      />
      <p className="hint" style={{ marginTop: 28 }}>
        {t("smart.refine.tip")}
      </p>
      <p className="hint" data-testid="refine-modes">
        {t("smart.refine.modes")}
      </p>
      <p className="hint">{t("smart.refine.cost")}</p>
    </Modal>
  );
}

export type SmartMode = "manual" | "auto";
export const modeOptions: {
  value: SmartMode;
  label: MessageKey;
  hint: MessageKey;
}[] = [
  {
    value: "manual",
    label: "smart.mode.manual",
    hint: "smart.mode.manualHint",
  },
  { value: "auto", label: "smart.mode.auto", hint: "smart.mode.autoHint" },
];

export function ModeSwitch({
  value,
  disabled,
  onChange,
}: {
  value: SmartMode;
  disabled: boolean;
  onChange: (v: SmartMode) => void;
}) {
  return (
    <fieldset className="mode-switch" disabled={disabled}>
      <legend>{t("smart.mode.legend")}</legend>
      <div className="mode-switch-options">
        {modeOptions.map((o) => (
          <label key={o.value} className="mode-option">
            <input
              type="radio"
              name="smart-mode"
              value={o.value}
              checked={value === o.value}
              onChange={() => onChange(o.value)}
            />
            <span className="mode-option-dot" aria-hidden="true" />
            <span className="mode-option-text">
              <span className="mode-option-label">{t(o.label)}</span>
              <span className="mode-option-hint">{t(o.hint)}</span>
            </span>
          </label>
        ))}
      </div>
    </fieldset>
  );
}

export type SuiteEstimate = {
  images: number;
  medianMS?: number;
  durationSamples?: number;
  llmTokens?: number;
  llmRuns?: number;
  costPerImage?: number;
  cost?: number;
  costUnit?: string;
  costSamples?: number;
};

/** What the suite is likely to take, from this computer's own records only. */
export function SuiteEstimateHint(p: {
  connectionID: string;
  model: string;
  size: string;
  count: number;
}) {
  // Keep the last estimate while a new one loads, so the button below never jumps.
  const q = useQuery<SuiteEstimate>({
    queryKey: ["suite-estimate", p.connectionID, p.model, p.size, p.count],
    queryFn: () =>
      call<SuiteEstimate>(
        "EstimateSuite",
        p.connectionID,
        p.model,
        p.size,
        p.count,
      ),
    placeholderData: (previous) => previous,
  });
  const e = q.data;
  if (!e || (!e.medianMS && !e.llmTokens && !e.cost)) return null;
  const parts: string[] = [];
  if (e.cost && e.costPerImage)
    parts.push(
      t("smart.estimate.cost", {
        cost: formatNumber(e.cost, fixed(2)),
        each: formatNumber(e.costPerImage, fixed(3)),
        unit: e.costUnit || "",
        samples: e.costSamples || 0,
      }),
    );
  if (e.medianMS)
    parts.push(
      t("smart.estimate.time", {
        seconds: Math.max(1, Math.round(e.medianMS / 1000)),
        samples: e.durationSamples || 0,
      }),
    );
  if (e.llmTokens)
    parts.push(
      t("smart.estimate.tokens", {
        tokens: e.llmTokens,
        runs: e.llmRuns || 0,
      }),
    );
  return (
    <p className="hint" id="suite-estimate">
      {t("smart.estimate.text", {
        parts: parts.join(t("smart.partSeparator")),
      })}
    </p>
  );
}
