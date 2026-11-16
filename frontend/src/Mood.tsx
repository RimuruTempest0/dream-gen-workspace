import { useEffect, useRef, useState, type Ref } from "react";
import { Alert, Button, Drawer } from "antd";
import { ArrowLeftOutlined, ArrowRightOutlined } from "@ant-design/icons";
import {
  errorText,
  type MoodChoice,
  type MoodLibrary,
  type MoodOption,
  type Task,
} from "./api";
import { useData } from "./ui";
import { MoodGlyph } from "./MoodGlyphs";

// ---- mood guide (0.7.26) -----------------------------------------------------
// The user does not describe a feeling: they pick a weather, a time of day, a
// distance and what the image should do for them, and the backend writes that
// into the image's atmosphere at generation time.

type StepKey = keyof MoodChoice;
const steps: { key: StepKey; name: string; title: string }[] = [
  { key: "weather", name: "天气", title: "此刻更像哪种天气？" },
  { key: "time", name: "时刻", title: "更像一天里的什么时候？" },
  { key: "distance", name: "远近", title: "你想离它多远？" },
  { key: "expectation", name: "期待", title: "你希望这张图怎样？" },
];
const confirmStep = steps.length;

export function useMoods() {
  return useData<MoodLibrary>(["moods"], "ListMoods");
}

function optionsOf(lib: MoodLibrary, key: StepKey): MoodOption[] {
  return lib[key];
}

function find(lib: MoodLibrary | undefined, key: StepKey, id?: string) {
  return lib?.[key].find((o) => o.id === id);
}

function complete(v: Partial<MoodChoice>): v is MoodChoice {
  return steps.every((s) => !!v[s.key]);
}

// "细雨 · 深夜 · 很远 · 缓一缓", the same label the backend gives each image.
export function moodLabel(lib: MoodLibrary | undefined, m?: MoodChoice) {
  if (!lib || !m) return "";
  const parts = steps.map((s) => find(lib, s.key, m[s.key])?.short);
  return parts.every(Boolean) ? parts.join(" · ") : "";
}

// The plain sentence on the confirmation screen, built locally from the phrases.
export function moodSentence(lib: MoodLibrary, m: MoodChoice) {
  const [w, t, d, e] = steps.map((s) => find(lib, s.key, m[s.key]));
  if (!w || !t || !d || !e) return "";
  return `此刻像一个${t.phrase}，${w.phrase}。你想${d.phrase}，让这张图${e.phrase}。`;
}

// Above the concept entry while the guide mode is on.
export function MoodBar({
  mood,
  disabled,
  onOpen,
  onClear,
  openRef,
}: {
  mood?: MoodChoice;
  disabled?: boolean;
  onOpen: () => void;
  onClear: () => void;
  openRef: Ref<HTMLButtonElement>;
}) {
  const lib = useMoods().data;
  const label = moodLabel(lib, mood);
  if (!label)
    return (
      <div className="mood-bar">
        <button
          ref={openRef}
          type="button"
          className="mood-entry"
          onClick={onOpen}
          disabled={disabled}
          aria-haspopup="dialog"
        >
          <span className="mood-entry-mark" aria-hidden="true">
            <MoodGlyph id="thin-cloud" />
          </span>
          <span className="mood-entry-copy">
            <strong>心境</strong>
            <span>挑几样东西，让图更像此刻的你</span>
          </span>
          <ArrowRightOutlined aria-hidden="true" />
        </button>
      </div>
    );
  return (
    <div className="mood-bar mood-bar-set">
      <span className="mood-entry-mark" aria-hidden="true">
        <MoodGlyph id={mood!.weather} />
      </span>
      <span className="mood-current">
        <span className="mood-current-kicker">心境</span>
        <strong>{label}</strong>
      </span>
      <span className="mood-bar-actions">
        <Button
          ref={openRef}
          size="small"
          onClick={onOpen}
          disabled={disabled}
          aria-haspopup="dialog"
        >
          更换
        </Button>
        <Button size="small" onClick={onClear}>
          清除
        </Button>
      </span>
    </div>
  );
}

