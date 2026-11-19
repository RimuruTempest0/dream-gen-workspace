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
import { t } from "../i18n";

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
  // Images being removed fade out before the list closes up.
  const [leaving, setLeaving] = useState<Set<string>>(new Set());
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
        t("assets.zipName", { count: String(ids.length) }),
      );
      if (!path) return;
      const count = await call<number>("ExportAssetsToPath", ids, path);
      notification.success({ message: t("assets.exported", { count }) });
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
      <Header title={t("shell.nav.assets")} sub={t("assets.sub")} />
      <section className="panel works-panel">
        <div className="panel-heading">
          <h2>{t("assets.count", { count: q.data?.total || 0 })}</h2>
          <Space wrap>
            {!selecting && (
              <Button
                icon={<CheckSquareOutlined aria-hidden="true" />}
                disabled={!items.length}
                onClick={() => setSelecting(true)}
              >
                {t("assets.batch")}
              </Button>
            )}
          </Space>
        </div>
        {selecting && (
          <div
            className="asset-batch-bar"
            role="toolbar"
            aria-label={t("assets.batchBar")}
          >
            <strong aria-live="polite">
              {t("assets.picked", { count: chosen.length })}
            </strong>
            <Button
              size="small"
              onClick={() =>
                setPicked(
                  allPicked ? new Set() : new Set(items.map((a) => a.id)),
                )
              }
            >
              {allPicked
                ? t("assets.unselectAll")
                : t("assets.selectAll", { count: items.length })}
            </Button>
            <Button
              size="small"
              icon={<DownloadOutlined aria-hidden="true" />}
              disabled={!chosen.length || actions.busy}
              onClick={exportChosen}
            >
              {t("assets.exportZip")}
            </Button>
            <Button
              size="small"
              danger
              icon={<DeleteOutlined aria-hidden="true" />}
              disabled={!chosen.length || actions.busy}
              onClick={() =>
                actions.confirm(
                  t("assets.removeMany", { count: ids.length }),
                  t("assets.removeBody"),
                  async () => {
                    setLeaving(new Set(ids));
                    try {
                      await Promise.all([
                        call("UpdateAssets", ids, "", true),
                        new Promise((done) => window.setTimeout(done, 200)),
                      ]);
                      setPicked(new Set());
                    } finally {
                      setLeaving(new Set());
                    }
                  },
                  true,
                )
              }
            >
              {t("assets.remove")}
            </Button>
            <Button size="small" type="text" onClick={stopSelecting}>
              {t("assets.done")}
            </Button>
          </div>
        )}
        <DataState
          query={q}
          empty={!q.data?.items.length}
          emptyText={t("assets.empty")}
          action={
            <Button type="primary" onClick={() => nav("/")}>
              {t("assets.goGenerate")}
            </Button>
          }
        >
          <div className="asset-grid">
            {q.data?.items.map((a) => (
              <article
                className={
                  "asset-card" +
                  (picked.has(a.id) ? " asset-card-picked" : "") +
                  (leaving.has(a.id) ? " is-leaving" : "")
                }
                key={a.id}
              >
                {selecting ? (
                  <button
                    className="image-button asset-pick"
                    onClick={() => toggle(a.id)}
                    aria-label={t("assets.select", { name: assetLabel(a) })}
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
                    aria-label={t("assets.preview", { name: assetLabel(a) })}
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
                    {a.kind === "reference"
                      ? t("assets.reference")
                      : t("assets.generated")}
                  </small>
                  <Space className="asset-card-actions">
                    <Button size="small" onClick={() => useReference(a)}>
                      {t("assets.asRef")}
                    </Button>
                    <Tooltip title={t("assets.exportOriginal")}>
                      <Button
                        size="small"
                        icon={<DownloadOutlined aria-hidden="true" />}
                        aria-label={t("assets.exportOriginal")}
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
                {t("assets.loadMore")}
              </Button>
            </div>
          )}
        </DataState>
      </section>
      <Modal
        title={t("assets.detail")}
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
                {t("assets.exportOriginal")}
              </Button>
              <Button
                danger
                icon={<DeleteOutlined aria-hidden="true" />}
                onClick={() =>
                  actions.confirm(
                    t("assets.removeOne"),
                    t("assets.removeBody"),
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
                {t("assets.remove")}
              </Button>
            </Space>
          </>
        )}
      </Modal>
    </>
  );
}
