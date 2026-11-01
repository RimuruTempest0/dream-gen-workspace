import { useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  Alert,
  App as AntApp,
  Button,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
} from "antd";
import {
  ArrowDownOutlined,
  ArrowUpOutlined,
  CopyOutlined,
  EyeOutlined,
  FolderOpenOutlined,
  PlusOutlined,
} from "@ant-design/icons";
import { call, errorText } from "../api";
import { DataState, Header, useActions, time } from "../ui";
import { useAdminState } from "./AdminApp";
import {
  allSlots,
  backupImageSlots,
  kindLabel,
  quotaWarning,
  requiredSlots,
  slotName,
  slotNeeds,
  useLabel,
  type Customer,
  type Delivery,
} from "./types";

function filled(c: Customer) {
  return allSlots.filter((s) => c.slots[s]).length;
}
function hasBackupImage(c: Customer) {
  return backupImageSlots.some((s) => c.slots[s]);
}
// A package needs image line 1, LLM line 1 and at least one backup image line.
function ready(c: Customer) {
  return requiredSlots.every((s) => c.slots[s]) && hasBackupImage(c);
}
// Image line 1 and LLM line 1 are the main lines; image lines 2–6 are backups of
// which at least one needs a key; LLM line 2 is optional.
function slotNeed(slot: string) {
  const kind = kindLabel[slotNeeds(slot)[0]];
  if (requiredSlots.includes(slot)) return `${kind} · 主线路 · 必需`;
  if (backupImageSlots.includes(slot)) return `${kind} · 备用 · 至少一条`;
  return `${kind} · 备用 · 可选`;
}

