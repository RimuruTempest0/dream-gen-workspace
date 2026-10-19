import { useState } from "react";
import {
  Button,
  Input,
  Modal,
  Select as UISelect,
  Space,
  Switch,
  Table,
  Tag,
} from "antd";
import { ReloadOutlined } from "@ant-design/icons";
import { DataState, Header, time, useData } from "./ui";

type LogEntry = {
  id: number;
  time: string;
  level: string;
  source: string;
  relatedID: string;
  message: string;
};
type LogPage = { items: LogEntry[]; total: number };
const levels: Record<string, string> = {
  info: "信息",
  warn: "警告",
  error: "错误",
};
const sources: Record<string, string> = {
  app: "应用",
  connection: "连接验证",
  agent: "智能分析",
  image: "生图任务",
};

export function Logs() {
  const [level, setLevel] = useState("");
  const [source, setSource] = useState("");
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [auto, setAuto] = useState(true);
  const [detail, setDetail] = useState<LogEntry | null>(null);
  const q = useData<LogPage>(
    ["logs", level, source, keyword, page],
    "ListLogs",
    [level, source, keyword, (page - 1) * 50, 50],
    auto ? 3000 : undefined,
  );
  return (
    <>
      <Header
        title="日志"
        sub="查看连接、智能分析和生图任务的运行记录。日志保存在当前数据目录，保留最近 5000 条。"
        action={
          <Button
            icon={<ReloadOutlined aria-hidden="true" />}
            loading={q.isFetching}
            onClick={() => void q.refetch()}
          >
            刷新日志
          </Button>
        }
      />
      <section className="panel" aria-label="日志记录">
        <div className="log-filters">
          <div className="log-filter">
            <label htmlFor="log-level">日志级别</label>
            <UISelect
              id="log-level"
              value={level}
              onChange={(v) => {
                setLevel(v);
                setPage(1);
              }}
              options={[
                { value: "", label: "全部级别" },
                ...Object.entries(levels).map(([value, label]) => ({
                  value,
                  label,
                })),
              ]}
            />
          </div>
          <div className="log-filter">
            <label htmlFor="log-source">日志来源</label>
            <UISelect
              id="log-source"
              value={source}
              onChange={(v) => {
                setSource(v);
                setPage(1);
              }}
              options={[
                { value: "", label: "全部来源" },
                ...Object.entries(sources).map(([value, label]) => ({
                  value,
                  label,
                })),
              ]}
            />
          </div>
          <div className="log-filter log-search">
            <label htmlFor="log-keyword">关键词或关联 ID</label>
            <Input.Search
              id="log-keyword"
              placeholder="输入后搜索"
              maxLength={200}
              allowClear
              enterButton="搜索"
              onSearch={(v) => {
                setKeyword(v.trim());
                setPage(1);
              }}
            />
          </div>
          <Space>
            <Switch id="log-auto" checked={auto} onChange={setAuto} />
            <label htmlFor="log-auto">自动刷新</label>
          </Space>
        </div>
        <p className="hint">
          仅记录阶段和错误摘要，密钥已脱敏，不记录提示词、图片内容或思考原文。从此版本开始记录，历史任务仍可在任务中心查看。
        </p>
        <div role="status" aria-live="polite" className="hint">
          {q.data ? `共 ${q.data.total} 条符合条件的日志` : "正在读取日志"}
        </div>
        <DataState query={q}>
          <Table<LogEntry>
            rowKey="id"
            dataSource={q.data?.items || []}
            scroll={{ x: 760 }}
            locale={{
              emptyText: "暂无符合条件的日志，可调整筛选条件或稍后刷新。",
            }}
            pagination={{
              current: page,
              pageSize: 50,
              total: q.data?.total || 0,
              showSizeChanger: false,
              onChange: setPage,
              hideOnSinglePage: true,
            }}
            columns={[
              {
                title: "时间",
                dataIndex: "time",
                width: 170,
                render: (v: string) => time(v),
              },
              {
                title: "级别",
                dataIndex: "level",
                width: 80,
                render: (v: string) => (
                  <Tag
                    color={
                      v === "error"
                        ? "error"
                        : v === "warn"
                          ? "warning"
                          : "default"
                    }
                  >
                    {levels[v] || v}
                  </Tag>
                ),
              },
              {
                title: "来源",
                dataIndex: "source",
                width: 110,
                render: (v: string) => sources[v] || v,
              },
              {
                title: "内容",
                dataIndex: "message",
                render: (v: string) => (
                  <span className="log-message">
                    {Array.from(v).slice(0, 140).join("")}
                    {Array.from(v).length > 140 ? "…" : ""}
                  </span>
                ),
              },
              {
                title: "操作",
                width: 95,
                render: (_, entry) => (
                  <Button
                    size="small"
                    aria-label={`查看日志 ${entry.id} 详情`}
                    onClick={() => setDetail(entry)}
                  >
                    查看详情
                  </Button>
                ),
              },
            ]}
          />
        </DataState>
      </section>
      <Modal
        title="日志详情"
        open={!!detail}
        onCancel={() => setDetail(null)}
        footer={<Button onClick={() => setDetail(null)}>关闭</Button>}
      >
        {detail && (
          <div className="log-detail">
            <p>时间：{time(detail.time)}</p>
            <p>
              级别：{levels[detail.level]} · 来源：{sources[detail.source]}
            </p>
            {detail.relatedID && (
              <p>
                关联 ID：<span className="mono">{detail.relatedID}</span>
              </p>
            )}
            <pre className="log-message">{detail.message}</pre>
          </div>
        )}
      </Modal>
    </>
  );
}
