# Moraxyc 软件包仓库

从 GitHub Releases 构建 APT 元数据，发布到 `https://pkgs.moraxyc.com/deb`。Cloudflare Pages 承载索引和 `302` 重定向，软件包二进制保留在 GitHub Releases。

## 开发

支持 `x86_64-linux` 和 `aarch64-linux`。

```sh
nix develop -c bash -euo pipefail -c 'just check && just preview && just build'
```

工具输出到 `.build/moraxyc-repo`。预览使用临时测试密钥，输出到 `.build/preview`。重复预览需换用新目录：

```sh
nix develop -c bash -euo pipefail -c \
  'MORAXYC_INTEGRATION=1 MORAXYC_PREVIEW_DIR="$PWD/.build/preview-2" go test ./tests -run "^TestSignedRepositoryAndTampering$" -count=1'
```

```sh
nix build
nix flake check
nix fmt
```

修改 Go 依赖后更新 vendor 哈希，一起提交 `go.mod`、`go.sum` 和 `nix/vendor-hash`：

```sh
nix develop -c just vendor-hash
nix build .#moraxyc-repo
```

`nix develop -c just update` 更新 Nix 输入和 vendor 哈希，不升级 Go 依赖版本。每周的 **Update dependencies** 工作流执行相同更新。

命令和参数使用 `.build/moraxyc-repo --help` 查询。

## 文档

- [输入与离线重放](docs/contracts.md)
- [项目接入](docs/project-onboarding.md)
- [发布与恢复](docs/operations.md)
