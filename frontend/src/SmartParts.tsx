// Pieces of the smart-generation page (SmartImages.tsx), kept apart to keep that file readable.
import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Alert, Button, Input, Modal, Tag } from "antd";
import { DeleteOutlined } from "@ant-design/icons";
import { call, type Task } from "./api";
import { type AgentRun } from "./agent-api";
import { AssetImage, rejectionOf, useActions } from "./ui";

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
              aria-label={`移除参考图 ${id.slice(0, 8)}`}
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
      {busy ? "已用时" : "累计用时"} {Math.floor(elapsed / 60)} 分{" "}
      {elapsed % 60} 秒{waiting && "（等待补充资料，计时已暂停）"}
    </span>
  );
}

// Stage texts written by the Go product browser (internal/core/product_browser.go).
export const browserReadingStage = "正在用浏览器读取商品页";
export const browserVerifyStage = "需要你在弹出的窗口里完成验证";

// Counts from the saved verification event, outside the live region so screen
// readers hear the stage once instead of every second.
export function VerificationWait({ run }: { run: AgentRun }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const interval = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(interval);
  }, []);
  const event = [...run.events]
    .reverse()
    .find((e) => e.text === browserVerifyStage);
  const since = Date.parse(event?.at || run.updatedAt);
  const seconds = Math.max(0, Math.floor((now - since) / 1000));
  return (
    <p className="muted">
      请在弹出的“完成验证后将自动继续”窗口中完成网站验证，完成后自动继续；关闭窗口将改为上传图片或粘贴描述。等待验证中（已{" "}
      {seconds} 秒）
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
export const basisNames: Record<string, { label: string; color?: string }> = {
  instruction: { label: "指令", color: "blue" },
  source: { label: "原链接", color: "cyan" },
  choice: { label: "自行选择", color: "purple" },
  habit: { label: "按你的习惯", color: "geekblue" },
  verify: { label: "需核对", color: "orange" },
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
        制作助手的判断
        {verify > 0 && (
          <small className="agent-judgments-verify">
            其中 {verify} 项需核对
          </small>
        )}
      </h3>
      <ul>
        {answered.map((q) => {
          const basis = basisNames[q.basis || ""] || { label: "自行判断" };
          return (
            <li key={q.id}>
              <span className="agent-judgment-question">{q.label}</span>
              <span className="agent-judgment-answer">
                <Tag color={basis.color}>{basis.label}</Tag>
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
      return `${n > 0 ? `第 ${n} 张` : ""}${area ? `「${area}」` : ""}样板图`;
    })
    .join("、");
  if (task.samplePending)
    return (
      <p className="hint">
        等{which}完成并核对后开始，照它画对应的设计；等待期间未提交、不计费。
      </p>
    );
  if (task.sampleAssetIDs?.length || task.sampleAssetID)
    return <p className="hint">已照{which}画对应的设计。</p>;
  if (task.status !== "queued")
    return <p className="hint">{which}没有成功，这张按文字说明生成。</p>;
  return null;
}

// The sample is checked against its design references before the set copies it.
export function SampleCheckNote({ task }: { task: Task }) {
  const redraw = task.sampleRedraws || 0;
  switch (task.sampleCheck) {
    case "checking":
      return (
        <p className="hint">正在对照设计依据核对样板图，其他图片核对完再画。</p>
      );
    case "passed":
      return (
        <p className="hint">
          {redraw > 0 ? `已按核对意见重画 ${redraw} 次，` : ""}
          样板图已核对，与设计依据一致。
        </p>
      );
    case "failed":
      return (
        <Alert
          type="warning"
          showIcon
          message={`样板图重画 ${redraw} 次后仍没通过核对，其他图片照它继续生成，请留意`}
          description={task.sampleProblems}
        />
      );
    case "skipped":
      return <p className="hint">{task.sampleProblems}</p>;
  }
  if (redraw > 0 && task.status !== "succeeded")
    return (
      <p className="hint" title={task.sampleProblems}>
        上一版样板没通过核对，正在重画（第 {redraw} 次）：{task.sampleProblems}
      </p>
    );
  return null;
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
      "已提交优化，完成后显示为这张图片的新版本",
    );
    if (batch !== undefined) onClose();
  }
  return (
    <Modal
      title="优化这张图片"
      open={!!target}
      onCancel={onClose}
      onOk={submit}
      okText="开始优化"
      cancelText="取消"
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
        想怎么调整？
      </label>
      <Input.TextArea
        id="refine-instruction"
        value={text}
        onChange={(e) => setText(e.target.value)}
        maxLength={1000}
        showCount
        autoSize={{ minRows: 3, maxRows: 8 }}
        placeholder="例如：把袖口的狗掌和 Amelia 摆正成水平的一行，狗掌在左"
      />
      <p className="hint" style={{ marginTop: 28 }}>
        写清要去掉什么、换成什么，并点名是谁或哪个位置。例如：右侧女性脱掉米白色针织衫，改穿和左侧同款的卫衣，颜色
        Maroon，胸前图案和袖口刺绣与左侧完全相同。
      </p>
      <p className="hint">
        将以这张图片为底图发起一次新的生图请求，可能产生费用。原图保留在“之前的版本”中。
      </p>
    </Modal>
  );
}

export type SmartMode = "manual" | "auto";
export const modeOptions: { value: SmartMode; label: string; hint: string }[] =
  [
    { value: "manual", label: "手动", hint: "缺资料时停下来问你" },
    { value: "auto", label: "自动", hint: "助手代答，标出需核对" },
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
      <legend>制作模式</legend>
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
              <span className="mode-option-label">{o.label}</span>
              <span className="mode-option-hint">{o.hint}</span>
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
      `图片费用约 ${e.cost.toFixed(2)} ${e.costUnit}（每张约 ${e.costPerImage.toFixed(3)} ${e.costUnit}，按最近 ${e.costSamples} 张的额度变化）`,
    );
  if (e.medianMS)
    parts.push(
      `每张生成约 ${Math.max(1, Math.round(e.medianMS / 1000))} 秒（最近 ${e.durationSamples} 张同模型同尺寸的中位数）`,
    );
  if (e.llmTokens)
    parts.push(
      `智能分析约 ${e.llmTokens.toLocaleString()} tokens（最近 ${e.llmRuns} 次的平均）`,
    );
  return (
    <p className="hint" id="suite-estimate">
      参考：{parts.join("；")}。实际以中转站和 LLM 供应商的计费为准。
    </p>
  );
}
