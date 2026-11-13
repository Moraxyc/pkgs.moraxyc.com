# 项目接入

## 注册

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

目录名是项目 ID，仓库需属于配置的 GitHub 组织。预览通过后设为 `enabled = true`。禁用会从下一次索引移除该项目。

## 项目工作流

复制[工作流模板](../examples/project/.github/workflows/release.yml)，配置：

- Variable `APT_PROJECT_ID` 为中央项目 ID。
- Variable `REPOSITORY_TOOLS_REF` 为本工具仓库的 40 位提交 SHA。
- Secret `CENTRAL_DISPATCH_TOKEN` 用于向中央仓库发送 dispatch。

项目需提供 `nix-shell --pure shell.nix --run 'just release'`，将最终软件包输出到 `dist/`。模板生成清单、校验上传产物、发布 Release，再回读资产并通知中央仓库。

默认发布稳定的 `v*` 标签，架构为 `amd64,arm64`，另接受 `all`。其他架构需修改 `project-ci --architectures` 和中央配置。prerelease 需为 publish 和 verify 调用添加 `--prerelease`，并启用项目的 `include_prereleases`。

上传中断可重跑同一标签以恢复草稿，不替换已完成的资产。重跑已公开的标签只校验其资产；旧工具留下的不完整公开 Release 仍须显式恢复。

## 本地生成清单

`dist/` 只放最终 `.deb` 和生成的清单：

```sh
moraxyc-repo manifest --artifacts dist --project example --tag v1.0.0
moraxyc-repo verify-artifacts --artifacts dist --project example --tag v1.0.0
```

历史 Release 也需提供清单才能纳入索引。
