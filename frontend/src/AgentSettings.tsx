import { useEffect, useRef, useState } from "react";
import {
  Alert,
  App as AntApp,
  Button,
  Empty,
  Form as UIForm,
  Input,
  InputNumber,
  Modal,
  Select as UISelect,
  Space,
  Switch,
  Tag,
  Tooltip,
  AutoComplete,
} from "antd";
import {
  ApiOutlined,
  DeleteOutlined,
  PlusOutlined,
  UploadOutlined,
} from "@ant-design/icons";
import { useQueryClient } from "@tanstack/react-query";
import { call, errorText, type Connection, type TestResult } from "./api";
import type {
  AgentRuntimeInfo,
  LLMCapabilities,
  SkillRecord,
} from "./agent-api";
import { DataState, SecretInput, useActions, useData } from "./ui";
import { t, type MessageKey } from "./i18n";

// The backend stores where a skill came from as a fixed Chinese word.
const skillSources: Record<string, MessageKey> = {
  内置: "agentSettings.skill.source.builtin",
  导入: "agentSettings.skill.source.imported",
  自定义: "agentSettings.skill.source.custom",
};
function skillSource(source: string) {
  const key = skillSources[source];
  return key ? t(key) : source;
}

export function ConnectionFields({
  editing,
  llm = false,
}: {
  editing: Connection | null | undefined;
  llm?: boolean;
}) {
  return (
    <>
      <UIForm.Item
        name="name"
        label={t("agentSettings.name")}
        rules={[{ required: true, message: t("agentSettings.name.required") }]}
      >
        <Input
          placeholder={
            llm
              ? t("agentSettings.name.placeholderLLM")
              : t("agentSettings.name.placeholderImage")
          }
          maxLength={100}
        />
      </UIForm.Item>
      <UIForm.Item
        name="baseURL"
        label="Base URL"
        extra={
          llm
            ? t("agentSettings.baseURL.hintLLM")
            : t("agentSettings.baseURL.hintImage")
        }
        rules={[
          { required: true, message: t("agentSettings.baseURL.required") },
          {
            validator: (_, v) => {
              try {
                const u = new URL(v);
                if (
                  !["http:", "https:"].includes(u.protocol) ||
                  u.username ||
                  u.password ||
                  u.search ||
                  u.hash
                )
                  throw new Error();
                return Promise.resolve();
              } catch {
                return Promise.reject(
                  new Error(t("agentSettings.baseURL.invalid")),
                );
              }
            },
          },
        ]}
      >
        <Input placeholder="https://example.com/v1" autoComplete="off" />
      </UIForm.Item>
      <UIForm.Item
        name="key"
        label="API Key"
        extra={
          editing?.hasKey
            ? t("agentSettings.key.keep")
            : t("agentSettings.key.hint")
        }
        rules={
          editing?.hasKey
            ? []
            : [{ required: true, message: t("agentSettings.key.required") }]
        }
      >
        <SecretInput
          placeholder={
            editing?.hasKey
              ? t("agentSettings.key.savedPlaceholder")
              : t("agentSettings.key.placeholder")
          }
          autoComplete="off"
        />
      </UIForm.Item>
      <UIForm.Item
        name="model"
        label={
          llm ? t("agentSettings.model") : t("agentSettings.model.default")
        }
        rules={
          llm
            ? [{ required: true, message: t("agentSettings.model.required") }]
            : []
        }
      >
        <Input placeholder={t("agentSettings.model.placeholder")} />
      </UIForm.Item>
      {!llm && (
        <UIForm.Item
          name="protocol"
          label={t("agentSettings.protocol")}
          extra={t("agentSettings.protocol.hint")}
        >
          <UISelect
            options={[
              {
                value: "openai-images",
                label: t("agentSettings.protocol.sync"),
              },
              { value: "async-jobs", label: t("agentSettings.protocol.async") },
            ]}
          />
        </UIForm.Item>
      )}
      {llm && (
        <UIForm.Item
          name="maxOutputTokens"
          label={t("agentSettings.maxTokens")}
          extra={t("agentSettings.maxTokens.hint")}
        >
          <InputNumber min={256} max={65536} className="full-width" />
        </UIForm.Item>
      )}
      {llm && (
        <div className="parameter-grid">
          <UIForm.Item name="mode" label={t("agentSettings.mode")}>
            <UISelect
              options={[
                { value: "claude", label: t("agentSettings.mode.claude") },
                {
                  value: "compatible",
                  label: t("agentSettings.mode.compatible"),
                },
              ]}
            />
          </UIForm.Item>
          <UIForm.Item name="authMode" label={t("agentSettings.auth")}>
            <UISelect
              options={[
                { value: "api-key", label: t("agentSettings.auth.apiKey") },
                { value: "bearer", label: "Bearer Token" },
              ]}
            />
          </UIForm.Item>
        </div>
      )}
    </>
  );
}

