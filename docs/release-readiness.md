# 发布准备与支持矩阵

状态：下列未完成验收项继续保留，不因生成或发布安装包而视为通过。平台验收证据见候选矩阵；此文件不构成完整平台支持承诺。

## 候选矩阵

当前发布范围仅为 macOS 15+ arm64 与 Windows amd64。表中区分自动化检查、安装包静态验证和仍待进行的桌面验收。

| 平台 | 候选版本 | 架构 | 状态 |
|---|---|---|---|
| macOS | 15+ | arm64 | 2026-11-17，标签 `v0.1.1`（`3588821`）的 GitHub Actions 编译、vet、单测、DMG 打包及签名/架构检查通过。本地下载 DMG 后核对 SHA-256、镜像完整性、`Signature=adhoc`、arm64、`LSMinimumSystemVersion=15.0`、版本 0.1.1 和 bundle ID；确认包内含 LICENSE、第三方通知和 MySQL v1.10.2 MPL 源码归档。桌面交互、首次打开、安装卸载及辅助功能验收仍待完成 |
| Windows | Windows 10/11 | amd64 | 2026-11-17，标签 `v0.1.1`（`3588821`）的 Windows runner 编译、vet、单测和 NSIS Setup 打包通过，包含 current-user-only protected ACL 测试；本地核对安装器 SHA-256 与 NSIS 文件类型。v0.1.0 因通用权限映射导致旧 ACL 断言失败未发布，v0.1.1 使用具体文件权限后通过。Windows 10/11 桌面启动、安装卸载和凭据 UI 生命周期仍待实机验收；安装器未签名 |

SQLite 保持 `CGO_ENABLED=0`，依赖版本以 go.mod 为准（本次 SQLite v1.62.0、MyGo v0.9.2）。MongoDB、etcd 和 AI 的版本/协议见 `docs/decisions/`。

## 构建

Go 版本与 MyGo CLI 均以当前 go.mod 为准：

```sh
go install github.com/egoist/mygo/cmd/mygo@$(go list -m -f '{{.Version}}' github.com/egoist/mygo)
CGO_ENABLED=0 mygo build -platform darwin/arm64,windows/amd64 -sign - -skip-notarize
```

构建会将 MyGo 原生 UI 打包为各平台安装产物。Windows/Linux 使用 `mygo.json` 指定的 `resources/icon.png`；该 PNG 也作为应用资源保留，用于原生更新弹窗。macOS 额外使用 `packaging/macos/Assets.car`，其资产目录源文件在 `packaging/macos/Assets.xcassets/`，圆角图源为 `resources/icon-macos.png`；`icon-macos.png` 通过 `resourcesExclude` 排除。MyGo 将 `Assets.car` 作为 macOS 图标资源优先于默认 `.icns`，其余平台继续使用通用图标。Windows 应用和安装器的图标仍内嵌在 EXE 中。发布采用 GitHub Releases 直接分发，不考虑各平台应用商店。macOS 使用 ad-hoc 签名，不要求 Developer ID 或 Apple 公证；Windows 安装器未签名。应用内更新使用独立 Ed25519 签名密钥，公钥随应用配置发布，私钥只存受控备份和 CI Secret。公共仓库为 `magalab/dbl`；Actions 构建和草稿发布流程见 [GitHub Actions 说明](github-actions.md)。Go runtime 依赖已生成版本化许可清单，完整上游许可/通知及 MPL-2.0 MySQL 源码已放入 MyGo 自动打包的 `resources/`；最新 darwin/arm64 app bundle 已核对包含 105 个许可/通知/源码文件，与源目录逐文件 SHA-256 一致。DBL 自有代码采用 MIT License，许可文本位于根目录 `LICENSE`，并复制到 `resources/LICENSE` 随应用分发；非 Go 打包资产许可审查和整体法律审阅仍待完成。

macOS 从“DBL”应用菜单、Linux/Windows 从“帮助”菜单打开“关于 DBL”面板；面板显示打包版本、DBL 名称和 MyGo 版本，未打包的开发运行显示“开发版”。

## 阻塞验收项

已识别数据库服务端错误的详情文本脱敏已由单测覆盖；这不替代 AI provider 的真实请求抓包、各平台凭据库检查或桌面交互安全验收。

- 在干净的 macOS arm64 / Windows amd64 环境安装、启动、更新策略演练和卸载，验证配置与数据库文件保留策略。
- 逐个平台确认密钥库读写/删除、设置文件权限、mTLS 私钥生命周期和应用退出后的连接/watch 关闭。macOS 正式 bundle ID 的 32 KiB `v2:` 跨进程 harness 已构建但尚未运行，ad-hoc 签名包重启后引用可用性仍待验证；Windows Credential Manager 尚未实测。
- 对 SQLite、PostgreSQL、MySQL、Redis、MongoDB、etcd 的关键路径做真实服务验收；Redis、MongoDB 与 etcd 明文单节点已验证；PostgreSQL/MySQL profile 私有 CA + verify-full、Redis ACL + verify-full TLS、MongoDB verify-full 证书验证与 SCRAM + TLS 加密路径已验证。PostgreSQL/MySQL 基础 profile 保存/测试/连接/查询已在 macOS 测试窗口分别对 PostgreSQL 17.11 和 MySQL 8.4.13 完成 trust/无密码认证、无 TLS smoke；MySQL `verify-full` + profile CA 路径的真实 TLS 集成也已通过，PG/MySQL CA 输入框和严格 TLS 提示有 UI tester 覆盖。PG/MySQL TLS profile 可见端到端流程与凭据 vault UI 生命周期、MongoDB/etcd/AI UI 人工验收及 etcd 目标部署版本仍缺证据；revision compaction 与短 lease 到期行为已有集成测试。
- 对 AI provider 抓包确认默认请求不含 SQL/错误/schema/真实值；勾选时显示准确发送内容；测试取消、重定向拒绝、失败提示和 key 清理。
- 完成中文输入法、键盘焦点、屏幕阅读器/辅助技术、大字号、窄窗口和高对比度检查。
- 完成 macOS ad-hoc 签名及下载后的首次打开流程；自动更新需在 macOS 用户可写的应用目录与 Windows 当前用户安装目录分别验证检查、签名失败拒绝、下载、重启安装、回滚 / 中断恢复和密钥恢复演练。v0.1.1 用户首次升级需手动安装带更新公钥的版本。SQL 草稿现支持默认关闭、主动开启的本机自动保存与恢复，结果不恢复；设置以原子文件更新保留。草稿重启恢复、未连接标签编辑、超限提示、关闭清空，以及普通 / 最大化 / 全屏启动与字体设置仍需跨平台桌面验收。设置包测试已在文件创建、写入、同步、关闭及替换边界通过子进程主动退出；另以操作系统强制终止子进程覆盖替换前/替换后，重启只读到完整旧值或完整新值。真实掉电/存储故障注入、Windows ACL/替换运行时验收仍待完成。Go runtime 清单、完整许可证文本和 MPL-2.0 驱动源代码已随 macOS bundle 验证；非 Go 资产审查和支持版本政策仍待完成。

满足这些项并附上可复现记录后，才能将相应平台从候选转为支持。
