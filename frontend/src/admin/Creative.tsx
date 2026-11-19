import { useEffect, useRef, useState } from "react";
import { Alert, App as AntApp, Button, Input, Space, Table } from "antd";
import { FolderOpenOutlined, LinkOutlined } from "@ant-design/icons";
import { call, errorText } from "../api";
import { Header, time, useActions } from "../ui";
import { useAdminState } from "./AdminApp";
import { LineFacts, useLineSource } from "./LineSource";
import type { CreativeRelease } from "./types";

// 发布大众版: build the mass-market line from the newest commit of its branch,
// check both packages, sign update.json. Uploading the two public files stays manual.
export function Creative() {
  const state = useAdminState();
  const source = useLineSource("creative");
  const actions = useActions();
  const { modal } = AntApp.useApp();
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
  // The English notes (app 0.8.0 shows them in every language but Chinese):
  // translated on request, read and edited here before the build signs them.
  const [english, setEnglish] = useState<{ version: string; text: string }>();
  const notesEn = english?.version === version ? english.text : "";
  const setNotesEn = (text: string) => setEnglish({ version, text });
  const [translating, setTranslating] = useState(false);
  const translate = () =>
    modal.confirm({
      title: "把更新说明翻译成英文？",
      content:
        "会用 Key 池里一个通过能力验证、没有分给客户的 LLM Key 发 1 个请求，产生少量费用。翻译结果会填进下面的英文更新说明，请看一遍再构建。",
      okText: "翻译（产生少量费用）",
      cancelText: "取消",
      onOk: async () => {
        setTranslating(true);
        try {
          const text = await actions.run(
            () => call<string>("TranslateNotes", notes ?? ""),
            "已翻译，请检查英文更新说明",
          );
          if (text !== undefined) setNotesEn(text);
        } finally {
          setTranslating(false);
        }
      },
    });

  // Propose the notes from the changelog of the commit to be built.
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
          <h2 id="creative-source-title">将要构建的代码</h2>
        </div>
        <LineFacts src={src} />
        {!src?.blocked && (
          <p className="hint">
            大众版按这个提交里的版本号发布。构建在管理工具自己的构建目录里进行，不会切换源码目录的分支。
          </p>
        )}
      </section>
      <section className="panel" aria-labelledby="creative-build-title">
        <div className="panel-heading">
          <h2 id="creative-build-title">构建并签名</h2>
        </div>
        <div className="admin-build">
          <label htmlFor="creative-notes" className="field-label">
            中文更新说明（中文界面的顾客在更新前看到）
          </label>
          <Input.TextArea
            id="creative-notes"
            value={notes ?? ""}
            autoSize={{ minRows: 4, maxRows: 12 }}
            onChange={(e) => setNotes(e.target.value)}
          />
          <p className="hint">
            已从 docs/CHANGELOG.md 取这一版的条目预填，可以改短。0.8.0
            以前的软件只显示中文更新说明。
          </p>
          <label htmlFor="creative-notes-en" className="field-label">
            英文更新说明（英语、越南语、日语、韩语界面的顾客看到）
          </label>
          <Input.TextArea
            id="creative-notes-en"
            lang="en"
            value={notesEn}
            placeholder="点“翻译成英文”生成，或直接填写"
            autoSize={{ minRows: 4, maxRows: 12 }}
            onChange={(e) => setNotesEn(e.target.value)}
          />
          <Space wrap>
            <Button
              loading={translating}
              disabled={!notes?.trim()}
              onClick={translate}
            >
              {notesEn.trim() ? "重新翻译" : "翻译成英文"}
            </Button>
          </Space>
          <p className="hint">
            翻译后请看一遍，可以直接修改；构建时签进 update.json
            的就是这里的文字。
          </p>
          <Space wrap>
            <Button
              type="primary"
              loading={building}
              disabled={
                !src ||
                !!src.blocked ||
                !version ||
                !notes?.trim() ||
                !notesEn.trim()
              }
              onClick={async () => {
                setLog([]);
                setDone(undefined);
                try {
                  setBuilding(true);
                  await call("BuildCreative", version, notes ?? "", notesEn);
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
