# 输入与开发速查

面向维护者；下游接入见[接入指南](project-onboarding.md)，发布和恢复见[运维速查](operations.md)。

## 输入和输出

| 查找什么 | 位置或规则 |
| --- | --- |
| 仓库地址、suite、架构、有效期、组织 | [config/repository.toml](../config/repository.toml)：当前 `stable/main`，`amd64`、`arm64`，有效期 7 天。 |
| 项目策略与资产映射 | [projects/](../projects/)：每个项目的 `project.toml` 和 `mapping.toml`。 |
| 清单格式 | [schemas/manifest-v1.json](../schemas/manifest-v1.json)。`Filename` 是 Release 资产名；APT 路径由 pool 模板生成。 |
| 已收集的 Release、资产、清单 | [publication/snapshot.json](../publication/snapshot.json)：只收集启用项目中符合策略且带清单的 Release，包括历史版本。中央收集器下载清单，不下载软件包；项目工作流回读验证软件包资产。 |
| 渲染时间和签名指纹 | [publication/context.json](../publication/context.json)。 |
| 历史索引 | 快照的 `index_history` 保留历史 by-hash 索引，直到元数据有效期结束后 300 秒，以支持跨部署请求。 |
| 站点构建 | [nix/site.nix](../nix/site.nix)；产物有 `public/`、`snapshot.json`、`publication.json`，仅部署 `public/`。 |

站点由 Cloudflare Pages 提供索引和 302 重定向；软件包本体留在 GitHub Releases。

## 开发与检查

支持 `x86_64-linux` 和 `aarch64-linux`。命令定义见 [justfile](../justfile)，CLI 参数见 `moraxyc-repo --help`。

```sh
nix develop -c bash -euo pipefail -c 'just check && just preview && just build'
nix build
nix flake check
nix fmt
```

`just build` 的工具路径为 `.build/moraxyc-repo`。`just preview` 使用临时测试密钥和 `.build/preview`；重复预览需要新的输出目录：

```sh
nix develop -c bash -euo pipefail -c   'MORAXYC_INTEGRATION=1 MORAXYC_PREVIEW_DIR="$PWD/.build/preview-2" go test ./tests -run "^TestSignedRepositoryAndTampering$" -count=1'
```

修改 Go 依赖后运行 `nix develop -c just vendor-hash`，并一起提交 `go.mod`、`go.sum` 和 `nix/vendor-hash`；用 `nix build .#moraxyc-repo` 验证。`nix develop -c just update` 更新 Nix 输入和 vendor 哈希，不升级 Go 依赖版本。

## 离线重放

检出发布时的源提交，运行 `nix develop -c just site`；未签名站点位于 `.build/site/public`。使用提交中保存的快照，不重新收集 GitHub 数据。相同提交应产生逐字节一致的未签名站点；签名含签名时间，不比较签名字节。
