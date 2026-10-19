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
};
export type ProductInfo = {
  sourceURL: string;
  title: string;
  description: string;
  facts: string;
  error: string;
  referenceIDs: string[];
};
export type AgentQuestion = {
  id: string;
  label: string;
  required: boolean;
  answer: string;
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
  error: string;
  input: AgentInput;
  llmConnection: Connection;
  skills: SkillRecord[];
  product: ProductInfo;
  questions: AgentQuestion[];
  plans: SuitePlan[];
  events: { at: string; text: string }[];
  batchID: string;
  submittedRevision: number;
};
export type AgentPage = { items: AgentRun[]; total: number };
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
export const agentStateNames: Record<string, string> = {
  analyzing: "分析中",
  needs_input: "待补充资料",
  review: "待确认方案",
  submitted: "已提交生图",
  interrupted: "已中断",
  failed: "分析失败",
};
