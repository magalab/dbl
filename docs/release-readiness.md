# 发布准备与支持矩阵

状态：准备 0.1.0 首次公开试发；下列未完成验收项继续保留，不因生成或发布安装包而视为通过。此文件记录候选范围及验收证据，不构成完整平台支持承诺。

## 候选矩阵

当前发布范围仅为 macOS 15+ arm64 与 Windows amd64。表中旧版跨架构构建记录仅作为历史证据；两个目标均需对最新安装包重新验收。

| 平台 | 候选版本 | 架构 | 状态 |
|---|---|---|---|
| macOS | 15+ | arm64 | 2026-10-29 基于提交 `07d5c27` 使用 MyGo 0.6.2 重建 universal `DBL.app`（ad-hoc 签名，跳过 DMG），其中包含 arm64；bundle ID `com.magalab.dbl`、版本 `0.1.0`、当时最低系统版本 12.0（当前配置已改为 15.0，需重新打包验收），`lipo` 架构和 `codesign --verify --deep --strict` 均通过。源 `resources/` 的 108 个文件全部与包内哈希一致，包内另有 `AppIcon.icns` 和 `Assets.car`。未启动该 bundle，连接弹窗、设置、快捷键、COUNT 取消与连接关闭行为仍缺最新包的可见交互验收；此前旧测试窗口的 SQLite、PostgreSQL、MySQL 基础连接/查询 smoke 不能替代本次验收。自定义 CA 表单、AI、生产环境提示尚无可见验收；数据库集成已覆盖 PostgreSQL/MySQL 私有 CA + verify-full、MongoDB/Redis/etcd 单机及认证/TLS；ad-hoc 签名包下载后的首次打开、干净安装/卸载未验证 |
| Windows | Windows 10/11 | amd64 | 2026-10-29 基于当前工作树，使用 MyGo 0.6.2 与 NSIS 3.13 从 macOS arm64 生成 amd64/arm64 的 PE32+ 主程序及 NSIS Setup 安装器；`file` 确认主程序架构分别为 x86-64/ARM64，安装器识别为 NSIS；两个架构的应用目录均包含 LICENSE 和第三方通知。目标系统启动、安装卸载、Windows ACL/凭据库及 Authenticode 签名仍待验证；本次构建未签名 |

SQLite 使用 `modernc.org/sqlite v1.61.0`，保持 `CGO_ENABLED=0`。MyGo 固定 `v0.6.2`。MongoDB、etcd 和 AI 的版本/协议见 `docs/decisions/`。

## 构建

Go 版本与 MyGo CLI 均以当前 go.mod 为准：

```sh
go install github.com/egoist/mygo/cmd/mygo@$(go list -m -f '{{.Version}}' github.com/egoist/mygo)
CGO_ENABLED=0 mygo build -platform darwin/arm64,windows/amd64 -sign - -skip-notarize
```

构建会将 MyGo 原生 UI 打包为各平台安装产物。Windows/Linux 使用 `mygo.json` 指定的 `resources/icon.png`；macOS 额外使用 `packaging/macos/Assets.car`，其资产目录源文件在 `packaging/macos/Assets.xcassets/`，圆角图源为 `resources/icon-macos.png`。MyGo 将 `Assets.car` 作为 macOS 图标资源优先于默认 `.icns`，其余平台继续使用通用图标。发布采用 GitHub Releases 直接分发，不考虑各平台应用商店。macOS 使用 ad-hoc 签名，不要求 Developer ID 或 Apple 公证；Windows 签名和应用内自动更新密钥尚未配置。公共仓库为 `magalab/dbl`；Actions 构建和草稿发布流程见 [GitHub Actions 说明](github-actions.md)。签名私钥、更新签名密钥只能来自 CI secret 或受控密钥库，不得写入 `mygo.json`、源码或发布日志。Go runtime 依赖已生成版本化许可清单，完整上游许可/通知及 MPL-2.0 MySQL 源码已放入 MyGo 自动打包的 `resources/`；最新 darwin/arm64 app bundle 已核对包含 105 个许可/通知/源码文件，与源目录逐文件 SHA-256 一致。DBL 自有代码采用 MIT License，许可文本位于根目录 `LICENSE`，并复制到 `resources/LICENSE` 随应用分发；非 Go 打包资产许可审查和整体法律审阅仍待完成。

macOS 从“DBL”应用菜单、Linux/Windows 从“帮助”菜单打开“关于 DBL”面板；面板显示打包版本、DBL 名称和 MyGo 版本，未打包的开发运行显示“开发版”。

## 阻塞验收项

已识别数据库服务端错误的详情文本脱敏已由单测覆盖；这不替代 AI provider 的真实请求抓包、各平台凭据库检查或桌面交互安全验收。

- 在干净的 macOS arm64 / Windows amd64 环境安装、启动、更新策略演练和卸载，验证配置与数据库文件保留策略。
- 逐个平台确认密钥库读写/删除、设置文件权限、mTLS 私钥生命周期和应用退出后的连接/watch 关闭。macOS 正式 bundle ID 的 32 KiB `v2:` 跨进程 harness 已构建但尚未运行，ad-hoc 签名包重启后引用可用性仍待验证；Windows Credential Manager 尚未实测。
- 对 SQLite、PostgreSQL、MySQL、Redis、MongoDB、etcd 的关键路径做真实服务验收；Redis、MongoDB 与 etcd 明文单节点已验证；PostgreSQL/MySQL profile 私有 CA + verify-full、Redis ACL + verify-full TLS、MongoDB verify-full 证书验证与 SCRAM + TLS 加密路径已验证。PostgreSQL/MySQL 基础 profile 保存/测试/连接/查询已在 macOS 测试窗口分别对 PostgreSQL 17.11 和 MySQL 8.4.13 完成 trust/无密码认证、无 TLS smoke；MySQL `verify-full` + profile CA 路径的真实 TLS 集成也已通过，PG/MySQL CA 输入框和严格 TLS 提示有 UI tester 覆盖。PG/MySQL TLS profile 可见端到端流程与凭据 vault UI 生命周期、MongoDB/etcd/AI UI 人工验收及 etcd 目标部署版本仍缺证据；revision compaction 与短 lease 到期行为已有集成测试。
- 对 AI provider 抓包确认默认请求不含 SQL/错误/schema/真实值；勾选时显示准确发送内容；测试取消、重定向拒绝、失败提示和 key 清理。
- 完成中文输入法、键盘焦点、屏幕阅读器/辅助技术、大字号、窄窗口和高对比度检查。
- 确认 macOS ad-hoc 签名及下载后的首次打开流程；Windows 若启用签名或后续启用自动更新，需执行相应密钥恢复演练；崩溃恢复策略已定为不恢复未保存的 SQL 草稿/结果、设置以原子文件更新保留。设置包测试已在文件创建、写入、同步、关闭及替换边界通过子进程主动退出；另以操作系统强制终止子进程覆盖替换前/替换后，重启只读到完整旧值或完整新值。真实掉电/存储故障注入、Windows ACL/替换运行时验收仍待完成。Go runtime 清单、完整许可证文本和 MPL-2.0 驱动源代码已随 macOS bundle 验证；非 Go 资产审查和支持版本政策仍待完成。

满足这些项并附上可复现记录后，才能将相应平台从候选转为支持。
