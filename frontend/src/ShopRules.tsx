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
        notification.success({ message: "店铺规则已保存" });
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
        <h2 id="shop-rules-title">店铺规则</h2>
        <Button onClick={() => open(null)}>新建店铺规则</Button>
      </div>
      <p className="hint">
        写下你的店铺每套图都要遵守的要求（模特、尺寸上限、刺绣位置、不能出现的元素等），可以附上参考图或文字资料。启用的规则在创作时优先于所有技能；已开始的制作沿用当时的规则。
      </p>
      <DataState query={q} empty={!q.data?.length} emptyText="还没有店铺规则">
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
                          <span aria-label={`删除附件 ${a.name}`}>×</span>
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
                  aria-label={`启用店铺规则 ${v.name}`}
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
                  添加附件
                </Button>
                <Button onClick={() => open(v)}>编辑</Button>
                <Button
                  danger
                  aria-label={`删除店铺规则 ${v.name}`}
                  icon={<DeleteOutlined aria-hidden="true" />}
                  onClick={() =>
                    actions.confirm(
                      `删除店铺规则“${v.name}”？`,
                      "已开始的制作保留当时的规则。",
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
        title={editing ? "编辑店铺规则" : "新建店铺规则"}
        open={editing !== undefined}
        onCancel={() => setEditing(undefined)}
        onOk={save}
        okText="保存"
        cancelText="取消"
        confirmLoading={actions.busy}
        width={720}
        destroyOnHidden
      >
        <label className="field-label" htmlFor="shop-rule-name">
          规则名称
        </label>
        <Input
          id="shop-rule-name"
          value={name}
          maxLength={100}
          onChange={(e) => setName(e.target.value)}
        />
        <label className="field-label" htmlFor="shop-rule-text">
          规则内容
        </label>
        <Input.TextArea
          id="shop-rule-text"
          value={text}
          autoSize={{ minRows: 8, maxRows: 20 }}
          placeholder="例如：所有图片使用欧美模特；刺绣宽度不超过 25 cm；全图清晰，不要虚化背景。"
          onChange={(e) => setText(e.target.value)}
        />
        {error && <Alert type="error" showIcon message={error} />}
      </Modal>
    </section>
  );
}
