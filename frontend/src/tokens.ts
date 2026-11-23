import { useMemo, useSyncExternalStore } from "react";
import { theme, type ThemeConfig } from "antd";

// 0.7.20: one dark "gallery wall" for the whole app. The interface steps back
// and the images are the only bright things on screen. No brand colour:
// ivory is both the text and the primary action; hue is kept for status.
// 0.8.3: this is 暗 (dark). 明 (light) below is the same wall by day.
export const colors = {
  // primary is ivory. Anything drawn on a primary fill uses `background`
  // or `surface` for its text, never white.
  primary: "#EFE8DA",
  primaryHover: "#FFFFFF",
  surface: "#1F1B16",
  background: "#16130F",
  // Dropdowns, popovers and modals.
  elevated: "#29241D",
  text: "#EFE8DA",
  success: "#86B98F",
  successBg: "#1D2A20",
  dangerBg: "#33201B",
  warningBg: "#33291A",
  danger: "#E2765F",
  muted: "#A69C8C",
  border: "#3A332B",
  soft: "#29241D",
  warning: "#D9A441",
  scrollbar: "#5A5045",
  // Concept stage: one step deeper than the wall so it still reads as a stage.
  ink: "#100E0B",
  inkRaised: "#1F1B16",
  inkSelected: "#2E2820",
  inkLine: "#3A332B",
  inkText: "#EFE8DA",
  inkMuted: "#A69C8C",
  inkAccent: "#EFE8DA",
  // The mark in the top-left corner: the lit part and the shadow of the moon.
  moonLit: "#EFE8DA",
  moonShade: "transparent",
  // Floating buttons over an image (works wall, previews).
  overlay: "rgb(22 19 15 / 82%)",
};
export type Palette = typeof colors;
// 0.8.3: 明, paper and ink. The mirror of the dark wall: ink is the text and
// the primary action, paper is drawn on it. Still no brand colour; status
// hues are darkened to keep 4.5:1 on the paper.
export const lightColors: Palette = {
  primary: "#1F1B16",
  primaryHover: "#000000",
  surface: "#FAF7F1",
  background: "#F2EDE3",
  elevated: "#FBF9F4",
  text: "#1F1B16",
  success: "#36703F",
  successBg: "#E2EDE0",
  dangerBg: "#F6E1DA",
  warningBg: "#F4E8CF",
  danger: "#A63F28",
  muted: "#6B6253",
  border: "#D8CFBF",
  soft: "#E7E0D3",
  warning: "#80590C",
  scrollbar: "#BFB4A3",
  ink: "#E9E3D7",
  inkRaised: "#F7F3EB",
  inkSelected: "#DDD5C6",
  inkLine: "#CFC5B4",
  inkText: "#1F1B16",
  inkMuted: "#6B6253",
  inkAccent: "#1F1B16",
  moonLit: "#FFFDF8",
  moonShade: "#1F1B16",
  overlay: "rgb(250 247 241 / 86%)",
};
export type Mode = "light" | "dark";
export const palettes: Record<Mode, Palette> = {
  dark: colors,
  light: lightColors,
};
export const font = '"Microsoft YaHei UI", "Segoe UI", sans-serif';
// Headings and the wordmark. The serif is not bundled yet; until it is, the
// stack falls through to the body font instead of SimSun, which is rough at
// display sizes.
export const serif = `"Noto Serif SC", "Source Han Serif SC", "思源宋体", ${font}`;
// 0.7.29: durations and one curve for every authored transition.
// instant = feedback on a control, base = a state change, layout = something
// moves or a view changes. The curve decelerates and never overshoots.
export const motion = {
  instant: "120ms",
  base: "200ms",
  layout: "360ms",
  // Only for a newly generated image coming up on the wall.
  arrive: "640ms",
  // Only for the moon in the top-left corner changing phase (明 / 暗).
  phase: "640ms",
  ease: "cubic-bezier(0.16, 1, 0.3, 1)",
};
export function installTokens(mode: Mode = "dark") {
  const root = document.documentElement.style;
  for (const [k, v] of Object.entries(palettes[mode]))
    root.setProperty(`--${k}`, v);
  root.setProperty("--serif", serif);
  root.setProperty("--motion-instant", motion.instant);
  root.setProperty("--motion-base", motion.base);
  root.setProperty("--motion-layout", motion.layout);
  root.setProperty("--motion-arrive", motion.arrive);
  root.setProperty("--motion-phase", motion.phase);
  root.setProperty("--ease-out", motion.ease);
  root.setProperty("color-scheme", mode);
  document.documentElement.dataset.mode = mode;
}

