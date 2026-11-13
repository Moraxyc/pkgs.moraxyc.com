# 签名、部署与恢复

## 初始生产配置

生产配置需单独授权；本地构建不执行。启用发布工作流前：

1. 创建 Cloudflare Pages Direct Upload 项目，将生产分支设为本仓库默认分支，并以 HTTPS 绑定 `pkgs.moraxyc.com`。
2. 建立离线主密钥和 CI 签名子密钥；审核两个公钥指纹后，只提交二进制 OpenPGP 公钥环 `keys/moraxyc-archive-keyring.gpg`（`gpg --export`，不加 `--armor`）。已有 ASCII-armored 公钥须先用 `gpg --dearmor` 转换；仅改扩展名不能使其用于 `gpgv --keyring`。
3. 用必需检查项和 CODEOWNERS 保护默认分支，将 `pages-production` 限制到该分支，并配置生产发布审批人。
4. 在 `publication/context.json` 中填写经评审的 `primary_fingerprint` 和 `signing_fingerprint`，使用大写 40 位指纹。在 Actions 的 **Variables** 中设置仓库变量 `CLOUDFLARE_ACCOUNT_ID` 和 `PAGES_PROJECT`，不要放在 Secrets 中。前者填写项目所属账户的 32 位十六进制 Account ID，后者填写 Pages 项目名，不是自定义域名、`*.pages.dev` 地址或项目 UUID。两者均不含引号或空白。依赖更新和发布输入更新均使用同一个 GitHub App 提交并直接推送。需在本仓库安装拥有 **Contents: write** 权限的 App，并配置仓库 secrets `APP_ID`（App Client ID）和 `APP_PRIVATE_KEY`。默认分支规则需允许该 App 推送；如要求 PR 或必需检查，须配置相应绕过权限。更新器不强制推送。
5. 在 `pages-production` 中以 `APT_SIGNING_SUBKEY` 添加经标准 Base64 编码的**秘密子密钥导出**（`gpg --export-secret-subkeys` 的二进制或 ASCII-armored 输出，附不含主私钥的密钥存根）。签名命令在导入前解码。无密码的子密钥无需设置 `APT_SIGNING_PASSPHRASE`；有密码时才设置。`CLOUDFLARE_API_TOKEN` 只放在该环境中，并限制为目标账户的 Pages 部署权限。
6. 启用一个经过评审的项目，手动发布一次，并记录自定义域名检查和客户端安装结果。

发布流程分为三个 workflow：

- `update-publication.yml` 收集并校验快照，只更新 context 中的 epoch，提交两个输入文件后把确切 commit 传给 Build。它每六小时、手动触发或收到 `project-release-published` 事件时运行；不把 dispatch payload 当作仓库数据。公钥指纹仍由人工评审维护，`update.yml` 单独负责依赖更新。
- `build.yml` 检出指定 commit，通过 `nix build .#site` 在锁定的 Nix 环境中构建，并导出运行时闭包和源 commit。产物名包含 run ID 和构建 attempt。手动 Build 使用默认分支的选定提交，`publish` 默认关闭；Update 调用时开启发布。
- `publish.yml` 接收成功 Build 的确切 commit、store 路径和产物名。它导入闭包，不再求值或构建项目；核对产物中的 snapshot/context 与该 commit 一致，只复制 `public/` 到可写目录，签名、用 APT 校验后上传。Pages 只接收 `public/`，其余元数据和工具留在 workflow 产物中。

Update 使用 Nix 的 publication 环境；Build 的产物导出和 Publish 的签名、校验、上传都使用 site 中的 `tools`。不单独执行发布工具包的构建命令，也不依赖 runner 预装的业务工具。Actions 和初始 Nix 安装属于启动环境。

三个阶段通过可复用 workflow 在同一次 Actions run 中串联，不依赖机器人 push 触发下一个 workflow。Update 的并发组串行化自动流程；Publish 另有并发组，设置 `cancel-in-progress: false`。Publish 失败可用 **Re-run failed jobs** 重试，并继续使用成功 Build 的产物；重跑全部 job 会生成新的构建 attempt 和产物。产物保留 30 天；APT 元数据过期后须重新 Update 和 Build，重新签名不会延长有效期。即使没有新 release，也要定期更新 epoch，以维持七天有效期。

Publish 在签名前和上传前检查提交是否仍为默认分支 HEAD，避免旧的排队任务发布已被取代的快照。这不能原子阻止外部上传期间的分支更新。生产回滚仍由人工执行。

pull request 只运行本地测试和合成密钥测试。checkout 凭据不会被持久化。签名和 Cloudflare 凭据只传给 `pages-production` job 中各自的步骤。私钥不是 Nix 输入；一次签名阶段生成 APT 所需的两份签名文件，Publish 不重新渲染元数据。

发布 Actions 固定到 commit SHA，工具版本由 `flake.lock` 固定。托管 runner 和外部服务响应不属于 site 的可复现范围。收集会更新保存的输入，上传会改变外部状态；可复现承诺只针对未签名 site 的字节。

<a id="key-lifecycle"></a>

## 密钥生命周期

使用离线认证主密钥和专用的 RSA 签名子密钥。RSA-2048 合成密钥已在本地测试；请在生产验收时选择并记录生产密钥长度和支持的最老客户端版本。生产密钥的生成和私钥导出是离线操作员任务。

