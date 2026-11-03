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
import { FileTextOutlined, PlusOutlined } from "@ant-design/icons";
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
      "模板已保存",
    );
    if (saved !== undefined) onClose();
  }
  function close() {
    if (form.isFieldsTouched())
      actions.confirm(
        "放弃未保存的修改？",
        "关闭后这些修改不会保存。",
        async () => onClose(),
      );
    else onClose();
  }
  const unset = { value: "", label: "不指定（保持当前设置）" };
  return (
    <Modal
      title={value.id ? "编辑模板" : "保存为模板"}
      open
      onCancel={close}
      onOk={save}
      confirmLoading={actions.busy}
      okText="保存"
      cancelText="取消"
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
          label="名称"
          rules={[{ required: true, message: "请填写名称" }]}
        >
          <Input maxLength={100} placeholder="例如：Etsy 极速套图指令" />
        </Form.Item>
        <Form.Item
          name="prompt"
          label="提示词"
          rules={[{ required: true, message: "请填写提示词" }]}
        >
          <Input.TextArea
            autoSize={{ minRows: 5, maxRows: 12 }}
            style={{ resize: "none" }}
          />
        </Form.Item>
        <div className="parameter-grid">
          <Form.Item name="size" label="尺寸（可选）">
            <Select
              options={[unset, ...withCurrent(presetSizes, size)]}
              aria-label="模板尺寸"
            />
          </Form.Item>
          <Form.Item name="quality" label="质量（可选）">
            <Select
              options={[unset, ...withCurrent(presetQualities, quality)]}
              aria-label="模板质量"
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
  function apply(t: Template, mode: TemplateMode) {
    setChoosing(null);
    onApply(t, mode);
    message.success(
      mode === "append" ? `已追加模板“${t.name}”` : `已套用模板“${t.name}”`,
    );
  }
  function pick(t: Template) {
    const current = text.trim();
    if (!current) apply(t, "replace");
    else if (current !== t.prompt.trim()) setChoosing(t);
  }
  const incoming = (loc.state as { templateID?: string } | null)?.templateID;
  const handled = useRef("");
  useEffect(() => {
    if (!incoming || !ready || !templates.data || handled.current === incoming)
      return;
    handled.current = incoming;
    nav(loc.pathname, { replace: true, state: null });
    const t = templates.data.find((v) => v.id === incoming);
    if (t) pick(t);
  }, [incoming, ready, templates.data]);
  return (
    <div className="template-bar">
      <Select
        aria-label="套用模板"
        placeholder="套用模板"
        className="template-select"
        value={null}
        disabled={disabled}
        showSearch
        optionFilterProp="label"
        popupMatchSelectWidth={320}
        placement="bottomRight"
        notFoundContent="还没有模板"
        options={(templates.data || []).map((t) => ({
          value: t.id,
          label: t.name,
          prompt: t.prompt,
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
                管理模板
              </Button>
            </div>
          </>
        )}
        onChange={(id) => {
          const t = templates.data?.find((v) => v.id === id);
          if (t) pick(t);
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
        存为模板
      </Button>
      <Modal
        title={`套用模板“${choosing?.name || ""}”`}
        open={!!choosing}
        onCancel={() => setChoosing(null)}
        footer={[
          <Button key="cancel" onClick={() => setChoosing(null)}>
            取消
          </Button>,
          <Button key="append" onClick={() => apply(choosing!, "append")}>
            追加到末尾
          </Button>,
          <Button
            key="replace"
            type="primary"
            onClick={() => apply(choosing!, "replace")}
          >
            替换当前内容
          </Button>,
        ]}
      >
        <p>输入框里已经有内容，要怎么处理？</p>
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
        title="提示词模板"
        sub="保存常用的提示词和制作指令，在生图工作台和智能生图里一键套用。"
        action={
          <Button
            type="primary"
            icon={<PlusOutlined aria-hidden="true" />}
            onClick={create}
          >
            新建模板
          </Button>
        }
      />
      <section className="panel">
        <DataState
          query={templates}
          empty={!templates.data?.length}
          emptyText="保存常用提示词，减少重复输入"
          action={
            <Button type="primary" onClick={create}>
              新建模板
            </Button>
          }
        >
          <div className="object-list template-list">
            {templates.data?.map((t) => (
              <div className="object-row" key={t.id}>
                <div>
                  <strong>
                    <FileTextOutlined aria-hidden="true" /> {t.name}
                  </strong>
                  <p className="line-clamp">{t.prompt}</p>
                  {(t.size || t.quality) && (
                    <Space size={4} className="template-tags">
                      {t.size && <Tag>{sizeName(t.size)}</Tag>}
                      {t.quality && <Tag>质量：{qualityName(t.quality)}</Tag>}
                    </Space>
                  )}
                </div>
                <Space wrap className="template-actions">
                  <Button
                    size="small"
                    onClick={() => nav("/", { state: { templateID: t.id } })}
                  >
                    用于生图工作台
                  </Button>
                  <Button
                    size="small"
                    onClick={() =>
                      nav("/smart", { state: { templateID: t.id } })
                    }
                  >
                    用于智能生图
                  </Button>
                  <Button size="small" onClick={() => setEditing(t)}>
                    编辑
                  </Button>
                  <Button
                    size="small"
                    danger
                    onClick={() =>
                      actions.confirm(
                        `删除模板“${t.name}”？`,
                        "已有任务不受影响。",
                        () => call("DeleteTemplate", t.id),
                        true,
                      )
                    }
                  >
                    删除
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