export function MoodGuide({
  open,
  value,
  onClose,
  onAfterClose,
  onApply,
}: {
  open: boolean;
  value?: MoodChoice;
  onClose: () => void;
  onAfterClose: (applied: boolean) => void;
  onApply: (mood: MoodChoice) => void;
}) {
  const library = useMoods();
  const lib = library.data;
  const [draft, setDraft] = useState<Partial<MoodChoice>>({});
  const [step, setStep] = useState(0);
  // Set when a step was reopened from the confirmation screen: picking returns there.
  const [revisit, setRevisit] = useState(false);
  // How the current screen was reached: forward, back, or already there when
  // the guide opened (then it does not animate at all).
  const [entry, setEntry] = useState<"forward" | "back" | "still">("forward");
  const heading = useRef<HTMLHeadingElement>(null);
  const applied = useRef(false);
  const advance = useRef<number | undefined>(undefined);
  useEffect(() => {
    if (!open) return;
    applied.current = false;
    setRevisit(false);
    // Opened again with a mood: straight to the confirmation screen.
    if (value && complete(value)) {
      setDraft(value);
      setStep(confirmStep);
      setEntry("still");
    } else {
      setDraft({});
      setStep(0);
      setEntry("forward");
    }
  }, [open]);
  useEffect(() => () => window.clearTimeout(advance.current), []);
  // Each screen announces its question; focus moves to it, not to an option.
  useEffect(() => {
    if (open) heading.current?.focus({ preventScroll: true });
  }, [step]);
  function goto(n: number) {
    window.clearTimeout(advance.current);
    setEntry(n < step ? "back" : "forward");
    setStep(n);
  }
  function pick(key: StepKey, id: string) {
    const next = { ...draft, [key]: id };
    setDraft(next);
    const target =
      revisit || complete(next) ? confirmStep : Math.min(step + 1, confirmStep);
    window.clearTimeout(advance.current);
    // A short pause lets the choice show before the next screen.
    advance.current = window.setTimeout(() => {
      setRevisit(false);
      setEntry("forward");
      setStep(target);
    }, 180);
  }
  const current = steps[step];
  const done = complete(draft);
  const body = library.error ? (
    <Alert
      type="error"
      message="选项读取失败"
      description={errorText(library.error)}
    />
  ) : !lib ? (
    <p className="mood-loading">正在读取</p>
  ) : current ? (
    <section
      className={`mood-step mood-step-${entry}`}
      key={current.key}
      aria-labelledby="mood-q"
    >
      <p className="mood-kicker" aria-hidden="true">
        {String(step + 1).padStart(2, "0")} /{" "}
        {String(steps.length).padStart(2, "0")}
      </p>
      <h3 id="mood-q" className="mood-question" ref={heading} tabIndex={-1}>
        {current.title}
      </h3>
      <div
        className={`mood-options mood-options-${optionsOf(lib, current.key).length}`}
        role="group"
        aria-labelledby="mood-q"
      >
        {optionsOf(lib, current.key).map((o) => (
          <button
            key={o.id}
            type="button"
            className="mood-option"
            aria-pressed={draft[current.key] === o.id}
            onClick={() => pick(current.key, o.id)}
          >
            <MoodGlyph id={o.id} />
            <span className="mood-option-name">{o.name}</span>
          </button>
        ))}
      </div>
    </section>
  ) : done ? (
    <section
      className={`mood-step mood-step-${entry} mood-confirm`}
      aria-labelledby="mood-q"
    >
      <p className="mood-kicker" aria-hidden="true">
        就是这样吗
      </p>
      <h3 id="mood-q" className="mood-sentence" ref={heading} tabIndex={-1}>
        {moodSentence(lib, draft)}
      </h3>
      <ul className="mood-picks" aria-label="已选，点一项可以改">
        {steps.map((s, i) => {
          const o = find(lib, s.key, draft[s.key]);
          return (
            <li key={s.key}>
              <button
                type="button"
                className="mood-pick"
                onClick={() => {
                  setRevisit(true);
                  goto(i);
                }}
                aria-label={`${s.name}：${o?.name}，点按更改`}
              >
                <MoodGlyph id={o?.id || ""} />
                <span className="mood-pick-step">{s.name}</span>
                <span className="mood-pick-name">{o?.name}</span>
              </button>
            </li>
          );
        })}
      </ul>
    </section>
  ) : null;
  const footer = (
    <div className="mood-footer">
      {step > 0 && step < confirmStep ? (
        <Button
          type="text"
          icon={<ArrowLeftOutlined aria-hidden="true" />}
          onClick={() => goto(step - 1)}
        >
          上一步
        </Button>
      ) : step === confirmStep ? (
        <Button
          type="text"
          onClick={() => {
            setDraft({});
            setRevisit(false);
            goto(0);
          }}
        >
          重新挑
        </Button>
      ) : (
        <span />
      )}
      {step === confirmStep && done ? (
        <Button
          type="primary"
          size="large"
          onClick={() => {
            applied.current = true;
            onApply(draft);
          }}
        >
          就用这个心境
        </Button>
      ) : (
        <span className="mood-footer-note">点一下就到下一步</span>
      )}
    </div>
  );
  return (
    <Drawer
      open={open}
      onClose={onClose}
      afterOpenChange={(v) => {
        if (!v) onAfterClose(applied.current);
      }}
      placement="right"
      width={560}
      rootClassName="mood-guide"
      aria-label="心境引导"
      title={
        <nav aria-label="心境引导步骤">
          <ol className="mood-progress">
            {steps.map((s, i) => {
              const reachable = !!draft[s.key] || i <= step;
              return (
                <li key={s.key}>
                  <button
                    type="button"
                    className="mood-progress-step"
                    data-done={!!draft[s.key]}
                    aria-current={i === step ? "step" : undefined}
                    disabled={!reachable}
                    onClick={() => {
                      setRevisit(done);
                      goto(i);
                    }}
                  >
                    <span className="mood-progress-line" aria-hidden="true" />
                    {s.name}
                  </button>
                </li>
              );
            })}
            <li>
              <button
                type="button"
                className="mood-progress-step"
                data-done={false}
                aria-current={step === confirmStep ? "step" : undefined}
                disabled={!done}
                onClick={() => goto(confirmStep)}
              >
                <span className="mood-progress-line" aria-hidden="true" />
                确认
              </button>
            </li>
          </ol>
        </nav>
      }
      footer={footer}
    >
      {body}
    </Drawer>
  );
}

