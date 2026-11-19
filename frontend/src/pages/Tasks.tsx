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
  hostedLineName,
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
import { t as tr } from "../i18n";

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
    actions.confirm(tr("tasks.retry.title"), tr("tasks.retry.body"), () =>
      call("Retry", t.id),
    );
  }
  return (
    <>
      <Header title={tr("shell.nav.tasks")} sub={tr("tasks.sub")} />
      <section className="panel records-panel">
        <div className="panel-heading">
          <h2>{tr("tasks.heading")}</h2>
          <UISelect
            aria-label={tr("tasks.statusFilter")}
            value={status}
            onChange={(v) => {
              setStatus(v);
              setPage(1);
            }}
            style={{ width: 160 }}
            options={[
              { value: "", label: tr("tasks.allStates") },
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
              showTotal: (n) => tr("tasks.total", { count: n }),
            }}
            columns={[
              {
                title: tr("tasks.col.image"),
                width: 96,
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
                title: tr("tasks.col.prompt"),
                dataIndex: "prompt",
                ellipsis: { showTitle: false },
                width: 290,
                render: (_, t) => <span title={t.prompt}>{taskTitle(t)}</span>,
              },
              { title: tr("tasks.col.model"), dataIndex: "model", width: 130 },
              {
                title: tr("tasks.col.status"),
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
                title: tr("tasks.col.created"),
                dataIndex: "createdAt",
                width: 120,
                render: time,
              },
              {
                title: tr("tasks.col.actions"),
                width: 250,
                render: (_, t) => (
                  <Space wrap>
                    <Button size="small" onClick={() => setDetail(t)}>
                      {tr("tasks.details")}
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
                            tr("tasks.stopped"),
                          )
                        }
                      >
                        {tr("tasks.stopQueue")}
                      </Button>
                    ) : (
                      <Button size="small" onClick={() => retry(t)}>
                        {tr("tasks.retry")}
                      </Button>
                    )}
                    {t.status === "download_failed" && (
                      <Button
                        size="small"
                        onClick={() =>
                          actions.run(
                            () => call("RetryDownload", t.id),
                            tr("tasks.redownloadQueued"),
                          )
                        }
                      >
                        {tr("tasks.redownload")}
                      </Button>
                    )}
                    {t.status === "blocked" && (
                      <Button
                        size="small"
                        onClick={() =>
                          actions.run(
                            () => call("ResumeBlocked", t.id),
                            tr("tasks.resumed"),
                          )
                        }
                      >
                        {tr("tasks.resume")}
                      </Button>
                    )}
                  </Space>
                ),
              },
            ]}
            locale={{ emptyText: <Empty description={tr("tasks.empty")} /> }}
          />
        </DataState>
      </section>
      <Modal
        title={tr("tasks.detail.title")}
        open={!!detail}
        footer={
          <Button onClick={() => setDetail(null)}>
            {tr("tasks.detail.close")}
          </Button>
        }
        onCancel={() => setDetail(null)}
        width={740}
      >
        {detail && (
          <>
            <Status state={detail.status} task={detail} />
            <TaskTiming task={detail} />
            <dl className="details">
              <dt>{tr("tasks.detail.connection")}</dt>
              <dd>
                {hostedLineName(detail.connection)} ·{" "}
                {detail.connection.baseURL}
              </dd>
              <dt>{tr("tasks.col.model")}</dt>
              <dd>{detail.model}</dd>
              <dt>{tr("tasks.detail.params")}</dt>
              <dd>
                {tr("tasks.detail.paramsValue", {
                  size: detail.size || "auto",
                  quality: detail.quality || "auto",
                })}
              </dd>
              <dt>{tr("tasks.col.prompt")}</dt>
              <dd className="prewrap">{detail.prompt}</dd>
              <dt>{tr("tasks.detail.id")}</dt>
              <dd className="mono">{detail.id}</dd>
              <dt>{tr("tasks.detail.submitted")}</dt>
              <dd>{time(detail.createdAt)}</dd>
              {!!detail.diagnostic?.startedAt && (
                <>
                  <dt>{tr("tasks.detail.url")}</dt>
                  <dd className="mono">{detail.diagnostic.requestURL}</dd>
                  <dt>{tr("tasks.detail.httpStatus")}</dt>
                  <dd>
                    {detail.diagnostic.httpStatus
                      ? `HTTP ${detail.diagnostic.httpStatus}`
                      : tr("tasks.detail.noStatus")}
                  </dd>
                  {!!detail.diagnostic.contentType && (
                    <>
                      <dt>{tr("tasks.detail.contentType")}</dt>
                      <dd>{detail.diagnostic.contentType}</dd>
                    </>
                  )}
                  {!!detail.diagnostic.httpProtocol && (
                    <>
                      <dt>{tr("tasks.detail.protocol")}</dt>
                      <dd>
                        {detail.diagnostic.httpProtocol}
                        {detail.diagnostic.transferEncoding
                          ? ` · ${detail.diagnostic.transferEncoding}`
                          : ""}
                      </dd>
                      <dt>{tr("tasks.detail.length")}</dt>
                      <dd>
                        {detail.diagnostic.contentLength! >= 0
                          ? tr("tasks.detail.bytes", {
                              count: detail.diagnostic.contentLength!,
                            })
                          : tr("tasks.detail.noLength")}
                      </dd>
                    </>
                  )}
                  {!!detail.diagnostic.networkRoute && (
                    <>
                      <dt>{tr("tasks.detail.network")}</dt>
                      <dd>
                        {detail.diagnostic.networkRoute === "proxy"
                          ? tr("tasks.detail.proxy")
                          : tr("tasks.detail.direct")}
                      </dd>
                    </>
                  )}
                  {!!detail.diagnostic.upstreamRequestID && (
                    <>
                      <dt>{tr("tasks.detail.upstreamID")}</dt>
                      <dd className="mono">
                        {detail.diagnostic.upstreamRequestID}
                      </dd>
                    </>
                  )}
                  <dt>{tr("tasks.detail.received")}</dt>
                  <dd>
                    {tr("tasks.detail.bytes", {
                      count: detail.diagnostic.bytesReceived,
                    })}
                  </dd>
                  {!!detail.diagnostic.networkError && (
                    <>
                      <dt>{tr("tasks.detail.networkError")}</dt>
                      <dd className="mono">{detail.diagnostic.networkError}</dd>
                    </>
                  )}
                </>
              )}
            </dl>
            {detailQuery.isError && (
              <Alert
                type="warning"
                message={tr("tasks.detail.refreshFailed")}
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
