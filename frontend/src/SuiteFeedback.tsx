import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  App as AntApp,
  Button,
  Checkbox,
  Input,
  Modal,
  Radio,
  Space,
} from "antd";
import { MessageOutlined } from "@ant-design/icons";
import { call, errorText } from "./api";
import { formatNumber, t, type MessageKey } from "./i18n";
import { time, useActions } from "./ui";
import { SuiteFeedbackImages, type FeedbackItem } from "./SuiteFeedbackImages";

// 问题反馈 on one 套图 (a smart run): the tester's verdict on the whole set, kept
// with it, and the 反馈包 saved on this computer for the provider.

export type SuiteFeedback = {
  runID: string;
  verdict: "" | Verdict;
  problems: string[];
  note: string;
  items?: FeedbackItem[];
  updatedAt: string;
};
type FeedbackPackagePreview = {
  files: { name: string; bytes: number }[];
  bytes: number;
};

type Verdict = "usable" | "partly-usable" | "unusable";
const verdicts: { value: Verdict; label: MessageKey }[] = [
  { value: "usable", label: "feedback.verdict.usable" },
  { value: "partly-usable", label: "feedback.verdict.partly" },
  { value: "unusable", label: "feedback.verdict.unusable" },
];
const problems: { value: string; label: MessageKey }[] = [
  { value: "missing-shot", label: "feedback.problem.missingShot" },
  { value: "too-similar", label: "feedback.problem.tooSimilar" },
  { value: "design-inconsistent", label: "feedback.problem.design" },
  { value: "product-changed", label: "feedback.problem.product" },
  { value: "edit-missed", label: "feedback.problem.editMissed" },
  { value: "interrupted", label: "feedback.problem.interrupted" },
  { value: "other", label: "feedback.problem.other" },
];
// What each entry of the package holds, as listed before saving.
const entryNotes: Record<string, MessageKey> = {
  "反馈.md": "feedback.preview.md",
  "run.json": "feedback.preview.run",
};

const size = (n: number) =>
  n < 1048576
    ? `${formatNumber(Math.max(n, 1) / 1024, { maximumFractionDigits: 1 })} KB`
    : `${formatNumber(n / 1048576, { minimumFractionDigits: 1, maximumFractionDigits: 1 })} MB`;

/** The 问题反馈 button of a suite and its panel. No prompt opens it by itself. */
export function SuiteFeedbackButton({ runID }: { runID: string }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button
        icon={<MessageOutlined aria-hidden="true" />}
        onClick={() => setOpen(true)}
      >
        {t("feedback.open")}
      </Button>
      <SuiteFeedbackDialog
        runID={runID}
        open={open}
        onClose={() => setOpen(false)}
      />
    </>
  );
}

function missing(v: SuiteFeedback): MessageKey | "" {
  if (!v.verdict) return "feedback.error.verdict";
  if (v.verdict !== "usable" && v.problems.length === 0 && !v.note.trim())
    return "feedback.error.detail";
  return "";
}

