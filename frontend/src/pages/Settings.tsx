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
import { useData, useActions, DataState, Header, hostedLineName } from "../ui";
import { LicensePanel, useLicense } from "../License";
import { UpdatePanel } from "../Updates";
import { LanguagePanel } from "../i18n/LanguageSwitch";
import { ShopRules } from "../ShopRules";
import { GatewayFailover, GatewayRow } from "../Gateways";
import {
  LLMConnections,
  AgentSettings,
  ConnectionFields,
} from "../AgentSettings";
import { t } from "../i18n";

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
      t("settings.move.title"),
      <div>
        <p>{t("settings.move.body")}</p>
        <p className="hint">{t("settings.move.current")}</p>
        <code className="data-path">{summary.data?.dataDir}</code>
        <p className="hint">{t("settings.move.new")}</p>
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
      t("settings.move.ok"),
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
        t("settings.saved"),
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
        t("settings.discard.title"),
        t("settings.discard.body"),
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
        title={t("settings.title")}
        sub={customer ? t("settings.sub.hosted") : t("settings.sub.open")}
        action={
          customer ? undefined : (
            <Button
              type="primary"
              icon={<PlusOutlined aria-hidden="true" />}
              onClick={() => open(null)}
            >
              {t("settings.add")}
            </Button>
          )
        }
      />
      <LanguagePanel />
      <LicensePanel />
      <UpdatePanel />
      <section className="panel">
        <div className="panel-heading">
          <h2>
            <ApiOutlined aria-hidden="true" /> {t("settings.images.title")}
          </h2>
          <span className="muted">{t("settings.images.protocol")}</span>
        </div>
        <DataState
          query={q}
          empty={!q.data?.some((c) => c.kind !== "llm")}
          emptyText={t("settings.images.empty")}
          action={
            <Button type="primary" onClick={() => open(null)}>
              {t("settings.images.fill")}
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
                      {hostedLineName(c)}{" "}
                      <Tag color={c.hasKey ? "success" : "warning"}>
                        {c.hasKey
                          ? t("settings.key.saved")
                          : t("settings.key.needed")}
                      </Tag>
                    </strong>
                    {c.managed ? (
                      <p className="hint">{t("settings.managed.by")}</p>
                    ) : (
                      <p className="mono">{c.baseURL}</p>
                    )}
                    <small>
                      {t("settings.defaultModel", {
                        model: c.model || t("settings.defaultModel.unset"),
                      })}
                    </small>
                    <GatewayRow
                      connection={c}
                      images={(q.data || []).filter((x) => x.kind !== "llm")}
                    />
                  </div>
                  {c.managed ? (
                    <Tag>{t("settings.managed.tag")}</Tag>
                  ) : (
                    <Space wrap>
                      <Button onClick={() => open(c)}>
                        {t("settings.editTest")}
                      </Button>
                      {c.hasKey && (
                        <Button
                          onClick={() =>
                            actions.confirm(
                              t("settings.clearKey.title"),
                              t("settings.clearKey.body"),
                              () =>
                                call("SaveConnection", {
                                  ...c,
                                  key: "",
                                  clearKey: true,
                                }),
                            )
                          }
                        >
                          {t("settings.clearKey")}
                        </Button>
                      )}
                      <Button
                        danger
                        aria-label={t("settings.delete.aria", {
                          name: hostedLineName(c),
                        })}
                        icon={<DeleteOutlined aria-hidden="true" />}
                        onClick={() =>
                          actions.confirm(
                            t("settings.delete.title", {
                              name: hostedLineName(c),
                            }),
                            t("settings.delete.body"),
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
          <h2>{t("settings.storage.title")}</h2>
          <Tag>{t("settings.storage.tag")}</Tag>
        </div>
        <p>{t("settings.storage.location")}</p>
        <code className="data-path">
          {summary.data?.dataDir || t("settings.storage.reading")}
        </code>
        <Space wrap className="location-actions">
          <Button
            icon={<FolderOutlined aria-hidden="true" />}
            disabled={!summary.data || moving}
            loading={actions.busy}
            onClick={() =>
              actions.run(
                () => call("OpenDataDirectory"),
                t("settings.storage.opened"),
              )
            }
          >
            {t("settings.storage.open")}
          </Button>
          <Button
            icon={<SettingOutlined aria-hidden="true" />}
            disabled={
              !summary.data || !!summary.data.active || moving || actions.busy
            }
            loading={moving}
            onClick={chooseLocation}
          >
            {t("settings.storage.change")}
          </Button>
        </Space>
        <p className="hint">{t("settings.storage.changeHint")}</p>
        <p className="hint">{t("settings.backup.hint")}</p>
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
            {t("settings.backup.create")}
          </Button>
          <Button
            icon={<ReloadOutlined aria-hidden="true" />}
            disabled={!!summary.data?.active || moving}
            onClick={() =>
              actions.confirm(
                t("settings.restore.title"),
                t("settings.restore.body"),
                () => call("Restore"),
                true,
              )
            }
          >
            {t("settings.restore")}
          </Button>
        </Space>
        <p className="hint">{t("settings.browser.hint")}</p>
        <Space wrap>
          <Button
            icon={<DeleteOutlined aria-hidden="true" />}
            disabled={moving}
            onClick={() =>
              actions.confirm(
                t("settings.browser.clear.title"),
                t("settings.browser.clear.body"),
                async () => {
                  await call("ClearProductBrowserData");
                  notification.success({
                    message: t("settings.browser.cleared"),
                  });
                },
                true,
                t("settings.browser.clear.ok"),
              )
            }
          >
            {t("settings.browser.clear")}
          </Button>
        </Space>
        {!!summary.data?.active && (
          <p className="hint">{t("settings.storage.busy")}</p>
        )}
      </section>
      <Modal
        title={editing ? t("settings.edit.title") : t("settings.add.title")}
        open={editing !== undefined}
        onCancel={close}
        onOk={save}
        okText={t("settings.edit.ok")}
        cancelText={t("settings.cancel")}
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
              {t("settings.test")}
            </Button>
            {testing && (
              <Button disabled={false} onClick={cancelTest}>
                {t("settings.test.cancel")}
              </Button>
            )}
            <span className="muted">{t("settings.test.free")}</span>
          </div>
          {testing && (
            <p role="status" className="hint">
              {t("settings.test.running")}
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
                  aria-label={t("settings.test.models")}
                  showSearch
                  optionFilterProp="label"
                  placeholder={t("settings.test.pick")}
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
