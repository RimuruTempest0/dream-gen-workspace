import { useMemo, useSyncExternalStore } from "react";
import { theme, type ThemeConfig } from "antd";

// 0.7.20: one dark "gallery wall" for the whole app. The interface steps back
// and the images are the only bright things on screen. No brand colour:
// ivory is both the text and the primary action; hue is kept for status.
export const colors = {
  // primary is ivory. Anything drawn on a primary fill uses `background`
  // or `surface` for its text, never white.
  primary: "#EFE8DA",
  surface: "#1F1B16",
  background: "#16130F",
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
  ease: "cubic-bezier(0.16, 1, 0.3, 1)",
};
export function installTokens() {
  const root = document.documentElement.style;
  for (const [k, v] of Object.entries(colors)) root.setProperty(`--${k}`, v);
  root.setProperty("--serif", serif);
  root.setProperty("--motion-instant", motion.instant);
  root.setProperty("--motion-base", motion.base);
  root.setProperty("--motion-layout", motion.layout);
  root.setProperty("--motion-arrive", motion.arrive);
  root.setProperty("--ease-out", motion.ease);
  root.setProperty("color-scheme", "dark");
}

const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
function watchReducedMotion(changed: () => void) {
  reducedMotion.addEventListener("change", changed);
  return () => reducedMotion.removeEventListener("change", changed);
}
// With "reduce motion" on in Windows, Ant Design's own motion (drawers, modals,
// collapses) is switched off through its theme, so its components never wait
// for a transition that the stylesheet has removed.
export function useAntdTheme(): ThemeConfig {
  const reduced = useSyncExternalStore(
    watchReducedMotion,
    () => reducedMotion.matches,
  );
  return useMemo(
    () =>
      reduced
        ? { ...antdTheme, token: { ...antdTheme.token, motion: false } }
        : antdTheme,
    [reduced],
  );
}

// Shared by the app and the admin tool so both stay on the same theme.
export const antdTheme: ThemeConfig = {
  algorithm: theme.darkAlgorithm,
  token: {
    colorPrimary: colors.primary,
    colorPrimaryHover: "#FFFFFF",
    colorPrimaryActive: colors.primary,
    colorLink: colors.primary,
    colorLinkHover: "#FFFFFF",
    colorLinkActive: colors.primary,
    colorSuccess: colors.success,
    colorSuccessBg: colors.successBg,
    colorSuccessBorder: colors.border,
    colorError: colors.danger,
    colorErrorBg: colors.dangerBg,
    colorErrorBorder: colors.border,
    colorWarning: colors.warning,
    colorWarningBg: colors.warningBg,
    colorWarningBorder: colors.border,
    colorInfo: colors.primary,
    colorInfoBg: colors.soft,
    colorInfoBorder: colors.border,
    colorPrimaryBg: colors.soft,
    colorText: colors.text,
    colorTextDescription: colors.muted,
    colorTextSecondary: colors.muted,
    colorTextDisabled: colors.text,
    colorBgBase: colors.background,
    colorBgLayout: colors.background,
    colorBgContainer: colors.surface,
    colorBgElevated: colors.soft,
    colorBorder: colors.border,
    colorBorderSecondary: colors.border,
    fontFamily: font,
    fontSize: 14,
    borderRadius: 8,
    controlHeight: 38,
  },
  components: {
    // Text on an ivory fill is dark.
    Button: {
      primaryColor: colors.background,
      primaryShadow: "none",
      defaultShadow: "none",
      dangerShadow: "none",
    },
    Checkbox: { colorWhite: colors.background },
    Radio: { buttonSolidCheckedColor: colors.background },
    // "On" is green so the white handle keeps its contrast.
    Switch: { colorPrimary: "#4E8A5B", colorPrimaryHover: "#5A9A68" },
    Tooltip: { colorBgSpotlight: "#3A332B" },
  },
};