function SuiteFeedbackDialog({
  runID,
  open,
  onClose,
}: {
  runID: string;
  open: boolean;
  onClose: () => void;
}) {
  const actions = useActions(),
    qc = useQueryClient();
  const { notification } = AntApp.useApp();
  const saved = useQuery({
    queryKey: ["suite-feedback", runID],
    queryFn: () => call<SuiteFeedback>("GetSuiteFeedback", runID),
    enabled: open && !!runID,
  });
  const [form, setForm] = useState<SuiteFeedback | null>(null);
  const [problem, setProblem] = useState<MessageKey | "">("");
  const [preview, setPreview] = useState<FeedbackPackagePreview | null>(null);
  useEffect(() => {
    if (!open) {
      setForm(null);
      setPreview(null);
      setProblem("");
    } else if (saved.data && !form)
      setForm({ ...saved.data, problems: saved.data.problems || [] });
  }, [open, saved.data, form]);
  function change(p: Partial<SuiteFeedback>) {
    setForm((v) => (v ? { ...v, ...p } : v));
    setProblem("");
  }
  // Saves the form; false when something required is missing or saving failed.
  async function save(notify: boolean): Promise<boolean> {
    if (!form) return false;
    const wrong = missing(form);
    setProblem(wrong);
    if (wrong) return false;
    const out = await actions.run(
      () =>
        call<SuiteFeedback>("SaveSuiteFeedback", {
          ...form,
          note: form.note.trim(),
        }),
      notify ? t("feedback.saved") : undefined,
    );
    if (!out) return false;
    setForm({ ...out, problems: out.problems || [] });
    qc.setQueryData(["suite-feedback", runID], out);
    return true;
  }
  async function showPackage() {
    if (!(await save(false))) return;
    const out = await actions.run(() =>
      call<FeedbackPackagePreview>("PreviewFeedbackPackage", runID),
    );
    if (out) setPreview(out);
  }
  async function savePackage() {
    const path = await actions.run(async () => {
      const path = await call<string>("ChooseFeedbackPath");
      if (path) await call("ExportFeedbackPackageToPath", runID, path);
      return path;
    });
    if (path) {
      notification.success({
        message: t("feedback.exported"),
        description: t("feedback.exported.body", { path }),
      });
      onClose();
    }
  }
  const footer = preview ? (
    <Space wrap>
      <Button onClick={() => setPreview(null)}>
        {t("feedback.preview.back")}
      </Button>
      <Button type="primary" loading={actions.busy} onClick={savePackage}>
        {t("feedback.preview.save")}
      </Button>
    </Space>
  ) : (
    <Space wrap>
      <Button onClick={onClose}>{t("feedback.close")}</Button>
      <Button
        disabled={!form}
        loading={actions.busy}
        onClick={() => save(true)}
      >
        {t("feedback.save")}
      </Button>
      <Button
        type="primary"
        disabled={!form}
        loading={actions.busy}
        onClick={showPackage}
      >
        {t("feedback.package")}
      </Button>
    </Space>
  );
  return (
    <Modal
      title={t(preview ? "feedback.preview.title" : "feedback.title")}
      open={open}
      onCancel={onClose}
      footer={footer}
      width={720}
      destroyOnHidden
    >
      {saved.error && (
        <Alert
          type="error"
          showIcon
          message={t("feedback.unreadable")}
          description={errorText(saved.error)}
        />
      )}
      {preview ? (
        <div className="feedback-preview">
          <ul aria-label={t("feedback.preview.title")}>
            {preview.files.map((f) => (
              <li key={f.name}>
                <strong>{f.name}</strong> · {size(f.bytes)}
                {entryNotes[f.name] && (
                  <p className="hint">{t(entryNotes[f.name])}</p>
                )}
              </li>
            ))}
          </ul>
          <p>
            {t("feedback.preview.total", {
              count: preview.files.length,
              size: size(preview.bytes),
            })}
          </p>
          <p className="hint">{t("feedback.preview.safe")}</p>
          <p className="hint">{t("feedback.preview.where")}</p>
        </div>
      ) : (
        form && (
          <div className="feedback-form">
            <p className="hint">{t("feedback.intro")}</p>
            <fieldset>
              <legend className="field-label">
                {t("feedback.verdict")}
                <span aria-hidden="true"> *</span>
              </legend>
              <Radio.Group
                aria-required="true"
                value={form.verdict || undefined}
                onChange={(e) => change({ verdict: e.target.value })}
                options={verdicts.map((v) => ({
                  value: v.value,
                  label: t(v.label),
                }))}
              />
            </fieldset>
            <fieldset>
              <legend className="field-label">{t("feedback.problems")}</legend>
              <Checkbox.Group
                value={form.problems}
                onChange={(v) => change({ problems: v as string[] })}
                options={problems.map((p) => ({
                  value: p.value,
                  label: t(p.label),
                }))}
              />
            </fieldset>
            <label htmlFor="suite-feedback-note" className="field-label">
              {t("feedback.note")}
            </label>
            <Input.TextArea
              id="suite-feedback-note"
              value={form.note}
              onChange={(e) => change({ note: e.target.value })}
              maxLength={8000}
              autoSize={{ minRows: 3, maxRows: 8 }}
              placeholder={t("feedback.note.placeholder")}
            />
            <SuiteFeedbackImages
              runID={runID}
              items={form.items || []}
              onChange={(items) => change({ items })}
            />
            {problem && (
              <Alert type="error" showIcon role="alert" message={t(problem)} />
            )}
            {form.updatedAt && (
              <p className="hint">
                {t("feedback.savedAt", { time: time(form.updatedAt) })}
              </p>
            )}
          </div>
        )
      )}
    </Modal>
  );
}
