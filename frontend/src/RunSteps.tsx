import { useEffect, useRef, useState } from "react";
import { CheckOutlined, LoadingOutlined } from "@ant-design/icons";
import type { AgentRun, RunProgress, RunStep } from "./agent-api";
import { formatClock, t, type MessageKey } from "./i18n";
import { durationText } from "./ui";

const runStepKeys: Record<RunStep, MessageKey> = {
  prepare: "run.step.prepare",
  product: "run.step.product",
  images: "run.step.images",
  references: "run.step.references",
  plan: "run.step.plan",
  submit: "run.step.submit",
};
export function runStepName(step: RunStep): string {
  return runStepKeys[step] ? t(runStepKeys[step]) : "";
}

// A reply with no new text for this long says so, to tell slow from stuck.
const replyQuietMS = 30_000;

export function runStepLabel(p: RunProgress) {
  return p.step === "product" && p.productVia === "browser"
    ? t("run.browserReading")
    : runStepName(p.step) || t("run.processing");
}

function span(ms: number) {
  return durationText(Math.max(0, Math.floor(ms / 1000)));
}

function stepDetail(step: RunStep, run: AgentRun): string {
  const p = run.progress!;
  switch (step) {
    case "product":
      if (run.product.error && p.step !== "product")
        return t("run.detail.productFailed");
      if (p.productVia === "browser")
        return p.productBrowserMS && p.step !== "product"
          ? t("run.detail.browserTime", { time: span(p.productBrowserMS) })
          : t("run.detail.browser");
      return p.productVia === "direct" ? t("run.detail.direct") : "";
    case "images":
      return p.imagesFound
        ? t("run.detail.images", {
            saved: p.imagesSaved || 0,
            found: p.imagesFound,
          })
        : "";
    case "references":
      return p.referencesSeen
        ? t("run.detail.references", { count: p.referencesSeen })
        : "";
    case "plan":
      return p.planWritten
        ? t("run.detail.planWritten", {
            written: p.planWritten,
            total: run.authorizedCount || run.input.targetCount,
          })
        : p.step === "plan"
          ? t("run.detail.planning")
          : "";
  }
  return "";
}

// What the model is doing right now: replying, or quiet for how long.
function replyText(p: RunProgress, now: number) {
  if (!p.replyActive) return "";
  const quiet = now - Date.parse(p.replyLastAt || "");
  if (!p.replyChars) return t("run.reply.waiting", { time: span(quiet) });
  if (quiet >= replyQuietMS) return t("run.reply.quiet", { time: span(quiet) });
  return t(p.replyThinking ? "run.reply.thinking" : "run.reply.replying", {
    chars: p.replyChars,
  });
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
      <ol className="run-steps" aria-label={t("run.steps.label")}>
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
              <strong>{runStepName(step)}</strong>
              {(detail || active) && (
                <small>
                  {detail}
                  {detail && active ? " · " : ""}
                  {active &&
                    t("run.step.elapsed", {
                      time: span(now - Date.parse(p.stepStartedAt)),
                    })}
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
    p?.llmRequests && t("run.activity.requests", { count: p.llmRequests }),
    p?.contextTokens && t("run.activity.context", { tokens: p.contextTokens }),
    p?.outputTokens && t("run.activity.output", { tokens: p.outputTokens }),
  ].filter(Boolean);
  return (
    <details
      className="run-activity"
      onToggle={(e) => e.currentTarget.open && toEnd()}
    >
      <summary>{t("run.activity.summary", { count: entries.length })}</summary>
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
          aria-label={t("run.activity.label")}
          tabIndex={0}
          onScroll={(e) => {
            const el = e.currentTarget;
            atEnd.current =
              el.scrollHeight - el.scrollTop - el.clientHeight < 32;
          }}
        >
          {entries.map((entry, index) => (
            <div key={index} className={`run-activity-line is-${entry.kind}`}>
              <time>{formatClock(entry.at, true)}</time>
              <span aria-hidden="true">{activityMarks[entry.kind]}</span>
              <span>{entry.text}</span>
            </div>
          ))}
        </div>
      </div>
    </details>
  );
}
