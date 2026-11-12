import { useState } from "react";
import { Alert, App as AntApp, Button, Checkbox, Input, Space } from "antd";
import { FolderOutlined } from "@ant-design/icons";
import { call } from "../api";
import { Header, useActions } from "../ui";
import { useAdminState } from "./AdminApp";

export function AdminSettings() {
  const state = useAdminState();
  const actions = useActions();
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [restore, setRestore] = useState("");
  const [replace, setReplace] = useState(false);
  const [updateURL, setUpdateURL] = useState<string | undefined>();
  const [release, setRelease] = useState({
    zip: "",
    url: "",
    minVersion: "",
    notes: "",
  });
  const { notification } = AntApp.useApp();
  const s = state.data;
  const mismatch = confirm !== "" && password !== confirm;
  return (
    <>
      <Header
        title="设置与签名密钥"
        sub="签名密钥保证客户软件只认你生成的交付包；源码目录用于构建交付包。"
      />
      <section className="panel" aria-labelledby="sign-title">
        <div className="panel-heading">
          <h2 id="sign-title">签名密钥</h2>
        </div>
        <p>
          公钥指纹：
          <code className="mono">
            {s?.signingPublicKey.slice(0, 16) || "—"}…
          </code>
        </p>
        <Alert
          type="warning"
          showIcon
          message="请务必备份签名密钥"
          description="已交付的客户软件只信任这把密钥。密钥丢失（例如重装系统、换电脑）后，将无法再给已有客户生成升级包，只能重新发放全新软件。备份文件用你设置的密码加密，请把文件和密码分开保存。"
        />
        <div className="admin-form-grid">
          <label htmlFor="backup-password" className="field-label">
            备份密码（至少 10 个字符）
          </label>
          <Input.Password
            id="backup-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
          />
          <label htmlFor="backup-confirm" className="field-label">
            再次输入
          </label>
          <Input.Password
            id="backup-confirm"
            value={confirm}
            status={mismatch ? "error" : undefined}
            aria-describedby={mismatch ? "backup-mismatch" : undefined}
            onChange={(e) => setConfirm(e.target.value)}
            autoComplete="new-password"
          />
          {mismatch && (
            <p id="backup-mismatch" className="field-error" role="alert">
              两次输入的密码不一致
            </p>
          )}
          <Button
            disabled={password.length < 10 || password !== confirm}
            onClick={() =>
              actions.run(async () => {
                if (await call<boolean>("ExportSigningBackup", password)) {
                  setPassword("");
                  setConfirm("");
                }
              }, undefined)
            }
          >
            导出加密备份
          </Button>
        </div>
        <h3 className="admin-subhead">从备份恢复</h3>
        <div className="admin-form-grid">
          <label htmlFor="restore-password" className="field-label">
            备份密码
          </label>
          <Input.Password
            id="restore-password"
            value={restore}
            onChange={(e) => setRestore(e.target.value)}
            autoComplete="off"
          />
          <Checkbox
            checked={replace}
            onChange={(e) => setReplace(e.target.checked)}
          >
            替换当前密钥（只有换电脑恢复时才需要；替换后，用当前密钥交付过的客户无法再升级）
          </Checkbox>
          <Button
            disabled={!restore}
            onClick={() =>
              actions.run(async () => {
                if (
                  await call<boolean>("ImportSigningBackup", restore, replace)
                )
                  setRestore("");
              }, "签名密钥已恢复")
            }
          >
            选择备份文件并恢复
          </Button>
        </div>
      </section>
      <section className="panel" aria-labelledby="repo-title">
        <div className="panel-heading">
          <h2 id="repo-title">源码目录与数据</h2>
        </div>
        <p className="hint">源码目录</p>
        <code className="data-path">{s?.repoRoot || "未设置"}</code>
        <Space wrap className="location-actions">
          <Button
            icon={<FolderOutlined aria-hidden="true" />}
            onClick={() => actions.run(() => call("ChooseRepoRoot"), undefined)}
          >
            选择源码目录
          </Button>
        </Space>
        <p className="hint">
          管理工具数据（台账与加密的 Key、激活码、签名密钥），与客户软件分开保存
        </p>
        <code className="data-path">{s?.dataDir}</code>
      </section>
      <section className="panel" aria-labelledby="update-title">
        <div className="panel-heading">
          <h2 id="update-title">程序更新</h2>
        </div>
        <p className="hint">
          客户软件从这个地址读取签名的
          update.json。设置后生成的交付包才会检查更新；留空则客户只能用完整包升级。
        </p>
        <div className="admin-form-grid">
          <label htmlFor="update-url" className="field-label">
            更新清单地址
          </label>
          <Input
            id="update-url"
            value={updateURL ?? s?.updateURL ?? ""}
            placeholder="https://example.com/imageworkbench/update.json"
            onChange={(e) => setUpdateURL(e.target.value)}
          />
          <Button
            onClick={() =>
              actions.run(async () => {
                await call("SetUpdateURL", updateURL ?? s?.updateURL ?? "");
                setUpdateURL(undefined);
              }, "已保存")
            }
          >
            保存地址
          </Button>
        </div>
        <h3 className="admin-subhead">发布程序更新</h3>
        <p className="hint">
          选择交付包旁边的
          ImageWorkbench-update-版本号.zip，填写客户下载它的地址，生成签名的
          update.json；再把两个文件上传到对应地址。
        </p>
        <div className="admin-form-grid">
          <span className="field-label">更新包</span>
          <Space wrap>
            <code className="data-path">{release.zip || "未选择"}</code>
            <Button
              onClick={() =>
                actions.run(async () => {
                  const zip = await call<string>("ChooseUpdatePackage");
                  if (zip) setRelease((r) => ({ ...r, zip }));
                })
              }
            >
              选择更新包
            </Button>
          </Space>
          <label htmlFor="update-download" className="field-label">
            更新包下载地址
          </label>
          <Input
            id="update-download"
            value={release.url}
            placeholder="https://example.com/imageworkbench/ImageWorkbench-update-0.7.13.zip"
            onChange={(e) => setRelease({ ...release, url: e.target.value })}
          />
          <label htmlFor="update-min" className="field-label">
            最低可直接升级的版本（可空）
          </label>
          <Input
            id="update-min"
            value={release.minVersion}
            placeholder="例如 0.7.13"
            onChange={(e) =>
              setRelease({ ...release, minVersion: e.target.value })
            }
          />
          <label htmlFor="update-notes" className="field-label">
            更新说明（客户可见）
          </label>
          <Input.TextArea
            id="update-notes"
            value={release.notes}
            autoSize={{ minRows: 3, maxRows: 8 }}
            onChange={(e) => setRelease({ ...release, notes: e.target.value })}
          />
          <Button
            type="primary"
            disabled={!release.zip || !release.url}
            onClick={() =>
              actions.run(async () => {
                if (await call<boolean>("PublishUpdate", release))
                  notification.success({
                    message: "update.json 已生成",
                    description:
                      "把 update.json 上传到更新清单地址，把更新包上传到下载地址。",
                  });
              })
            }
          >
            生成 update.json
          </Button>
        </div>
      </section>
    </>
  );
}
