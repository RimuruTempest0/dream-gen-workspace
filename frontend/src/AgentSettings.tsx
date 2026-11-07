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
        label="连接名称"
        rules={[{ required: true, message: "请填写便于识别的连接名称" }]}
      >
        <Input
          placeholder={llm ? "例如：我的 LLM 供应商" : "例如：我的生图中转站"}
          maxLength={100}
        />
      </UIForm.Item>
      <UIForm.Item
        name="baseURL"
        label="Base URL"
        extra={
          llm
            ? "填写 Anthropic Messages API 前缀，例如 https://example.com/anthropic/v1；保留路径，不重复追加 /v1。"
            : "填写 API 前缀，例如 https://example.com/v1；保留你的路径，不自动追加 /v1。"
        }
        rules={[
          { required: true, message: "请填写 Base URL" },
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
                  new Error(
                    "请输入完整 HTTP/HTTPS 地址，不能包含凭证、查询参数或片段",
                  ),
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
            ? "留空保留原密钥；修改时请填写新密钥。"
            : "密钥使用当前 Windows 用户加密，不进入备份。"
        }
        rules={
          editing?.hasKey ? [] : [{ required: true, message: "请填写 API Key" }]
        }
      >
        <SecretInput
          placeholder={editing?.hasKey ? "已保存 · 留空保留" : "输入 API Key"}
          autoComplete="off"
        />
      </UIForm.Item>
      <UIForm.Item
        name="model"
        label={llm ? "LLM 模型 ID" : "默认模型 ID（可选）"}
        rules={llm ? [{ required: true, message: "请填写 LLM 模型 ID" }] : []}
      >
        <Input placeholder="从查询结果选择，或手动填写" />
      </UIForm.Item>
      {llm && (
        <UIForm.Item
          name="maxOutputTokens"
          label="单次输出上限（tokens）"
          extra="默认 65536（包括模型思考）；模型支持的上限更低时再调低。"
        >
          <InputNumber min={256} max={65536} className="full-width" />
        </UIForm.Item>
      )}
      {llm && (
        <div className="parameter-grid">
          <UIForm.Item name="mode" label="模型模式">
            <UISelect
              options={[
                { value: "claude", label: "Claude 模型" },
                { value: "compatible", label: "其他模型 · 兼容接入" },
              ]}
            />
          </UIForm.Item>
          <UIForm.Item name="authMode" label="认证方式">
            <UISelect
              options={[
                { value: "api-key", label: "x-api-key（默认）" },
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
        "LLM 连接已保存",
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
      actions.confirm("放弃未保存的连接修改？", "密钥仅在保存后持久化。", done);
    else void done();
  }
  function validate(c: Connection) {
    actions.confirm(
      "验证 Agent 能力？",
      `将使用“${c.name}”的 ${c.model} 发送 4 项小型验证请求（文本、图片、工具、流式）。可能产生 LLM 费用，不会生成商品图片。`,
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
                message: "Agent 验证未完成",
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
      "开始验证",
    );
  }
  const llms = q.data?.filter((c) => c.kind === "llm") || [];
  return (
    <section className="panel">
      <div className="panel-heading">
        <h2>
          <ApiOutlined aria-hidden="true" /> LLM 供应商
        </h2>
        {!llms.some((c) => c.managed) && (
          <Button
            icon={<PlusOutlined aria-hidden="true" />}
            onClick={() => open(null)}
          >
            添加 LLM 供应商
          </Button>
        )}
      </div>
      <p className="hint">
        用于链接分析、图片分析和套图规划。其他模型需提供 Anthropic Messages
        兼容接口，能力以验证结果为准。
      </p>
      <DataState
        query={q}
        empty={!llms.length}
        emptyText="配置智能生图使用的 LLM 供应商"
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
                    {c.hasKey ? "密钥已保存" : "需要密钥"}
                  </Tag>
                  <Tag>{c.mode === "compatible" ? "兼容接入" : "Claude"}</Tag>
                </strong>
                {c.managed ? (
                  <p className="hint">由软件授权提供</p>
                ) : (
                  <p className="mono">{c.baseURL}</p>
                )}
                <small>{c.model || "未设置模型"}</small>
                <div className="capability-tags">
                  {(
                    [
                      ["text", "文本"],
                      ["vision", "图片"],
                      ["tools", "工具"],
                      ["streaming", "流式"],
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
                        ? "通过"
                        : c.capabilities?.checkedAt
                          ? "未通过"
                          : "待验证"}
                    </Tag>
                  ))}
                </div>
              </div>
              {c.managed ? (
                <Tag>由软件授权管理</Tag>
              ) : (
                <Space wrap>
                  <Button disabled={!!validating} onClick={() => open(c)}>
                    编辑 / 获取模型
                  </Button>
                  <Button
                    disabled={!c.hasKey || !c.model || !!validating}
                    loading={validating === c.id}
                    onClick={() => validate(c)}
                  >
                    验证 Agent 能力
                  </Button>
                  {c.hasKey && (
                    <Button
                      disabled={!!validating}
                      onClick={() =>
                        actions.confirm(
                          "清除 LLM 密钥？",
                          "新分析将无法启动，模型能力验证会重置。",
                          () =>
                            call("SaveConnection", {
                              ...c,
                              key: "",
                              clearKey: true,
                            }),
                        )
                      }
                    >
                      清除密钥
                    </Button>
                  )}
                  <Button
                    danger
                    disabled={!!validating}
                    aria-label={`删除 LLM 连接 ${c.name}`}
                    icon={<DeleteOutlined aria-hidden="true" />}
                    onClick={() =>
                      actions.confirm(
                        `删除连接“${c.name}”？`,
                        "历史分析与方案保留。",
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
          <span role="status">正在验证模型能力，最多等待 2 分钟</span>
          <Button
            onClick={() => {
              const id = validationID.current;
              validationID.current = null;
              setValidating(null);
              if (id) call("CancelConnectionTest", id).catch(() => {});
            }}
          >
            取消能力验证
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
          message="Agent 能力验证结果"
          description={
            <div className="prewrap">{capResult.details.join("\n")}</div>
          }
        />
      )}
      <Modal
        title={editing ? "编辑 LLM 连接" : "添加 LLM 供应商"}
        open={editing !== undefined}
        onCancel={close}
        onOk={save}
        okText="保存连接"
        cancelText="取消"
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
              获取模型
            </Button>
            {testing && (
              <Button disabled={false} onClick={cancelTest}>
                取消查询
              </Button>
            )}
            <span className="muted">仅查询模型，不调用 LLM</span>
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
                  aria-label="查询到的 LLM 模型"
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
  function open(v: SkillRecord | null) {
    setEditing(v);
    setName(v?.name || "");
    setContent(
      v?.content ||
        "---\nname: my-image-skill\ndescription: 描述什么时候使用这个技能\n---\n\n写下图片制作规则和操作步骤。\n",
    );
    setError("");
    setDirty(false);
  }
  async function importSkill(directory: boolean) {
    await actions.run(async () => {
      const path = await call<string>("ChooseSkillSource", directory);
      if (!path) return;
      try {
        await call("ImportSkill", path, false);
        notification.success({ message: "Skill 已导入" });
      } catch (e) {
        if (errorText(e).includes("已存在")) {
          actions.confirm(
            "覆盖同名 Skill？",
            "现有内容将被替换。正在执行的分析保留已冻结版本。",
            () => call("ImportSkill", path, true),
            true,
            "覆盖导入",
          );
        } else throw e;
      }
    });
  }
  async function save() {
    setError("");
    if (!name.trim()) {
      setError("请填写技能名称");
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
        notification.success({ message: "Skill 已保存" });
      } catch (e) {
        setError(errorText(e));
        throw e;
      }
    });
  }
  function close() {
    if (dirty)
      actions.confirm(
        "放弃未保存的 Skill 修改？",
        "已保存版本仍会保留。",
        async () => setEditing(undefined),
      );
    else setEditing(undefined);
  }
  return (
    <section className="panel">
      <div className="panel-heading">
        <h2>Claude Code 与 Skills</h2>
        <Space wrap>
          <Button onClick={() => runtime.refetch()}>检查运行时</Button>
          <Button onClick={() => open(null)}>新建 Skill</Button>
          <Button
            icon={<UploadOutlined aria-hidden="true" />}
            onClick={() => importSkill(false)}
            disabled={actions.busy}
          >
            导入 ZIP
          </Button>
          <Button onClick={() => importSkill(true)} disabled={actions.busy}>
            导入文件夹
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
      <p className="hint">
        Skills 用于商品分析、套图规划与工艺规则。支持 SKILL.md
        和文本、JSON、图片附件；第一版不运行脚本。
      </p>
      <DataState
        query={q}
        empty={!q.data?.length}
        emptyText="创建或导入你的第一个 Skill"
      >
        <div className="connection-list">
          {q.data?.map((v) => (
            <div className="connection-row" key={v.id}>
              <div className="connection-info">
                <strong>
                  {v.name}{" "}
                  <Tag>
                    {v.source} · v{v.version}
                  </Tag>
                </strong>
                <p>{v.description}</p>
                {!!v.attachments?.length && (
                  <small>{v.attachments.length} 个附件</small>
                )}
              </div>
              <Space>
                <Switch
                  checked={v.enabled}
                  aria-label={`启用技能 ${v.name}`}
                  disabled={actions.busy}
                  onChange={(enabled) =>
                    actions.run(() => call("SetSkillEnabled", v.id, enabled))
                  }
                />
                <Button onClick={() => open(v)}>编辑 Skill</Button>
                <Button
                  danger
                  aria-label={`删除技能 ${v.name}`}
                  icon={<DeleteOutlined aria-hidden="true" />}
                  onClick={() =>
                    actions.confirm(
                      `删除技能“${v.name}”？`,
                      "历史分析保留此技能的内容快照。",
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
        title={editing ? "编辑 Skill" : "新建 Skill"}
        open={editing !== undefined}
        onCancel={close}
        onOk={save}
        okText="保存 Skill"
        cancelText="取消"
        confirmLoading={actions.busy}
        width={800}
        destroyOnHidden
      >
        <label className="field-label" htmlFor="skill-name">
          技能名称
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
