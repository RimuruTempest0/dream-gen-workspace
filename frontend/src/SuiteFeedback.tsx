import { useEffect, useId, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  App as AntApp,
  Button,
  Checkbox,
  Form,
  Input,
  Modal,
  Progress,
  Radio,
  Space,
} from "antd";
import { MessageOutlined } from "@ant-design/icons";
import { call, errorText } from "./api";
import { formatNumber, t, type MessageKey } from "./i18n";
import { time, useActions } from "./ui";
import {
  SuiteFeedbackImages,
  noteCount,
  type FeedbackItem,
} from "./SuiteFeedbackImages";

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
  generatedImages: number;
  referenceImages: number;
  uploads: number;
};
// Packing reports image `image` of `images`; done once the package is saved.
type FeedbackPackageProgress = {
  runID: string;
  image: number;
  images: number;
  done: boolean;
};
const isImage = (name: string) => name.startsWith("images/");

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
// The most characters (code points) of the note on the set: maxFeedbackNote in
// internal/core/feedback.go.
const maxNote = 8000;

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

// The form's fields; runID and updatedAt stay outside it.
type Values = {
  verdict?: Verdict;
  problems: string[];
  note: string;
  items: FeedbackItem[];
};
const valuesOf = (v: SuiteFeedback): Values => ({
  verdict: v.verdict || undefined,
  problems: v.problems || [],
  note: v.note || "",
  items: v.items || [],
});

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
  const formName = "suite-feedback-" + useId();
  const [form] = Form.useForm<Values>();
  const saved = useQuery({
    queryKey: ["suite-feedback", runID],
    queryFn: () => call<SuiteFeedback>("GetSuiteFeedback", runID),
    enabled: open && !!runID,
  });
  // The values the form starts from (set once per opening) and when the
  // feedback was last saved.
  const [initial, setInitial] = useState<Values | null>(null);
  const [updatedAt, setUpdatedAt] = useState("");
  // Changed since it was opened or last saved.
  const [dirty, setDirty] = useState(false);
  const [preview, setPreview] = useState<FeedbackPackagePreview | null>(null);
  const [packing, setPacking] = useState<FeedbackPackageProgress | null>(null);
  // The images are packed in the background; generation keeps running.
  useEffect(() => {
    if (!open) return;
    const off = window.runtime?.EventsOn(
      "feedback:progress",
      (p: FeedbackPackageProgress) => {
        if (p.runID === runID) setPacking(p.done ? null : p);
      },
    );
    return () => {
      off?.();
      setPacking(null);
    };
  }, [open, runID]);
  useEffect(() => {
    if (!open) {
      setInitial(null);
      setPreview(null);
      setDirty(false);
    } else if (saved.data && !initial) {
      setInitial(valuesOf(saved.data));
      setUpdatedAt(saved.data.updatedAt);
    }
  }, [open, saved.data, initial]);

  // Moves to the first field that is wrong: its control, else its first option.
  function focusField(name: keyof Values) {
    form.scrollToField(name, { block: "center" });
    const el = document.getElementById(`${formName}_${name}`);
    const target =
      el && el.matches("input, textarea")
        ? el
        : el?.querySelector<HTMLElement>("input, textarea");
    target?.focus();
  }
  // Saves the form; false when something required is missing or saving failed.
  async function save(notify: boolean): Promise<boolean> {
    let values: Values;
    try {
      values = await form.validateFields();
    } catch (e) {
      const first = (e as { errorFields?: { name: (string | number)[] }[] })
        .errorFields?.[0];
      if (first) focusField(first.name[0] as keyof Values);
      return false;
    }
    const out = await actions.run(
      () =>
        call<SuiteFeedback>("SaveSuiteFeedback", {
          runID,
          verdict: values.verdict,
          problems: values.problems || [],
          note: (values.note || "").trim(),
          items: values.items || [],
        }),
      notify ? t("feedback.saved") : undefined,
    );
    if (!out) return false;
    form.setFieldsValue(valuesOf(out));
    setUpdatedAt(out.updatedAt);
    setDirty(false);
    qc.setQueryData(["suite-feedback", runID], out);
    return true;
  }
  // Closing with unsaved changes asks first.
  function close() {
    if (dirty && !preview)
      actions.confirm(
        t("feedback.discard.title"),
        t("feedback.discard.body"),
        async () => onClose(),
      );
    else onClose();
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
      <Button
        type="primary"
        loading={actions.busy || !!packing}
        onClick={savePackage}
      >
        {t("feedback.preview.save")}
      </Button>
    </Space>
  ) : (
    <Space wrap>
      <Button onClick={close}>{t("feedback.close")}</Button>
      <Button
        disabled={!initial}
        loading={actions.busy}
        onClick={() => save(true)}
      >
        {t("feedback.save")}
      </Button>
      <Button
        type="primary"
        disabled={!initial}
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
      onCancel={close}
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
      {preview && (
        <div className="feedback-preview">
          <ul aria-label={t("feedback.preview.title")}>
            {preview.files
              .filter((f) => !isImage(f.name))
              .map((f) => (
                <li key={f.name}>
                  <strong>{f.name}</strong> · {size(f.bytes)}
                  {entryNotes[f.name] && (
                    <p className="hint">{t(entryNotes[f.name])}</p>
                  )}
                </li>
              ))}
            <li>
              <strong>images/</strong> ·{" "}
              {size(
                preview.files
                  .filter((f) => isImage(f.name))
                  .reduce((n, f) => n + f.bytes, 0),
              )}
              <p className="hint">
                {t("feedback.preview.images", {
                  generated: preview.generatedImages,
                  references: preview.referenceImages,
                })}
              </p>
              <p className="hint">
                {preview.uploads > 0
                  ? t("feedback.preview.uploads", { count: preview.uploads })
                  : t("feedback.preview.noUploads")}
              </p>
            </li>
          </ul>
          <p>
            {t("feedback.preview.total", {
              count: preview.files.length,
              size: size(preview.bytes),
            })}
          </p>
          <p className="hint">{t("feedback.preview.safe")}</p>
          <p className="hint">{t("feedback.preview.where")}</p>
          {packing && (
            <div role="status">
              <p>
                {t("feedback.packing", {
                  image: packing.image,
                  images: packing.images,
                })}
              </p>
              <Progress
                percent={Math.round(
                  ((packing.image - 1) / Math.max(packing.images, 1)) * 100,
                )}
                showInfo={false}
                size="small"
              />
            </div>
          )}
        </div>
      )}
      {/* Kept (hidden) while the package is previewed, so 返回修改 finds it as left. */}
      {initial && (
        <Form
          form={form}
          name={formName}
          layout="vertical"
          noValidate
          className="feedback-form"
          style={preview ? { display: "none" } : undefined}
          initialValues={initial}
          onValuesChange={(changed) => {
            setDirty(true);
            // The detail rule reads the verdict and the note: check it again
            // while it shows an error.
            if (
              !("problems" in changed) &&
              form.getFieldError("problems").length > 0
            )
              form.validateFields(["problems"]).catch(() => {});
          }}
        >
          <p className="hint">{t("feedback.intro")}</p>
          <fieldset>
            <legend className="field-label">
              {t("feedback.verdict")}
              <span aria-hidden="true"> *</span>
            </legend>
            <Form.Item
              name="verdict"
              rules={[{ required: true, message: t("feedback.error.verdict") }]}
            >
              <Radio.Group
                aria-required="true"
                options={verdicts.map((v) => ({
                  value: v.value,
                  label: t(v.label),
                }))}
              />
            </Form.Item>
          </fieldset>
          <fieldset>
            <legend className="field-label">{t("feedback.problems")}</legend>
            <Form.Item
              name="problems"
              rules={[
                ({ getFieldValue }) => ({
                  validator(_, value?: string[]) {
                    const verdict = getFieldValue("verdict");
                    const note = (getFieldValue("note") || "") as string;
                    if (
                      verdict &&
                      verdict !== "usable" &&
                      !value?.length &&
                      !note.trim()
                    )
                      return Promise.reject(
                        new Error(t("feedback.error.detail")),
                      );
                    return Promise.resolve();
                  },
                }),
              ]}
            >
              <Checkbox.Group
                options={problems.map((p) => ({
                  value: p.value,
                  label: t(p.label),
                }))}
              />
            </Form.Item>
          </fieldset>
          <Form.Item name="note" label={t("feedback.note")}>
            <Input.TextArea
              count={noteCount(maxNote)}
              autoSize={{ minRows: 3, maxRows: 8 }}
              style={{ resize: "none" }}
              placeholder={t("feedback.note.placeholder")}
            />
          </Form.Item>
          <Form.Item name="items" noStyle>
            <SuiteFeedbackImages runID={runID} />
          </Form.Item>
          {updatedAt && (
            <p className="hint">
              {t("feedback.savedAt", { time: time(updatedAt) })}
            </p>
          )}
        </Form>
      )}
    </Modal>
  );
}
