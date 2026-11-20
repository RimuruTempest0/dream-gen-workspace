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
  Switch,
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
import { useQueryClient } from "@tanstack/react-query";
import { call, errorText } from "../api";
import { DataState, Header, dateTime, useActions, useData, time } from "../ui";
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
  type AdminState,
  type Delivery,
  type HostedSource,
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
        title="托管版顾客"
        sub="大众版托管版的顾客都在这里。每人一个软件标识和激活码；图片线路 1、至少一条备用图片线路（2–6）和 LLM 线路 1 分配好 Key 即可生成交付包，其余线路可选。"
        action={
          <Button
            type="primary"
            icon={<PlusOutlined aria-hidden="true" />}
            onClick={() => {
              form.resetFields();
              setOpen(true);
            }}
          >
            新建
          </Button>
        }
      />
      <section className="panel">
        <DataState
          query={state}
          empty={!state.data?.customers.length}
          emptyText="还没有顾客"
        >
          <Table<Customer>
            rowKey="id"
            dataSource={state.data?.customers}
            pagination={false}
            scroll={{ x: 760 }}
            columns={[
              {
                title: "名称",
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
        title="新建托管版顾客"
        open={open}
        okText="创建"
        cancelText="取消"
        onCancel={() => setOpen(false)}
        onOk={async () => {
          const v = await form.validateFields();
          const c = await actions.run(
            () => call<Customer>("CreateCustomer", v.name, v.trialImages || 20),
            "已创建，已生成软件标识和激活码",
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
            label="名称"
            rules={[{ required: true, message: "请填写名称" }]}
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
  const { modal, notification } = AntApp.useApp();
  const nav = useNavigate();
  const c = state.data?.customers.find((x) => x.id === id);
  // A hosted user's package is the newest built mass-market program plus their
  // licence file: nothing is built for it. A record without the line marker is
  // left from the e-commerce line, retired in admin 1.12.0.
  const retired = !!c && c.line !== "creative";
  const program = useData<HostedSource>(
    ["hosted-source"],
    "HostedSource",
    [],
    5000,
  );
  const version = program.data?.version || "";
  const blocked = retired
    ? "这条记录属于已停用的电商版，不能再生成交付包或授权文件。"
    : program.data?.blocked;
  const [log, setLog] = useState<string[]>([]);
  const [building, setBuilding] = useState(false);
  const [notes] = Form.useForm();
  const [event, setEvent] = useState("");
  const logRef = useRef<HTMLDivElement>(null);
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
        program.refetch();
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
        expiresOn: dateInput(c.expiresAt),
      });
  }, [c?.id]);
  if (!c)
    return (
      <DataState query={state} empty emptyText="顾客不存在">
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
        "已交付过的旧 Key 会被永久停用，不会再分给其他顾客。需要重新生成交付包，顾客覆盖升级后自动换上新 Key，激活码不变。",
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
      title: `删除顾客“${c!.name}”？`,
      content: (
        <>
          <p>
            顾客、激活码和全部记录一并删除，无法恢复；以后不能再给这位顾客补发激活码或生成升级包。
          </p>
          <p>
            {delivered > 0
              ? `已交付过的 ${delivered} 个 Key 在顾客的软件里，会在 Key 池中永久停用、不再分给别人；顾客的软件仍能用到它们的额度用完为止，要立即停用请到上游后台停用这些 Key。`
              : "这位顾客还没有交付过，分配的 Key 全部回到 Key 池。"}
            {delivered > 0 && "从未交付的 Key 回到 Key 池。"}
          </p>
          <p>已生成的交付包文件不会删除。</p>
        </>
      ),
      okText: "删除顾客",
      okButtonProps: { danger: true },
      cancelText: "取消",
      onOk: () =>
        actions.run(async () => {
          await call("DeleteCustomer", c!.id);
          nav("/");
        }, "已删除顾客"),
    });
  }
  return (
    <>
      <Header
        title={c.name}
        sub={`${retired ? "电商版（已停用）" : "托管版"} · 软件标识 ${c.softwareID} · 创建于 ${time(c.createdAt)}`}
        action={
          <Space size="middle">
            <Link to="/">返回顾客列表</Link>
            <Button danger onClick={removeCustomer}>
              删除顾客
            </Button>
          </Space>
        }
      />
      {c.changed && c.deliveries.length > 0 && (
        <Alert
          className="license-notice"
          type="warning"
          showIcon
          message="线路有变更：重新生成交付包后，顾客覆盖升级即可生效（激活码不变）。"
        />
      )}
      <section className="panel" aria-labelledby="code-title">
        <div className="panel-heading">
          <h2 id="code-title">激活码</h2>
        </div>
        <ActivationCode customer={c} />
        <p className="hint">
          激活码只能打开这位顾客的软件，可随时查看或补发。请在收款并调高额度后再通过私下渠道发给顾客。
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
            交付前：在上游后台把这位顾客每个 Key 的额度设为试用额度——图片线路 1
            约 {c.trialImages + 2} 张图的金额、LLM 线路 1 少量，其余 Key
            设为最低。软件试用期只使用图片线路 1 和 LLM 线路 1。
          </li>
          <li>
            生成交付包并发给顾客，顾客无需配置即可试用 {c.trialImages} 张。
          </li>
          <li>
            收款后：在上游后台调高各 Key
            的额度，再点下方按钮，然后把激活码发给顾客。
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
                  "请先在上游后台把这位顾客各 Key 的额度调到正式额度。标记后即可把激活码发给顾客。",
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
            actions.run(async () => {
              await call(
                "SetCustomerNotes",
                c.id,
                v.planNote || "",
                v.quotaNote || "",
                v.trialImages || 0,
              );
              await call("SetCustomerExpiry", c.id, endOfDay(v.expiresOn));
            }, "已保存")
          }
        >
          <Form.Item
            name="expiresOn"
            label="有效期至（当天 23:59 到期；留空为不限）"
            extra="写进下一次的交付包或续费授权文件。到期后顾客不能生成新图片，已有图片不受影响；到期前 7 天软件会提醒。"
          >
            <Input type="date" style={{ width: 180 }} />
          </Form.Item>
          <Form.Item name="planNote" label="套餐备注">
            <Input placeholder="例如：标准版，按年付费" />
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
      <CustomerSkills customer={c} />
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
          <dl className="admin-facts">
            <dt>大众版程序</dt>
            <dd>
              {program.data?.version ? (
                <>
                  {program.data.version}
                  <span className="hint">
                    {" "}
                    · 构建于 {time(program.data.at)}
                  </span>
                </>
              ) : (
                "—"
              )}
            </dd>
          </dl>
          {blocked && <Alert type="warning" showIcon message={blocked} />}
          <Space wrap>
            <Button
              type="primary"
              loading={building}
              disabled={!complete || !version || !!blocked}
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
              生成交付包 {version}
            </Button>
          </Space>
          <p className="hint">
            托管版的包不重新构建：用“发布大众版”里最新构建的那一版程序，加上这位顾客的授权文件，打包为
            ImageWorkbench-{c.name}-{version || "版本"}
            .zip，并检查包内没有任何 Key
            明文。顾客解压就能用，不用自己配置；以后和开源版一样一键更新。
          </p>
          <Space wrap>
            <Button
              disabled={retired || !complete}
              onClick={() =>
                actions.run(async () => {
                  const saved = await call<boolean>("SaveCustomerFile", c.id);
                  if (saved)
                    notification.success({
                      message: "授权文件已保存",
                      description:
                        "把这个 customer.dat 发给顾客，顾客在“连接 → 托管版授权 → 导入授权文件”导入，再输入激活码即可（大众版 0.7.34 起）。",
                    });
                })
              }
            >
              只生成授权文件
            </Button>
            <span className="hint">
              顾客已经装了大众版（0.7.34
              或更高）时，只发这个小文件：导入后就是托管版，不用重装。续费、改有效期或换
              Key 后也只发它。
              {c.expiresAt
                ? `当前有效期至 ${dateTime(c.expiresAt)}。`
                : "当前不限有效期。"}
            </span>
          </Space>
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

