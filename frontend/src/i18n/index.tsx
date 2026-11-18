// Interface language (界面语言): text tables, the current language, switching,
// and date/number formatting. See messages/index.ts for adding text.
//
//   t("shell.nav.create")                       → "创作" / "Create" / …
//   t("shell.rail.version", { version: "0.8.0" }) → fills {version}
//   formatNumber(1234), formatDateTime(iso), formatTime(iso), formatDate(iso)
//   const { lang, setLanguage } = useI18n();
//
// Switching re-mounts the page tree (see LanguageProvider), so t() and the
// formatters can be called anywhere during render, including plain helper
// functions. Do not call them at module level: a constant computed on import
// keeps the language of the first start. Keep keys there and call t() later.
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { areas, type MessageKey } from "./messages";
import {
  defaultLang,
  isLang,
  languageInfo,
  languages,
  type Lang,
  type LanguageInfo,
} from "./languages";

export type { Lang, LanguageInfo, MessageKey };
export { languages, languageInfo, isLang };

type Table = Record<string, string>;
const tables = {} as Record<Lang, Table>;
for (const l of languages) tables[l.code] = {};
for (const area of areas)
  for (const l of languages)
    Object.assign(tables[l.code], (area as Record<Lang, Table>)[l.code]);

let current: Lang = defaultLang;

/** The current interface language. */
export function currentLanguage(): Lang {
  return current;
}

export type Params = Record<string, string | number>;

/**
 * The text for key in the current language, with {name} placeholders filled
 * from params. Numbers are formatted for the language (thousands separators).
 * A key missing in a table falls back to Chinese, then to the key itself.
 */
export function t(key: MessageKey, params?: Params): string {
  const text = tables[current][key] ?? tables[defaultLang][key] ?? key;
  if (!params) return text;
  return text.replace(/\{(\w+)\}/g, (m, name: string) => {
    const v = params[name];
    if (v === undefined) return m;
    return typeof v === "number" ? formatNumber(v) : v;
  });
}

/** The program name: 生图工作台 in Chinese, Image Workbench otherwise. */
export function appName(): string {
  return t("shell.appName");
}

function info() {
  return languageInfo(current);
}
export function formatNumber(n: number, options?: Intl.NumberFormatOptions) {
  return new Intl.NumberFormat(info().intl, options).format(n);
}
function dateFormat(
  value: string | number | Date,
  o: Intl.DateTimeFormatOptions,
) {
  const d = value instanceof Date ? value : new Date(value);
  return new Intl.DateTimeFormat(info().intl, {
    timeZone: info().timeZone,
    ...o,
  }).format(d);
}
/** Date with the year (2026/11/19, Nov 19, 2026, …). */
export function formatDate(value: string | number | Date) {
  return dateFormat(value, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  });
}
/** Date and time with the year, for dates that matter beyond this year. */
export function formatDateTime(value: string | number | Date) {
  return dateFormat(value, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}
/** Month, day and time, for recent events. */
export function formatTime(value: string | number | Date) {
  return dateFormat(value, {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}
/** Time of day only. */
export function formatClock(value: string | number | Date, seconds = false) {
  return dateFormat(value, {
    hour: "2-digit",
    minute: "2-digit",
    ...(seconds ? { second: "2-digit" } : {}),
  });
}

// Page language tag, fonts and window title follow the language.
function apply(lang: Lang) {
  current = lang;
  const l = languageInfo(lang);
  const root = document.documentElement;
  root.lang = lang;
  root.style.setProperty("--font", l.font);
  root.style.setProperty("--serif", l.serif);
}

type Ctx = {
  lang: Lang;
  info: LanguageInfo;
  setLanguage: (lang: Lang) => Promise<void>;
};
const LanguageContext = createContext<Ctx | null>(null);

/** The current language and the way to switch it. */
export function useI18n() {
  const ctx = useContext(LanguageContext);
  if (!ctx) throw new Error("useI18n outside LanguageProvider");
  return { ...ctx, t, languages };
}

type Backend = Record<string, (...a: unknown[]) => Promise<unknown>>;
function backend(): Backend | undefined {
  return window.go?.main?.App;
}

/**
 * The saved language from the backend. The desktop bindings appear a moment
 * after the page loads; without them (a plain browser) the page is Chinese.
 */
export async function loadLanguage(): Promise<Lang> {
  for (let i = 0; i < 150 && !backend(); i++)
    await new Promise((r) => setTimeout(r, 20));
  try {
    const v = await backend()?.InterfaceLanguage?.();
    if (isLang(v)) return v;
  } catch {}
  return defaultLang;
}

/**
 * Holds the interface language. children is a function of the language so the
 * caller can key the page tree on it: everything re-renders in the new
 * language at once, without a restart.
 */
export function LanguageProvider({
  initial,
  children,
}: {
  initial: Lang;
  children: (lang: Lang, info: LanguageInfo) => ReactNode;
}) {
  const [lang, setLang] = useState<Lang>(() => {
    apply(initial);
    return initial;
  });
  useEffect(() => apply(lang), [lang]);
  const setLanguage = useCallback(async (next: Lang) => {
    const set = backend()?.SetInterfaceLanguage;
    if (set) await set(next);
    apply(next);
    setLang(next);
  }, []);
  const value = useMemo(
    () => ({ lang, info: languageInfo(lang), setLanguage }),
    [lang, setLanguage],
  );
  return (
    <LanguageContext.Provider value={value}>
      {children(lang, value.info)}
    </LanguageContext.Provider>
  );
}
