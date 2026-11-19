import { useState } from "react";
import {
  Alert,
  App as AntApp,
  Button,
  Input,
  Modal,
  Space,
  Switch,
  Tag,
} from "antd";
import { DeleteOutlined, PaperClipOutlined } from "@ant-design/icons";
import { call, errorText } from "./api";
import { DataState, useActions, useData } from "./ui";
import { t } from "./i18n";

export type ShopRule = {
  id: string;
  name: string;
  text: string;
  enabled: boolean;
  version: number;
  updatedAt: string;
  attachments: { name: string; path: string }[];
};

/** The shop's own house rules: given to the Agent ahead of the skills. */
export function ShopRules() {
  const q = useData<ShopRule[]>(["shop-rules"], "ListShopRules");
  const actions = useActions();
  const { notification } = AntApp.useApp();
  const [editing, setEditing] = useState<ShopRule | null | undefined>();
  const [name, setName] = useState("");
  const [text, setText] = useState("");
  const [error, setError] = useState("");
  function open(v: ShopRule | null) {
    setEditing(v);
    setName(v?.name || "");
    setText(v?.text || "");
    setError("");
  }
  async function save() {
    setError("");
    await actions.run(async () => {
      try {
        await call("SaveShopRule", {
          ...(editing || {}),
          id: editing?.id || "",
          name,
          text,
          enabled: editing?.enabled ?? true,
        });
        setEditing(undefined);
        notification.success({ message: t("shopRules.saved") });
      } catch (e) {
        setError(errorText(e));
        throw e;
      }
    });
  }
  return (
    <section
      className="panel"
      id="shop-rules"
      aria-labelledby="shop-rules-title"
    >
      <div className="panel-heading">
        <h2 id="shop-rules-title">{t("shopRules.title")}</h2>
        <Button onClick={() => open(null)}>{t("shopRules.new")}</Button>
      </div>
      <p className="hint">{t("shopRules.hint")}</p>
      <DataState query={q} empty={!q.data?.length} emptyText={t("shopRules.empty")}>
        <div className="connection-list">
          {q.data?.map((v) => (
            <div className="connection-row" key={v.id}>
              <div className="connection-info">
                <strong>
                  {v.name} <Tag>v{v.version}</Tag>
                </strong>
                <p className="shop-rule-preview">{v.text}</p>
                {v.attachments.length > 0 && (
                  <Space wrap size={4}>
                    {v.attachments.map((a) => (
                      <Tag
                        key={a.name}
                        closable
                        closeIcon={
                          <span
                            aria-label={t("shopRules.removeFile", {
                              name: a.name,
                            })}
                          >
                            ×
                          </span>
                        }
                        onClose={(e) => {
                          e.preventDefault();
                          actions.run(() =>
                            call("RemoveShopRuleFile", v.id, a.name),
                          );
                        }}
                      >
                        {a.name}
                      </Tag>
                    ))}
                  </Space>
                )}
              </div>
              <Space>
                <Switch
                  checked={v.enabled}
                  aria-label={t("shopRules.enable", { name: v.name })}
                  disabled={actions.busy}
                  onChange={(on) =>
                    actions.run(() => call("SetShopRuleEnabled", v.id, on))
                  }
                />
                <Button
                  icon={<PaperClipOutlined aria-hidden="true" />}
                  onClick={() =>
                    actions.run(() => call("AddShopRuleFiles", v.id))
                  }
                >
                  {t("shopRules.addFiles")}
                </Button>
                <Button onClick={() => open(v)}>{t("shopRules.edit")}</Button>
                <Button
                  danger
                  aria-label={t("shopRules.delete", { name: v.name })}
                  icon={<DeleteOutlined aria-hidden="true" />}
                  onClick={() =>
                    actions.confirm(
                      t("shopRules.delete.title", { name: v.name }),
                      t("shopRules.delete.body"),
                      () => call("DeleteShopRule", v.id),
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
        title={editing ? t("shopRules.editTitle") : t("shopRules.new")}
        open={editing !== undefined}
        onCancel={() => setEditing(undefined)}
        onOk={save}
        okText={t("shopRules.save")}
        cancelText={t("shopRules.cancel")}
        confirmLoading={actions.busy}
        width={720}
        destroyOnHidden
      >
        <label className="field-label" htmlFor="shop-rule-name">
          {t("shopRules.name")}
        </label>
        <Input
          id="shop-rule-name"
          value={name}
          maxLength={100}
          onChange={(e) => setName(e.target.value)}
        />
        <label className="field-label" htmlFor="shop-rule-text">
          {t("shopRules.text")}
        </label>
        <Input.TextArea
          id="shop-rule-text"
          value={text}
          autoSize={{ minRows: 8, maxRows: 20 }}
          placeholder={t("shopRules.placeholder")}
          onChange={(e) => setText(e.target.value)}
        />
        {error && <Alert type="error" showIcon message={error} />}
      </Modal>
    </section>
  );
}