// The rendering and nudge names of a mood image, for its tags.
export function MoodTags({ task }: { task: Task }) {
  const lib = useMoods().data;
  if (!task.mood) return null;
  const rendering = lib?.renderings.find((r) => r.id === task.rendering);
  return (
    <>
      <span className="mood-tag" title="心境">
        {task.mood}
      </span>
      {rendering && <span className="mood-tag">{rendering.name}</span>}
      {task.nudge && <span className="mood-tag">{task.nudge}</span>}
    </>
  );
}

// 再靠近一点: three pairs of directions under a mood image.
export function MoodNudges({
  disabled,
  onNudge,
}: {
  disabled?: boolean;
  onNudge: (direction: MoodOption) => void;
}) {
  const lib = useMoods().data;
  if (!lib) return null;
  const pairs = [0, 2, 4].map((i) => lib.nudges.slice(i, i + 2));
  return (
    <div className="mood-nudges" role="group" aria-label="再靠近一点">
      <span className="mood-nudges-title" aria-hidden="true">
        再靠近一点
      </span>
      {pairs.map((pair) => (
        <span className="mood-nudge-pair" key={pair[0]?.id}>
          {pair.map((n) => (
            <button
              key={n.id}
              type="button"
              className="mood-nudge"
              disabled={disabled}
              onClick={() => onNudge(n)}
            >
              {n.name}
            </button>
          ))}
        </span>
      ))}
    </div>
  );
}
