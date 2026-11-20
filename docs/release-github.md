# 用 GitHub 发布在线更新

大众版的一键更新用公开的源码仓库 **[RimuruTempest0/dream-gen-workspace](https://github.com/RimuruTempest0/dream-gen-workspace)** 的 Releases 托管 `update.json` 和程序更新包（2026-11-12 用户决定：不另建发布仓库，源码仓库已改名并设为公开）。

> 电商版已于 2026-11-20 停掉（`docs/adr/0005-retire-ecommerce-line.md`），它的发布流程（每版一个 `v<版本>` 的 Latest Release、管理工具里的“发布程序更新”）不再使用。现在只有下面的大众版发布。

签名密钥只在管理工具里，**绝不上传**。`update.json` 用它签名，软件只认这个签名，所以别人即使能在你的仓库旁边放文件，也伪造不了更新。`release/` 目录被 `.gitignore` 排除，不会随源码推送，推送前仍要用 `git status` 确认没有误加交付包或数据。

GitHub 会把下载地址跳转到它的文件服务器（另一个域名）。软件会跟随跳转（不允许从 https 跳到 http），信任只来自 `update.json` 的签名和更新包的 SHA-256、大小，跟域名无关。

## 绝不能上传的文件

- `release/customers/` 下的任何**托管版交付包**（`ImageWorkbench-<顾客>-<版本>.zip`）和任何 `customer.dat`：里面有这位顾客的线路和（混淆的）上游 Key、激活校验值。
- `release/ImageWorkbench-portable-x64-*.zip` 和 `*-update.zip`（本机 `build.ps1` 直接产出的包）：这是没有产品线标记和签名公钥的开发版。只发布管理工具“发布大众版”生成的更新包。
- `release/ImageWorkbench-*/` 文件夹、`data/`、`data-location.txt`、`.local/`、任何 `app.db` 或 `secrets/`。
- 签名密钥及其备份文件、管理工具（`release/admin/`）及其数据目录。
- 本机的私有资料：`.local/`、`.tools/`、`ledger.json` 等管理工具文件（这些都在 `.gitignore` 或仓库之外，推送前确认没有被强制加入）。

## 大众版（0.7.32 起，管理工具 1.6.0 起）

大众版的更新包公开、完整包不公开。它分开源版和托管版（0.7.34 起，见 `docs/adr/0003-hosted-edition-same-program.md`），两者是同一个程序、共用下面这一个更新发布位；托管版顾客的授权文件永远不上传。清单格式和固定发布位的取舍见 `docs/adr/0001-creative-line-updates.md`。

### 一次性准备

在仓库的 Releases 里有一个标签为 `creative` 的 Release，标记为 **Pre-release**。大众版软件固定读：

`https://github.com/RimuruTempest0/dream-gen-workspace/releases/download/creative/update.json`

这个地址、标签名和产品线名都写进了发出去的程序，**不能改**；改了之后已装出去的大众版收不到更新，只能再发完整包。

### 每次发布

1. 要发布的改动全部提交到 `main` 分支（2026-11-20 前叫 `creative-concepts`），`wails.json` 里是要发布的版本号。源码目录当时在哪个分支都可以（管理工具 1.7.0 起在自己的构建目录里构建，见 `docs/adr/0002-build-folders-per-product-line.md`）。
2. 管理工具 → 发布大众版：核对“将要构建的代码”里的版本号和提交，改好更新说明，点“构建大众版”。页面提示有未提交的改动时，那些改动不会进这次的包。
3. 构建完成后，在同一页的表格里点这一版的“上传到 GitHub”（管理工具 1.11.0 起）。确认框列出要传的两个文件、目标 Release 和 Release 上现在的版本，点“上传”。管理工具会：
   - 先确认 `creative` Release 仍是 Pre-release、不是 Latest，否则拒绝；
   - 拒绝用旧版本替换 Release 上更新的版本；
   - 确认两个文件和构建时签名的一致（被改动过就拒绝，请重新构建）；
   - 先传更新包，再替换 `update.json`；
   - 从公开地址把两个文件读回来，和本机的逐字节比较，一致才记为“已上传”。
   这一步用的是本机已登录的 GitHub CLI（`gh`）。没有安装或没有登录时，管理工具会说明；在命令行运行 `gh auth login` 登录一次即可。
4. 也可以照旧手工上传：点“要上传的两个文件”打开 `release/creative/<版本>/upload/`，点“打开 creative Release”，在网页上编辑这个 Release，先上传 `ImageWorkbench-update-<版本>.zip`，再删除原来的 `update.json`、上传新的，保存。**保持 Pre-release，不要勾选 “Set as the latest release”。**
5. 在一台装着旧版大众版的电脑上点“检查更新”，确认能看到新版本并更新成功。
6. 旧版本的更新包可以留着，也可以删掉；软件只读 `update.json` 指向的那一个。

顺序很重要：先有包，再换清单。反过来的话，中间那段时间检查更新的顾客会下载失败。

### 为什么必须是 Pre-release

电商版虽已停掉，但已交付出去的电商版旧程序仍每天读 `releases/latest/download/update.json`。GitHub 不会把 Pre-release 当作 Latest，所以大众版的发布不会出现在那个地址上。即使出现了，电商版程序也只接受格式 1 的清单，会拒绝大众版的格式 2 清单并显示一条更新出错的提示，不会装上大众版。

### 大众版只能上传的文件

| 可以上传 | 说明 |
|---|---|
| `release/creative/<版本>/upload/update.json` | 管理工具生成并签名 |
| `release/creative/<版本>/upload/ImageWorkbench-update-<版本>.zip` | 更新包，不带内置 Claude Code |

**不要上传**：`release/creative/<版本>/ImageWorkbench-<版本>.zip`（完整包，含内置 Claude Code，只私下发）、`release/creative/build/` 下的任何东西。

### 需要完整包的情况

- 0.7.32 之前发出去的大众版没有更新功能。
- 某一版换了内置 Claude Code：更新包不带它，所有人这一版都要完整包。
- 某一版换了浏览器运行组件：更新包会带上它，但跳过了这一版的顾客之后更新时会被拒绝，需要完整包。

## 公开发布后仍然存在的风险

- **源码是公开的**：任何人都能从 `RimuruTempest0/dream-gen-workspace` 自行构建一个不带签名公钥的开发者版，自己填线路和 Key 不受限地使用。授权（试用、激活、有效期）只对使用官方交付包的托管版顾客有效；它保护的是你的线路、Key 和交付包，不是软件本身。
- 仓库里能看到你用过的上游中转站和 LLM 地址（文档和教程里的示例），以及历史更新记录里出现过的软件标识（`IW-…`）。软件标识本身不能激活软件，但能对应到具体的人；需要的话可以从文档里删掉（历史提交里仍会保留）。

- 内置技能本身对顾客公开（用户决定，0.7.17 起），不算泄露。
- 更新包对外公开后，任何人都能下载程序本体；没有 `customer.dat` 时不能生图。有效的 `customer.dat` 被复制到别的电脑仍可使用（没有设备绑定），也要等网关。