签名器从不打开开发者默认的 GnuPG home。它把提供的 CI 材料导入新建的 0700 目录，并拒绝存在可用主私钥或缺少预期秘密子密钥的情况。它使用 GnuPG 的 `!` 选择符指定精确的签名指纹，签署 `Release.gpg` 和 `InRelease`，并在另一个临时 home 中用提交的公钥校验两者。它还会把还原出的 InRelease 文本与 Release 比较。临时 agent 和 home 会被清理；没有任何私钥材料进入 public 目录树或制品。

轮换步骤：

1. 离线签发新的签名子密钥；保留旧的子密钥。
2. 发布同时包含新旧子密钥的公钥环，但仍然用旧子密钥签名。安排密钥环的投递；客户端在安装时下载一次**不会**带来密钥环的自动更新。
3. 等待密钥环分发和缓存传播；测试有代表性的老客户端和新客户端。更新 CI 密钥和预期的签名指纹。
4. 切换签名，校验两种格式和 APT 客户端，并观察一个 Valid-Until 周期后才淘汰旧子密钥。
5. 如果校验失败，切回保留的旧子密钥并重新构建。

在证明过渡完成之前，不要移除旧的公钥。遇到泄露时，暂停发布，执行离线撤销和沟通流程，保留事故取证，并评审哪些客户端需要新的信任根。

## 用户安装

生产密钥环和域名通过验证后，再发布以下命令：

```sh
sudo install -d -m 0755 /etc/apt/keyrings
curl -fsSL https://pkgs.moraxyc.com/deb/keys/moraxyc-archive-keyring.gpg \
  -o /tmp/moraxyc-archive-keyring.gpg

# 与独立发布的 release 记录核对公钥指纹
gpg --show-keys --with-fingerprint /tmp/moraxyc-archive-keyring.gpg
sudo install -m 0644 /tmp/moraxyc-archive-keyring.gpg /etc/apt/keyrings/moraxyc-archive-keyring.gpg
printf '%s\n' 'deb [signed-by=/etc/apt/keyrings/moraxyc-archive-keyring.gpg] https://pkgs.moraxyc.com/deb stable main' \
  | sudo tee /etc/apt/sources.list.d/moraxyc.list
sudo apt-get update
```

该软件源使用 `Signed-By`。经评审的生产指纹应与验收记录一起发布；占位指纹不能作为信任根。

## 故障与恢复

| 故障 | 恢复方式 |
| --- | --- |
| GitHub 不可用或被限流 | 保留当前部署；重试完整收集或等待定时任务。 |
| 清单或资产缺失、非法 | 修复所属项目的 Release 或策略；不要静默跳过某个失败的项目。 |
| 重复的包三元组或 pool 路径 | 解决冲突的源记录，然后重建所有项目。 |
| 签名或指纹不匹配 | 在部署前停止；修正经评审的 CI/密钥环配置，并重新渲染到新目录。 |
| Cloudflare API `7003`，无法路由到 Pages 项目 | 核对 Actions Variables 中的 Account ID 和 Pages 项目名，并确认项目属于该账户。检查 `pages-production` 的同名环境变量是否覆盖了仓库变量。API token 须有该账户的 Cloudflare Pages Edit 权限。 |
| 部署 API 失败 | 记录失败，并在重试前检查平台的部署状态。 |
| 自定义域名冒烟失败 | 新部署可能已经在承载流量。检查缓存/DNS 和签名结果，必要时回滚到已记录的已知良好部署。 |

不要在失败运行中删除上一个部署。平台上传失败不等于生产流量未变；检查 deployment ID 和状态。部署后的冒烟检查比较精确元数据（含历史 by-hash 索引）、密钥环和一条 pool 重定向，不下载软件包二进制。请求失败或内容不匹配时每五秒重试一次，总时限七分钟；调用方设定更早时限时以其为准。本地输入错误立即失败。重试窗口覆盖 300 秒元数据缓存期，持续不匹配仍会使发布校验失败。

每次发布前保留上一个已知良好的 Pages deployment ID。用保留的部署回滚，然后用匹配的快照和签名检查自定义域名。回滚是一次显式的生产操作；工作流不会自动执行它。如果保留的 Release 已过期，就用新的 epoch 从它的快照重建并重新签名。保留源提交、快照、epoch、指纹和工具版本以便重放。

GitHub Release 的保留策略会影响已有索引：删除或替换资产会破坏索引，即使当前元数据已经被撤回。请显式规划撤回，保留旧的已签名部署记录，并对变更过的二进制使用新标签。

## 发布记录

unsigned artifact 保存源 commit、Nix 运行时闭包及路径列表；闭包中的 site 产物包含 `public/`、`snapshot.json`、`publication.json` 和指向锁定发布工具的 `tools` 链接；signed artifact 保存快照、发布参数和已签名目录树。审计信息包括源提交、run ID、产物名、触发方式、epoch、指纹、site/工具 store 路径以及锁文件、输入和 Release 的 SHA256。Wrangler 的结构化输出记录 deployment ID、URL、项目和环境；工作流要求得到生产部署结果。首次上线和恢复演练另行记录上一个可用部署 ID、客户端版本、安装结果和回滚结果。

对连续刷新失败、有效期剩余时间小于一个刷新周期、签名者意外变化、签名失败和重定向目标变化配置监控。GitHub job 状态提供基线失败信号；外部告警和生产回滚演练仍属于部署配置任务。
