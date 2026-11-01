import { useState } from "react";
import {
  Alert,
  App as AntApp,
  Button,
  Checkbox,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Table,
  Tag,
} from "antd";
import type { CheckboxProps } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import { call } from "../api";
import { DataState, Header, useActions } from "../ui";
import { useAdminState } from "./AdminApp";
import {
  kindLabel,
  quotaWarning,
  slotName,
  statusLabel,
  useLabel,
  type ImportResult,
  type PoolKey,
  type Upstream,
} from "./types";

export function KeyPool() {
  const state = useAdminState();
  const actions = useActions();
  const { modal, notification } = AntApp.useApp();
  const [editing, setEditing] = useState<Partial<Upstream> | null>(null);
  const [upForm] = Form.useForm();
  const [importForm] = Form.useForm();
  const [filter, setFilter] = useState({ kind: "", use: "", status: "" });
  const s = state.data;
  const upstreamName = (id: string) =>
    s?.upstreams.find((u) => u.id === id)?.name || "—";
  const customerName = (id?: string) =>
    s?.customers.find((c) => c.id === id)?.name || "";
  const keys = (s?.keys || []).filter(
    (k) =>
      (!filter.kind || k.kind === filter.kind) &&
      (!filter.use || k.use === filter.use) &&
      (!filter.status || k.status === filter.status),
  );
  // Bulk actions work on the ticked rows that the current filter shows.
  const [selected, setSelected] = useState<string[]>([]);
  const picked = keys.filter((k) => selected.includes(k.id));
  const pickedAssigned = picked.filter((k) => k.status === "assigned").length;
  const pickedDelivered = picked.filter(
    (k) => k.status !== "assigned" && (k.delivered || k.deliveredBefore),
  ).length;
  const changeFilter = (f: typeof filter) => {
    setFilter(f);
    setSelected([]);
  };
  function bulk(kind: "disable" | "delete") {
    const ids = picked.map((k) => k.id);
    const n = ids.length;
    modal.confirm({
      title: kind === "delete" ? `删除 ${n} 个 Key？` : `停用 ${n} 个 Key？`,
      content:
        kind === "delete"
          ? "这些 Key 会从 Key 池和本机加密存储中删除，无法恢复；以后需要时可以重新导入。"
          : "停用后不会再分配给任何客户，需要时可以逐个重新启用。已停用的 Key 保留原来的停用原因。",
      okText: kind === "delete" ? "删除" : "停用",
      okButtonProps: { danger: true },
      cancelText: "取消",
      onOk: async () => {
        const ok = await actions.run(
          () =>
            kind === "delete"
              ? call("DeleteKeys", ids)
              : call("DisableKeys", ids, "手动停用"),
          kind === "delete" ? `已删除 ${n} 个 Key` : `已停用 ${n} 个 Key`,
        );
        if (ok) setSelected([]);
      },
    });
  }
  const counts = (kind: string, use: string) =>
    (s?.keys || []).filter(
      (k) => k.kind === kind && k.use === use && k.status === "available",
    ).length;
  return (
    <>
      <Header
        title="Key 池"
        sub="按上游导入 Key，标注主用或备用。试用和正式使用是同一套 Key；同一个 Key 只能导入一次、只能分给一位客户。"
      />
      <section className="panel" aria-labelledby="up-title">
        <div className="panel-heading">
          <h2 id="up-title">上游</h2>
          <Button
            icon={<PlusOutlined aria-hidden="true" />}
            onClick={() => {
              upForm.resetFields();
              upForm.setFieldsValue({
                kind: "image",
                mode: "compatible",
                authMode: "api-key",
                perKeyQuota: false,
              });
              setEditing({});
            }}
          >
            添加上游
          </Button>
        </div>
        <DataState
          query={state}
          empty={!s?.upstreams.length}
          emptyText="先添加一个上游（例如幻灵、1 Route）"
        >
          <Table<Upstream>
            rowKey="id"
            dataSource={s?.upstreams}
            pagination={false}
            scroll={{ x: 720 }}
            columns={[
              { title: "名称", dataIndex: "name" },
              { title: "类型", render: (_, u) => kindLabel[u.kind] },
              {
                title: "地址",
                render: (_, u) => <code className="mono">{u.baseURL}</code>,
              },
              { title: "模型", dataIndex: "model" },
              {
                title: "单 Key 额度",
                render: (_, u) =>
                  u.perKeyQuota ? (
                    <Tag color="success">支持</Tag>
                  ) : (
                    <Tag color="warning">不支持</Tag>
                  ),
              },
              {
                title: "操作",
                render: (_, u) => (
                  <Space>
                    <Button
                      size="small"
                      onClick={() => {
                        upForm.setFieldsValue(u);
                        setEditing(u);
                      }}
                    >
                      编辑
                    </Button>
                    <Button
                      size="small"
                      danger
                      onClick={() =>
                        modal.confirm({
                          title: `删除上游“${u.name}”？`,
                          content:
                            "此上游下未分配、未交付的 Key 会一起删除。已分配或交付过的 Key 要保留记录，有这类 Key 时不能删除。",
                          okText: "删除",
                          cancelText: "取消",
                          onOk: () =>
                            actions.run(() => call("DeleteUpstream", u.id)),
                        })
                      }
                    >
                      删除
                    </Button>
                  </Space>
                ),
              },
            ]}
          />
        </DataState>
      </section>
      <section className="panel" aria-labelledby="import-title">
        <div className="panel-heading">
          <h2 id="import-title">导入 Key</h2>
          <span className="muted">
            可用：图片 主用 {counts("image", "primary")} / 备用{" "}
            {counts("image", "backup")}；LLM 主用 {counts("llm", "primary")} /
            备用 {counts("llm", "backup")}
          </span>
        </div>
        <p className="hint">
          每位客户需要：图片主用 3、备用 3；LLM 主用 1、备用
          1。交付前请在上游后台把这些 Key 的额度设为试用额度（图片线路 1 约 22
          张图的金额，其余设为最低），收款后再调高。
        </p>
        <Form
          form={importForm}
          layout="vertical"
          initialValues={{ use: "primary" }}
          onFinish={(v) =>
            actions.run(async () => {
              const r = await call<ImportResult>(
                "ImportKeys",
                v.upstreamID,
                v.use,
                v.keys,
                v.quotaNote || "",
              );
              notification.success({
                message: `已导入 ${r.added} 个`,
                description:
                  r.duplicates || r.invalid
                    ? `跳过重复 ${r.duplicates} 个、格式不对 ${r.invalid} 个`
                    : undefined,
              });
              importForm.setFieldsValue({ keys: "" });
            })
          }
        >
          <Space wrap align="start">
            <Form.Item
              name="upstreamID"
              label="上游"
              // A custom rule: antd would put aria-required on the Select wrapper div.
              rules={[
                {
                  validator: (_, v) =>
                    v
                      ? Promise.resolve()
                      : Promise.reject(new Error("请选择上游")),
                },
              ]}
            >
              <Select
                style={{ width: 200 }}
                options={(s?.upstreams || []).map((u) => ({
                  value: u.id,
                  label: `${u.name}（${kindLabel[u.kind]}）`,
                }))}
              />
            </Form.Item>
            <Form.Item name="use" label="用途">
              <Select
                style={{ width: 140 }}
                options={[
                  { value: "primary", label: "主用" },
                  { value: "backup", label: "备用" },
                ]}
              />
            </Form.Item>
            <Form.Item name="quotaNote" label="额度备注">
              <Input
                style={{ width: 240 }}
                placeholder="例如：¥300 / 约 22 张"
              />
            </Form.Item>
          </Space>
          <Form.Item noStyle shouldUpdate>
            {() => {
              const u = s?.upstreams.find(
                (x) => x.id === importForm.getFieldValue("upstreamID"),
              );
              return u && !u.perKeyQuota ? (
                <Alert
                  type="warning"
                  showIcon
                  className="license-notice"
                  message="这个上游不能限制单个 Key"
                  description={quotaWarning(u)}
                />
              ) : null;
            }}
          </Form.Item>
          <Form.Item
            name="keys"
            label="Key（每行一个）"
            rules={[{ required: true, message: "请粘贴 Key" }]}
          >
            <Input.TextArea rows={4} spellCheck={false} autoComplete="off" />
          </Form.Item>
          <Button type="primary" htmlType="submit">
            导入
          </Button>
        </Form>
      </section>
      <section className="panel" aria-labelledby="keys-title">
        <div className="panel-heading">
          <h2 id="keys-title">全部 Key</h2>
          <Space wrap>
            <Select
              aria-label="按类型筛选"
              value={filter.kind}
              style={{ width: 110 }}
              onChange={(kind) => changeFilter({ ...filter, kind })}
              options={[
                { value: "", label: "全部类型" },
                { value: "image", label: "图片" },
                { value: "llm", label: "LLM" },
              ]}
            />
            <Select
              aria-label="按用途筛选"
              value={filter.use}
              style={{ width: 110 }}
              onChange={(use) => changeFilter({ ...filter, use })}
              options={[
                { value: "", label: "全部用途" },
                { value: "primary", label: "主用" },
                { value: "backup", label: "备用" },
              ]}
            />
            <Select
              aria-label="按状态筛选"
              value={filter.status}
              style={{ width: 110 }}
              onChange={(status) => changeFilter({ ...filter, status })}
              options={[
                { value: "", label: "全部状态" },
                { value: "available", label: "可用" },
                { value: "assigned", label: "已分配" },
                { value: "disabled", label: "已停用" },
              ]}
            />
          </Space>
        </div>
        {picked.length > 0 && (
          <div className="bulk-bar" role="group" aria-label="批量操作">
            <strong>已选 {picked.length} 个 Key</strong>
            <Button
              size="small"
              danger
              disabled={pickedAssigned > 0}
              onClick={() => bulk("disable")}
            >
              停用所选
            </Button>
            <Button
              size="small"
              danger
              disabled={pickedAssigned > 0 || pickedDelivered > 0}
              onClick={() => bulk("delete")}
            >
              删除所选
            </Button>
            <Button size="small" onClick={() => setSelected([])}>
              取消选择
            </Button>
            {(pickedAssigned > 0 || pickedDelivered > 0) && (
              <span className="muted">
                {[
                  pickedAssigned > 0 &&
                    `${pickedAssigned} 个已分配给客户，先在客户页撤下后才能停用或删除`,
                  pickedDelivered > 0 &&
                    `${pickedDelivered} 个交付过，只能停用、不能删除`,
                ]
                  .filter(Boolean)
                  .join("；")}
              </span>
            )}
          </div>
        )}
        <div
          className="table-scroll"
          tabIndex={0}
          role="region"
          aria-label="Key 列表，可左右滚动"
        >
          <Table<PoolKey>
            rowKey="id"
            dataSource={keys}
            rowSelection={{
              selectedRowKeys: picked.map((k) => k.id),
              onChange: (ids) => setSelected(ids as string[]),
              selections: [Table.SELECTION_ALL, Table.SELECTION_NONE],
              getTitleCheckboxProps: () => ({ "aria-label": "选择本页 Key" }),
              // antd passes aria-* through to the input but its type omits them.
              getCheckboxProps: (k) =>
                ({
                  "aria-label": `选择 …${k.tail}`,
                }) as unknown as CheckboxProps,
            }}
            style={{ minWidth: 980 }}
            pagination={
              keys.length > 20
                ? { pageSize: 20, showSizeChanger: false }
                : false
            }
            locale={{ emptyText: "没有符合条件的 Key" }}
            columns={[
              { title: "上游", render: (_, k) => upstreamName(k.upstreamID) },
              {
                title: "类型",
                render: (_, k) => `${useLabel[k.use]} · ${kindLabel[k.kind]}`,
              },
              {
                title: "Key",
                render: (_, k) => <code className="mono">…{k.tail}</code>,
              },
              {
                title: "状态",
                render: (_, k) => (
                  <span className="monitor-source">
                    <Tag
                      color={
                        k.status === "available"
                          ? "success"
                          : k.status === "assigned"
                            ? "processing"
                            : "default"
                      }
                    >
                      {statusLabel[k.status]}
                    </Tag>
                    {k.deliveredBefore && (
                      <Tag
                        color="warning"
                        title="这个 Key 曾随交付包发给客户，之后被重新启用"
                      >
                        曾交付
                      </Tag>
                    )}
                    {k.status === "disabled" && k.disabledFor && (
                      <span className="muted">{k.disabledFor}</span>
                    )}
                  </span>
                ),
              },
              {
                title: "分配给",
                render: (_, k) =>
                  k.customerID
                    ? `${customerName(k.customerID)} · ${slotName(k.slot || "")}`
                    : "—",
              },
              {
                title: "额度备注",
                render: (_, k) => {
                  const u = s?.upstreams.find((x) => x.id === k.upstreamID);
                  return (
                    <span className="monitor-source">
                      {k.quotaNote || "—"}
                      {u && !u.perKeyQuota && (
                        <Tag color="warning" title={quotaWarning(u)}>
                          无单 Key 额度
                        </Tag>
                      )}
                    </span>
                  );
                },
              },
              {
                title: "能力",
                render: (_, k) =>
                  k.kind !== "llm" ? (
                    "—"
                  ) : k.verified ? (
                    <Tag color="success">已验证</Tag>
                  ) : (
                    <Button
                      size="small"
                      onClick={() =>
                        modal.confirm({
                          title: "验证这个 LLM Key 的能力？",
                          content:
                            "会用这个 Key 发 4 个很短的 LLM 请求（文本、图片、工具、流式），产生少量费用。客户版的智能生图只使用验证通过的 LLM 线路。",
                          okText: "验证（产生少量费用）",
                          cancelText: "取消",
                          onOk: () =>
                            actions.run(
                              () => call("VerifyLLM", k.id),
                              "能力验证通过",
                            ),
                        })
                      }
                    >
                      验证能力
                    </Button>
                  ),
              },
              {
                title: "操作",
                render: (_, k) => (
                  <Space>
                    <Button
                      size="small"
                      onClick={() => {
                        let note = k.quotaNote || "";
                        modal.confirm({
                          title: "额度备注",
                          content: (
                            <Input
                              aria-label="额度备注"
                              defaultValue={note}
                              onChange={(e) => (note = e.target.value)}
                            />
                          ),
                          okText: "保存",
                          cancelText: "取消",
                          onOk: () =>
                            actions.run(() => call("SetKeyNote", k.id, note)),
                        });
                      }}
                    >
                      备注
                    </Button>
                    {s?.upstreams.find((x) => x.id === k.upstreamID)
                      ?.consoleURL && (
                      <Button
                        size="small"
                        onClick={() =>
                          actions.run(
                            () => call("OpenConsole", k.upstreamID),
                            undefined,
                          )
                        }
                      >
                        打开后台
                      </Button>
                    )}
                    {k.status === "available" && (
                      <Button
                        size="small"
                        onClick={() =>
                          actions.run(
                            () =>
                              call(
                                "SetKeyUse",
                                k.id,
                                k.use === "primary" ? "backup" : "primary",
                              ),
                            k.use === "primary" ? "已改为备用" : "已改为主用",
                          )
                        }
                      >
                        {k.use === "primary" ? "改为备用" : "改为主用"}
                      </Button>
                    )}
                    {k.status === "available" && (
                      <Button
                        size="small"
                        danger
                        onClick={() =>
                          modal.confirm({
                            title: "停用这个 Key？",
                            content: "停用后不会再分配给任何客户。",
                            okText: "停用",
                            cancelText: "取消",
                            onOk: () =>
                              actions.run(() =>
                                call("DisableKey", k.id, "手动停用"),
                              ),
                          })
                        }
                      >
                        停用
                      </Button>
                    )}
                    {k.status === "disabled" && (
                      <Button
                        size="small"
                        onClick={() =>
                          modal.confirm({
                            title: "重新启用这个 Key？",
                            content: k.delivered ? (
                              <>
                                <p>
                                  这个 Key 已随交付包发给客户（
                                  {k.disabledFor || "已停用"}
                                  ）。拿到过那个交付包的人仍能从软件里取出并使用它；重新启用后再分给别的客户，双方会共用这个
                                  Key 的额度和账单。
                                </p>
                                <p>
                                  只在确定那个交付包没有发出去、或已在上游后台确认这个
                                  Key
                                  没被别人使用时重新启用。否则请在上游后台新建
                                  Key 再导入。
                                </p>
                              </>
                            ) : (
                              "重新启用后可以再分配给客户。请先确认上游后台没有停用或删除这个 Key。"
                            ),
                            okText: "重新启用",
                            okButtonProps: { danger: k.delivered },
                            cancelText: "取消",
                            onOk: () =>
                              actions.run(
                                () => call("EnableKey", k.id),
                                "Key 已重新启用",
                              ),
                          })
                        }
                      >
                        重新启用
                      </Button>
                    )}
                  </Space>
                ),
              },
            ]}
          />
        </div>
      </section>
      <Modal
        title={editing?.id ? "编辑上游" : "添加上游"}
        open={!!editing}
        okText="保存"
        cancelText="取消"
        onCancel={() => setEditing(null)}
        onOk={async () => {
          const v = await upForm.validateFields();
          const ok = await actions.run(
            () => call("SaveUpstream", { ...editing, ...v }),
            "上游已保存",
          );
          if (ok) setEditing(null);
        }}
      >
        <Form form={upForm} layout="vertical">
          <Form.Item
            name="name"
            label="名称"
            rules={[{ required: true, message: "请填写名称" }]}
          >
            <Input />
          </Form.Item>
          <Form.Item name="kind" label="类型">
            <Select
              disabled={!!editing?.id}
              options={[
                { value: "image", label: "图片中转站" },
                { value: "llm", label: "LLM 供应商" },
              ]}
            />
          </Form.Item>
          <Form.Item
            name="baseURL"
            label="API 地址"
            rules={[{ required: true, message: "请填写地址" }]}
          >
            <Input placeholder="https://example.com/v1" />
          </Form.Item>
          <Form.Item
            name="model"
            label="模型"
            rules={[{ required: true, message: "请填写模型" }]}
          >
            <Input placeholder="gpt-image-2" />
          </Form.Item>
          <Form.Item name="perKeyQuota" valuePropName="checked">
            <Checkbox>
              可以给单个 Key 设硬额度（例如 Sub2API：幻灵、1 Route）
            </Checkbox>
          </Form.Item>
          <Form.Item
            name="usageURL"
            label="剩余额度查询地址（可选；Sub2API 为 …/v1/usage，1 Route 用 https://api.1route.dev/v1/usage）"
          >
            <Input placeholder="留空则按 API 地址 + /usage" />
          </Form.Item>
          <Form.Item name="consoleURL" label="后台地址（可选，用于手动设额度）">
            <Input placeholder="https://example.com/keys" />
          </Form.Item>
          <Form.Item noStyle shouldUpdate>
            {() =>
              upForm.getFieldValue("kind") === "llm" && (
                <Space wrap>
                  <Form.Item name="mode" label="接入方式">
                    <Select
                      style={{ width: 160 }}
                      options={[
                        { value: "compatible", label: "兼容接入" },
                        { value: "claude", label: "Claude" },
                      ]}
                    />
                  </Form.Item>
                  <Form.Item name="authMode" label="认证方式">
                    <Select
                      style={{ width: 160 }}
                      options={[
                        { value: "api-key", label: "x-api-key" },
                        { value: "bearer", label: "Bearer" },
                      ]}
                    />
                  </Form.Item>
                  <Form.Item name="maxOutputTokens" label="输出上限">
                    <InputNumber min={256} max={65536} placeholder="32768" />
                  </Form.Item>
                </Space>
              )
            }
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}
