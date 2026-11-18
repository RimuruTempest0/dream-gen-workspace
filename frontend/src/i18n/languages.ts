import type { Locale } from "antd/es/locale";
import zhCN from "antd/locale/zh_CN";
import enUS from "antd/locale/en_US";
import viVN from "antd/locale/vi_VN";
import jaJP from "antd/locale/ja_JP";
import koKR from "antd/locale/ko_KR";

// The five 界面语言. zh-CN is the default and the fallback for missing text.
export type Lang = "zh-CN" | "en" | "vi" | "ja" | "ko";
export const defaultLang: Lang = "zh-CN";

export type LanguageInfo = {
  code: Lang;
  // Shown in the language switch in its own script, never translated.
  native: string;
  // Locale for Intl date and number formatting.
  intl: string;
  // Ant Design's built-in text (pagination, empty states, pickers, modals).
  antd: Locale;
  // Windows fonts that cover the script in full (Vietnamese tone marks
  // included); the serif stack is for headings and the wordmark.
  font: string;
  serif: string;
  // Dates and times keep the zone they always used in Chinese; the other
  // languages use the computer's own zone.
  timeZone?: string;
};

const latin = '"Segoe UI", "Microsoft YaHei UI", sans-serif';
export const languages: LanguageInfo[] = [
  {
    code: "zh-CN",
    native: "中文",
    intl: "zh-CN",
    antd: zhCN,
    font: '"Microsoft YaHei UI", "Segoe UI", sans-serif',
    serif:
      '"Noto Serif SC", "Source Han Serif SC", "思源宋体", "Microsoft YaHei UI", "Segoe UI", sans-serif',
    timeZone: "Asia/Shanghai",
  },
  {
    code: "en",
    native: "English",
    intl: "en-US",
    antd: enUS,
    font: latin,
    serif: `"Noto Serif", Georgia, ${latin}`,
  },
  {
    code: "vi",
    native: "Tiếng Việt",
    intl: "vi-VN",
    antd: viVN,
    // Segoe UI carries the stacked Vietnamese marks; YaHei's Latin does not.
    font: latin,
    serif: `"Noto Serif", ${latin}`,
  },
  {
    code: "ja",
    native: "日本語",
    intl: "ja-JP",
    antd: jaJP,
    // Japanese glyph forms, not the Chinese ones YaHei would draw.
    font: '"Yu Gothic UI", "Meiryo UI", Meiryo, "Segoe UI", sans-serif',
    serif:
      '"Noto Serif JP", "Yu Mincho", "Yu Gothic UI", Meiryo, "Segoe UI", sans-serif',
  },
  {
    code: "ko",
    native: "한국어",
    intl: "ko-KR",
    antd: koKR,
    font: '"Malgun Gothic", "Segoe UI", sans-serif',
    serif: '"Noto Serif KR", "Malgun Gothic", "Segoe UI", sans-serif',
  },
];

export function isLang(v: unknown): v is Lang {
  return languages.some((l) => l.code === v);
}
export function languageInfo(code: Lang): LanguageInfo {
  return languages.find((l) => l.code === code) ?? languages[0];
}
