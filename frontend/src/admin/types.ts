export type Upstream = {
  id: string;
  name: string;
  kind: "image" | "llm";
  baseURL: string;
  model: string;
  mode?: string;
  authMode?: string;
  maxOutputTokens?: number;
  /** Image upstreams: "" (OpenAI Images) or "async-jobs" (needs app 0.7.14+). */
  protocol?: string;
  perKeyQuota: boolean;
  consoleURL?: string;
  usageURL?: string;
};
export type PoolKey = {
  id: string;
  tail: string;
  upstreamID: string;
  kind: "image" | "llm";
  use: "primary" | "backup";
  quotaNote?: string;
  status: "available" | "assigned" | "disabled" | "revoked";
  customerID?: string;
  slot?: string;
  delivered?: boolean;
  deliveredBefore?: boolean;
  verified?: boolean;
  verifiedAt?: string;
  importedAt: string;
  disabledFor?: string;
  /** When it was marked as cancelled at the upstream (status revoked). */
  revokedAt?: string;
};
export type Delivery = {
  version: string;
  at: string;
  zip: string;
  sha256: string;
  vaultRevision: number;
};
export type AdminEvent = { at: string; kind: string; text: string };
export type Customer = {
  id: string;
  name: string;
  /** "creative": a hosted user of the mass-market line. None marks a record of
   * the e-commerce line, retired in admin 1.12.0. */
  line?: string;
  softwareID: string;
  createdAt: string;
  trialImages: number;
  planNote?: string;
  quotaNote?: string;
  /** When the licence ends (RFC 3339, UTC); none = never. */
  expiresAt?: string;
  /** Built-in skills the package comes with; none = all. */
  skills?: string[];
  slots: Record<string, string>;
  imageBackups: string[];
  vaultRevision: number;
  changed: boolean;
  paid: boolean;
  paidAt?: string;
  deliveries: Delivery[];
  events: AdminEvent[];
};
export type CreativeRelease = {
  version: string;
  at: string;
  fullZip: string;
  uploadDir: string;
  updateZip: string;
  manifest: string;
  notes: string;
  notesEn?: string;
  /** When the tool put this version on the creative release (admin 1.11.0). */
  uploadedAt?: string;
};
export type UploadPlan = {
  version: string;
  files: { name: string; path: string; size: number }[];
  page: string;
  tag: string;
  /** The version update.json on the release announces now; "" = none. */
  published: string;
};
export type HostedSource = { version: string; at: string; blocked: string };
export type LineSource = {
  line: string;
  branch: string;
  version: string;
  commit: string;
  subject: string;
  at: string;
  uncommitted: number;
  blocked: string;
  releasePage?: string;
  releaseURL?: string;
};
export type AdminState = {
  signingPublicKey: string;
  repoRoot: string;
  creative: CreativeRelease[];
  builtinSkills: { id: string; name: string; description: string }[];
  dataDir: string;
  upstreams: Upstream[];
  keys: PoolKey[];
  customers: Customer[];
};

export const allSlots = [
  "image-1",
  "image-2",
  "image-3",
  "image-4",
  "image-5",
  "image-6",
  "llm-1",
  "llm-2",
];
// Lines that need a key before a package can be built, plus at least one of the
// backup image lines; the rest are optional.
export const requiredSlots = ["image-1", "llm-1"];
export const backupImageSlots = [
  "image-2",
  "image-3",
  "image-4",
  "image-5",
  "image-6",
];
export function slotNeeds(slot: string): [PoolKey["kind"], PoolKey["use"]] {
  if (slot === "llm-1") return ["llm", "primary"];
  if (slot === "llm-2") return ["llm", "backup"];
  if (["image-1", "image-2", "image-3"].includes(slot))
    return ["image", "primary"];
  return ["image", "backup"];
}
export function slotName(slot: string) {
  if (slot === "llm-1") return "LLM 线路 1";
  if (slot === "llm-2") return "LLM 线路 2";
  return "图片线路 " + slot.replace("image-", "");
}
export const kindLabel = { image: "图片", llm: "LLM" };
export const useLabel = { primary: "主用", backup: "备用" };
export const statusLabel = {
  available: "可用",
  assigned: "已分配",
  disabled: "已停用",
  revoked: "已注销",
};
export type ImportResult = {
  added: number;
  duplicates: number;
  invalid: number;
};

// Shown wherever a key sits on an upstream that cannot cap a single key: every
// key ships in the package before payment, so only its quota guards it.
export function quotaWarning(u?: Upstream) {
  return `“${u?.name || "此上游"}”不支持给单个 Key 设额度：Key 随软件包交付，收款前被取出也可能用光整个账户的余额。可二选一：每位顾客单独开一个只充小额的账户、收款后再充值，或把这条线路放到支持单 Key 额度的中转（如 Sub2API）后面。`;
}