const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
function watchReducedMotion(changed: () => void) {
  reducedMotion.addEventListener("change", changed);
  return () => reducedMotion.removeEventListener("change", changed);
}
// With "reduce motion" on in Windows, Ant Design's own motion (drawers, modals,
// collapses) is switched off through its theme, so its components never wait
// for a transition that the stylesheet has removed.
export function useAntdTheme(mode: Mode = "dark"): ThemeConfig {
  const reduced = useSyncExternalStore(
    watchReducedMotion,
    () => reducedMotion.matches,
  );
  return useMemo(() => {
    const base = antdThemes[mode];
    return reduced
      ? { ...base, token: { ...base.token, motion: false } }
      : base;
  }, [mode, reduced]);
}

function makeAntdTheme(c: Palette, mode: Mode): ThemeConfig {
  return {
    algorithm: mode === "dark" ? theme.darkAlgorithm : theme.defaultAlgorithm,
    token: {
      colorPrimary: c.primary,
      colorPrimaryHover: c.primaryHover,
      colorPrimaryActive: c.primary,
      colorLink: c.primary,
      colorLinkHover: c.primaryHover,
      colorLinkActive: c.primary,
      colorSuccess: c.success,
      colorSuccessBg: c.successBg,
      colorSuccessBorder: c.border,
      colorError: c.danger,
      colorErrorBg: c.dangerBg,
      colorErrorBorder: c.border,
      colorWarning: c.warning,
      colorWarningBg: c.warningBg,
      colorWarningBorder: c.border,
      colorInfo: c.primary,
      colorInfoBg: c.soft,
      colorInfoBorder: c.border,
      colorPrimaryBg: c.soft,
      colorText: c.text,
      colorTextDescription: c.muted,
      colorTextSecondary: c.muted,
      colorTextDisabled: c.text,
      // antd's default placeholder (a quarter of the text colour) is too faint
      // on the wall.
      colorTextPlaceholder: c.muted,
      colorBgBase: c.background,
      colorBgLayout: c.background,
      colorBgContainer: c.surface,
      colorBgElevated: c.elevated,
      colorBorder: c.border,
      colorBorderSecondary: c.border,
      fontFamily: font,
      fontSize: 14,
      borderRadius: 8,
      controlHeight: 38,
    },
    components: {
      // Text on a primary fill is the wall colour: dark on ivory, paper on ink.
      Button: {
        primaryColor: c.background,
        primaryShadow: "none",
        defaultShadow: "none",
        dangerShadow: "none",
      },
      Checkbox: { colorWhite: c.background },
      Radio: { buttonSolidCheckedColor: c.background },
      // "On" is green so the white handle keeps its contrast.
      Switch: { colorPrimary: "#4E8A5B", colorPrimaryHover: "#5A9A68" },
      // Tooltips stay dark in both modes.
      Tooltip: { colorBgSpotlight: mode === "dark" ? "#3A332B" : "#2E2820" },
    },
  };
}

const antdThemes: Record<Mode, ThemeConfig> = {
  dark: makeAntdTheme(colors, "dark"),
  light: makeAntdTheme(lightColors, "light"),
};
// The admin tool stays on 暗.
export const antdTheme = antdThemes.dark;
