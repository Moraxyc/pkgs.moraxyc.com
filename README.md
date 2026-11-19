# Moraxyc 软件包仓库

适用于 Debian 和 Ubuntu 的 APT 软件包仓库，提供 `amd64` 和 `arm64` 软件包。

## 项目

| Project | Source |
| --- | --- |
| fetchrix | [GitHub](https://github.com/moraxyc/fetchrix) |
| remora | [GitHub](https://github.com/moraxyc/Remora) |
| whois42d-ng | [GitHub](https://github.com/moraxyc/whois42d-ng) |

## 添加仓库

需要 `curl`、`gpg` 和管理员权限，以与 [GitHub 公钥](https://github.com/moraxyc.gpg)的主密钥指纹核对：

```sh
curl -fsSL https://pkgs.moraxyc.com/deb/keys/moraxyc-archive-keyring.gpg -o /tmp/moraxyc-archive-keyring.gpg
gpg --show-keys --with-fingerprint /tmp/moraxyc-archive-keyring.gpg
curl -fsSL https://github.com/moraxyc.gpg | gpg --show-keys --with-fingerprint
```

主密钥指纹一致后，添加仓库：

```sh
sudo install -d -m 0755 /etc/apt/keyrings
sudo install -m 0644 /tmp/moraxyc-archive-keyring.gpg /etc/apt/keyrings/moraxyc-archive-keyring.gpg
printf '%s\n' 'deb [signed-by=/etc/apt/keyrings/moraxyc-archive-keyring.gpg] https://pkgs.moraxyc.com/deb stable main' | sudo tee /etc/apt/sources.list.d/moraxyc.list
sudo apt-get update
```

## 安装软件

例如安装 fetchrix：

```sh
sudo apt-get install fetchrix
```

如果找不到软件包，请确认该项目已发布适用于本机架构的版本，并重新运行 `sudo apt-get update`。

## 移除仓库

```sh
sudo rm /etc/apt/sources.list.d/moraxyc.list /etc/apt/keyrings/moraxyc-archive-keyring.gpg
sudo apt-get update
```

这不会卸载已安装的软件包。

## 其他文档

- [下游项目接入指南](docs/project-onboarding.md)：为项目 Agent 编写。
- [输入与开发速查](docs/contracts.md) · [发布与恢复速查](docs/operations.md)：供维护者检索。
