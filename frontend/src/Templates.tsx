import { useEffect, useId, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import {
  App as AntApp,
  Button,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Tag,
} from "antd";
import { PlusOutlined } from "@ant-design/icons";
import { call, type Template } from "./api";
import {
  DataState,
  Header,
  presetQualities,
  presetSizes,
  useActions,
  useData,
  withCurrent,
} from "./ui";
import { t } from "./i18n";

// How a template joins the text already in the box.
export type TemplateMode = "replace" | "append";

const useTemplates = () => useData<Template[]>(["templates"], "ListTemplates");

function TemplateEditor({
  value,
  onClose,
}: {
  value: Partial<Template>;
  onClose: () => void;
}) {
  const formName = "template-" + useId();
  const actions = useActions();
  const [form] = Form.useForm();
  const size = Form.useWatch("size", form);
  const quality = Form.useWatch("quality", form);
  async function save() {
    let values;
    try {
      values = await form.validateFields();
    } catch {
      form.scrollToField("name");
      return;
    }
    const saved = await actions.run(
      () => call("SaveTemplate", { ...value, ...values }),
      t("templates.saved"),
    );
    if (saved !== undefined) onClose();
  }
  function close() {
    if (form.isFieldsTouched())
      actions.confirm(
        t("templates.discard.title"),
        t("templates.discard.body"),
        async () => onClose(),
      );
    else onClose();
  }
  const unset = { value: "", label: t("templates.unset") };
  return (
    <Modal
      title={value.id ? t("templates.edit.title") : t("templates.saveAs.title")}
      open
      onCancel={close}
      onOk={save}
      confirmLoading={actions.busy}
      okText={t("templates.save")}
      cancelText={t("templates.cancel")}
    >
      {/* The name prefixes field ids: the pages behind this dialog use id="prompt". */}
      <Form
        form={form}
        name={formName}
        layout="vertical"
        noValidate
        initialValues={{ size: "", quality: "", ...value }}
      >
        <Form.Item
          name="name"
          label={t("templates.name")}
          rules={[{ required: true, message: t("templates.name.required") }]}
        >
          <Input
            maxLength={100}
            placeholder={t("templates.name.placeholder")}
          />
        </Form.Item>
        <Form.Item
          name="prompt"
          label={t("templates.prompt")}
          rules={[{ required: true, message: t("templates.prompt.required") }]}
        >
          <Input.TextArea
            autoSize={{ minRows: 5, maxRows: 12 }}
            style={{ resize: "none" }}
          />
        </Form.Item>
        <div className="parameter-grid">
          <Form.Item name="size" label={t("templates.size")}>
            <Select
              options={[unset, ...withCurrent(presetSizes, size)]}
              aria-label={t("templates.size.aria")}
            />
          </Form.Item>
          <Form.Item name="quality" label={t("templates.quality")}>
            <Select
              options={[unset, ...withCurrent(presetQualities, quality)]}
              aria-label={t("templates.quality.aria")}
            />
          </Form.Item>
        </div>
      </Form>
    </Modal>
  );
}

// Above a prompt box: pick a saved template, or save the current text as one. Text
// already in the box is never overwritten without asking. A template opened from the
// template page arrives as route state and goes through the same steps.
export function TemplateBar({
  text,
  size,
  quality,
  ready,
  disabled,
  onApply,
}: {
  text: string;
  size?: string;
  quality?: string;
  ready: boolean;
  disabled?: boolean;
  onApply: (template: Template, mode: TemplateMode) => void;
}) {
  const templates = useTemplates();
  const nav = useNavigate();
  const loc = useLocation();
  const { message } = AntApp.useApp();
  const [choosing, setChoosing] = useState<Template | null>(null);
  const [saving, setSaving] = useState<Partial<Template> | null>(null);
  function apply(tpl: Template, mode: TemplateMode) {
    setChoosing(null);
    onApply(tpl, mode);
    message.success(
      mode === "append"
        ? t("templates.appended", { name: tpl.name })
        : t("templates.applied", { name: tpl.name }),
    );
  }
  function pick(tpl: Template) {
    const current = text.trim();
    if (!current) apply(tpl, "replace");
    else if (current !== tpl.prompt.trim()) setChoosing(tpl);
  }
  const incoming = (loc.state as { templateID?: string } | null)?.templateID;
  const handled = useRef("");
  useEffect(() => {
    if (!incoming || !ready || !templates.data || handled.current === incoming)
      return;
    handled.current = incoming;
    nav(loc.pathname, { replace: true, state: null });
    const tpl = templates.data.find((v) => v.id === incoming);
    if (tpl) pick(tpl);
  }, [incoming, ready, templates.data]);
  return (
    <div className="template-bar">
      <Select
        aria-label={t("templates.apply")}
        placeholder={t("templates.apply")}
        className="template-select"
        value={null}
        disabled={disabled}
        showSearch
        optionFilterProp="label"
        popupMatchSelectWidth={320}
        placement="bottomRight"
        notFoundContent={t("templates.none")}
        options={(templates.data || []).map((tpl) => ({
          value: tpl.id,
          label: tpl.name,
          prompt: tpl.prompt,
        }))}
        optionRender={(option) => (
          <div className="template-option">
            <strong>{option.data.label}</strong>
            <small>{option.data.prompt}</small>
          </div>
        )}
        popupRender={(menu) => (
          <>
            {menu}
            <div className="template-popup-footer">
              <Button
                type="link"
                size="small"
                onClick={() => nav("/templates")}
              >
                {t("templates.manage")}
              </Button>
            </div>
          </>
        )}
        onChange={(id) => {
          const tpl = templates.data?.find((v) => v.id === id);
          if (tpl) pick(tpl);
        }}
      />
      <Button
        disabled={disabled || !text.trim()}
        onClick={() =>
          setSaving({
            prompt: text,
            size: size && size !== "auto" ? size : "",
            quality: quality && quality !== "auto" ? quality : "",
          })
        }
      >
        {t("templates.saveAs")}
      </Button>
      <Modal
        title={t("templates.choose.title", { name: choosing?.name || "" })}
        open={!!choosing}
        onCancel={() => setChoosing(null)}
        footer={[
          <Button key="cancel" onClick={() => setChoosing(null)}>
            {t("templates.cancel")}
          </Button>,
          <Button key="append" onClick={() => apply(choosing!, "append")}>
            {t("templates.choose.append")}
          </Button>,
          <Button
            key="replace"
            type="primary"
            onClick={() => apply(choosing!, "replace")}
          >
            {t("templates.choose.replace")}
          </Button>,
        ]}
      >
        <p>{t("templates.choose.body")}</p>
        <p className="line-clamp">{choosing?.prompt}</p>
      </Modal>
      {saving && (
        <TemplateEditor value={saving} onClose={() => setSaving(null)} />
      )}
    </div>
  );
}

export function TemplatesPage() {
  const templates = useTemplates();
  const actions = useActions();
  const nav = useNavigate();
  const [editing, setEditing] = useState<Partial<Template> | null>(null);
  const create = () => setEditing({});
  const sizeName = (v: string) =>
    presetSizes.find((s) => s.value === v)?.label || v;
  const qualityName = (v: string) =>
    presetQualities.find((s) => s.value === v)?.label || v;
  return (
    <>
      <Header
        title={t("templates.title")}
        sub={t("templates.sub")}
        action={
          <Button
            type="primary"
            icon={<PlusOutlined aria-hidden="true" />}
            onClick={create}
          >
            {t("templates.new")}
          </Button>
        }
      />
      <section className="panel">
        <DataState
          query={templates}
          empty={!templates.data?.length}
          emptyText={t("templates.empty")}
          action={
            <Button type="primary" onClick={create}>
              {t("templates.new")}
            </Button>
          }
        >
          <div className="object-list template-list">
            {templates.data?.map((tpl) => (
              <div className="object-row" key={tpl.id}>
                <div>
                  <strong>{tpl.name}</strong>
                  <p className="line-clamp">{tpl.prompt}</p>
                  {(tpl.size || tpl.quality) && (
                    <Space size={4} className="template-tags">
                      {tpl.size && <Tag>{sizeName(tpl.size)}</Tag>}
                      {tpl.quality && (
                        <Tag>
                          {t("templates.qualityTag", {
                            quality: qualityName(tpl.quality),
                          })}
                        </Tag>
                      )}
                    </Space>
                  )}
                </div>
                <Space wrap className="template-actions">
                  <Button
                    size="small"
                    onClick={() =>
                      nav("/pro", { state: { templateID: tpl.id } })
                    }
                  >
                    {t("templates.usePro")}
                  </Button>
                  <Button
                    size="small"
                    onClick={() => nav("/", { state: { templateID: tpl.id } })}
                  >
                    {t("templates.useCreate")}
                  </Button>
                  <Button
                    size="small"
                    type="text"
                    className="template-edit"
                    onClick={() => setEditing(tpl)}
                  >
                    {t("templates.edit")}
                  </Button>
                  <Button
                    size="small"
                    type="text"
                    danger
                    onClick={() =>
                      actions.confirm(
                        t("templates.delete.title", { name: tpl.name }),
                        t("templates.delete.body"),
                        () => call("DeleteTemplate", tpl.id),
                        true,
                      )
                    }
                  >
                    {t("templates.delete")}
                  </Button>
                </Space>
              </div>
            ))}
          </div>
        </DataState>
      </section>
      {editing && (
        <TemplateEditor value={editing} onClose={() => setEditing(null)} />
      )}
    </>
  );
}
