import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Button,
  Select as UISelect,
  Modal,
  Alert,
  Empty,
  Table,
  Space,
} from "antd";
import { PictureOutlined } from "@ant-design/icons";
import { call, stateNames, type Task, type Page } from "../api";
import {
  useData,
  useActions,
  DataState,
  Header,
  Status,
  TaskTiming,
  TaskFailureAlert,
  AssetImage,
  CancelTaskButton,
  taskTitle,
  time,
} from "../ui";

export function Tasks() {
  const [status, setStatus] = useState("");
  const [page, setPage] = useState(1);
  const [selected, setDetail] = useState<Task | null>(null);
  const detailQuery = useQuery<Task>({
    queryKey: ["task", selected?.id],
    queryFn: () => call<Task>("GetTask", selected!.id),
    enabled: !!selected,
    refetchInterval: 2500,
  });
  const detail = selected ? detailQuery.data || selected : null;
  const q = useData<Page<Task>>(
    ["tasks", status, page],
    "ListTasks",
    [status, (page - 1) * 10, 10],
    2500,
  );
  const actions = useActions();
  useEffect(() => {
    if (page > 1 && q.data && q.data.total <= (page - 1) * 10)
      setPage(Math.max(1, Math.ceil(q.data.total / 10)));
  }, [q.data]);
  function retry(t: Task) {
    actions.confirm(
      "重新生成这张图片？",
      "这会提交新的计费请求。结果未知的任务请先核对平台记录。",
      () => call("Retry", t.id),
    );
  }
  return (
    <>
      <Header
        title="任务中心"
        sub="查看持久化队列、生成参数和结果。离开页面不会中断任务。"
      />
      <section className="panel">
        <div className="panel-heading">
          <h2>生成任务</h2>
          <UISelect
            aria-label="任务状态"
            value={status}
            onChange={(v) => {
              setStatus(v);
              setPage(1);
            }}
            style={{ width: 160 }}
            options={[
              { value: "", label: "全部状态" },
              ...Object.entries(stateNames).map(([value, label]) => ({
                value,
                label,
              })),
            ]}
          />
        </div>
        <DataState query={q}>
          <Table<Task>
            rowKey="id"
            loading={q.isFetching && q.isPending}
            dataSource={q.data?.items || []}
            scroll={{ x: 1000 }}
            pagination={{
              current: page,
              pageSize: 10,
              total: q.data?.total || 0,
              onChange: setPage,
              showSizeChanger: false,
              showTotal: (n) => `共 ${n} 个任务`,
            }}
            columns={[
              {
                title: "图片",
                width: 76,
                render: (_, t) =>
                  t.assetIDs[0] ? (
                    <div className="task-thumb">
                      <AssetImage id={t.assetIDs[0]} />
                    </div>
                  ) : (
                    <div className="task-thumb task-thumb-empty">
                      <PictureOutlined aria-hidden="true" />
                    </div>
                  ),
              },
              {
                title: "提示词",
                dataIndex: "prompt",
                ellipsis: { showTitle: false },
                width: 290,
                render: (_, t) => <span title={t.prompt}>{taskTitle(t)}</span>,
              },
              { title: "模型", dataIndex: "model", width: 130 },
              {
                title: "状态",
                dataIndex: "status",
                width: 190,
                render: (v, task) => (
                  <>
                    <Status state={v} task={task} />
                    <TaskTiming task={task} />
                  </>
                ),
              },
              {
                title: "创建时间",
                dataIndex: "createdAt",
                width: 120,
                render: time,
              },
              {
                title: "操作",
                width: 250,
                render: (_, t) => (
                  <Space wrap>
                    <Button size="small" onClick={() => setDetail(t)}>
                      详情
                    </Button>
                    <CancelTaskButton task={t} size="small" />
                    {[
                      "queued",
                      "submitting",
                      "downloading",
                      "blocked",
                    ].includes(t.status) ? (
                      <Button
                        size="small"
                        onClick={() =>
                          actions.run(
                            () => call("StopBatch", t.batchID),
                            "已停止待执行项",
                          )
                        }
                      >
                        停止队列
                      </Button>
                    ) : (
                      <Button size="small" onClick={() => retry(t)}>
                        重新生成
                      </Button>
                    )}
                    {t.status === "download_failed" && (
                      <Button
                        size="small"
                        onClick={() =>
                          actions.run(
                            () => call("RetryDownload", t.id),
                            "已加入补下载队列",
                          )
                        }
                      >
                        补下载
                      </Button>
                    )}
                    {t.status === "blocked" && (
                      <Button
                        size="small"
                        onClick={() =>
                          actions.run(
                            () => call("ResumeBlocked", t.id),
                            "未提交任务已恢复",
                          )
                        }
                      >
                        恢复
                      </Button>
                    )}
                  </Space>
                ),
              },
            ]}
            locale={{ emptyText: <Empty description="还没有生成任务" /> }}
          />
        </DataState>
      </section>
      <Modal
        title="任务详情"
        open={!!detail}
        footer={<Button onClick={() => setDetail(null)}>关闭</Button>}
        onCancel={() => setDetail(null)}
        width={740}
      >
        {detail && (
          <>
            <Status state={detail.status} task={detail} />
            <TaskTiming task={detail} />
            <dl className="details">
              <dt>连接</dt>
              <dd>
                {detail.connection.name} · {detail.connection.baseURL}
              </dd>
              <dt>模型</dt>
              <dd>{detail.model}</dd>
              <dt>参数</dt>
              <dd>
                尺寸 {detail.size || "auto"} · 质量 {detail.quality || "auto"}
              </dd>
              <dt>提示词</dt>
              <dd className="prewrap">{detail.prompt}</dd>
              <dt>任务标识</dt>
              <dd className="mono">{detail.id}</dd>
              <dt>提交时间</dt>
              <dd>{time(detail.createdAt)}</dd>
              {!!detail.diagnostic?.startedAt && (
                <>
                  <dt>请求地址</dt>
                  <dd className="mono">{detail.diagnostic.requestURL}</dd>
                  <dt>响应状态</dt>
                  <dd>
                    {detail.diagnostic.httpStatus
                      ? `HTTP ${detail.diagnostic.httpStatus}`
                      : "尚未收到响应状态"}
                  </dd>
                  {!!detail.diagnostic.contentType && (
                    <>
                      <dt>响应类型</dt>
                      <dd>{detail.diagnostic.contentType}</dd>
                    </>
                  )}
                  {!!detail.diagnostic.httpProtocol && (
                    <>
                      <dt>传输协议</dt>
                      <dd>
                        {detail.diagnostic.httpProtocol}
                        {detail.diagnostic.transferEncoding
                          ? ` · ${detail.diagnostic.transferEncoding}`
                          : ""}
                      </dd>
                      <dt>声明长度</dt>
                      <dd>
                        {detail.diagnostic.contentLength! >= 0
                          ? `${detail.diagnostic.contentLength?.toLocaleString()} 字节`
                          : "平台未声明响应长度"}
                      </dd>
                    </>
                  )}
                  {!!detail.diagnostic.networkRoute && (
                    <>
                      <dt>网络连接</dt>
                      <dd>
                        {detail.diagnostic.networkRoute === "proxy"
                          ? "通过代理连接"
                          : "直接连接"}
                      </dd>
                    </>
                  )}
                  {!!detail.diagnostic.upstreamRequestID && (
                    <>
                      <dt>平台请求标识</dt>
                      <dd className="mono">
                        {detail.diagnostic.upstreamRequestID}
                      </dd>
                    </>
                  )}
                  <dt>已接收</dt>
                  <dd>
                    {detail.diagnostic.bytesReceived.toLocaleString()} 字节
                  </dd>
                  {!!detail.diagnostic.networkError && (
                    <>
                      <dt>中断原因</dt>
                      <dd className="mono">{detail.diagnostic.networkError}</dd>
                    </>
                  )}
                </>
              )}
            </dl>
            {detailQuery.isError && (
              <Alert
                type="warning"
                message="详情刷新失败，当前显示最近记录。"
              />
            )}
            <TaskFailureAlert task={detail} />
            <div className="asset-grid">
              {detail.assetIDs.map((id) => (
                <AssetImage key={id} id={id} thumbnail={false} />
              ))}
            </div>
          </>
        )}
      </Modal>
    </>
  );
}
