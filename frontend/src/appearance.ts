import { useSyncExternalStore } from "react";
import { installTokens, type Mode } from "./tokens";

// 0.8.3: 明 / 暗. The saved preference is "system" (follow the Windows light/
// dark app setting, also while the app is open), "light" or "dark". Clicking
// the mark in the top-left corner fixes the opposite of what is showing; the
// settings page can go back to following Windows. The backend saves the choice
// and recolours the native title bar.
export type AppearancePref = "system" | Mode;

type Backend = {
  Appearance?: () => Promise<string>;
  SetAppearance?: (v: string) => Promise<void>;
  SystemAppearance?: () => Promise<string>;
};
function backend(): Backend | undefined {
  return (window as unknown as { go?: { main?: { App?: Backend } } }).go?.main
    ?.App;
}
const isPref = (v: unknown): v is AppearancePref =>
  v === "system" || v === "light" || v === "dark";

// The Windows app mode comes from the backend, which reads the same setting
// as the title bar and sends "appearance:system" when it changes. The
// embedded WebView2 does not reliably report prefers-color-scheme (it said
// light on a dark Windows), so that is only the fallback for a plain browser.
const systemQuery = window.matchMedia("(prefers-color-scheme: dark)");
let systemMode: Mode = systemQuery.matches ? "dark" : "light";
const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");

export type AppearanceState = {
  pref: AppearancePref;
  mode: Mode;
};
// Before the backend answers (and in a plain browser) the app is 暗, as
// before 0.8.3.
let state: AppearanceState = { pref: "dark", mode: "dark" };
const listeners = new Set<() => void>();

function resolve(pref: AppearancePref): Mode {
  if (pref !== "system") return pref;
  return systemMode;
}

let settle = 0;
// Colours change together over --motion-layout: a class switches on a
// transition for every element just for the switch, so nothing else in the
// app animates its colours by accident.
function apply(mode: Mode, animate: boolean) {
  const root = document.documentElement;
  if (animate && !reducedMotion.matches && root.dataset.mode !== mode) {
    root.classList.add("theme-switching");
    window.clearTimeout(settle);
    settle = window.setTimeout(
      () => root.classList.remove("theme-switching"),
      400,
    );
  }
  installTokens(mode);
}

function update(pref: AppearancePref, animate: boolean) {
  const mode = resolve(pref);
  if (pref === state.pref && mode === state.mode) return;
  if (mode !== state.mode) apply(mode, animate);
  state = { pref, mode };
  listeners.forEach((l) => l());
}

function systemChanged(mode: Mode) {
  if (mode === systemMode) return;
  systemMode = mode;
  update(state.pref, true);
}

/**
 * The saved appearance from the backend, applied before the first render so
 * a light start never shows the dark wall. Without the desktop bindings the
 * app stays 暗.
 */
export async function loadAppearance(): Promise<void> {
  for (let i = 0; i < 150 && !backend(); i++)
    await new Promise((r) => setTimeout(r, 20));
  let pref: AppearancePref = "dark";
  const b = backend();
  try {
    const v = await b?.Appearance?.();
    if (isPref(v)) pref = v;
    const sys = await b?.SystemAppearance?.();
    if (sys === "light" || sys === "dark") systemMode = sys;
  } catch {}
  if (b && window.runtime)
    window.runtime.EventsOn("appearance:system", (v: string) => {
      if (v === "light" || v === "dark") systemChanged(v);
    });
  else
    systemQuery.addEventListener("change", () =>
      systemChanged(systemQuery.matches ? "dark" : "light"),
    );
  state = { pref, mode: resolve(pref) };
  installTokens(state.mode);
}

/** Saves the preference and switches the colours. */
export async function setAppearance(pref: AppearancePref): Promise<void> {
  const set = backend()?.SetAppearance;
  if (set) await set(pref);
  update(pref, true);
}

export function useAppearance(): AppearanceState {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => state,
  );
}