export function LLMConnections() {
  const q = useData<Connection[]>(["connections"], "ListConnections");
  const actions = useActions();
  const qc = useQueryClient();
  const { notification } = AntApp.useApp();
  const [editing, setEditing] = useState<Connection | null | undefined>();
  const [form] = UIForm.useForm();
  const [result, setResult] = useState<TestResult | null>(null);
  const [error, setError] = useState("");
  const [testing, setTesting] = useState(false);
  const testID = useRef<string | null>(null);
  const [validating, setValidating] = useState<string | null>(null);
  const validationID = useRef<string | null>(null);
  const [capResult, setCapResult] = useState<LLMCapabilities | null>(null);
  useEffect(
    () => () => {
      if (testID.current)
        call("CancelConnectionTest", testID.current).catch(() => {});
      if (validationID.current)
        call("CancelConnectionTest", validationID.current).catch(() => {});
    },
    [],
  );
  function open(c: Connection | null) {
    setEditing(c);
    setError("");
    setResult(null);
    form.resetFields();
    form.setFieldsValue(
      c
        ? { ...c, key: "" }
        : {
            name: "",
            baseURL: "",
            model: "",
            key: "",
            mode: "claude",
            authMode: "api-key",
            maxOutputTokens: 65536,
          },
    );
  }
  function cancelTest() {
    const id = testID.current;
    testID.current = null;
    setTesting(false);
    if (id) call("CancelConnectionTest", id).catch(() => {});
  }
  async function getModels() {
    if (testID.current) return;
    const id = crypto.randomUUID();
    testID.current = id;
    setTesting(true);
    setResult(null);
    setError("");
    try {
      await form.validateFields(["baseURL", "key"]);
      const out = await call<TestResult>("TestConnection", {
        ...form.getFieldsValue(),
        id: editing?.id || "",
        kind: "llm",
        testRequestID: id,
      });
      if (testID.current === id) setResult(out);
    } catch (e) {
      if (
        testID.current === id &&
        !(e && typeof e === "object" && "errorFields" in e)
      )
        setError(errorText(e));
    } finally {
      if (testID.current === id) {
        setTesting(false);
        testID.current = null;
      }
    }
  }
  async function save() {
    try {
      const values = await form.validateFields();
      const out = await actions.run(
        () =>
          call<Connection>("SaveConnection", {
            ...values,
            id: editing?.id || "",
            kind: "llm",
            clearKey: false,
          }),
        t("agentSettings.llm.saved"),
      );
      if (out) {
        setEditing(undefined);
        form.resetFields();
      }
    } catch {
      form.scrollToField("baseURL", { focus: true });
    }
  }
  function close() {
    if (testing || actions.busy) return;
    const done = async () => {
      setEditing(undefined);
      form.resetFields();
    };
    if (form.isFieldsTouched())
      actions.confirm(
        t("agentSettings.llm.discard.title"),
        t("agentSettings.llm.discard.body"),
        done,
      );
    else void done();
  }
  function validate(c: Connection) {
    actions.confirm(
      t("agentSettings.validate.title"),
      t("agentSettings.validate.body", { name: c.name, model: c.model }),
      async () => {
        const id = crypto.randomUUID();
        validationID.current = id;
        setValidating(c.id);
        setCapResult(null);
        setError("");
        void call<LLMCapabilities>("ValidateLLMCapabilities", c.id, id)
          .then(async (out) => {
            if (validationID.current === id) setCapResult(out);
            await qc.invalidateQueries({ queryKey: ["connections"] });
          })
          .catch((e) => {
            if (validationID.current === id)
              notification.error({
                message: t("agentSettings.validate.failed"),
                description: errorText(e),
                duration: 0,
              });
          })
          .finally(() => {
            if (validationID.current === id) {
              validationID.current = null;
              setValidating(null);
            }
          });
      },
      false,
      t("agentSettings.validate.ok"),
    );
  }
  const llms = q.data?.filter((c) => c.kind === "llm") || [];
  return (
    <section className="panel">
      <div className="panel-heading">
        <h2>
          <ApiOutlined aria-hidden="true" /> {t("agentSettings.llm.title")}
        </h2>
        {!llms.some((c) => c.managed) && (
          <Button
            icon={<PlusOutlined aria-hidden="true" />}
            onClick={() => open(null)}
          >
            {t("agentSettings.llm.add")}
          </Button>
        )}
      </div>
      <p className="hint">{t("agentSettings.llm.hint")}</p>
      <DataState
        query={q}
        empty={!llms.length}
        emptyText={t("agentSettings.llm.empty")}
      >
        <div className="connection-list">
          {llms.map((c) => (
            <div className="connection-row" key={c.id}>
              <div className="connection-icon">
                <ApiOutlined aria-hidden="true" />
              </div>
              <div className="connection-info">
                <strong>
                  {c.name}{" "}
                  <Tag color={c.hasKey ? "success" : "warning"}>
                    {c.hasKey
                      ? t("agentSettings.key.saved")
                      : t("agentSettings.key.needed")}
                  </Tag>
                  <Tag>
                    {c.mode === "compatible"
                      ? t("agentSettings.mode.compatibleTag")
                      : "Claude"}
                  </Tag>
                </strong>
                {c.managed ? (
                  <p className="hint">{t("agentSettings.managed.by")}</p>
                ) : (
                  <p className="mono">{c.baseURL}</p>
                )}
                <small>{c.model || t("agentSettings.model.unset")}</small>
                <div className="capability-tags">
                  {(
                    [
                      ["text", t("agentSettings.cap.text")],
                      ["vision", t("agentSettings.cap.vision")],
                      ["tools", t("agentSettings.cap.tools")],
                      ["streaming", t("agentSettings.cap.streaming")],
                    ] as const
                  ).map(([key, label]) => (
                    <Tag
                      key={key}
                      color={
                        c.capabilities?.[key]
                          ? "success"
                          : c.capabilities?.checkedAt
                            ? "error"
                            : "default"
                      }
                    >
                      {label} ·{" "}
                      {c.capabilities?.[key]
                        ? t("agentSettings.cap.passed")
                        : c.capabilities?.checkedAt
                          ? t("agentSettings.cap.failed")
                          : t("agentSettings.cap.pending")}
                    </Tag>
                  ))}
                </div>
              </div>
              {c.managed ? (
                <Tag>{t("agentSettings.managed.tag")}</Tag>
              ) : (
                <Space wrap>
                  <Button disabled={!!validating} onClick={() => open(c)}>
                    {t("agentSettings.llm.edit")}
                  </Button>
                  <Button
                    disabled={!c.hasKey || !c.model || !!validating}
                    loading={validating === c.id}
                    onClick={() => validate(c)}
                  >
                    {t("agentSettings.validate")}
                  </Button>
                  {c.hasKey && (
                    <Button
                      disabled={!!validating}
                      onClick={() =>
                        actions.confirm(
                          t("agentSettings.llm.clearKey.title"),
                          t("agentSettings.llm.clearKey.body"),
                          () =>
                            call("SaveConnection", {
                              ...c,
                              key: "",
                              clearKey: true,
                            }),
                        )
                      }
                    >
                      {t("agentSettings.clearKey")}
                    </Button>
                  )}
                  <Button
                    danger
                    disabled={!!validating}
                    aria-label={t("agentSettings.llm.delete.aria", {
                      name: c.name,
                    })}
                    icon={<DeleteOutlined aria-hidden="true" />}
                    onClick={() =>
                      actions.confirm(
                        t("agentSettings.llm.delete.title", { name: c.name }),
                        t("agentSettings.llm.delete.body"),
                        () => call("DeleteConnection", c.id),
                        true,
                      )
                    }
                  />
                </Space>
              )}
            </div>
          ))}
        </div>
      </DataState>
      {validating && (
        <Space>
          <span role="status">{t("agentSettings.validate.running")}</span>
          <Button
            onClick={() => {
              const id = validationID.current;
              validationID.current = null;
              setValidating(null);
              if (id) call("CancelConnectionTest", id).catch(() => {});
            }}
          >
            {t("agentSettings.validate.cancel")}
          </Button>
        </Space>
      )}
      {capResult && (
        <Alert
          type={
            capResult.text &&
            capResult.vision &&
            capResult.tools &&
            capResult.streaming
              ? "success"
              : "warning"
          }
          showIcon
          message={t("agentSettings.validate.result")}
          description={
            <div className="prewrap">{capResult.details.join("\n")}</div>
          }
        />
      )}
      <Modal
        title={
          editing
            ? t("agentSettings.llm.editTitle")
            : t("agentSettings.llm.add")
        }
        open={editing !== undefined}
        onCancel={close}
        onOk={save}
        okText={t("agentSettings.llm.saveOk")}
        cancelText={t("agentSettings.cancel")}
        confirmLoading={actions.busy}
        okButtonProps={{ disabled: testing }}
        maskClosable={!testing}
        width={640}
        destroyOnHidden
      >
        <UIForm form={form} layout="vertical" noValidate disabled={testing}>
          <ConnectionFields editing={editing} llm />
          <Space wrap>
            <Button loading={testing} onClick={getModels}>
              {t("agentSettings.llm.getModels")}
            </Button>
            {testing && (
              <Button disabled={false} onClick={cancelTest}>
                {t("agentSettings.llm.cancelQuery")}
              </Button>
            )}
            <span className="muted">{t("agentSettings.llm.queryOnly")}</span>
          </Space>
          {error && <Alert type="error" showIcon message={error} />}{" "}
          {result && (
            <div className="test-result">
              <Alert
                type={result.ids.length ? "success" : "warning"}
                showIcon
                message={result.auth}
                description={result.models}
              />
              {!!result.ids.length && (
                <UISelect
                  className="full-width"
                  showSearch
                  aria-label={t("agentSettings.llm.models")}
                  options={result.ids.map((id) => ({ value: id, label: id }))}
                  onChange={(id) => form.setFieldValue("model", id)}
                />
              )}
            </div>
          )}
        </UIForm>
      </Modal>
    </section>
  );
}

