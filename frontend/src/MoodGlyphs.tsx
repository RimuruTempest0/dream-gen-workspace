import type { ReactNode } from "react";

// Line drawings for the mood guide (0.7.26): one per option, the same 48×48 box
// as the concept glyphs, currentColor strokes of 1.5 and no fill.

const cloud = "M13 31H35A6 6 0 0 0 35.5 19A9 9 0 0 0 18.5 17A7 7 0 0 0 13 31Z";
const smallCloud =
  "M11 25H29A5 5 0 0 0 29 15A7.5 7.5 0 0 0 15.5 13.5A6 6 0 0 0 11 25Z";

function rays(cx: number, cy: number, r1: number, r2: number, n = 8, from = 0) {
  return Array.from({ length: n }, (_, i) => {
    const a = from + (Math.PI * 2 * i) / n;
    return `M${(cx + r1 * Math.cos(a)).toFixed(2)} ${(cy + r1 * Math.sin(a)).toFixed(2)}L${(cx + r2 * Math.cos(a)).toFixed(2)} ${(cy + r2 * Math.sin(a)).toFixed(2)}`;
  }).join("");
}

function figure(x: number, y: number, h = 4) {
  return (
    <g key={`f-${x}-${y}`}>
      <circle cx={x} cy={y} r={h / 4} />
      <path d={`M${x} ${y + h / 2.5}V${y + h * 1.4}`} />
    </g>
  );
}

const glyphs: Record<string, ReactNode> = {
  // ---- weather ----
  clear: (
    <>
      <circle cx="24" cy="24" r="7" />
      <path d={rays(24, 24, 11, 15)} />
    </>
  ),
  "thin-cloud": (
    <>
      <path d="M21.5 17.5A7 7 0 1 1 33 25" opacity="0.55" />
      <path d={rays(27, 21, 10.5, 13.5, 4, -Math.PI * 0.95)} opacity="0.55" />
      <path d={cloud} />
    </>
  ),
  overcast: (
    <>
      <path d={smallCloud} opacity="0.55" />
      <path d="M15 37H37A6 6 0 0 0 37.5 25A9 9 0 0 0 20.5 23A7 7 0 0 0 15 37Z" />
    </>
  ),
  drizzle: (
    <>
      <path d={cloud} />
      <path d="M17 35.5L15.5 39M23 35.5L21.5 39M29 35.5L27.5 39M20 40.5L18.8 43.2M26 40.5L24.8 43.2" />
    </>
  ),
  fog: (
    <>
      <path d="M8 16H40" />
      <path d="M12 22H36" opacity="0.55" />
      <path d="M6 28H34" />
      <path d="M14 34H42" opacity="0.55" />
      <path d="M10 40H30" opacity="0.35" />
    </>
  ),
  storm: (
    <>
      <path d={cloud} />
      <path d="M25 31L21 38H26L23 45" />
      <path d="M15 35L13.5 39M33 35L31.5 39" opacity="0.55" />
    </>
  ),
  snow: (
    <>
      <path d={cloud} />
      <path d="M17 36.5V40.5M15 38.5H19M27 36.5V40.5M25 38.5H29" />
      <path d="M22 42.5h.1M32 42.5h.1M12 42.5h.1" />
    </>
  ),

  // ---- time of day ----
  dawn: (
    <>
      <path d="M4 34H44" />
      <path d="M15 34A6 6 0 0 1 27 34" />
      <path d={rays(21, 34, 9, 12, 5, Math.PI * 1.1)} opacity="0.55" />
      <path d="M8 39H22M28 39H40" opacity="0.35" />
    </>
  ),
  noon: (
    <>
      <circle cx="24" cy="11" r="5" />
      <path d={rays(24, 11, 8, 10.5)} opacity="0.55" />
      <path d="M24 30V40" />
      <path d="M4 40H44" />
      <path d="M22 41.5H26" opacity="0.55" />
    </>
  ),
  dusk: (
    <>
      <path d="M4 30H44" />
      <path d="M16 30A8 8 0 0 1 32 30" />
      <path d="M8 35H40" opacity="0.55" />
      <path d="M12 40H36" opacity="0.35" />
    </>
  ),
  night: (
    <>
      <path d="M30 9A13 13 0 1 0 39 32A11 11 0 0 1 30 9Z" />
      <path d="M11 11h.1M16 6h.1M8 22h.1" />
      <path d="M38 12V16M36 14H40" opacity="0.55" />
    </>
  ),

  // ---- distance ----
  far: (
    <>
      <rect x="4" y="6" width="40" height="36" rx="1" opacity="0.35" />
      <path d="M4 33H44" />
      {figure(31, 28, 3)}
    </>
  ),
  within: (
    <>
      <path d="M8 42V12H40V42" opacity="0.55" />
      <path d="M4 42H44" />
      <path d="M8 42L18 30H30L40 42" opacity="0.35" />
      {figure(24, 20, 8)}
    </>
  ),
  close: (
    <>
      <rect x="4" y="6" width="40" height="36" rx="1" opacity="0.35" />
      <path d="M14 42C14 26 20 14 33 10C40 18 41 30 36 42" />
      <path d="M33 10C29 20 26 31 25 42" opacity="0.55" />
    </>
  ),

  // ---- expectation ----
  resonate: (
    <>
      <path d="M9 41V14M9 14H15" />
      <path d="M13 17L17 17" opacity="0.55" />
      {figure(30, 27, 8)}
      <path d="M4 41H44" />
      <path
        d="M20 8L18.5 11.5M30 8L28.5 11.5M40 8L38.5 11.5M25 14L23.5 17.5M35 15L33.5 18.5"
        opacity="0.55"
      />
    </>
  ),
  soothe: (
    <>
      <path d="M8 22L24 10L40 22" />
      <path d="M12 19V40H36V19" />
      <rect x="19" y="25" width="10" height="9" />
      <path d="M24 25V34M19 29.5H29" opacity="0.55" />
      <path d="M4 40H44" />
    </>
  ),
  lift: (
    <>
      <path d="M4 16C10 12 16 13 20 15L22 9" />
      <path d="M44 14C38 11 32 12 28 15L26 9" />
      <path d="M22 17L14 40M26 17L34 40M24 17V40" opacity="0.55" />
      <path d="M4 40H44" />
    </>
  ),
};

export function MoodGlyph({ id }: { id: string }) {
  return (
    <svg
      className="mood-glyph"
      viewBox="0 0 48 48"
      width="48"
      height="48"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      {glyphs[id]}
    </svg>
  );
}
