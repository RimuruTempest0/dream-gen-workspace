// Text-table registry. Each area (page or part of the program) has one JSON
// file here holding the five language tables side by side:
//   { "zh-CN": { "<area>.<key>": "…" }, "en": {…}, "vi": {…}, "ja": {…}, "ko": {…} }
// Keys start with the file name. To add an area: create <area>.json, then add
// one import and one entry below (keep one per line; on a merge conflict keep
// both lines). internal/i18n/messages_test.go checks every table.
import shell from "./shell.json";
import run from "./run.json";
import settings from "./settings.json";
import gateways from "./gateways.json";
import agentSettings from "./agentSettings.json";
import license from "./license.json";
import updates from "./updates.json";
import monitor from "./monitor.json";
import logs from "./logs.json";
import templates from "./templates.json";
import shopRules from "./shopRules.json";

// prettier-ignore
export const areas = [
  shell,
  run,
  settings,
  gateways,
  agentSettings,
  license,
  updates,
  monitor,
  logs,
  templates,
  shopRules,
] as const;

type Area = (typeof areas)[number];
type KeysOf<A> = A extends { "zh-CN": infer Z } ? keyof Z & string : never;
// Every text key; t() only accepts these, so a typo fails the typecheck.
export type MessageKey = KeysOf<Area>;
