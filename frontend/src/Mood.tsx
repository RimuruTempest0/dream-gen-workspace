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
import { t, useI18n, type MessageKey } from "./i18n";

// ---- mood guide (0.7.26) -----------------------------------------------------
// The user does not describe a feeling: they pick a weather, a time of day, a
// distance and what the image should do for them, and the backend writes that
// into the image's atmosphere at generation time.

type StepKey = keyof MoodChoice;
// Text keys, read with t() at render time.
const steps: { key: StepKey; name: MessageKey; title: MessageKey }[] = [
  { key: "weather", name: "mood.step.weather", title: "mood.question.weather" },
  { key: "time", name: "mood.step.time", title: "mood.question.time" },
  {
    key: "distance",
    name: "mood.step.distance",
    title: "mood.question.distance",
  },
  {
    key: "expectation",
    name: "mood.step.expectation",
    title: "mood.question.expectation",
  },
];
const confirmStep = steps.length;

// The backend serves the options in the interface language (0.8.0), so the
// cached library is per language.
export function useMoods() {
  const { lang } = useI18n();
  return useData<MoodLibrary>(["moods", lang], "ListMoods");
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
  const [w, tm, d, e] = steps.map((s) => find(lib, s.key, m[s.key]));
  if (!w || !tm || !d || !e) return "";
  const parts: Record<string, string> = {
    time: tm.phrase,
    weather: w.phrase,
    distance: d.phrase,
    expectation: e.phrase,
  };
  return lib.sentence.replace(
    /\{(\w+)\}/g,
    (all, k: string) => parts[k] ?? all,
  );
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
            <strong>{t("mood.name")}</strong>
            <span>{t("mood.entry.hint")}</span>
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
        <span className="mood-current-kicker">{t("mood.name")}</span>
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
          {t("mood.change")}
        </Button>
        <Button size="small" onClick={onClear}>
          {t("mood.clear")}
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
      message={t("mood.loadFailed")}
      description={errorText(library.error)}
    />
  ) : !lib ? (
    <p className="mood-loading">{t("mood.loading")}</p>
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
        {t(current.title)}
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
        {t("mood.confirm.kicker")}
      </p>
      <h3 id="mood-q" className="mood-sentence" ref={heading} tabIndex={-1}>
        {moodSentence(lib, draft)}
      </h3>
      <ul className="mood-picks" aria-label={t("mood.picks")}>
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
                aria-label={t("mood.pick.change", {
                  step: t(s.name),
                  name: o?.name ?? "",
                })}
              >
                <MoodGlyph id={o?.id || ""} />
                <span className="mood-pick-step">{t(s.name)}</span>
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
          {t("mood.back")}
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
          {t("mood.restart")}
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
          {t("mood.apply")}
        </Button>
      ) : (
        <span className="mood-footer-note">{t("mood.next.note")}</span>
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
      aria-label={t("mood.guide")}
      title={
        <nav aria-label={t("mood.guide.steps")}>
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
                    {t(s.name)}
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
                {t("mood.step.confirm")}
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
      <span className="mood-tag" title={t("mood.name")}>
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
    <div className="mood-nudges" role="group" aria-label={t("mood.nudges")}>
      <span className="mood-nudges-title" aria-hidden="true">
        {t("mood.nudges")}
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
