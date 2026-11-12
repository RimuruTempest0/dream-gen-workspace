import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useInfiniteQuery } from "@tanstack/react-query";
import { App as AntApp, Button, Modal, Space, Tooltip } from "antd";
import {
  DownloadOutlined,
  DeleteOutlined,
  CheckSquareOutlined,
  CheckOutlined,
} from "@ant-design/icons";
import {
  call,
  blankDraft,
  type GenerateInput,
  type Asset,
  type Page,
} from "../api";
import {
  useActions,
  useImageDownloads,
  DataState,
  Header,
  AssetImage,
  assetLabel,
  time,
} from "../ui";

export function Assets() {
  const downloads = useImageDownloads();
  const [detail, setDetail] = useState<Asset | null>(null);
  const pages = useInfiniteQuery({
    queryKey: ["assets"],
    initialPageParam: 0,
    queryFn: ({ pageParam }) =>
      call<Page<Asset>>("ListAssets", "", pageParam, 24),
    getNextPageParam: (last, all) => {
      const count = all.reduce((sum, p) => sum + p.items.length, 0);
      return count < last.total ? count : undefined;
    },
  });
  const q = {
    ...pages,
    data: pages.data
      ? {
          items: pages.data.pages.flatMap((p) => p.items),
          total: pages.data.pages[0].total,
        }
      : undefined,
  };
  const actions = useActions();
  const { notification } = AntApp.useApp();
  const nav = useNavigate();
  // Batch mode: clicking a card selects it. Only loaded, still-listed images count,
  // in list order, so removed images drop out by themselves.
  const [selecting, setSelecting] = useState(false);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const items = q.data?.items || [];
  const chosen = items.filter((a) => picked.has(a.id));
  const ids = chosen.map((a) => a.id);
  const allPicked = items.length > 0 && chosen.length === items.length;
  function toggle(id: string) {
    setPicked((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }
  function stopSelecting() {
    setSelecting(false);
    setPicked(new Set());
  }
  async function exportChosen() {
    await actions.run(async () => {
      const path = await call<string>(
        "ChooseExportPath",
        `素材-${ids.length}张.zip`,
      );
      if (!path) return;
      const count = await call<number>("ExportAssetsToPath", ids, path);
      notification.success({ message: `已导出素材 ZIP（${count} 张）` });
    });
  }
  async function useReference(a: Asset) {
    await actions.run(async () => {
      const draft = await call<GenerateInput>("GetDraft");
      await call("SetDraft", {
        ...blankDraft,
        ...draft,
        references: [...new Set([...(draft.references || []), a.id])].slice(
          0,
          4,
        ),
      });
      nav("/");
    });
  }
  return (
    <>
      <Header
        title="素材库"
        sub="生成原图与参考图保存在本机，随时预览、导出或再次使用。"
      />
      <section className="panel">
        <div className="panel-heading">
          <h2>{q.data?.total || 0} 张本地图片</h2>
          <Space wrap>
            {!selecting && (
              <Button
                icon={<CheckSquareOutlined aria-hidden="true" />}
                disabled={!items.length}
                onClick={() => setSelecting(true)}
              >
                批量管理
              </Button>
            )}
          </Space>
        </div>
        {selecting && (
          <div
            className="asset-batch-bar"
            role="toolbar"
            aria-label="批量管理素材"
          >
            <strong aria-live="polite">已选 {chosen.length} 张</strong>
            <Button
              size="small"
              onClick={() =>
                setPicked(
                  allPicked ? new Set() : new Set(items.map((a) => a.id)),
                )
              }
            >
              {allPicked ? "取消全选" : `全选已加载（${items.length} 张）`}
            </Button>
            <Button
              size="small"
              icon={<DownloadOutlined aria-hidden="true" />}
              disabled={!chosen.length || actions.busy}
              onClick={exportChosen}
            >
              导出 ZIP
            </Button>
            <Button
              size="small"
              danger
              icon={<DeleteOutlined aria-hidden="true" />}
              disabled={!chosen.length || actions.busy}
              onClick={() =>
                actions.confirm(
                  `从素材库移除 ${ids.length} 张图片？`,
                  "图片会从列表隐藏，文件和任务引用仍保留。",
                  async () => {
                    await call("UpdateAssets", ids, "", true);
                    setPicked(new Set());
                  },
                  true,
                )
              }
            >
              移除
            </Button>
            <Button size="small" type="text" onClick={stopSelecting}>
              完成
            </Button>
          </div>
        )}
        <DataState
          query={q}
          empty={!q.data?.items.length}
          emptyText="还没有本地图片"
          action={
            <Button type="primary" onClick={() => nav("/")}>
              去生成图片
            </Button>
          }
        >
          <div className="asset-grid">
            {q.data?.items.map((a) => (
              <article
                className={
                  "asset-card" + (picked.has(a.id) ? " asset-card-picked" : "")
                }
                key={a.id}
              >
                {selecting ? (
                  <button
                    className="image-button asset-pick"
                    onClick={() => toggle(a.id)}
                    aria-label={`选择 ${assetLabel(a)}`}
                    aria-pressed={picked.has(a.id)}
                  >
                    <AssetImage id={a.id} />
                    <span className="asset-pick-box" aria-hidden="true">
                      {picked.has(a.id) && <CheckOutlined />}
                    </span>
                  </button>
                ) : (
                  <button
                    className="image-button"
                    onClick={() => setDetail(a)}
                    aria-label={`预览 ${assetLabel(a)}`}
                  >
                    <AssetImage id={a.id} />
                  </button>
                )}
                <div className="asset-card-copy">
                  <strong title={a.name}>
                    {a.kind === "reference" ? a.name : time(a.createdAt)}
                  </strong>
                  <small>
                    {a.width} × {a.height} ·{" "}
                    {a.kind === "reference" ? "参考图" : "生成图片"}
                  </small>
                  <Space>
                    <Button size="small" onClick={() => useReference(a)}>
                      作为参考图
                    </Button>
                    <Tooltip title="导出原图">
                      <Button
                        size="small"
                        icon={<DownloadOutlined aria-hidden="true" />}
                        aria-label="导出原图"
                        onClick={() =>
                          actions.run(
                            () =>
                              call<boolean>(
                                "ExportAsset",
                                a.id,
                                a.path.split("/").pop(),
                              ),
                            undefined,
                          )
                        }
                      />
                    </Tooltip>
                  </Space>
                </div>
              </article>
            ))}
          </div>
          {q.data && q.data.items.length < q.data.total && (
            <div className="load-more">
              <Button
                onClick={() => pages.fetchNextPage()}
                loading={q.isFetching}
              >
                加载更多
              </Button>
            </div>
          )}
        </DataState>
      </section>
      <Modal
        title="图片详情"
        open={!!detail}
        onCancel={() => setDetail(null)}
        footer={null}
        width={800}
      >
        {detail && (
          <>
            <AssetImage id={detail.id} thumbnail={false} />
            <p>
              {assetLabel(detail)} · {detail.width} × {detail.height}
            </p>
            <Space wrap>
              <Button
                icon={<DownloadOutlined aria-hidden="true" />}
                disabled={downloads.busy}
                onClick={() => downloads.downloadAsset(detail.id)}
              >
                导出原图
              </Button>
              <Button
                danger
                icon={<DeleteOutlined aria-hidden="true" />}
                onClick={() =>
                  actions.confirm(
                    "从素材库移除？",
                    "图片会从列表隐藏，文件和任务引用仍保留。",
                    async () => {
                      await call(
                        "UpdateAsset",
                        detail.id,
                        detail.projectID,
                        true,
                      );
                      setDetail(null);
                    },
                    true,
                  )
                }
              >
                移除
              </Button>
            </Space>
          </>
        )}
      </Modal>
    </>
  );
}
