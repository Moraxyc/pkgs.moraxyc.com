# 发布与恢复

## 初始生产配置

1. 创建 Cloudflare Pages Direct Upload 项目，将生产分支设为仓库默认分支，绑定 `pkgs.moraxyc.com`。
2. 建立离线主密钥和 CI 签名子密钥。用 `gpg --export` 导出二进制公钥环到 `keys/moraxyc-archive-keyring.gpg`，将两个大写 40 位指纹填入 `publication/context.json`。
3. 创建 GitHub 环境 `pages-production`，配置分支限制和审批人。
4. 安装拥有 **Contents: write** 权限的 GitHub App，允许它向默认分支推送更新。

配置以下 Actions 参数：

| 位置 | 名称 | 内容 |
| --- | --- | --- |
| 仓库 Variables | `CLOUDFLARE_ACCOUNT_ID` | 32 位十六进制 Account ID |
| 仓库 Variables | `PAGES_PROJECT` | Pages 项目名 |
| 仓库 Secrets | `APP_ID` | App Client ID |
| 仓库 Secrets | `APP_PRIVATE_KEY` | App 私钥 |
| `pages-production` Secrets | `APT_SIGNING_SUBKEY` | `gpg --export-secret-subkeys` 输出的标准 Base64 编码，不含可用主私钥 |
| `pages-production` Secrets | `APT_SIGNING_PASSPHRASE` | 子密钥口令，无密码时留空 |
| `pages-production` Secrets | `CLOUDFLARE_API_TOKEN` | 目标账户的 Pages 部署 token |

## 发布

`update-publication.yml` 每六小时、手动或收到 `project-release-published` 时刷新快照和 epoch，提交后调用 `build.yml` 构建，再由 `publish.yml` 签名、运行 APT 检查并上传。

unsigned artifact 只包含未签名 `public/`、快照、发布参数和源 commit，不包含 Nix 运行时闭包。Publish 从同一提交单独构建 `publication-tools`，不重新渲染站点。

手动 Build 默认不发布，需开启 `publish`。Publish 失败可用 **Re-run failed jobs** 复用构建产物。元数据过期后重新运行 Update，单独重签不会延长有效期。

<a id="key-lifecycle"></a>

## 密钥轮换

先分发包含新旧子密钥的公钥环，仍用旧子密钥签名。客户端的密钥环需要单独更新。完成分发后更新 CI 子密钥和 context 中的签名指纹，验证 APT 客户端，再移除旧子密钥。

## 用户安装

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

## 恢复

发布记录包含源提交、输入快照、签名站点和 Pages deployment ID。回滚时选择上一个可用部署，并检查自定义域名。Release 已过期时，使用原快照和新的 epoch 重建并签名。

上传或部署后检查失败时，先检查 Pages 的实际部署状态，因为新站点可能已经承载流量。修改软件包使用新标签，已有索引仍可能引用旧资产。
