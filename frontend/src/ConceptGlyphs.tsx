import type { ReactNode } from "react";

// Line drawings for the concept cards (0.7.18): one per concept, same 48×48 box,
// currentColor strokes of 1.5 and no fill, so the card decides the colour.

function hex(cx: number, cy: number, r: number) {
  const points = [0, 1, 2, 3, 4, 5].map((i) => {
    const a = (Math.PI / 3) * i + Math.PI / 6;
    return `${(cx + r * Math.cos(a)).toFixed(2)},${(cy + r * Math.sin(a)).toFixed(2)}`;
  });
  return <polygon key={`${cx}-${cy}`} points={points.join(" ")} />;
}

function figure(x: number, y: number) {
  return (
    <g key={`f-${x}-${y}`}>
      <circle cx={x} cy={y} r={1} />
      <path d={`M${x} ${y + 1.6}V${y + 5.5}`} />
    </g>
  );
}

const glyphs: Record<string, ReactNode> = {
  // ---- forms ----
  beton: (
    <>
      <rect x="9" y="12" width="30" height="28" />
      <path d="M9 19.5H39M9 26H39M9 32.5H39" opacity="0.55" />
      <path d="M14 15.7h.1M24 15.7h.1M34 15.7h.1M14 22.7h.1M24 22.7h.1M34 22.7h.1M14 29.2h.1M24 29.2h.1M34 29.2h.1M14 36.2h.1M24 36.2h.1M34 36.2h.1" />
      <path d="M4 40H44" />
    </>
  ),
  mega: (
    <>
      <rect x="11" y="5" width="32" height="37" />
      <path d="M19 5V42M27 5V42M35 5V42M11 14H43M11 23H43" opacity="0.55" />
      <path d="M3 42H45" />
      {figure(6, 35.5)}
    </>
  ),
  monolith: (
    <>
      <path d="M19 40V9.5L29.5 8V40" />
      <path d="M4 40H44" />
      <path d="M29.5 40L38 40" opacity="0.55" />
    </>
  ),
  levitation: (
    <>
      <rect x="14" y="7" width="20" height="17" />
      <path d="M14 24L18 27H38L34 24M38 27V10L34 7" opacity="0.55" />
      <ellipse cx="25" cy="38" rx="12" ry="2.2" />
      <path d="M4 41H44" opacity="0.55" />
    </>
  ),
  shell: (
    <>
      <path d="M5 35C12 14 20 10 24 10C28 10 36 14 43 35" />
      <path d="M11 35C16 22 21 18 24 18C27 18 32 22 37 35" opacity="0.55" />
      <path d="M3 35H45" />
    </>
  ),
  glass: (
    <>
      <rect x="9" y="15" width="30" height="21" />
      <path d="M19 15V36M29 15V36" strokeDasharray="2 2.5" />
      <path d="M7 15H41" />
      <path d="M13 31L18 20M23 31L26 24" opacity="0.55" />
      <path d="M3 36H45" />
    </>
  ),
  vault: (
    <>
      <path d="M9 42V22A15 15 0 0 1 39 22V42" />
      <path d="M22.5 8L18.5 42M25.5 8L29.5 42" strokeDasharray="2 2.2" />
      <path d="M4 42H44" />
    </>
  ),
  courtyard: (
    <>
      <rect x="7" y="7" width="34" height="34" />
      <rect x="16" y="16" width="16" height="16" />
      <path d="M7 7L16 16M41 7L32 16M7 41L16 32M41 41L32 32" opacity="0.55" />
    </>
  ),
  labyrinth: (
    <>
      <path d="M4 42H12V36H19V30H26V24H33V18H40V12H44" />
      <path d="M8 26V19A3.5 3.5 0 0 1 15 19V26M24 14V8A3.5 3.5 0 0 1 31 8V14" />
      <path d="M8 26H15M24 14H31" opacity="0.55" />
    </>
  ),
  ruin: (
    <>
      <path d="M8 41V16L12 13L15 17L18 15V41" />
      <path d="M27 41V27L30 25L33 29L36 26V41" />
      <path d="M18 29H27" strokeDasharray="2 2" />
      <path d="M33 25C34 21 37 20 40 20C40 23 37 25 33 25Z" />
      <path d="M3 41H45" />
    </>
  ),
  frame: (
    <>
      <path d="M5 14H43M5 30H43M5 14V30M17.7 14V30M30.3 14V30M43 14V30" />
      <path d="M5 30L17.7 14L30.3 30L43 14" opacity="0.7" />
      <path d="M11 30V42M37 30V42M3 42H45" />
      <circle cx="17.7" cy="14" r="1.2" />
      <circle cx="30.3" cy="30" r="1.2" />
    </>
  ),
  hive: (
    <>
      {hex(24, 24, 6)}
      {hex(13.6, 18, 6)}
      {hex(34.4, 18, 6)}
      {hex(13.6, 30, 6)}
      {hex(34.4, 30, 6)}
      {hex(24, 12, 6)}
      {hex(24, 36, 6)}
    </>
  ),
  // ---- stances ----
  romanticism: (
    <>
      <circle cx="33" cy="12" r="4.5" />
      <path d="M3 37L15 21L22 28L31 17L45 37" />
      <path d="M6 31H18M27 33H42" strokeDasharray="3 2.5" opacity="0.7" />
      <path d="M3 41H45" />
      {figure(10, 34.5)}
    </>
  ),
  expressionism: (
    <>
      <path d="M5 40L13 11L20 33L29 7L35 30L43 17" />
      <path d="M3 44L45 36" />
      <path d="M8 22L15 26" opacity="0.6" />
    </>
  ),
  impressionism: (
    <>
      <circle cx="24" cy="16" r="6.5" strokeDasharray="2.2 2" />
      <path d="M4 28H44" />
      <path
        d="M9 32.5H17M21 32.5H27M31 32.5H39M12 37H18M23 37H34M16 41.5H21M26 41.5H31"
        opacity="0.7"
      />
    </>
  ),
  realism: (
    <>
      <path
        d="M8 13C10 10 14 10 16 12C18 9 23 9 25 12C28 11 31 12 31 15H8Z"
        opacity="0.6"
      />
      <rect x="9" y="21" width="15" height="17" />
      <rect x="13" y="25" width="7" height="6" />
      <path d="M3 38H45" />
      {figure(34, 31)}
    </>
  ),
  classicism: (
    <>
      <path d="M7 16L24 7L41 16Z" />
      <path d="M11 16V36M19 16V36M29 16V36M37 16V36" />
      <path d="M7 36H41M5 40H43" />
    </>
  ),
  minimalism: (
    <>
      <rect x="18" y="20" width="9" height="9" />
      <path d="M27 29L42 29" opacity="0.7" />
    </>
  ),
  abstraction: (
    <>
      <rect x="7" y="7" width="17" height="23" />
      <circle cx="31" cy="29" r="9.5" />
      <path d="M4 41H44M36 7V18" />
    </>
  ),
  constructivism: (
    <>
      <path d="M6 33L33 6L39 12L12 39Z" />
      <circle cx="34" cy="34" r="6" />
      <path d="M4 22H20" opacity="0.7" />
    </>
  ),
  futurism: (
    <>
      <path d="M5 14H27M9 21H33M5 28H29M11 35H37" />
      <path d="M31 9L41 21.5L31 34" />
      <path d="M36 9L46 21.5L36 34" opacity="0.5" />
    </>
  ),
  surrealism: (
    <>
      <rect x="17" y="7" width="14" height="21" />
      <path d="M17 21H31" />
      <ellipse cx="24" cy="39" rx="7" ry="1.6" />
      <path d="M3 34H45" opacity="0.55" />
    </>
  ),
  existentialism: (
    <>
      <rect x="4" y="5" width="40" height="31" />
      <path d="M4 36L1 44M44 36L47 44M1 44H47" opacity="0.55" />
      {figure(24, 37)}
    </>
  ),
  wabisabi: (
    <>
      <path d="M9 22.5C12 21.6 18 22.4 24 22C30 21.6 35 22.8 39 22.2" />
      <path d="M9 22.5C10 33 16 38 24 38C32 38 38 32 39 22.2" />
      <path d="M25 22.3L23 27L26 30L24.5 33" />
      <path d="M17 41H31" opacity="0.55" />
    </>
  ),
};

export function ConceptGlyph({ id }: { id: string }) {
  return (
    <svg
      className="concept-glyph"
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
