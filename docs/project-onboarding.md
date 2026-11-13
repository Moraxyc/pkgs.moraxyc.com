# 项目接入

## 注册项目

添加 `projects/<id>/project.toml`：

```toml
[project]
repository = "moraxyc/example"
enabled = false
manifest_asset = "moraxyc-manifest.json"
include_prereleases = false
```

同时添加 `mapping.toml`：

```toml
[[rules]]
package = "example"
architecture = "*"
asset = "example_*.deb"
pool = "pool/{project}/{release_tag}/{filename}"
```

评审包名、架构、路径模板和不可变标签策略。在发布门禁和中央预览通过前保持项目禁用。禁用会从下一次完整索引中移除该项目的所有 Release；将其视为显式撤回，并保留可回滚部署。

## 生成并校验资产

项目仓库负责软件包构建和运行时测试；release 目录只能包含最终的 `.deb` 文件和 `moraxyc-manifest.json`。本版本不包含可选的 `SHA256SUMS` 文件。工具读取每个软件包的 Debian control 字段，并根据实际字节计算文件大小和 SHA256：

```sh
moraxyc-repo manifest --artifacts dist --project example --tag v1.0.0
moraxyc-repo verify-artifacts --artifacts dist --project example --tag v1.0.0
```

两条命令都接受 `--architectures amd64,arm64`，并额外接受 `all`。生成器先写临时清单，再原子改名；校验器拒绝多余文件、符号链接、被修改的字节和不匹配的字段。

## 安装项目工作流

[工作流模板](../examples/project/.github/workflows/release.yml) 包含：

1. `build-and-test`：运行项目的 `nix-shell --pure shell.nix --run 'just release'`，在全新的 `dist/` 目录构建并测试所有软件包，生成清单，并用 `if-no-files-found: error` 上传文件集。
2. `artifact-round-trip`：在另一个 job 中下载该文件集并校验它。
3. `publish-release`：确认已有标签指向被测试的提交；创建或恢复草稿，补齐缺失资产，下载并验证字节后再公开；对已公开的 Release 仅作比较，不替换资产。
4. `release-round-trip`：把 Release 资产下载到新目录，重新校验，并将每个资产的摘要与已测试的文件集比较。
5. `notify-central`：只有在 Release 回读成功后才发送 `project-release-published`。中央服务仍会执行完整收集。

复制模板到项目仓库并配置：

- `APT_PROJECT_ID`：经过评审的中央目录 ID。
- `REPOSITORY_TOOLS_REF`：本工具仓库经过评审的 40 位提交 SHA。模板拒绝分支名或空值。
- `CENTRAL_DISPATCH_TOKEN`：只被允许向中央仓库发送 dispatch 的凭据，仅在通知步骤中提供。

模板发布稳定的 `v*` 标签。如果需要 prerelease，请把两处项目门禁调用改为使用 `--prerelease`，并评审中央项目的 `include_prereleases` 策略。如果架构不同，还要修改复制后的工作流中的 `moraxyc-repo project-ci` 命令及中央策略。

项目工具通过 `GH_TOKEN`（缺失时使用 `GITHUB_TOKEN`）认证 GitHub API 请求。重定向后的资产下载使用不带认证的客户端，只允许配置的 API 主机及 GitHub 资产 CDN 主机。构建 job 没有仓库写权限，只有发布 job 获得 `contents: write`。

上传中断会留下草稿，中央收集器会忽略它。重跑同一标签时会找到该草稿，删除失败上传的 `starter` 占位资产，并只补齐缺失文件，不替换已完成的资产。全部资产下载后须与已测试文件集一致，再次确认标签指向被测试的提交后才公开。资产被修改或出现额外资产时校验失败，需要评审恢复方案。重跑已公开的标签只校验其资产；旧工具留下的不完整公开 Release 仍须显式恢复。

要将历史 Release 纳入 APT 索引，须为它们提供有效清单。

首次门禁成功后保存中央快照并预览。确认预期的包/版本/架构记录和 pool URL 后再启用项目，并将首次 `apt update`、包查询和 `apt install` 结果记入生产验收记录。
