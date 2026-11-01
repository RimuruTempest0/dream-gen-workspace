import { useState } from "react";
import { Alert, Button, Checkbox, Input, Space } from "antd";
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
    </>
  );
}
