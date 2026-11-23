import { useEffect, useLayoutEffect, useRef } from "react";
import { App as AntApp, Radio, Tooltip } from "antd";
import { t } from "./i18n";
import {
  setAppearance,
  useAppearance,
  type AppearancePref,
} from "./appearance";
import type { Mode } from "./tokens";

// 0.8.3: the mark in the top-left corner is the 明 / 暗 switch. It is a moon:
// 暗 keeps the 0.7 mark, a thin lit crescent on the dark wall; 明 is the full
// moon, an ink ring on paper. A switch sweeps the terminator across the disc
// over --motion-phase while the colours change over --motion-layout; hovering
// leans it a little towards where a click would take it. It is still
// otherwise. With reduced motion the phase changes at once.
//
// The terminator is half an ellipse from the top of the disc to the bottom;
// `phase` is its half-width as a share of the radius, positive when it bulges
// right (a crescent lit on the right), negative when it bulges left.
const R = 12.5;
const rest: Record<Mode, number> = { dark: 0.36, light: -1 };
const lean = 0.14;
const phaseMs = 640;
const hoverMs = 120;
const settleMs = 200;

function terminator(phase: number) {
  const rx = Math.max(Math.abs(phase) * R, 0.01).toFixed(3);
  return `A${rx} ${R} 0 0 ${phase > 0 ? 0 : 1} 14 1.5Z`;
}
// The app's one curve, cubic-bezier(0.16, 1, 0.3, 1).
function ease(x: number) {
  const bx = (t: number) =>
    3 * (1 - t) ** 2 * t * 0.16 + 3 * (1 - t) * t * t * 0.3 + t ** 3;
  const by = (t: number) => 3 * (1 - t) ** 2 * t + 3 * (1 - t) * t * t + t ** 3;
  let lo = 0;
  let hi = 1;
  for (let i = 0; i < 20; i++) {
    const m = (lo + hi) / 2;
    if (bx(m) < x) lo = m;
    else hi = m;
  }
  return by((lo + hi) / 2);
}
const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");

export function ThemeMark() {
  const { pref, mode } = useAppearance();
  const { message } = AntApp.useApp();
  const lit = useRef<SVGPathElement>(null);
  const shade = useRef<SVGPathElement>(null);
  const phase = useRef(rest[mode]);
  const frame = useRef(0);

  const draw = (p: number) => {
    phase.current = p;
    const end = terminator(p);
    lit.current?.setAttribute("d", `M14 1.5A${R} ${R} 0 0 1 14 26.5${end}`);
    shade.current?.setAttribute("d", `M14 1.5A${R} ${R} 0 0 0 14 26.5${end}`);
  };
  const go = (to: number, ms: number) => {
    cancelAnimationFrame(frame.current);
    if (reducedMotion.matches || ms <= 0) {
      draw(to);
      return;
    }
    const from = phase.current;
    const start = performance.now();
    const step = (now: number) => {
      const k = Math.min(1, (now - start) / ms);
      draw(from + (to - from) * ease(k));
      if (k < 1) frame.current = requestAnimationFrame(step);
    };
    frame.current = requestAnimationFrame(step);
  };

  useLayoutEffect(() => draw(phase.current), []);
  // Any change of mode (a click, the settings page, Windows) sweeps the phase.
  const shown = useRef(mode);
  useEffect(() => {
    if (shown.current === mode) return;
    shown.current = mode;
    go(rest[mode], phaseMs);
  }, [mode]);
  useEffect(() => () => cancelAnimationFrame(frame.current), []);

  const next: Mode = mode === "dark" ? "light" : "dark";
  const label = t(
    next === "light" ? "shell.appearance.toLight" : "shell.appearance.toDark",
  );
  const toggle = () =>
    setAppearance(next).catch((e) =>
      message.error(
        `${t("shell.appearance.failed")}: ${e instanceof Error ? e.message : e}`,
      ),
    );
  return (
    <Tooltip
      placement="right"
      title={
        <>
          {label}
          {pref === "system" && (
            <span className="rail-mark-note">
              {t("shell.appearance.following")}
            </span>
          )}
        </>
      }
    >
      <button
        type="button"
        className="rail-mark"
        aria-label={label}
        onClick={toggle}
        onPointerEnter={() =>
          go(rest[mode] + (mode === "dark" ? -lean : lean), hoverMs)
        }
        onPointerLeave={() => go(rest[mode], settleMs)}
      >
        <svg viewBox="0 0 28 28" aria-hidden="true">
          <path ref={shade} className="rail-mark-shade" />
          <path ref={lit} className="rail-mark-lit" />
          <circle cx="14" cy="14" r={R} />
        </svg>
      </button>
    </Tooltip>
  );
}

/** Appearance section of the settings page: the way back to following Windows. */
export function AppearancePanel() {
  const { pref, mode } = useAppearance();
  const { message } = AntApp.useApp();
  const choose = (next: AppearancePref) => {
    if (next === pref) return;
    setAppearance(next).catch((e) =>
      message.error(
        `${t("shell.appearance.failed")}: ${e instanceof Error ? e.message : e}`,
      ),
    );
  };
  return (
    <section className="panel appearance-panel">
      <div className="panel-heading">
        <h2 id="appearance-heading">
          <span className="anticon" aria-hidden="true">
            <svg viewBox="0 0 28 28" width="1em" height="1em">
              <circle
                cx="14"
                cy="14"
                r={R}
                fill="none"
                stroke="currentColor"
                strokeWidth="2.5"
              />
              <path
                d={`M14 1.5A${R} ${R} 0 0 ${mode === "dark" ? 1 : 0} 14 26.5${terminator(0)}`}
                fill="currentColor"
              />
            </svg>
          </span>{" "}
          {t("shell.appearance.label")}
        </h2>
      </div>
      <div role="radiogroup" aria-labelledby="appearance-heading">
        <Radio.Group
          optionType="button"
          value={pref}
          onChange={(e) => choose(e.target.value as AppearancePref)}
          options={(["system", "light", "dark"] as const).map((v) => ({
            value: v,
            label: t(`shell.appearance.${v}`),
          }))}
        />
      </div>
      <p className="hint">{t("shell.appearance.hint")}</p>
    </section>
  );
}
