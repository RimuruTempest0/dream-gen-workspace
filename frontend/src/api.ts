import type { LLMCapabilities } from "./agent-api";
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
};
export type Task = {
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
  if (!fn) throw new Error("桌面服务尚未连接，请使用完整便携包启动应用。");
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
export const stateNames: Record<string, string> = {
  queued: "等待执行",
  submitting: "等待中转站",
  running: "生成中",
  downloading: "保存图片",
  succeeded: "已完成",
  failed: "生成失败",
  unknown: "结果未知",
  download_failed: "下载失败",
  canceled: "已取消",
  blocked: "已暂停",
};
export function errorText(e: unknown) {
  return e instanceof Error ? e.message : String(e);
}
