import type { LLMCapabilities } from "./agent-api";
import { t, type MessageKey } from "./i18n";
export type Connection = {
  // Set on lines provided by the customer licence; they cannot be edited.
  managed?: string;
  kind: "image" | "llm";
  maxOutputTokens?: number;
  mode?: "claude" | "compatible";
  authMode?: "api-key" | "bearer";
  capabilities?: LLMCapabilities;
  id: string;
  name: string;
  baseURL: string;
  model: string;
  protocol: string;
  hasKey: boolean;
};
export type ConnectionInput = {
  kind?: "image" | "llm";
  maxOutputTokens?: number;
  mode?: "claude" | "compatible";
  authMode?: "api-key" | "bearer";
  id: string;
  name: string;
  baseURL: string;
  model: string;
  key: string;
  clearKey?: boolean;
  protocol?: "openai-images" | "async-jobs";
};
export type GenerateInput = {
  requestID: string;
  connectionID: string;
  model: string;
  prompts: string[];
  count: number;
  references: string[];
  size: string;
  quality: string;
  projectID: string;
  // Concept stage label of each prompt (same index as prompts), 0.7.18.
  conceptLabels?: string[];
  // Mood guide (0.7.26): the mood, a concept picked with it, and 对比出图.
  mood?: MoodChoice;
  concept?: ConceptChoice;
  compare?: boolean;
};
export type MoodChoice = {
  weather: string;
  time: string;
  distance: string;
  expectation: string;
};
export type ConceptChoice = { form: string; stance: string; strength: number };
export type MoodOption = {
  id: string;
  name: string;
  short: string;
  phrase: string;
};
export type MoodLibrary = {
  weather: MoodOption[];
  time: MoodOption[];
  distance: MoodOption[];
  expectation: MoodOption[];
  renderings: MoodOption[];
  nudges: MoodOption[];
  // The confirmation sentence in the interface language, with {time}
  // {weather} {distance} {expectation} for the options' phrases.
  sentence: string;
};
export type NudgeInput = {
  requestID: string;
  taskID: string;
  assetID: string;
  direction: string;
};
export type Task = {
  concept?: string;
  // Mood guide (0.7.26): label, choice, rendering (realistic/painterly), the
  // user's own prompt before the rewrite, and the 再靠近一点 source and direction.
  mood?: string;
  moodChoice?: MoodChoice;
  conceptChoice?: ConceptChoice;
  rendering?: string;
  basePrompt?: string;
  nudgeOf?: string;
  nudge?: string;
  rewordFor?: string;
  agentRunID?: string;
  planItemID?: string;
  refinedFrom?: string;
  refineInstruction?: string;
  previousAssetIDs?: string[];
  // Detail sample: the sample image itself, and images drawn from it.
  sample?: boolean;
  sampleItem?: string;
  samplePending?: boolean;
  sampleAssetID?: string;
  // Since 0.7.9 one sample per design area: a sample's area, the samples an image
  // waits for and the sample images it was given.
  sampleArea?: string;
  sampleItems?: string[];
  sampleAssetIDs?: string[];
  // The LLM's check of the detail sample: checking, passed, redrawn, failed, skipped.
  sampleCheck?: string;
  sampleProblems?: string;
  sampleRedraws?: number;
  id: string;
  batchID: string;
  createdAt: string;
  updatedAt: string;
  status: string;
  error: string;
  prompt: string;
  model: string;
  size: string;
  quality: string;
  references: string[];
  projectID: string;
  connection: Connection;
  assetIDs: string[];
  routedFrom?: string;
  routedFromName?: string;
  routeReason?: string;
  autoRetryOf?: string;
  autoRetriedAs?: string;
  autoDownloads?: number;
  downloadRetryAt?: string;
  policyRetryOf?: string;
  policyRetriedAs?: string;
  policyStep?: number;
  rewording?: boolean;
  diagnostic?: {
    stage: string;
    startedAt: string;
    requestURL: string;
    httpStatus: number;
    contentType: string;
    contentLength?: number;
    transferEncoding?: string;
    httpProtocol?: string;
    networkRoute?: string;
    upstreamRequestID: string;
    elapsedMS: number;
    bytesReceived: number;
    networkError: string;
    downloadStartedAt?: string;
    downloadMS?: number;
  };
};
export type RecentBatch = {
  id: string;
  createdAt: string;
  total: number;
  completed: number;
  active: number;
  imageCount: number;
  tasks: Task[];
};
export type Asset = {
  id: string;
  name: string;
  path: string;
  thumbnail: string;
  kind: string;
  taskID: string;
  projectID: string;
  width: number;
  height: number;
  createdAt: string;
};
export type Project = { id: string; name: string; createdAt: string };
export type Template = {
  id: string;
  name: string;
  prompt: string;
  size: string;
  quality: string;
};
// Concept stage (0.7.18): a form and one to three stances rewrite the prompt.
export type Concept = {
  id: string;
  name: string;
  en: string;
  group: string;
  look: string;
};
export type ConceptPick = { name: string; form: string; stance: string };
export type ConceptLibrary = {
  forms: Concept[];
  stances: Concept[];
  picks: ConceptPick[];
};
export type ConceptInput = {
  prompt: string;
  form: string;
  stances: string[];
  // 0 accent, 1 balanced, 2 dominant.
  strength: number;
  hasReferences: boolean;
};
export type ConceptResult = {
  prompts: string[];
  labels: string[];
  usedLLM: boolean;
};
export type Page<T> = { items: T[]; total: number };
export type Summary = {
  tasks: number;
  assets: number;
  active: number;
  dataDir: string;
  version: string;
};
export type TestResult = {
  auth: string;
  models: string;
  generation: string;
  ids: string[];
};
declare global {
  interface Window {
    go?: {
      main?: { App?: Record<string, (...args: unknown[]) => Promise<unknown>> };
    };
    runtime?: {
      EventsOn: (name: string, cb: (...args: any[]) => void) => () => void;
    };
  }
}
export async function call<T = void>(
  name: string,
  ...args: unknown[]
): Promise<T> {
  const fn = window.go?.main?.App?.[name];
  if (!fn) throw new Error(t("ui.desktopMissing"));
  return (await fn(...args)) as T;
}
export const blankDraft: GenerateInput = {
  requestID: "",
  connectionID: "",
  model: "",
  prompts: [],
  count: 1,
  references: [],
  size: "auto",
  quality: "auto",
  projectID: "",
};
// Task status names. Each is a getter, so the text follows the interface
// language when a page reads it (t() must not run on import).
const stateKeys: Record<string, MessageKey> = {
  queued: "ui.state.queued",
  submitting: "ui.state.submitting",
  running: "ui.state.running",
  downloading: "ui.state.downloading",
  succeeded: "ui.state.succeeded",
  failed: "ui.state.failed",
  unknown: "ui.state.unknown",
  download_failed: "ui.state.download_failed",
  canceled: "ui.state.canceled",
  blocked: "ui.state.blocked",
};
export const stateNames: Record<string, string> = Object.defineProperties(
  {},
  Object.fromEntries(
    Object.entries(stateKeys).map(([state, key]) => [
      state,
      { enumerable: true, get: () => t(key) },
    ]),
  ),
);
export function errorText(e: unknown) {
  return e instanceof Error ? e.message : String(e);
}
