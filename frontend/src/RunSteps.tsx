import { useEffect, useRef, useState } from "react";
import { CheckOutlined, LoadingOutlined } from "@ant-design/icons";
import type { AgentRun, RunProgress, RunStep } from "./agent-api";

export const runStepNames: Record<RunStep, string> = {
  prepare: "准备素材",
  product: "读取商品页",
  images: "保存商品图",
  references: "分析资料和参考图",
  plan: "规划套图",
  submit: "提交生成",
};

// A reply with no new text for this long says so, to tell slow from stuck.
const replyQuietMS = 30_000;

export function runStepLabel(p: RunProgress) {
  return p.step === "product" && p.productVia === "browser"
    ? "正在用浏览器读取商品页"
    : runStepNames[p.step] || "处理中";
}

function span(ms: number) {
  const seconds = Math.max(0, Math.floor(ms / 1000));
  return seconds < 60
    ? `${seconds} 秒`
    : `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`;
}

function stepDetail(step: RunStep, run: AgentRun): string {
  const p = run.progress!;
  switch (step) {
    case "product":
      if (run.product.error && p.step !== "product") return "没有读取到商品页";
      if (p.productVia === "browser")
        return p.productBrowserMS && p.step !== "product"
          ? `网站拦截，改用浏览器读取（${span(p.productBrowserMS)}）`
          : "网站拦截，改用浏览器读取";
      return p.productVia === "direct" ? "直接读取" : "";
    case "images":
      return p.imagesFound
        ? `已保存 ${p.imagesSaved || 0} / ${p.imagesFound} 张`
        : "";
    case "references":
      return p.referencesSeen ? `已查看 ${p.referencesSeen} 张参考图` : "";
    case "plan":
      return p.planWritten
        ? `已写好 ${p.planWritten} / ${run.authorizedCount || run.input.targetCount} 张`
        : p.step === "plan"
          ? "正在写方案"
          : "";
  }
  return "";
}

// What the model is doing right now: replying, or quiet for how long.
function replyText(p: RunProgress, now: number) {
  if (!p.replyActive) return "";
  const quiet = now - Date.parse(p.replyLastAt || "");
  if (!p.replyChars) return `等待模型回复（已等 ${span(quiet)}）`;
  if (quiet >= replyQuietMS) return `模型暂时没有新内容（已等 ${span(quiet)}）`;
  return `${p.replyThinking ? "模型正在思考" : "模型正在回复"}，已收到约 ${p.replyChars.toLocaleString("en-US")} 字`;
}

// The steps of an analysis. The Agent chooses its own order, so an earlier step can
// become current again; a step is done once it was visited and is not current.
export function RunSteps({ run }: { run: AgentRun }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);
  const p = run.progress!;
  const steps: RunStep[] = [
    "prepare",
    ...(run.input.productURL ? (["product", "images"] as const) : []),
    "references",
    "plan",
    "submit",
  ];
  const reply = replyText(p, now);
  return (
    <>
      <ol className="run-steps" aria-label="制作步骤">
        {steps.map((step) => {
          const active = step === p.step;
          const done = !active && p.visited?.includes(step);
          const detail = stepDetail(step, run);
          return (
            <li
              key={step}
              className={
                active ? "run-step-active" : done ? "run-step-done" : undefined
              }
              aria-current={active ? "step" : undefined}
            >
              <span className="run-step-mark" aria-hidden="true">
                {active ? (
                  <LoadingOutlined spin />
                ) : done ? (
                  <CheckOutlined />
                ) : (
                  <span className="run-step-dot" />
                )}
              </span>
              <strong>{runStepNames[step]}</strong>
              {(detail || active) && (
                <small>
                  {detail}
                  {detail && active ? " · " : ""}
                  {active &&
                    `本步已用时 ${span(now - Date.parse(p.stepStartedAt))}`}
                </small>
              )}
            </li>
          );
        })}
      </ol>
      {reply && <p className="run-step-reply">{reply}</p>}
      {p.lineNote && <p className="run-step-note">{p.lineNote}</p>}
    </>
  );
}

const activityMarks = { agent: "●", tool: "›", llm: "◆", warn: "!" };

// A terminal-style feed of what Claude Code and the LLM did. It shows names, counts
// and the Agent's short messages, never prompts or skill text. Since 0.7.0 it stays
// folded until the user opens it; opening it shows the newest lines.
export function RunActivity({ run }: { run: AgentRun }) {
  const entries = run.activity || [];
  const list = useRef<HTMLDivElement>(null);
  const atEnd = useRef(true);
  const toEnd = () => {
    if (atEnd.current && list.current)
      list.current.scrollTop = list.current.scrollHeight;
  };
  useEffect(toEnd, [entries.length]);
  if (!entries.length) return null;
  const p = run.progress;
  const facts = [
    p?.agentVersion && `Claude Code ${p.agentVersion}`,
    run.llmConnection?.model,
    p?.llmRequests && `${p.llmRequests} 次 LLM 请求`,
    p?.contextTokens && `上下文 ${p.contextTokens.toLocaleString("en-US")}`,
    p?.outputTokens && `输出 ${p.outputTokens.toLocaleString("en-US")} tokens`,
  ].filter(Boolean);
  return (
    <details
      className="run-activity"
      onToggle={(e) => e.currentTarget.open && toEnd()}
    >
      <summary>运行详情（{entries.length} 条）</summary>
      <div className="run-activity-body">
        {facts.length > 0 && (
          <div className="run-activity-facts">
            {facts.map((fact) => (
              <span key={String(fact)}>{fact}</span>
            ))}
          </div>
        )}
        <div
          ref={list}
          className="run-activity-log"
          role="log"
          aria-label="运行详情"
          tabIndex={0}
          onScroll={(e) => {
            const el = e.currentTarget;
            atEnd.current =
              el.scrollHeight - el.scrollTop - el.clientHeight < 32;
          }}
        >
          {entries.map((entry, index) => (
            <div key={index} className={`run-activity-line is-${entry.kind}`}>
              <time>
                {new Date(entry.at).toLocaleTimeString("zh-CN", {
                  hour12: false,
                })}
              </time>
              <span aria-hidden="true">{activityMarks[entry.kind]}</span>
              <span>{entry.text}</span>
            </div>
          ))}
        </div>
      </div>
    </details>
  );
}
