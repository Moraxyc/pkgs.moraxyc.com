# 下游项目接入指南

本文供在下游仓库工作的 Agent 使用。目标是让项目的 GitHub Release 提供经过验证的 Debian 软件包及清单，并由中央仓库收录。中央仓库是 `moraxyc/pkgs.moraxyc.com`；用户安装步骤见[首页](../README.md)。

## 开始前

1. 确认下游仓库属于中央配置的 GitHub 组织，确定中央项目 ID、软件包名、资产名和目标架构。中央当前配置为 `moraxyc` 组织、`amd64` 和 `arm64`；也接受架构无关的 `all` 软件包。
2. 确认项目能通过 `nix-shell --pure shell.nix --run 'just release'` 构建并测试最终软件包，输出到 `dist/`。该目录只应包含最终 `.deb` 和生成的清单。
3. 确定经审查的中央工具仓库 40 位小写提交 SHA。确认谁能修改下游 Actions 设置和中央项目注册；不要自行创建远程配置、推送标签或发布 Release，除非得到相应授权。

## 修改下游仓库

1. 以 [release.yml 模板](../examples/project/.github/workflows/release.yml)为基础添加发布工作流；按项目实际构建方式调整构建步骤，保留最终产物的生成和验证。
2. 请有权限的人配置下游仓库 Actions 变量：
   - `APT_PROJECT_ID`：中央注册目录名。
   - `REPOSITORY_TOOLS_REF`：经审查的中央仓库 40 位小写提交 SHA。
3. 配置下游 Actions secret `CENTRAL_DISPATCH_TOKEN`，使其有权向中央仓库发送 `repository_dispatch`。不要将 token 写入仓库或日志。

模板监听 `v*` 标签，依次构建并测试、生成清单、验证 Actions artifact、发布 Release、回读验证 Release 资产，最后通知中央仓库。它默认发布稳定版本；项目和中央两边都需配合才能收录 prerelease：在 `project-ci publish` 和 `project-ci verify` 调用中添加 `--prerelease`，并在中央启用 `include_prereleases`。其他架构需要调整 `project-ci --architectures` 和中央配置。

## 注册中央项目

在中央仓库添加 `projects/<id>/project.toml`：

```toml
[project]
repository = "moraxyc/example"
enabled = false
manifest_asset = "moraxyc-manifest.json"
include_prereleases = false
```

添加 `projects/<id>/mapping.toml`，按实际包名和资产名修改：

```toml
[[rules]]
package = "example"
architecture = "*"
asset = "example_*.deb"
pool = "pool/{project}/{release_tag}/{filename}"
```

目录名就是项目 ID，必须与 `APT_PROJECT_ID` 一致。预览并检查映射后设为 `enabled = true`；禁用项目会在下次索引更新时移除该项目。如果当前只能修改下游仓库，把上述中央变更作为交接事项，不要宣称接入已完成。

## 验证与恢复

- 本地构建工具后，可对最终产物执行：

  ```sh
  moraxyc-repo manifest --artifacts dist --project example --tag v1.0.0
  moraxyc-repo verify-artifacts --artifacts dist --project example --tag v1.0.0
  ```

  参数须替换为实际项目 ID 和标签。命令详情以 `moraxyc-repo --help` 为准。
- 中央配置运行 `nix develop -c just check`；预览和离线构建见[输入与开发速查](contracts.md)。
- 经授权发布后，检查下游各 job 成功、Release 包含清单和预期资产，中央 Update publication 成功，随后在目标架构的 APT 客户端确认索引与安装。仅工作流成功不等于客户端已验证。
- 上传中断可重跑同一标签恢复草稿，不替换已完成的资产；重跑已公开标签只校验资产。旧工具留下的不完整公开 Release 需要显式恢复。历史 Release 只有提供清单才会纳入索引。
- 中央发布或客户端失败时查阅[发布与恢复速查](operations.md)；不要通过关闭签名验证绕过问题。
