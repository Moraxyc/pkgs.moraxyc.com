# 仓库契约

## 配置与路径

`config/repository.toml` 严格解析，键名按大小写精确匹配；未知或重复键、缺少字段、非法数组和不支持的 suite 均拒绝。版本 1 支持 `stable/main`、以 `/deb` 结尾的 HTTPS 基地址和显式目标架构列表。`all` 只用于软件包记录。有效期为 1 到 30 天。

项目目录 ID 只能包含小写字母、数字和连字符。仓库必须属于配置的 GitHub 组织。资产名和发布标签必须是单段字符串，匹配 `[A-Za-z0-9_][A-Za-z0-9_.+~-]*` 且不含 `..`；不支持含斜杠的标签。注册表中的符号链接和非普通文件会使校验失败。

映射包含精确包名、精确架构或 `*`、文件名 glob，以及带 `{project}`、`{release_tag}` 和 `{filename}` 的 `pool/` 模板。每条记录只能匹配一条规则，路径必须是规范形式。未知变量、路径穿越、跨组织仓库和源路径碰撞都会在发布前被拒绝。

## 清单与快照

[`manifest-v1.json`](../schemas/manifest-v1.json) 内嵌在 Go 二进制中。JSON Schema 校验先于语义校验；重复键和尾随数据拒绝。版本 1 接受 `Package`、`Version`、`Architecture`、`Filename`、`Size`、`SHA256`、`Description` 及可选的 `Depends`，不透传其他 control 字段。Description 续行以一个空格开头，空段落编码为 ` .`，其他控制字符禁止出现。

清单中的 `Filename` 是精确的 `.deb` 资产名，生成的 APT `Filename` 来自匹配的 pool 模板。所有 Release 都保留，不选择隐式的最新版本。已启用注册表中出现重复的包/版本/架构三元组会失败。

收集器通过 GitHub API 读取 Release 和资产分页，只下载选中的清单资产；可用时校验清单资产的大小和摘要，并校验每个软件包资产的大小及可选的 API 摘要。缺少摘要的资产保持**未验证**状态。中央服务不证明软件包二进制内容，该证明来自项目最终的 Release 回读。

请求设有连接、响应头和整体超时，清单响应上限为 1 MiB。通过 go-github 提取分页页码，请求始终使用原来源和资源，重复页码会使收集失败。限流错误直接使本次收集失败，不自动重试。CLI 将完整收集限制在 15 分钟内。重定向后的清单下载使用不带认证的客户端，并限制目标主机。任一项目失败都会中止刷新，只有全部项目通过校验后才替换旧快照。

快照 v1 保存项目标识、Release ID/标签/发布时间、资产 ID/名称/大小/摘要、清单和收集时间戳。快照按项目 ID、Release ID 和资产 ID 规范化；软件包按包名、版本、架构和文件名的字节序排列。收集时间戳只用于审计，不参与渲染字节。

## 渲染与校验

`Architecture: all` 的记录出现在每个已配置的二进制索引中。软件包字段顺序固定。gzip 使用 Go 的 level-9 压缩器，不写入文件名和时间戳。Nix site derivation 使用 `flake.lock` 固定的工具链和 Go 版本；明文和压缩索引都有普通文件形式的 SHA256 和 SHA512 by-hash 副本，支持 APT 优先选用 Release 中的 SHA512 摘要。

`update-publication` 把当前及历史索引字节保存到快照的可选 `index_history` 对象。键为精确的 by-hash 路径，值包含 `data`（base64 编码字节）和 `expires_at`（UTC Unix 秒）。保留期为该索引最近一次发布 epoch 加当时配置的有效期，再加 300 秒缓存窗口；内容不变时随刷新延长。刷新会清理过期条目，跳过部署不会丢失前次历史。渲染及校验检查每条历史路径和摘要，只使用在显式 render epoch 时尚未过期的条目，包括已移除架构的索引。历史字节作为提交中的输入保存，离线重建无需访问旧部署。渲染器在 Pages 的 20,000 文件限制内为非索引输出预留十个文件，超限会失败，不会静默丢弃未过期历史。

`apt-ftparchive release` 对完成的索引计算哈希。Date 来自必需的 render epoch，Valid-Until 为 Date 加配置的有效期。校验器重算每个输出的 MD5、SHA1、SHA256 和 SHA512 条目，并要求每个索引都有 SHA256 覆盖；同时从快照重新生成 Packages、gzip、by-hash、请求头和重定向，拒绝被修改或未声明的文件、目录、符号链接及缺失输出。detached 签名和 clear 签名必须用预期的签名子密钥和主密钥验证当前 Release。

重定向只使用已配置的 GitHub 仓库及通过校验的精确标签和资产名，并生成排序后的静态 `302` 规则。超过 Pages 的 2000 条静态重定向上限、单条规则 1000 字节或单个索引文件 25 MiB 时，渲染失败。声明 `404.html` 会关闭 Pages 对未知路径的隐式 SPA 回退；超出容量需评审路由设计。输出不包含 `.deb` 二进制文件。

渲染会新建同级临时目录，校验 Release 哈希后将其重命名为不存在的目标；不会覆盖已有的 public 目录树。签名失败的结果不可部署，重试前须重新渲染到新的输出目录。

## 重放已保存的输入

检出记录的源提交及两个锁文件、公钥环。Update 已将发布输入提交到 Git，无需恢复其他文件。导入 unsigned artifact 的 Nix 闭包后，可用 site 产物中的 `snapshot.json` 和 `publication.json` 审计输入。然后重建：

```sh
nix develop -c just site
# Unsigned public tree: .build/site/public
```

site 产物包含未签名站点 `public/`、输入文件 `snapshot.json` 和 `publication.json`，以及指向发布工具的 `tools`。只部署 `public/`。`just site` 使用锁定的 Nix/go2nix 求值器；CI 配置好该求值器后执行 `nix build .#site`。

`publication/context.json` 固定 `epoch`、`primary_fingerprint` 和 `signing_fingerprint`。`nix/site.nix` 提供 `packages.site`，使用 Git 跟踪的发布输入、锁定工具链、UTC 和 C locale 渲染，并按记录的 epoch 检查公钥有效期。求值为纯模式，构建不访问网络或签名凭据。初始参数尚未配置；构建生产 site 前需填写正数 epoch、经评审的指纹并提供公钥环。收集 GitHub 数据属于外部输入更新，重放必须使用已保存的快照。

配置和密钥环必须来自同一已审计提交。相同的快照、配置、公钥环、epoch 和工具链应产生逐字节一致的未签名输出。签名包含签名时间，只验证签名，不比较字节。回滚到已过期的元数据需要新的显式 epoch 和签名；不要关闭 APT 过期检查。
