import { useEffect, useState, useRef } from "react";
import {
  App as AntApp,
  Button,
  Select as UISelect,
  Form as UIForm,
  Modal,
  Alert,
  Tag,
  Space,
} from "antd";
import {
  FolderOutlined,
  SettingOutlined,
  PlusOutlined,
  DownloadOutlined,
  DeleteOutlined,
  ReloadOutlined,
  ApiOutlined,
} from "@ant-design/icons";
import {
  call,
  errorText,
  type Connection,
  type Summary,
  type TestResult,
} from "../api";
import { useData, useActions, DataState, Header } from "../ui";
import { LicensePanel, useLicense } from "../License";
import { UpdatePanel } from "../Updates";
import { ShopRules } from "../ShopRules";
import { GatewayFailover, GatewayRow } from "../Gateways";
import {
  LLMConnections,
  AgentSettings,
  ConnectionFields,
} from "../AgentSettings";

export function Settings() {
  const q = useData<Connection[]>(["connections"], "ListConnections");
  const summary = useData<Summary>(["summary"], "Dashboard");
  const license = useLicense();
  const customer = !!license.data && license.data.mode !== "developer";
  const actions = useActions();
  const { notification } = AntApp.useApp();
  const [moving, setMoving] = useState(false);
  async function chooseLocation() {
    const destination = await actions.run(() =>
      call<string>("ChooseDataDirectory"),
    );
    if (!destination) return;
    actions.confirm(
      "更改保存位置？",
      <div>
        <p>
          现有图片、任务和配置会一起迁移，原目录保留。新目录必须是空文件夹。
        </p>
        <p className="hint">当前目录</p>
        <code className="data-path">{summary.data?.dataDir}</code>
        <p className="hint">新目录</p>
        <code className="data-path">{destination}</code>
      </div>,
      async () => {
        setMoving(true);
        try {
          await call("ChangeDataDirectory", destination);
        } finally {
          setMoving(false);
        }
      },
      false,
      "迁移并使用",
    );
  }
  const [editing, setEditing] = useState<Connection | null | undefined>(
    undefined,
  );
  const [form] = UIForm.useForm();
  const [result, setResult] = useState<TestResult | null>(null);
  const [testing, setTesting] = useState(false);
  const [testError, setTestError] = useState("");
  const testRun = useRef<string | null>(null);
  function cancelTest() {
    const id = testRun.current;
    testRun.current = null;
    setTesting(false);
    if (id) call("CancelConnectionTest", id).catch(() => {});
  }
  useEffect(
    () => () => {
      if (testRun.current)
        call("CancelConnectionTest", testRun.current).catch(() => {});
    },
    [],
  );
  function open(c: Connection | null) {
    setEditing(c);
    form.resetFields();
    form.setFieldsValue(
      c
        ? { ...c, key: "" }
        : {
            name: "",
            baseURL: "",
            model: "",
            key: "",
            protocol: "openai-images",
          },
    );
    setResult(null);
    setTestError("");
  }
  async function save() {
    try {
      const values = await form.validateFields();
      const saved = await actions.run(
        () =>
          call<Connection>("SaveConnection", {
            ...values,
            id: editing?.id || "",
            clearKey: false,
          }),
        "连接已保存",
      );
      if (saved) {
        setEditing(undefined);
        form.resetFields();
      }
    } catch {
      form.scrollToField("baseURL");
    }
  }
  async function test() {
    if (testRun.current) return;
    const requestID = crypto.randomUUID();
    testRun.current = requestID;
    setTesting(true);
    setResult(null);
    setTestError("");
    try {
      const v = await form.validateFields(["baseURL", "key"]);
      if (testRun.current !== requestID) return;
      const out = await call<TestResult>("TestConnection", {
        ...form.getFieldsValue(),
        ...v,
        id: editing?.id || "",
        testRequestID: requestID,
      });
      if (testRun.current === requestID) setResult(out);
    } catch (e) {
      if (testRun.current !== requestID) return;
      if (e && typeof e === "object" && "errorFields" in e) {
        const fields = (e as { errorFields: { name: string[] }[] }).errorFields;
        if (fields.length) form.scrollToField(fields[0].name, { focus: true });
      } else setTestError(errorText(e));
    } finally {
      if (testRun.current === requestID) {
        testRun.current = null;
        setTesting(false);
      }
    }
  }
  function close() {
    if (testing) return;
    if (form.isFieldsTouched())
      actions.confirm(
        "放弃未保存的连接修改？",
        "API Key 仅在点击保存后持久化。",
        async () => {
          setEditing(undefined);
          form.resetFields();
        },
      );
    else {
      setEditing(undefined);
      form.resetFields();
    }
  }
  return (
    <>
      <Header
        title="连接"
        sub={
          customer
            ? "授权、线路状态和本地数据。线路由软件提供方配置，无需填写地址或 Key。"
            : "连接图片服务和 AI 模型，管理技能、数据位置和授权。密钥只加密保存在这台电脑上。"
        }
        action={
          customer ? undefined : (
            <Button
              type="primary"
              icon={<PlusOutlined aria-hidden="true" />}
              onClick={() => open(null)}
            >
              添加连接
            </Button>
          )
        }
      />
      <LicensePanel />
      <UpdatePanel />
      <section className="panel">
        <div className="panel-heading">
          <h2>
            <ApiOutlined aria-hidden="true" /> 图片服务
          </h2>
          <span className="muted">Images 兼容协议</span>
        </div>
        <DataState
          query={q}
          empty={!q.data?.some((c) => c.kind !== "llm")}
          emptyText="添加你的第一个图片服务"
          action={
            <Button type="primary" onClick={() => open(null)}>
              填写 Base URL 与 API Key
            </Button>
          }
        >
          <div className="connection-list">
            {q.data
              ?.filter((c) => c.kind !== "llm")
              .map((c) => (
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
                    </strong>
                    {c.managed ? (
                      <p className="hint">由软件授权提供</p>
                    ) : (
                      <p className="mono">{c.baseURL}</p>
                    )}
                    <small>
                      默认模型：{c.model || "未设置，可在工作台填写"}
                    </small>
                    <GatewayRow
                      connection={c}
                      images={(q.data || []).filter((x) => x.kind !== "llm")}
                    />
                  </div>
                  {c.managed ? (
                    <Tag>由软件授权管理</Tag>
                  ) : (
                    <Space wrap>
                      <Button onClick={() => open(c)}>编辑 / 测试</Button>
                      {c.hasKey && (
                        <Button
                          onClick={() =>
                            actions.confirm(
                              "清除此连接的密钥？",
                              "新任务将无法提交。已提交任务保存的加密凭证版本仍用于恢复；密钥不会从任务中返回到界面。",
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
                        aria-label={`删除连接 ${c.name}`}
                        icon={<DeleteOutlined aria-hidden="true" />}
                        onClick={() =>
                          actions.confirm(
                            `删除连接“${c.name}”？`,
                            "需要先停止队列。历史任务与图片保留，重新生成需要重新配置连接。",
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
      </section>
      <GatewayFailover />
      <LLMConnections />
      <ShopRules />
      <AgentSettings />
      <section className="panel storage-panel">
        <div className="panel-heading">
          <h2>本地数据与备份</h2>
          <Tag>本机存储</Tag>
        </div>
        <p>数据位置</p>
        <code className="data-path">{summary.data?.dataDir || "正在读取"}</code>
        <Space wrap className="location-actions">
          <Button
            icon={<FolderOutlined aria-hidden="true" />}
            disabled={!summary.data || moving}
            loading={actions.busy}
            onClick={() =>
              actions.run(() => call("OpenDataDirectory"), "已打开数据目录")
            }
          >
            打开数据位置
          </Button>
          <Button
            icon={<SettingOutlined aria-hidden="true" />}
            disabled={
              !summary.data || !!summary.data.active || moving || actions.busy
            }
            loading={moving}
            onClick={chooseLocation}
          >
            更改保存位置
          </Button>
        </Space>
        <p className="hint">
          可以自选空文件夹作为保存位置，现有数据会一起迁移。新位置立即生效，重启后继续使用。
        </p>
        <p className="hint">
          备份包含模板、任务与图片，不包含 API Key
          或界面缓存。换电脑后重新输入密钥。
        </p>
        <Space wrap>
          <Button
            icon={<DownloadOutlined aria-hidden="true" />}
            loading={actions.busy}
            disabled={moving}
            onClick={() =>
              actions.run(async () => {
                const saved = await call<boolean>("Backup");
                if (saved) return "saved";
                return undefined;
              }, undefined)
            }
          >
            创建备份
          </Button>
          <Button
            icon={<ReloadOutlined aria-hidden="true" />}
            disabled={!!summary.data?.active || moving}
            onClick={() =>
              actions.confirm(
                "从备份恢复数据？",
                "当前数据会保留在恢复前目录。任务和图片将以备份为准，密钥需要重新输入。请先停止并结束活动任务。",
                () => call("Restore"),
                true,
              )
            }
          >
            恢复备份
          </Button>
        </Space>
        <p className="hint">
          商品链接被网站拦截时，创作会用应用内浏览器读取页面，Cookie
          保存在当前数据目录，不进入备份。清除后，下次读取可能需要重新完成验证。
        </p>
        <Space wrap>
          <Button
            icon={<DeleteOutlined aria-hidden="true" />}
            disabled={moving}
            onClick={() =>
              actions.confirm(
                "清除商品页浏览器数据？",
                "将删除应用内浏览器保存的 Cookie 与缓存，不影响图片、任务、连接和密钥。",
                async () => {
                  await call("ClearProductBrowserData");
                  notification.success({ message: "已清除商品页浏览器数据" });
                },
                true,
                "清除",
              )
            }
          >
            清除商品页浏览器数据
          </Button>
        </Space>
        {!!summary.data?.active && (
          <p className="hint">
            当前有活动或等待任务，完成或停止后才能更改保存位置或恢复备份。
          </p>
        )}
      </section>
      <Modal
        title={editing ? "编辑图片服务" : "添加图片服务"}
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
          <ConnectionFields editing={editing} />
          <div className="test-row">
            <Button
              icon={<ApiOutlined aria-hidden="true" />}
              onClick={test}
              loading={testing}
            >
              测试连接 / 获取模型
            </Button>
            {testing && (
              <Button disabled={false} onClick={cancelTest}>
                取消测试
              </Button>
            )}
            <span className="muted">不会提交生图任务</span>
          </div>
          {testing && (
            <p role="status" className="hint">
              正在连接并获取模型，最多等待 30 秒。可以取消测试。
            </p>
          )}
          {testError && <Alert type="error" showIcon message={testError} />}{" "}
          {result && (
            <div className="test-result">
              <Alert
                type={result.ids.length ? "success" : "warning"}
                showIcon
                message={result.auth}
                description={
                  <>
                    {result.models}
                    <br />
                    {result.generation}
                  </>
                }
              />
              {!!result.ids.length && (
                <UISelect
                  className="full-width"
                  aria-label="查询到的模型"
                  showSearch
                  optionFilterProp="label"
                  placeholder="选择一个默认模型"
                  options={result.ids.map((id) => ({ value: id, label: id }))}
                  onChange={(id) => form.setFieldValue("model", id)}
                />
              )}
            </div>
          )}
        </UIForm>
      </Modal>
    </>
  );
}
