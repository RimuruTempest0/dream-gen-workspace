import { useEffect, useRef, useState } from "react";
import { Alert, Button, Input, Space, Table } from "antd";
import { FolderOpenOutlined, LinkOutlined } from "@ant-design/icons";
import { call, errorText } from "../api";
import { Header, time, useActions, useData } from "../ui";
import { useAdminState } from "./AdminApp";
import type { CreativeRelease, CreativeSource } from "./types";

// 发布大众版: build the mass-market line from its own branch, check both
// packages, sign update.json. Uploading the two public files stays manual.
export function Creative() {
  const state = useAdminState();
  const source = useData<CreativeSource>(
    ["creative-source"],
    "CreativeSource",
    [],
    5000,
  );
  const actions = useActions();
  // Notes proposed from the changelog, kept per version; what the provider
  // typed wins until the version changes.
  const [proposed, setProposed] = useState<{ version: string; text: string }>();
  const [edited, setEdited] = useState<{ version: string; text: string }>();
  const [log, setLog] = useState<string[]>([]);
  const [building, setBuilding] = useState(false);
  const [done, setDone] = useState<CreativeRelease | undefined>();
  const logRef = useRef<HTMLDivElement>(null);
  const src = source.data;
  const version = src?.version || "";

  const notes =
    edited?.version === version
      ? edited.text
      : proposed?.version === version
        ? proposed.text
        : undefined;
  const setNotes = (text: string) => setEdited({ version, text });

  // Propose the notes from the changelog for the version in the source folder.
  useEffect(() => {
    if (!version) return;
    let current = true;
    call<string>("CreativeNotes", version).then(
      (text) => current && setProposed({ version, text }),
    );
    return () => {
      current = false;
    };
  }, [version]);
  useEffect(() => {
    const offLog = window.runtime?.EventsOn("creative:log", (line: string) =>
      setLog((l) => [...l, line].slice(-200)),
    );
    const offDone = window.runtime?.EventsOn(
      "creative:done",
      (r: { ok: boolean; error?: string; release?: CreativeRelease }) => {
        setBuilding(false);
        setDone(r.ok ? r.release : undefined);
        setLog((l) => [
          ...l,
          r.ok ? "大众版已构建并签名。" : "失败：" + r.error,
        ]);
        state.refetch();
        source.refetch();
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

  const releases = [...(state.data?.creative ?? [])].reverse();
  return (
    <>
      <Header
        title="发布大众版"
        sub="大众版没有授权，顾客自己配置服务。这里构建、检查并签名；上传由你在 GitHub 网页上完成。"
      />
      <section className="panel" aria-labelledby="creative-source-title">
        <div className="panel-heading">
          <h2 id="creative-source-title">源码状态</h2>
        </div>
        <dl className="admin-facts">
          <dt>版本号</dt>
          <dd>{version || "—"}</dd>
          <dt>当前分支</dt>
          <dd>{src?.branch || "—"}</dd>
          <dt>未提交的改动</dt>
          <dd>
            {src?.changed.length
              ? `${src.changed.length} 个文件`
              : src?.branch
                ? "没有"
                : "—"}
          </dd>
        </dl>
        {src?.blocked ? (
          <Alert type="warning" showIcon message={src.blocked} />
        ) : (
          <p className="hint">
            大众版按源码里的版本号发布，只能从 creative-concepts
            分支、在没有未提交改动时构建。
          </p>
        )}
      </section>
      <section className="panel" aria-labelledby="creative-build-title">
        <div className="panel-heading">
          <h2 id="creative-build-title">构建并签名</h2>
        </div>
        <div className="admin-build">
          <label htmlFor="creative-notes" className="field-label">
            更新说明（顾客在更新前看到）
          </label>
          <Input.TextArea
            id="creative-notes"
            value={notes ?? ""}
            autoSize={{ minRows: 4, maxRows: 12 }}
            onChange={(e) => setNotes(e.target.value)}
          />
          <p className="hint">
            已从 docs/CHANGELOG.md 取这一版的条目预填，可以改短。
          </p>
          <Space wrap>
            <Button
              type="primary"
              loading={building}
              disabled={!src || !!src.blocked || !version || !notes?.trim()}
              onClick={async () => {
                setLog([]);
                setDone(undefined);
                try {
                  setBuilding(true);
                  await call("BuildCreative", version, notes ?? "");
                } catch (e) {
                  setBuilding(false);
                  setLog([errorText(e)]);
                }
              }}
            >
              构建大众版 {version}
            </Button>
          </Space>
          <p className="hint">
            完整包留在本机，由你私下发给顾客。更新包不带内置 Claude
            Code，和签名的 update.json 一起放进 upload 文件夹。
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
      </section>
      <section className="panel" aria-labelledby="creative-upload-title">
        <div className="panel-heading">
          <h2 id="creative-upload-title">上传到 GitHub</h2>
        </div>
        {done && (
          <Alert
            type="success"
            showIcon
            message={`${done.version} 已准备好，还没有上传`}
          />
        )}
        <ol className="admin-steps">
          <li>
            打开标签为 <code>creative</code> 的 Release，点编辑。它必须保持
            Pre-release，不要勾选 “Set as the latest
            release”：电商版客户的软件每天读 Latest。
          </li>
          <li>
            先上传 upload 文件夹里的更新包
            <code>ImageWorkbench-update-版本号.zip</code>。
          </li>
          <li>
            再删除 Release 里原来的 <code>update.json</code>，上传新的
            <code>update.json</code>，保存。顺序不要反：先有包，再换清单。
          </li>
          <li>upload 文件夹以外的文件都不要上传，完整包只私下发。</li>
        </ol>
        <Space wrap>
          <Button
            icon={<LinkOutlined aria-hidden="true" />}
            onClick={() => actions.run(() => call("OpenCreativeRelease"))}
          >
            打开 creative Release
          </Button>
        </Space>
        <Table<CreativeRelease>
          rowKey={(r) => r.version}
          dataSource={releases}
          pagination={false}
          size="small"
          locale={{ emptyText: "还没有构建过大众版" }}
          columns={[
            { title: "版本", dataIndex: "version", width: 90 },
            { title: "构建时间", render: (_, r) => time(r.at), width: 170 },
            {
              title: "文件",
              render: (_, r) => (
                <Space wrap>
                  <Button
                    size="small"
                    icon={<FolderOpenOutlined aria-hidden="true" />}
                    onClick={() =>
                      actions.run(() => call("ShowInFolder", r.manifest))
                    }
                  >
                    要上传的两个文件
                  </Button>
                  <Button
                    size="small"
                    icon={<FolderOpenOutlined aria-hidden="true" />}
                    onClick={() =>
                      actions.run(() => call("ShowInFolder", r.fullZip))
                    }
                  >
                    完整包（私下发）
                  </Button>
                </Space>
              ),
            },
          ]}
        />
      </section>
    </>
  );
}