export function Customers() {
  const state = useAdminState();
  const actions = useActions();
  const nav = useNavigate();
  const [open, setOpen] = useState(false);
  const [form] = Form.useForm();
  return (
    <>
      <Header
        title="客户"
        sub="每位客户一个软件标识和激活码；图片线路 1、至少一条备用图片线路（2–6）和 LLM 线路 1 分配好 Key 即可生成交付包，其余线路可选。"
        action={
          <Button
            type="primary"
            icon={<PlusOutlined aria-hidden="true" />}
            onClick={() => {
              form.resetFields();
              setOpen(true);
            }}
          >
            新建客户
          </Button>
        }
      />
      <section className="panel">
        <DataState
          query={state}
          empty={!state.data?.customers.length}
          emptyText="还没有客户"
        >
          <Table<Customer>
            rowKey="id"
            dataSource={state.data?.customers}
            pagination={false}
            scroll={{ x: 760 }}
            columns={[
              {
                title: "客户",
                render: (_, c) => (
                  <Link to={`/customer/${c.id}`}>{c.name}</Link>
                ),
              },
              {
                title: "软件标识",
                render: (_, c) => <code className="mono">{c.softwareID}</code>,
              },
              {
                title: "线路",
                render: (_, c) => (
                  <Tag color={ready(c) ? "success" : "warning"}>
                    {filled(c)} / {allSlots.length}
                  </Tag>
                ),
              },
              {
                title: "最近交付",
                render: (_, c) => {
                  const last = c.deliveries[c.deliveries.length - 1];
                  return last ? `${last.version} · ${time(last.at)}` : "未交付";
                },
              },
              {
                title: "状态",
                render: (_, c) =>
                  c.changed && c.deliveries.length ? (
                    <Tag color="warning">线路有变更，需重新打包</Tag>
                  ) : !c.deliveries.length ? (
                    <Tag>待交付</Tag>
                  ) : c.paid ? (
                    <Tag color="success">已收款</Tag>
                  ) : (
                    <Tag color="processing">试用中</Tag>
                  ),
              },
            ]}
          />
        </DataState>
      </section>
      <Modal
        title="新建客户"
        open={open}
        okText="创建"
        cancelText="取消"
        onCancel={() => setOpen(false)}
        onOk={async () => {
          const v = await form.validateFields();
          const c = await actions.run(
            () => call<Customer>("CreateCustomer", v.name, v.trialImages || 20),
            "已创建客户，已生成软件标识和激活码",
          );
          if (c) {
            setOpen(false);
            nav(`/customer/${c.id}`);
          }
        }}
      >
        <Form form={form} layout="vertical" initialValues={{ trialImages: 20 }}>
          <Form.Item
            name="name"
            label="客户名称"
            rules={[{ required: true, message: "请填写客户名称" }]}
          >
            <Input autoFocus />
          </Form.Item>
          <Form.Item name="trialImages" label="试用张数">
            <InputNumber min={1} max={200} style={{ width: 140 }} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}

function ActivationCode({ customer }: { customer: Customer }) {
  const [code, setCode] = useState("");
  const { notification } = AntApp.useApp();
  return (
    <div className="admin-code">
      <code className="mono" aria-label="激活码">
        {code || "•••••-•••••-•••••-•••••-•••••"}
      </code>
      <Space>
        <Button
          icon={<EyeOutlined aria-hidden="true" />}
          onClick={async () =>
            setCode(code ? "" : await call<string>("RevealCode", customer.id))
          }
        >
          {code ? "隐藏" : "显示"}
        </Button>
        <Button
          icon={<CopyOutlined aria-hidden="true" />}
          onClick={async () => {
            const value =
              code || (await call<string>("RevealCode", customer.id));
            await navigator.clipboard.writeText(value);
            notification.success({ message: "激活码已复制" });
          }}
        >
          复制
        </Button>
      </Space>
    </div>
  );
}

export function CustomerDetail() {
  const { id } = useParams();
  const state = useAdminState();
  const actions = useActions();
  const { modal } = AntApp.useApp();
  const nav = useNavigate();
  const c = state.data?.customers.find((x) => x.id === id);
  const [version, setVersion] = useState("");
  const [log, setLog] = useState<string[]>([]);
  const [building, setBuilding] = useState(false);
  const [notes] = Form.useForm();
  const [event, setEvent] = useState("");
  const logRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    call<string>("CurrentVersion").then((v) => setVersion((old) => old || v));
  }, []);
  useEffect(() => {
    const offLog = window.runtime?.EventsOn("build:log", (line: string) =>
      setLog((l) => [...l, line].slice(-200)),
    );
    const offDone = window.runtime?.EventsOn(
      "build:done",
      (r: { ok: boolean; error?: string; delivery?: Delivery }) => {
        setBuilding(false);
        setLog((l) => [...l, r.ok ? "交付包已生成。" : "失败：" + r.error]);
        state.refetch();
      },
    );
    return () => {
      offLog?.();
      offDone?.();
    };
  }, []);
  useEffect(() => {
    logRef.current?.scrollTo({ top: logRef.current.scrollHeight });
  }, [log]);
  useEffect(() => {
    if (c)
      notes.setFieldsValue({
        planNote: c.planNote,
        quotaNote: c.quotaNote,
        trialImages: c.trialImages,
      });
  }, [c?.id]);
  if (!c)
    return (
      <DataState query={state} empty emptyText="客户不存在">
        {null}
      </DataState>
    );
  const keys = state.data?.keys || [];
  const upstream = (uid: string) =>
    state.data?.upstreams.find((u) => u.id === uid)?.name || "—";
  const complete = ready(c);
  // Lines whose upstream cannot cap a single key (e.g. DeepSeek official): every
  // key ships before payment, so only its own account balance limits it.
  const uncapped = allSlots
    .map((slot) => {
      const k = keys.find((x) => x.id === c.slots[slot]);
      const u = state.data?.upstreams.find((x) => x.id === k?.upstreamID);
      return k && u && !u.perKeyQuota ? { slot, u } : null;
    })
    .filter((x) => x !== null);
  const uncappedUpstreams = [
    ...new Map(uncapped.map((x) => [x.u.id, x.u])).values(),
  ];
  function assign(slot: string, keyID: string) {
    const replacing = !!c!.slots[slot];
    const go = () =>
      actions.run(
        () => call("AssignKey", c!.id, slot, keyID),
        replacing ? "已更换 Key" : "已分配",
      );
    if (!replacing) return go();
    modal.confirm({
      title: `更换${slotName(slot)}的 Key？`,
      content:
        "已交付过的旧 Key 会被永久停用，不会再分给其他客户。需要重新生成交付包，客户覆盖升级后自动换上新 Key，激活码不变。",
      okText: "更换",
      cancelText: "取消",
      onOk: go,
    });
  }
  function removeCustomer() {
    const delivered = allSlots.filter(
      (s) => keys.find((x) => x.id === c!.slots[s])?.delivered,
    ).length;
    modal.confirm({
      title: `删除客户“${c!.name}”？`,
      content: (
        <>
          <p>
            客户、激活码和全部记录一并删除，无法恢复；以后不能再给这位客户补发激活码或生成升级包。
          </p>
          <p>
            {delivered > 0
              ? `已交付过的 ${delivered} 个 Key 在客户的软件里，会在 Key 池中永久停用、不再分给别人；客户的软件仍能用到它们的额度用完为止，要立即停用请到上游后台停用这些 Key。`
              : "这位客户还没有交付过，分配的 Key 全部回到 Key 池。"}
            {delivered > 0 && "从未交付的 Key 回到 Key 池。"}
          </p>
          <p>已生成的交付包文件不会删除。</p>
        </>
      ),
      okText: "删除客户",
      okButtonProps: { danger: true },
      cancelText: "取消",
      onOk: () =>
        actions.run(async () => {
          await call("DeleteCustomer", c!.id);
          nav("/");
        }, "已删除客户"),
    });
  }
  return (
    <>
      <Header
        title={c.name}
        sub={`软件标识 ${c.softwareID} · 创建于 ${time(c.createdAt)}`}
        action={
          <Space size="middle">
            <Link to="/">返回客户列表</Link>
            <Button danger onClick={removeCustomer}>
              删除客户
            </Button>
          </Space>
        }
      />
      {c.changed && c.deliveries.length > 0 && (
        <Alert
          className="license-notice"
          type="warning"
          showIcon
          message="线路有变更：重新生成交付包后，客户覆盖升级即可生效（激活码不变）。"
        />
      )}
      <section className="panel" aria-labelledby="code-title">
        <div className="panel-heading">
          <h2 id="code-title">激活码</h2>
        </div>
        <ActivationCode customer={c} />
        <p className="hint">
          激活码只能打开这位客户的软件，可随时查看或补发。请在收款并调高额度后再通过私下渠道发给客户。
        </p>
      </section>
      <section className="panel" aria-labelledby="flow-title">
        <div className="panel-heading">
          <h2 id="flow-title">交付与收款</h2>
          <Tag color={c.paid ? "success" : "processing"}>
            {c.paid ? "已收款 · 可发激活码" : "试用中"}
          </Tag>
        </div>
        <ol className="admin-flow">
          <li>
            交付前：在上游后台把这位客户每个 Key 的额度设为试用额度——图片线路 1
            约 {c.trialImages + 2} 张图的金额、LLM 线路 1 少量，其余 Key
            设为最低。软件试用期只使用图片线路 1 和 LLM 线路 1。
          </li>
          <li>
            生成交付包并发给客户，客户无需配置即可试用 {c.trialImages} 张。
          </li>
          <li>
            收款后：在上游后台调高各 Key
            的额度，再点下方按钮，然后把激活码发给客户。
          </li>
        </ol>
        {c.paid ? (
          <p className="hint">
            已于 {time(c.paidAt || "")} 标记收款。续费时在下方“记录”里记为续费。
          </p>
        ) : (
          <Button
            onClick={() =>
              modal.confirm({
                title: "确认已收款并调高额度？",
                content:
                  "请先在上游后台把这位客户各 Key 的额度调到正式额度。标记后即可把激活码发给客户。",
                okText: "已调高，标记收款",
                cancelText: "取消",
                onOk: () =>
                  actions.run(() => call("MarkPaid", c.id, ""), "已标记收款"),
              })
            }
          >
            已收款并调高额度
          </Button>
        )}
      </section>
      <section className="panel" aria-labelledby="lines-title">
        <div className="panel-heading">
          <h2 id="lines-title">线路与 Key</h2>
          <Button
            onClick={() =>
              actions.run(async () => {
                const missing = await call<string[]>("AutoAssign", c.id);
                const required = missing.filter((m) =>
                  requiredSlots.some((s) => slotName(s) === m),
                );
                // Every backup image line still empty: none has a key.
                if (
                  backupImageSlots.every((s) => missing.includes(slotName(s)))
                )
                  required.push("至少一条备用图片线路（2–6）");
                if (required.length)
                  modal.warning({
                    title: "必需线路没有可用的 Key",
                    content: `请先在 Key 池导入：${required.join("、")}`,
                  });
                else if (missing.length)
                  modal.info({
                    title: "部分可选线路没有可用的 Key",
                    content: `${missing.join("、")}留空，不影响生成交付包；以后导入 Key 后可以再分配并重新打包。`,
                  });
              })
            }
          >
            自动分配空缺线路
          </Button>
        </div>
        {uncappedUpstreams.map((u) => (
          <Alert
            key={u.id}
            type="warning"
            showIcon
            className="license-notice"
            message={`${uncapped
              .filter((x) => x.u.id === u.id)
              .map((x) => slotName(x.slot))
              .join("、")}不能单独限额`}
            description={quotaWarning(u)}
          />
        ))}
        <Table
          rowKey={(s) => s}
          dataSource={allSlots}
          pagination={false}
          scroll={{ x: 820 }}
          columns={[
            { title: "线路", render: (_, slot: string) => slotName(slot) },
            {
              title: "需要",
              render: (_, slot: string) => slotNeed(slot),
            },
            {
              title: "Key",
              render: (_, slot: string) => {
                const k = keys.find((x) => x.id === c.slots[slot]);
                const [kind, use] = slotNeeds(slot);
                // Every free key of the line's kind; ones labelled for this role first.
                const options = keys
                  .filter((x) => x.status === "available" && x.kind === kind)
                  .sort((a, b) => Number(b.use === use) - Number(a.use === use))
                  .map((x) => ({
                    value: x.id,
                    label: `${upstream(x.upstreamID)} · …${x.tail} · ${useLabel[x.use]}${x.kind === "llm" && !x.verified ? " · 未验证" : ""}`,
                  }));
                return (
                  <Select
                    aria-label={`${slotName(slot)} 的 Key`}
                    value={k?.id}
                    placeholder="选择 Key"
                    style={{ width: 260 }}
                    options={
                      k
                        ? [
                            {
                              value: k.id,
                              label: `${upstream(k.upstreamID)} · …${k.tail}`,
                            },
                            ...options,
                          ]
                        : options
                    }
                    notFoundContent="没有可用的 Key"
                    onChange={(v) => assign(slot, v)}
                  />
                );
              },
            },
            {
              title: "状态",
              render: (_, slot: string) => {
                const k = keys.find((x) => x.id === c.slots[slot]);
                if (!k)
                  return requiredSlots.includes(slot) ||
                    (backupImageSlots.includes(slot) && !hasBackupImage(c)) ? (
                    <Tag color="warning">未分配</Tag>
                  ) : (
                    <Tag>未分配 · 可选</Tag>
                  );
                if (k.kind === "llm" && !k.verified)
                  return <Tag color="warning">待能力验证</Tag>;
                return (
                  <Tag color="success">{k.delivered ? "已交付" : "已分配"}</Tag>
                );
              },
            },
            {
              title: "操作",
              render: (_, slot: string) =>
                c.slots[slot] ? (
                  <Button
                    size="small"
                    onClick={() =>
                      modal.confirm({
                        title: `撤下${slotName(slot)}的 Key？`,
                        content: "已交付过的 Key 撤下后永久停用。",
                        okText: "撤下",
                        cancelText: "取消",
                        onOk: () =>
                          actions.run(() => call("UnassignKey", c.id, slot)),
                      })
                    }
                  >
                    撤下
                  </Button>
                ) : null,
            },
          ]}
        />
      </section>
      <section className="panel" aria-labelledby="order-title">
        <div className="panel-heading">
          <h2 id="order-title">备用图片线路顺序</h2>
        </div>
        <p className="hint">
          图片线路 1
          为主用；变慢或不可用时按以下顺序改用备用，未分配的线路跳过。
        </p>
        <ol className="admin-order">
          {c.imageBackups.map((slot, i) => (
            <li key={slot}>
              <span>
                {slotName(slot)}
                {!c.slots[slot] && "（未分配，跳过）"}
              </span>
              <Space size={4}>
                <Button
                  size="small"
                  type="text"
                  aria-label={`${slotName(slot)} 上移`}
                  icon={<ArrowUpOutlined aria-hidden="true" />}
                  disabled={i === 0}
                  onClick={() => {
                    const next = [...c.imageBackups];
                    [next[i - 1], next[i]] = [next[i], next[i - 1]];
                    actions.run(() => call("SetImageBackups", c.id, next));
                  }}
                />
                <Button
                  size="small"
                  type="text"
                  aria-label={`${slotName(slot)} 下移`}
                  icon={<ArrowDownOutlined aria-hidden="true" />}
                  disabled={i === c.imageBackups.length - 1}
                  onClick={() => {
                    const next = [...c.imageBackups];
                    [next[i + 1], next[i]] = [next[i], next[i + 1]];
                    actions.run(() => call("SetImageBackups", c.id, next));
                  }}
                />
              </Space>
            </li>
          ))}
        </ol>
      </section>
      <section className="panel" aria-labelledby="plan-title">
        <div className="panel-heading">
          <h2 id="plan-title">套餐与额度</h2>
        </div>
        <Form
          form={notes}
          layout="vertical"
          onFinish={(v) =>
            actions.run(
              () =>
                call(
                  "SetCustomerNotes",
                  c.id,
                  v.planNote || "",
                  v.quotaNote || "",
                  v.trialImages || 0,
                ),
              "已保存",
            )
          }
        >
          <Form.Item name="planNote" label="套餐与有效期">
            <Input placeholder="例如：标准版，2026-11-01 至 2027-10-31" />
          </Form.Item>
          <Form.Item name="quotaNote" label="额度备注">
            <Input placeholder="例如：主用 Key 各 ¥300，备用 Key 各 ¥100" />
          </Form.Item>
          <Form.Item name="trialImages" label="试用张数（改动后需重新打包）">
            <InputNumber min={1} max={200} style={{ width: 140 }} />
          </Form.Item>
          <Button htmlType="submit">保存</Button>
        </Form>
      </section>
      <section className="panel" aria-labelledby="pkg-title">
        <div className="panel-heading">
          <h2 id="pkg-title">生成交付包</h2>
        </div>
        {!complete && (
          <Alert
            type="info"
            showIcon
            message="图片线路 1、至少一条备用图片线路（2–6）和 LLM 线路 1 分配好 Key 后才能生成交付包，其余线路可选。"
          />
        )}
        <div className="admin-build">
          <label htmlFor="pkg-version" className="field-label">
            软件版本
          </label>
          <Space wrap>
            <Input
              id="pkg-version"
              value={version}
              onChange={(e) => setVersion(e.target.value)}
              style={{ width: 140 }}
            />
            <Button
              type="primary"
              loading={building}
              disabled={!complete || !version}
              onClick={async () => {
                setLog([]);
                try {
                  setBuilding(true);
                  await call("BuildPackage", c.id, version);
                } catch (e) {
                  setBuilding(false);
                  setLog([errorText(e)]);
                }
              }}
            >
              生成交付包
            </Button>
          </Space>
          <p className="hint">
            构建一份不含你本机数据的程序，写入签名的 customer.dat，打包为
            ImageWorkbench-{c.name}-{version || "版本"}.zip，并检查包内没有任何
            Key 明文。
          </p>
          {log.length > 0 && (
            <div
              className="admin-log"
              ref={logRef}
              role="log"
              aria-live="polite"
              tabIndex={0}
              aria-label="构建记录"
            >
              {log.map((l, i) => (
                <div key={i}>{l}</div>
              ))}
            </div>
          )}
        </div>
        <Table<Delivery>
          rowKey={(d) => d.at}
          dataSource={[...c.deliveries].reverse()}
          pagination={false}
          locale={{ emptyText: "还没有交付记录" }}
          scroll={{ x: 760 }}
          columns={[
            { title: "版本", dataIndex: "version" },
            { title: "时间", render: (_, d) => time(d.at) },
            { title: "线路版本", dataIndex: "vaultRevision" },
            {
              title: "SHA-256",
              render: (_, d) => (
                <Tooltip title={d.sha256}>
                  <code className="mono">{d.sha256.slice(0, 12)}…</code>
                </Tooltip>
              ),
            },
            {
              title: "文件",
              render: (_, d) => (
                <Button
                  size="small"
                  icon={<FolderOpenOutlined aria-hidden="true" />}
                  onClick={() => actions.run(() => call("ShowInFolder", d.zip))}
                >
                  打开位置
                </Button>
              ),
            },
          ]}
        />
      </section>
      <section className="panel" aria-labelledby="events-title">
        <div className="panel-heading">
          <h2 id="events-title">记录</h2>
        </div>
        <Space.Compact style={{ width: "100%", maxWidth: 640 }}>
          <Input
            aria-label="新记录"
            value={event}
            placeholder="例如：续费 12 个月，主用 Key 额度各加 ¥300"
            onChange={(e) => setEvent(e.target.value)}
          />
          <Button
            onClick={() =>
              actions.run(async () => {
                await call("AddEvent", c.id, "renewed", event);
                setEvent("");
              }, "已记录")
            }
            disabled={!event.trim()}
          >
            记为续费
          </Button>
          <Button
            onClick={() =>
              actions.run(async () => {
                await call("AddEvent", c.id, "note", event);
                setEvent("");
              }, "已记录")
            }
            disabled={!event.trim()}
          >
            记为备注
          </Button>
        </Space.Compact>
        <ul className="admin-events">
          {[...c.events].reverse().map((e, i) => (
            <li key={i}>
              <span className="muted">{time(e.at)}</span>{" "}
              <Tag>
                {{
                  created: "创建",
                  paid: "收款",
                  assigned: "分配",
                  rekeyed: "换 Key",
                  delivered: "交付",
                  renewed: "续费",
                  note: "备注",
                }[e.kind] || e.kind}
              </Tag>{" "}
              {e.text}
            </li>
          ))}
        </ul>
      </section>
    </>
  );
}
