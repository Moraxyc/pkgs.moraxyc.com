# Moraxyc 软件包仓库

从 GitHub Releases 构建 APT 元数据，发布到 `https://pkgs.moraxyc.com/deb`。Cloudflare Pages 承载索引和精确 `302` 重定向，软件包二进制仍在 GitHub Releases。

## 本地检查

`just check` 做严格配置校验、Go 测试与 vet、项目 Release 交接测试、工作流与 Shell lint；`just preview` 还运行隔离的 APT 客户端，把签名的预览目录写到 `.build/preview`。临时测试密钥**不是**生产信任根；重复预览请换用新的输出目录：

```sh
nix develop -c bash -euo pipefail -c 'just check && just preview && just build'
```

```sh
nix develop -c bash -euo pipefail -c \
  'MORAXYC_INTEGRATION=1 MORAXYC_PREVIEW_DIR="$PWD/.build/preview-2" go test ./tests -run "^TestSignedRepositoryAndTampering$" -count=1'
```

## 开发环境

`nix develop` 使用 flake-parts 的 `dev` 分区。`flake.lock` 固定 Nix 输入和工具链，`go.mod`、`go.sum`、`go2nix.toml` 固定 Go 依赖，构建缓存位于 `.build/`。

`go2nix` 为每个包生成独立 derivation，构建 CLI 并运行 Go 测试。求值插件需要匹配的 Nix ABI，请使用锁定版本 wrapper：

```sh
nix run .#nix-go2nix -- build
nix run .#nix-go2nix -- flake check
nix fmt
```

`nix develop` 提供 `nix-go2nix`；`just build` 用它输出 `.build/moraxyc-repo`。支持 `x86_64-linux` 和 `aarch64-linux`。修改 Go 依赖后运行 `nix develop -c go2nix generate`，一起提交 `go.mod`、`go.sum`、`go2nix.toml`；`go2nix check` 检查锁定文件漂移。

`nix develop -c just update` 刷新 Nix 输入并重新生成模块哈希。每周的 **Update dependencies** 工作流也执行这些操作，运行 `flake check` 和 `check`/`integration`/`build` 流程，再把 `flake.lock`、`go2nix.toml` 提交到默认分支。它不升级 `go.mod` 中的版本，也不自动合并；推送前运行这些校验，推送会触发 Check 工作流。默认分支若要求 PR 或必需状态检查，直接推送会被拒绝。

## 命令

在 `nix develop` 中运行 `just build`，再执行 `.build/moraxyc-repo`。

| 命令 | 输入与结果 |
| --- | --- |
| `check-config` | 严格校验 `config/`，发现 `projects/*/project.toml`。 |
| `collect` | 获取 Release 元数据/清单，原子写入 `.build/snapshot.json`。 |
| `validate-snapshot` | 校验启用项目、资产、映射和全局冲突。 |
| `render` | 用快照、公钥环和显式 epoch 生成 `.build/public`。 |
| `verify` | 重算索引、重定向和 Release 哈希，拒绝未声明文件。 |
| `sign` | 校验目录树；导入 CI 子密钥到临时 home，签名并校验两份签名。 |
| `apt-smoke` | 校验已签名目录树，隔离运行 `apt-get update`，查询已索引软件包版本。 |
| `smoke` | 校验本地目录树，比较自定义域名的元数据和重定向响应。 |
| `manifest` | 从最终 `.deb` 文件生成并校验清单。 |
| `verify-artifacts` | 校验精确资产名、大小、哈希和 Debian control 字段。 |

`render`、`verify`、`sign`、`apt-smoke`、`smoke` 需要 `--primary-fingerprint`、`--signing-fingerprint`、`--epoch`（或 `SOURCE_DATE_EPOCH`）；签名后使用 `verify --signed`。显式 flag 可覆盖路径，`--root` 只选择配置和项目注册表。渲染离线执行，快照必须包含全部且仅包含已启用项目。

## 生产就绪状态

实现和合成预览可用，生产环境尚未配置：`projects/` 没有启用项目，`keys/` 没有归档密钥环。维护者仍需提供经评审的公钥、签名指纹、CI 密钥、Pages 项目和自定义域名；完成前发布流程会失败。本地检查不会创建生产服务。

- [契约与离线重放](docs/contracts.md)
- [项目接入与发布门禁](docs/project-onboarding.md)
- [签名、部署与恢复](docs/operations.md)
