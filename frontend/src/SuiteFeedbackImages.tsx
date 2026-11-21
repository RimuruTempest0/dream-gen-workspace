import { useQuery } from "@tanstack/react-query";
import { Alert, Button, Checkbox, Input, Radio } from "antd";
import { call, errorText } from "./api";
import { t, type MessageKey } from "./i18n";
import { AssetImage, Status } from "./ui";

// 逐张核验 (optional) in the 问题反馈 panel: every card of the 套图, failed,
// 结果未知 and paused ones included, and every version of an edited image with
// the change asked for then. Each can be marked fine or wrong, with problem
// types and one sentence.

export type FeedbackItem = {
  planItemID?: string;
  taskID?: string;
  verdict: "ok" | "problem";
  problems?: string[];
  note?: string;
};
type FeedbackImage = {
  slot: number;
  planItemID: string;
  purpose: string;
  taskID: string;
  version: number;
  versions: number;
  current: boolean;
  status: string;
  assetIDs: string[];
  refineInstruction?: string;
};

/** Caps a note at max characters counted as code points, like the backend. */
export const noteCount = (max: number) => ({
  max,
  strategy: (text: string) => [...text].length,
  exceedFormatter: (text: string, { max }: { max: number }) =>
    [...text].slice(0, max).join(""),
});

const itemProblems:{ value: string; label: MessageKey }[] = [
  { value: "shot-wrong", label: "feedbackImages.problem.shotWrong" },
  { value: "too-similar", label: "feedbackImages.problem.tooSimilar" },
  { value: "design-wrong", label: "feedbackImages.problem.design" },
  { value: "product-changed", label: "feedbackImages.problem.product" },
  { value: "edit-missed", label: "feedbackImages.problem.editMissed" },
  { value: "no-image", label: "feedbackImages.problem.noImage" },
  { value: "other", label: "feedbackImages.problem.other" },
];

// The most characters (code points, as the backend counts them) of an image's note.
const maxItemNote = 2000;

/** The per-image marks as a form control: value and onChange come from Form.Item. */
export function SuiteFeedbackImages({
  runID,
  value,
  onChange = () => {},
}: {
  runID: string;
  value?: FeedbackItem[];
  onChange?: (items: FeedbackItem[]) => void;
}) {
  const items = value || [];
  const images = useQuery({
    queryKey: ["feedback-images", runID],
    queryFn: () => call<FeedbackImage[]>("GetFeedbackImages", runID),
  });
  const byTask = new Map(items.map((v) => [v.taskID || "", v]));
  function set(image: FeedbackImage, v: Partial<FeedbackItem> | null) {
    const rest = items.filter((i) => i.taskID !== image.taskID);
    if (!v) return onChange(rest);
    const was = byTask.get(image.taskID);
    const next: FeedbackItem = {
      planItemID: image.planItemID,
      taskID: image.taskID,
      verdict: "ok",
      ...was,
      ...v,
    };
    // Keep the slot order of the list.
    const order = (images.data || []).map((i) => i.taskID);
    onChange(
      [...rest, next].sort(
        (a, b) => order.indexOf(a.taskID || "") - order.indexOf(b.taskID || ""),
      ),
    );
  }
  return (
    <fieldset className="feedback-images">
      <legend className="field-label">{t("feedbackImages.title")}</legend>
      <p className="hint">
        {t("feedbackImages.intro")}
        {items.length > 0 &&
          ` ${t("feedbackImages.marked", { count: items.length })}`}
      </p>
      {images.error && (
        <Alert
          type="error"
          showIcon
          message={t("feedbackImages.unreadable")}
          description={errorText(images.error)}
        />
      )}
      {images.data?.length === 0 && (
        <p className="hint">{t("feedbackImages.empty")}</p>
      )}
      {images.data?.map((image) => {
        const name =
          image.versions > 1
            ? t("feedbackImages.slotVersion", {
                n: image.slot,
                version: image.version,
              })
            : t("feedbackImages.slot", { n: image.slot });
        const item = byTask.get(image.taskID);
        return (
          <section
            key={image.taskID}
            className="feedback-image"
            aria-label={name}
          >
            <div className="feedback-image-thumb">
              {image.assetIDs[0] && <AssetImage id={image.assetIDs[0]} />}
            </div>
            <div className="feedback-image-body">
              <div className="feedback-image-head">
                <strong>{name}</strong>
                {image.purpose && <span>{image.purpose}</span>}
                <Status state={image.status} />
              </div>
              {image.versions > 1 && (
                <p className="hint">
                  {t(
                    image.current
                      ? "feedbackImages.version.current"
                      : "feedbackImages.version.earlier",
                    { count: image.versions },
                  )}
                </p>
              )}
              {image.refineInstruction && (
                <p className="hint refine-note">
                  {t("feedbackImages.refine", {
                    text: image.refineInstruction,
                  })}
                </p>
              )}
              <div className="feedback-image-verdict">
                <Radio.Group
                  aria-label={t("feedbackImages.verdict", { image: name })}
                  value={item?.verdict}
                  onChange={(e) => set(image, { verdict: e.target.value })}
                  options={[
                    { value: "ok", label: t("feedbackImages.verdict.ok") },
                    {
                      value: "problem",
                      label: t("feedbackImages.verdict.problem"),
                    },
                  ]}
                />
                {item && (
                  <Button
                    size="small"
                    type="link"
                    aria-label={t("feedbackImages.clearLabel", {
                      image: name,
                    })}
                    onClick={() => set(image, null)}
                  >
                    {t("feedbackImages.clear")}
                  </Button>
                )}
              </div>
              {item?.verdict === "problem" && (
                <Checkbox.Group
                  aria-label={t("feedbackImages.problems", { image: name })}
                  value={item.problems || []}
                  onChange={(v) => set(image, { problems: v as string[] })}
                  options={itemProblems.map((p) => ({
                    value: p.value,
                    label: t(p.label),
                  }))}
                />
              )}
              {item && (
                <Input
                  aria-label={t("feedbackImages.note", { image: name })}
                  value={item.note || ""}
                  count={noteCount(maxItemNote)}
                  placeholder={t("feedbackImages.note.placeholder")}
                  onChange={(e) => set(image, { note: e.target.value })}
                />
              )}
            </div>
          </section>
        );
      })}
    </fieldset>
  );
}
