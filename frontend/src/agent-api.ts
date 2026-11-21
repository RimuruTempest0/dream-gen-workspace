import type { Connection, Task } from "./api";
export type LLMCapabilities = {
  model: string;
  text: boolean;
  vision: boolean;
  tools: boolean;
  streaming: boolean;
  checkedAt: string;
  details: string[];
};
export type AgentInput = {
  productURL: string;
  instruction: string;
  productText: string;
  references: string[];
  projectID: string;
  targetCount: number;
  ratio: string;
  size: string;
  quality: string;
  llmConnectionID: string;
  imageConnectionID: string;
  imageModel: string;
  skillIDs: string[];
  // Automatic mode never asks: the Agent decides missing facts itself.
  autoMode?: boolean;
  /** Uses the skills filed under this category, plus the uncategorised ones. */
  category?: string;
};
export type ProductInfo = {
  sourceURL: string;
  title: string;
  description: string;
  facts: string;
  // error: the page could not be read; imageNote: page read, some images not saved.
  error: string;
  imageNote?: string;
  referenceIDs: string[];
};
export type AgentQuestion = {
  id: string;
  label: string;
  required: boolean;
  answer: string;
  // Set when an automatic run answered the question itself.
  answeredBy?: "agent";
  basis?: "instruction" | "source" | "choice" | "habit" | "verify";
  reason?: string;
};
export type SuiteItem = {
  id: string;
  purpose: string;
  scene: string;
  prompt: string;
  references: string[];
  size: string;
  quality: string;
};
export type SuitePlan = {
  revision: number;
  createdAt: string;
  locked: string[];
  editable: string[];
  notes: string;
  items: SuiteItem[];
};
export type SkillRecord = {
  id: string;
  name: string;
  description: string;
  content: string;
  version: number;
  enabled: boolean;
  source: string;
  attachments: { name: string; path: string }[];
  /** Set by ImportSkill when a skill of that name exists (nothing imported). */
  conflict?: boolean;
  sealed?: boolean;
  /** Product category; empty = used for every category. */
  category?: string;
};
export type AgentRun = {
  autoGeneration?: boolean;
  authorizedCount?: number;
  imageConnection?: Connection;
  analysisSucceeded?: boolean;
  analysisStartedAt?: string;
  id: string;
  createdAt: string;
  updatedAt: string;
  status: string;
  stage: string;
  /** Code of a stage the page reacts to, whatever the stage text's language. */
  stageCode?: string;
  error: string;
  llmReconnects?: number;
  llmReconnectError?: string;
  // Time spent waiting for the user's answers; the timer leaves it out.
  waitedMS?: number;
  progress?: RunProgress;
  activity?: ActivityEntry[];
  stopReason?: string;
  input: AgentInput;
  llmConnection: Connection;
  skills: SkillRecord[];
  product: ProductInfo;
  questions: AgentQuestion[];
  plans: SuitePlan[];
  events: { at: string; text: string; code?: string }[];
  batchID: string;
  submittedRevision: number;
};
export type AgentPage = { items: AgentRun[]; total: number };

// A run or task stored by an older version may lack lists; the pages read their
// length, so a missing list is an empty one.
export function normalizeAgentRun(r: AgentRun): AgentRun {
  return {
    ...r,
    input: {
      ...r.input,
      references: r.input?.references || [],
      skillIDs: r.input?.skillIDs || [],
    },
    skills: r.skills || [],
    product: { ...r.product, referenceIDs: r.product?.referenceIDs || [] },
    questions: r.questions || [],
    plans: (r.plans || []).map((p) => ({
      ...p,
      locked: p.locked || [],
      editable: p.editable || [],
      items: (p.items || []).map((i) => ({
        ...i,
        references: i.references || [],
      })),
    })),
    events: r.events || [],
  };
}
export function normalizeTask(t: Task): Task {
  return {
    ...t,
    references: t.references || [],
    assetIDs: t.assetIDs || [],
  };
}
export type RunProgress = {
  step: RunStep;
  stepStartedAt: string;
  visited: RunStep[];
  productVia?: "direct" | "browser";
  productBrowserMS?: number;
  imagesFound?: number;
  imagesSaved?: number;
  referencesSeen?: number;
  planWritten?: number;
  agentVersion?: string;
  llmRequests?: number;
  contextTokens?: number;
  outputTokens?: number;
  lineNote?: string;
  replyThinking?: boolean;
  replyActive?: boolean;
  replyChars?: number;
  replyLastAt?: string;
};
export type ActivityEntry = {
  at: string;
  kind: "agent" | "tool" | "llm" | "warn";
  text: string;
};
export type RunStep =
  | "prepare"
  | "product"
  | "images"
  | "references"
  | "plan"
  | "submit";
export type SmartProgress = {
  step?: RunStep;
  runID: string;
  state: "" | "analyzing" | "needs_input" | "generating" | "paused";
  done: number;
  total: number;
};
export type AgentRuntimeInfo = {
  available: boolean;
  version: string;
  message: string;
};
export type AgentTasks = Task[];
export const blankAgentDraft: AgentInput = {
  productURL: "",
  instruction: "",
  productText: "",
  references: [],
  projectID: "",
  targetCount: 12,
  ratio: "1:1",
  size: "1024x1024",
  quality: "auto",
  llmConnectionID: "",
  imageConnectionID: "",
  imageModel: "",
  skillIDs: ["product-analysis", "etsy-suite", "embroidery-rules"],
};
