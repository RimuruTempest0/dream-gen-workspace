# 用 GitHub 发布在线更新

在线更新（软件 0.7.13 起、管理工具 1.2.0 起）用公开的源码仓库 **[RimuruTempest0/dream-gen-workspace](https://github.com/RimuruTempest0/dream-gen-workspace)** 的 Releases 托管 `update.json` 和程序更新包（2026-11-12 用户决定：不另建发布仓库，源码仓库已改名并设为公开）。

> **先决条件：只公开 0.7.16 及以后的版本。** 0.7.16 之前，客户版程序在缺少 `customer.dat` 时会当作开发者版运行（不受授权限制），任何人下载到公开的更新包就能直接使用。0.7.16 起这种情况进入“授权无效”，不能生图、不能自行添加线路。所以第一次公开发布的更新包必须来自 0.7.16 或更新的版本，更早的更新包不要上传。

## 一次性准备

1. 发布仓库就是源码仓库 `https://github.com/RimuruTempest0/dream-gen-workspace`（公开）。Releases 里只放下面“只能上传的文件”；`release/` 目录被 `.gitignore` 排除，不会随源码推送，推送前仍要用 `git status` 确认没有误加交付包或数据。
2. 管理工具 → 设置与签名密钥 → 程序更新 → 更新清单地址（已设好）：
   `https://github.com/RimuruTempest0/dream-gen-workspace/releases/latest/download/update.json`
   之后生成的交付包才会检查这个地址。之前交付的客户需要再用完整交付包升级一次。
3. 签名密钥照旧只在管理工具里，**绝不上传**。`update.json` 用它签名，客户软件只认这个签名，所以别人即使能在你的仓库旁边放文件，也伪造不了更新。

## 每次发布

1. 用管理工具给任意一位客户生成交付包（版本号 = 新版本）。交付包旁会多出 `release/customers/<软件标识>/ImageWorkbench-update-<版本>.zip`，这就是要发布的**程序更新包**：不含 `customer.dat`，同一把签名密钥的所有客户通用，已经通过不含 Key 和本机连接的检查。
2. 在仓库的 Releases 新建 Release，标签用 `v<版本>`（例如 `v0.7.17`），先把更新包作为附件上传，得到它的下载地址：
   `https://github.com/RimuruTempest0/dream-gen-workspace/releases/download/v<版本>/ImageWorkbench-update-<版本>.zip`
3. 管理工具 → 发布程序更新：选择这个更新包，填上面的下载地址、最低可直接升级的版本（可空）和更新说明，生成 `update.json`。
4. 把 `update.json` 也上传到**同一个 Release**，文件名必须正好是 `update.json`，并把这个 Release 设为 **Latest**。`releases/latest/download/update.json` 永远指向最新的那个 Release。
5. 在一台装着旧版本的测试机上点“检查更新”，确认能看到新版本、下载并校验通过。

GitHub 会把这两个地址跳转到它的文件服务器（另一个域名）。软件会跟随跳转（不允许从 https 跳到 http），信任只来自 `update.json` 的签名和更新包的 SHA-256、大小，跟域名无关。

## 只能上传的文件

| 可以上传 | 说明 |
|---|---|
| `update.json` | 管理工具生成并签名 |
| `ImageWorkbench-update-<版本>.zip` | 交付包旁的程序更新包 |

## 绝不能上传的文件

- `release/customers/` 下的任何**客户交付包**（`ImageWorkbench-<客户>-<版本>.zip`）和任何 `customer.dat`：里面有该客户的线路和（混淆的）上游 Key、激活校验值。
- `release/ImageWorkbench-portable-x64-*.zip` 和 `*-update.zip`（本机 `build.ps1` 直接产出的包）：这是**没有内置签名公钥的开发者版**，任何人都能不受授权限制使用。只发布管理工具生成的更新包。
- `release/ImageWorkbench-*/` 文件夹、`data/`、`data-location.txt`、`.local/`、任何 `app.db` 或 `secrets/`。
- 签名密钥及其备份文件、管理工具（`release/admin/`）及其数据目录。
- 本机的私有资料：`.local/`、`.tools/`、`ledger.json` 等管理工具文件（这些都在 `.gitignore` 或仓库之外，推送前确认没有被强制加入）。

## 公开发布后仍然存在的风险

- **源码是公开的**：任何人都能从 `RimuruTempest0/dream-gen-workspace` 自行构建一个不带签名公钥的开发者版，自己填线路和 Key 不受限地使用。授权（试用、激活、有效期、在线更新）只对使用官方交付包的客户有效；它保护的是你的线路、Key 和交付包，不是软件本身。
- 仓库里能看到你用过的上游中转站和 LLM 地址（文档和教程里的示例），以及历史更新记录里出现过的客户软件标识（`IW-…`）。软件标识本身不能激活软件，但能对应到具体客户；需要的话可以从文档里删掉（历史提交里仍会保留）。

- 内置技能本身对客户公开（用户决定，0.7.17 起），不算泄露。
- 更新包对外公开后，任何人都能下载程序本体；没有 `customer.dat` 时不能生图。有效的 `customer.dat` 被复制到别的电脑仍可使用（没有设备绑定），也要等网关。