// The date input works in local days; the licence stores a UTC instant.
function dateInput(iso?: string) {
  if (!iso) return "";
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

function endOfDay(day?: string) {
  if (!day) return "";
  const [y, m, d] = day.split("-").map(Number);
  return new Date(y, m - 1, d, 23, 59, 59)
    .toISOString()
    .replace(/\.\d{3}Z$/, "Z");
}

// Which built-in skills the package comes with (default all). The others start
// switched off in the customer's app, who can still turn them on.
function CustomerSkills({ customer }: { customer: Customer }) {
  const state = useAdminState();
  const qc = useQueryClient();
  const { notification } = AntApp.useApp();
  const all = state.data?.builtinSkills || [];
  const current = customer.skills ?? all.map((b) => b.id);
  // Each tick shows at once (the cached state is updated first) and is saved.
  function save(next: string[]) {
    const everything = next.length === all.length;
    qc.setQueryData<AdminState>(
      ["admin-state"],
      (old) =>
        old && {
          ...old,
          customers: old.customers.map((c) =>
            c.id === customer.id
              ? { ...c, skills: everything ? undefined : next }
              : c,
          ),
        },
    );
    call("SetCustomerSkills", customer.id, next)
      .catch((e) =>
        notification.error({
          message: "技能选择未保存",
          description: errorText(e),
        }),
      )
      .finally(() => qc.invalidateQueries({ queryKey: ["admin-state"] }));
  }
  return (
    <section className="panel" aria-labelledby="customer-skills-title">
      <div className="panel-heading">
        <h2 id="customer-skills-title">内置技能</h2>
      </div>
      <p className="hint">
        选择交付包带哪些内置技能（默认全部，改动立即保存）。没带的在顾客软件里一开始是停用的，顾客可以自己再打开。写进下一次的交付包或续费授权文件。
      </p>
      <ul className="admin-skill-list">
        {all.map((b) => (
          <li key={b.id}>
            <Switch
              size="small"
              checked={current.includes(b.id)}
              aria-label={`交付包带上 ${b.name}`}
              onChange={(on) =>
                save(
                  all
                    .map((x) => x.id)
                    .filter((id) => (id === b.id ? on : current.includes(id))),
                )
              }
            />
            <span>{b.name}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}
