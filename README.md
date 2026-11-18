# DBL

一个原生桌面数据库工作台，在同一个应用中连接 SQLite、PostgreSQL、MySQL、Redis、MongoDB 和 etcd。

DBL 提供 SQL 查询、多标签工作区、数据浏览与编辑，以及可选的 AI 助手。界面支持中文和英文。

## 功能

| 数据源 | 主要能力 |
|---|---|
| SQLite | 打开或新建数据库文件、只读连接、对象与列浏览、SQL 查询 |
| PostgreSQL / MySQL | TLS 连接、数据库与 Schema 选择、SQL 查询与对象浏览 |
| Redis | SCAN 分页、按类型筛选、键值读取与编辑、TTL、多选与批量删除 |
| MongoDB | 数据库与集合浏览、find、聚合 pipeline、文档插入与 diff 后替换、索引查看 |
| etcd | 多 endpoint、前缀与精确键读取、revision、lease TTL、watch、写入与删除预览 |

- **查询工作区**：最多 16 个标签，SQL 格式化、取消查询、分页结果、复制单元格或行，以及 CSV / JSON 导出。
- **连接管理**：分组、收藏、复制、最近连接、只读模式和开发 / 预发 / 生产环境标记。
- **SQL 草稿**：可在设置 → 常规中开启本机自动保存，重启恢复 SQL 标签、顺序、当前标签及连接 / Schema 引用；不自动连接或执行。默认关闭，关闭后清除已保存草稿。
- **界面设置**：更改会自动保存。普通、最大化或全屏启动在下次打开应用时生效，界面和 SQL / JSON 编辑器的字体及字号在保存成功后即时生效。
- **应用内更新**：顶栏按钮可随时打开原生更新窗口，查看更新说明、下载进度并重启安装；更新包经签名验证。后台检查默认关闭，可在设置 → 常规开启并设置检查间隔。需安装在当前用户可写的目录。
- **数据编辑**：SQL 表数据和结构修改通过预览与确认后提交；可写 SQL 连接上的写入及风险不明确的语句执行前也需确认。
- **AI 助手**：配置多个 OpenAI-compatible provider，获取或选择模型，流式生成与解释 SQL、生成 MongoDB 查询草稿、提供 Redis / etcd 操作建议。

## 安装

当前发布目标为 **macOS 15+ arm64（Apple Silicon）** 和 **Windows 10/11 amd64（x64）**，通过 [GitHub Releases](https://github.com/magalab/dbl/releases) 直接分发。

### macOS

下载 arm64 的 DMG，将 `DBL.app` 拖入“应用程序”。应用使用 ad-hoc 签名，未经 Apple 公证，首次打开时先到“系统设置 → 隐私与安全性”允许打开 DBL。如果仍提示应用处于隔离状态，可移除 DBL.app 的下载隔离属性：

```sh
xattr -dr com.apple.quarantine /Applications/DBL.app
```

此命令只移除 DBL.app 的下载隔离标记；Homebrew 安装后也会显示这条提示。

也可通过 Homebrew 安装：

```sh
brew install --cask magalab/tap/dbl
```

### Windows

下载 amd64 的 Setup 安装器并运行。

安装包与 Homebrew Cask 的可用状态以 Releases 和 [Homebrew tap](https://github.com/magalab/homebrew-tap) 为准。最新安装包的验收进度见 [发布准备清单](docs/release-readiness.md)。

## 开始使用

1. 新建连接，选择数据源并填写连接信息；SQLite 可直接选择或新建文件。
2. 连接后，从侧栏选择对象；SQL 数据源可新建查询标签编写并执行 SQL。
3. 在设置中调整语言、外观和查询历史；需要 AI 时，再配置 provider 并打开助手。

| 操作 | macOS | Windows |
|---|---|---|
| 新建连接 | `⌘ N` | `Ctrl N` |
| 打开 SQLite 文件 | `⌘ O` | `Ctrl O` |
| 新建查询 | `⌘ T` | `Ctrl T` |
| 执行 SQL | `⌘ Enter` | `Ctrl Enter` |
| 打开设置 | `⌘ ,` | `Ctrl ,` |
| 切换侧栏 | `⌘ B` | `Ctrl B` |
| 切换 AI 助手 | `⌘ Shift B` | `Ctrl Shift B` |

## 数据与隐私

AI 请求由用户点击发送后提交给所选 provider。SQL、错误、Schema 等上下文仅在明确勾选时附带；生成内容作为草稿保留，不会自动执行 SQL 或修改数据。

数据库密码、API key 和 TLS 私钥保存在本机加密凭据库中，TOML 设置文件只保存凭据引用。密文和解密密钥位于同一私有目录，适用于防止直接从设置文件读取秘密，不能防护整个配置目录被复制或当前系统账户被攻破。配置位于用户配置目录的 `dbl/settings.toml` 和 `dbl/credentials/`。旧版 `settings.json` 会在首次启动时自动迁移。

查询历史默认只在内存保留最近 100 条，也可在历史面板开启本机保存并随时清除。SQL 草稿默认不落盘；开启自动保存后，停止输入约 750 毫秒保存一次，正常退出前补存，最多保存 16 个 SQL 标签、每条 SQL 上限 256 KiB。草稿和历史一样可能包含敏感值，以明文保存在仅当前用户可访问的 `settings.toml` 中。超限草稿不保存并显示提示，关闭标签会移除其草稿。结果集、表格 / 结构编辑草稿不恢复；异常退出可能丢失最后一次自动保存后的输入。

应用内更新由 MyGo 对 GitHub Releases 中的平台更新包进行签名校验。更新包签名公钥随应用发布，私钥保存在当前用户受限的 MyGo 密钥目录和 GitHub Actions Secret 中；还应在密码管理器中保留离线备份。旧版 v0.1.1 未内置更新公钥，首次获得应用内更新能力需要先手动安装包含更新配置的新版本。

## 使用边界

- 结果表排序仅作用于已读取的数据；完整排序应在 SQL 中使用 `ORDER BY`。
- CSV / JSON 完整导出会在只读连接上重新执行生成结果的查询。并发写入或非确定性 SQL 可能使导出内容与当前显示不同。
- 切换查询标签会停止原标签的后续结果读取，并保留已读部分；大结果集使用有界内存，较早的行可能被释放。
- MongoDB 聚合禁止 `$out` / `$merge`；etcd 默认精确键删除，前缀删除需显式开启并先预览。

## 开发

使用 [go.mod](go.mod) 指定的 Go 版本，所有 SQLite 构建保持纯 Go：

```sh
CGO_ENABLED=0 go run .
CGO_ENABLED=0 go build ./...
```

开发、测试和数据库集成命令见 [开发与验证](docs/development.md)。发布工作流、签名策略及 Homebrew 自动更新见 [GitHub Actions 与发布](docs/github-actions.md)。

## 许可

DBL 使用 [MIT License](LICENSE)。第三方组件遵循各自许可，见 [第三方通知](THIRD_PARTY_NOTICES.md)。完整许可文本及相关源码随安装包分发。