export function AgentSettings() {
  const runtime = useData<AgentRuntimeInfo>(["agent-runtime"], "AgentRuntime");
  const q = useData<SkillRecord[]>(["skills"], "ListSkills");
  const actions = useActions();
  const { notification } = AntApp.useApp();
  const [editing, setEditing] = useState<SkillRecord | null | undefined>();
  const [name, setName] = useState("");
  const [content, setContent] = useState("");
  const [error, setError] = useState("");
  const [dirty, setDirty] = useState(false);
  const [categoryFor, setCategoryFor] = useState<SkillRecord | null>(null);
  const [category, setCategory] = useState("");
  const categories = useData<string[]>(["skill-categories"], "SkillCategories");
  function open(v: SkillRecord | null) {
    setEditing(v);
    setName(v?.name || "");
    setContent(v?.content || t("agentSettings.skill.template"));
    setError("");
    setDirty(false);
  }
  async function importSkill(directory: boolean) {
    await actions.run(async () => {
      const path = await call<string>("ChooseSkillSource", directory);
      if (!path) return;
      // A skill of the same name comes back flagged, not as an error text.
      const r = await call<SkillRecord>("ImportSkill", path, false);
      if (r?.conflict) {
        actions.confirm(
          t("agentSettings.skill.overwrite.title"),
          t("agentSettings.skill.overwrite.body"),
          () => call("ImportSkill", path, true),
          true,
          t("agentSettings.skill.overwrite.ok"),
        );
        return;
      }
      notification.success({ message: t("agentSettings.skill.imported") });
    });
  }
  async function save() {
    setError("");
    if (!name.trim()) {
      setError(t("agentSettings.skill.name.required"));
      return;
    }
    await actions.run(async () => {
      try {
        await call("SaveSkill", {
          ...(editing || {}),
          id: editing?.id || "",
          name,
          content,
          enabled: editing?.enabled ?? true,
        });
        setEditing(undefined);
        setDirty(false);
        notification.success({ message: t("agentSettings.skill.saved") });
      } catch (e) {
        setError(errorText(e));
        throw e;
      }
    });
  }
  function close() {
    if (dirty)
      actions.confirm(
        t("agentSettings.skill.discard.title"),
        t("agentSettings.skill.discard.body"),
        async () => setEditing(undefined),
      );
    else setEditing(undefined);
  }
  return (
    <section className="panel">
      <div className="panel-heading">
        <h2>{t("agentSettings.skills.title")}</h2>
        <Space wrap>
          <Button onClick={() => runtime.refetch()}>
            {t("agentSettings.runtime.check")}
          </Button>
          <Button onClick={() => open(null)}>
            {t("agentSettings.skill.new")}
          </Button>
          <Button
            icon={<UploadOutlined aria-hidden="true" />}
            onClick={() => importSkill(false)}
            disabled={actions.busy}
          >
            {t("agentSettings.skill.importZip")}
          </Button>
          <Button onClick={() => importSkill(true)} disabled={actions.busy}>
            {t("agentSettings.skill.importFolder")}
          </Button>
        </Space>
      </div>
      <DataState query={runtime}>
        <Alert
          type={runtime.data?.available ? "success" : "warning"}
          showIcon
          message={`Claude Code ${runtime.data?.version || ""}`}
          description={runtime.data?.message}
        />
      </DataState>
      <p className="hint">{t("agentSettings.skills.hint")}</p>
      <DataState
        query={q}
        empty={!q.data?.length}
        emptyText={t("agentSettings.skills.empty")}
      >
        <div className="connection-list">
          {q.data?.map((v) => (
            <div className="connection-row" key={v.id}>
              <div className="connection-info">
                <strong>
                  {v.name}{" "}
                  <Tag>
                    {skillSource(v.source)} · v{v.version}
                  </Tag>
                </strong>
                <p>{v.description}</p>
                {!!v.attachments?.length && (
                  <small>
                    {t("agentSettings.skill.attachments", {
                      count: v.attachments.length,
                    })}
                  </small>
                )}
                <small>
                  {t("agentSettings.skill.category", {
                    category:
                      v.category || t("agentSettings.skill.category.all"),
                  })}{" "}
                  <Button
                    type="link"
                    size="small"
                    aria-label={t("agentSettings.skill.setCategory.aria", {
                      name: v.name,
                    })}
                    onClick={() => {
                      setCategoryFor(v);
                      setCategory(v.category || "");
                    }}
                  >
                    {t("agentSettings.skill.setCategory")}
                  </Button>
                </small>
              </div>
              <Space>
                <Switch
                  checked={v.enabled}
                  aria-label={t("agentSettings.skill.enable", { name: v.name })}
                  disabled={actions.busy}
                  onChange={(enabled) =>
                    actions.run(() => call("SetSkillEnabled", v.id, enabled))
                  }
                />
                <Button onClick={() => open(v)}>
                  {t("agentSettings.skill.edit")}
                </Button>
                <Button
                  danger
                  aria-label={t("agentSettings.skill.delete.aria", {
                    name: v.name,
                  })}
                  icon={<DeleteOutlined aria-hidden="true" />}
                  onClick={() =>
                    actions.confirm(
                      t("agentSettings.skill.delete.title", { name: v.name }),
                      t("agentSettings.skill.delete.body"),
                      () => call("DeleteSkill", v.id),
                      true,
                    )
                  }
                />
              </Space>
            </div>
          ))}
        </div>
      </DataState>
      <Modal
        title={t("agentSettings.category.title", {
          name: categoryFor?.name || "",
        })}
        open={!!categoryFor}
        onCancel={() => setCategoryFor(null)}
        onOk={() =>
          actions.run(async () => {
            await call("SetSkillCategory", categoryFor!.id, category);
            setCategoryFor(null);
          })
        }
        okText={t("agentSettings.save")}
        cancelText={t("agentSettings.cancel")}
        destroyOnHidden
      >
        <p className="hint">{t("agentSettings.category.hint")}</p>
        <label className="field-label" htmlFor="skill-category">
          {t("agentSettings.category.label")}
        </label>
        <AutoComplete
          id="skill-category"
          className="full-width"
          value={category}
          options={(categories.data || []).map((value) => ({ value }))}
          onChange={setCategory}
          placeholder={t("agentSettings.category.placeholder")}
        />
      </Modal>
      <Modal
        title={
          editing ? t("agentSettings.skill.edit") : t("agentSettings.skill.new")
        }
        open={editing !== undefined}
        onCancel={close}
        onOk={save}
        okText={t("agentSettings.skill.saveOk")}
        cancelText={t("agentSettings.cancel")}
        confirmLoading={actions.busy}
        width={800}
        destroyOnHidden
      >
        <label className="field-label" htmlFor="skill-name">
          {t("agentSettings.skill.name")}
        </label>
        <Input
          id="skill-name"
          value={name}
          onChange={(e) => {
            setName(e.target.value);
            setDirty(true);
          }}
          maxLength={100}
        />
        <label className="field-label" htmlFor="skill-content">
          SKILL.md
        </label>
        <Input.TextArea
          id="skill-content"
          value={content}
          onChange={(e) => {
            setContent(e.target.value);
            setDirty(true);
          }}
          autoSize={{ minRows: 14, maxRows: 24 }}
          className="mono"
          style={{ resize: "none" }}
        />
        {editing?.attachments?.map((a) => (
          <Tag key={a.name}>{a.name}</Tag>
        ))}
        {error && <Alert type="error" message={error} showIcon />}
      </Modal>
    </section>
  );
}
