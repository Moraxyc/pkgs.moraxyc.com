# 输入与离线重放

## 输入

- `config/repository.toml` 配置组织、架构和元数据有效期，目前支持 `stable/main`。
- `projects/<id>/` 保存项目策略和 pool 映射。
- `publication/snapshot.json` 保存收集的 Release、资产和清单。只收集启用项目中符合策略且带清单的 Release，包含历史版本。
- `publication/context.json` 保存渲染 epoch、主密钥和签名子密钥指纹。

清单格式由 [JSON Schema](../schemas/manifest-v1.json) 定义。`Filename` 是 Release 中的资产名，APT 路径由项目的 pool 模板生成。中央收集器只下载清单，软件包字节由项目发布工作流回读验证。

快照中的 `index_history` 保存历史 by-hash 索引，保留到元数据有效期结束后 300 秒，供跨部署的 APT 请求使用。

## 重放

检出发布时的源提交，然后构建：

```sh
nix develop -c just site
# Unsigned public tree: .build/site/public
```

site 产物包含 `public/`、`snapshot.json` 和 `publication.json`。只部署 `public/`。

相同提交应生成逐字节一致的未签名站点。签名包含签名时间，不比较字节。离线重放使用已保存的快照，不重新收集 GitHub 数据。
