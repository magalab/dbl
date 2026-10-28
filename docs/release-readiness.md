# 发布准备与支持矩阵

状态：发布被阻塞；此文件记录候选范围和必须完成的证据，不构成平台支持承诺。

## 候选矩阵

| 平台 | 候选版本 | 架构 | 状态 |
|---|---|---|---|
| macOS | 12+ | arm64 | 2026-10-29 基于提交 `5a95ac0` 使用 MyGo 0.6.2 重建候选包 `/private/tmp/dbl-latest-candidate/darwin-arm64/DBL.app`（ad-hoc 签名，跳过 DMG）；bundle ID `com.magalab.dbl`、版本 `0.1.0`、最低系统版本 12.0，arm64 Mach-O 与 `codesign --verify --deep --strict` 均通过。源 `resources/` 的 108 个文件全部与包内哈希一致，包内另有 `AppIcon.icns` 和 `Assets.car`。未启动该 bundle，故连接弹窗、设置、快捷键和本轮 COUNT 取消行为仍缺最新包的可见交互验收；此前旧测试窗口的 SQLite、PostgreSQL、MySQL 基础连接/查询 smoke 不能替代本次验收。自定义 CA 表单、AI、生产环境提示尚无可见 bundle 验收；数据库集成已覆盖 PostgreSQL/MySQL 私有 CA + verify-full、MongoDB/Redis/etcd 单机及认证/TLS；正式签名、公证、干净安装/卸载未验证 |
| macOS | 12+ | amd64/universal | 2026-10-29 基于提交 `5a95ac0` 使用 MyGo 0.6.2 重建 universal `DBL.app`（ad-hoc 签名，跳过 DMG）；`lipo -archs` 确认 arm64 + x86_64，bundle ID `com.magalab.dbl`、版本 `0.1.0`、最低系统版本 12.0、codesign 校验通过，源 `resources/` 的 108 个文件全部与包内哈希一致。此次 DMG 构建在 `hdiutil create` 阶段失败，宿主返回“设备未配置”；旧 DMG 的验证不代表本次提交。应用未启动，Intel 硬件启动和最新 UI 交互仍待验证 |
| Linux | GTK 3 发行版 | amd64、arm64 | 2026-10-29 基于提交 `005883a` 使用 MyGo 0.6.2 生成 amd64/arm64 可执行文件、`.deb` 与 tar.gz；ELF 架构正确，源 `resources/` 的 108 个文件在两种架构的 staging、tar.gz 和 `.deb` 中全部逐文件哈希匹配。此前 Debian 12 arm64 与 OrbStack/QEMU amd64 容器安装/启动/卸载 smoke、GNOME Keyring/D-Bus 三进程 smoke 针对之前构建，不能替代本提交包的运行验收。原生桌面/登录会话、其他发行版与目标硬件仍待验证 |
| Windows | Windows 10/11 | amd64、arm64 | 2026-10-29 基于提交 `005883a` 使用 MyGo 0.6.2 生成 amd64/arm64 PE32+ GUI 可执行文件；架构正确，源 `resources/` 的 108 个文件在两种架构 staging 目录均逐文件哈希匹配。当前构建机没有 NSIS（`makensis`），本轮未生成 Setup 安装器；目标系统运行、安装卸载、ACL、凭据库和正式签名仍待验证 |

SQLite 使用 `modernc.org/sqlite v1.61.0`，保持 `CGO_ENABLED=0`。MyGo 固定 `v0.6.2`。MongoDB、etcd 和 AI 的版本/协议见 `docs/decisions/`。

## 构建

需要 Go 1.27.1+；MyGo CLI 可安装为独立工具：

```sh
go install github.com/egoist/mygo/cmd/mygo@v0.6.2
CGO_ENABLED=0 mygo build -platform darwin/arm64,windows/amd64,windows/arm64,linux/amd64,linux/arm64
```

构建会将 MyGo 原生 UI 打包为各平台安装产物。Windows/Linux 使用 `mygo.json` 指定的 `resources/icon.png`；macOS 额外使用 `packaging/macos/Assets.car`，其资产目录源文件在 `packaging/macos/Assets.xcassets/`，圆角图源为 `resources/icon-macos.png`。MyGo 将 `Assets.car` 作为 macOS 图标资源优先于默认 `.icns`，其余平台继续使用通用图标。macOS 开发者签名/公证、Windows 签名、自动更新密钥和发布仓库尚未配置。签名私钥、更新签名密钥只能来自 CI secret 或受控密钥库，不得写入 `mygo.json`、源码或发布日志。Go runtime 依赖已生成版本化许可清单，完整上游许可/通知及 MPL-2.0 MySQL 源码已放入 MyGo 自动打包的 `resources/`；最新 darwin/arm64 app bundle 已核对包含 105 个许可/通知/源码文件，与源目录逐文件 SHA-256 一致。DBL 自有代码许可证待项目所有者确认；非 Go 打包资产和法律审阅仍待完成。

macOS 从“DBL”应用菜单、Linux/Windows 从“帮助”菜单打开“关于 DBL”面板；面板显示打包版本、DBL 名称和 MyGo 版本，未打包的开发运行显示“开发版”。

## 阻塞验收项

已识别数据库服务端错误的详情文本脱敏已由单测覆盖；这不替代 AI provider 的真实请求抓包、各平台凭据库检查或桌面交互安全验收。

- 在干净的 macOS/Linux/Windows 环境安装、启动、更新策略演练和卸载，验证配置与数据库文件保留策略。
- 逐个平台确认密钥库读写/删除、设置文件权限、mTLS 私钥生命周期和应用退出后的连接/watch 关闭。Linux Debian 12 容器中的 GNOME Keyring/D-Bus 已通过正式 app ID 对 32 KiB `v2:` 分块凭据进行三进程存取删除 smoke；实际桌面会话和其他发行版仍待验证。macOS 正式 bundle ID 的 32 KiB `v2:` 跨进程 harness 已构建但尚未运行，正式签名身份和重启后引用可用性仍待验证；Windows Credential Manager 尚未实测。
- 对 SQLite、PostgreSQL、MySQL、Redis、MongoDB、etcd 的关键路径做真实服务验收；Redis、MongoDB 与 etcd 明文单节点已验证；PostgreSQL/MySQL profile 私有 CA + verify-full、Redis ACL + verify-full TLS、MongoDB verify-full 证书验证与 SCRAM + TLS 加密路径已验证。PostgreSQL/MySQL 基础 profile 保存/测试/连接/查询已在 macOS 测试窗口分别对 PostgreSQL 17.11 和 MySQL 8.4.13 完成 trust/无密码认证、无 TLS smoke；MySQL `verify-full` + profile CA 路径的真实 TLS 集成也已通过，PG/MySQL CA 输入框和严格 TLS 提示有 UI tester 覆盖。PG/MySQL TLS profile 可见端到端流程与凭据 vault UI 生命周期、MongoDB/etcd/AI UI 人工验收及 etcd 目标部署版本仍缺证据；revision compaction 与短 lease 到期行为已有集成测试。
- 对 AI provider 抓包确认默认请求不含 SQL/错误/schema/真实值；勾选时显示准确发送内容；测试取消、重定向拒绝、失败提示和 key 清理。
- 完成中文输入法、键盘焦点、屏幕阅读器/辅助技术、大字号、窄窗口和高对比度检查。
- 执行签名和自动更新密钥恢复演练；崩溃恢复策略已定为不恢复未保存的 SQL 草稿/结果、设置以原子文件更新保留。设置包测试已在文件创建、写入、同步、关闭及替换边界通过子进程主动退出；另以操作系统强制终止子进程覆盖替换前/替换后，重启只读到完整旧值或完整新值。真实掉电/存储故障注入、Windows ACL/替换运行时验收仍待完成。Go runtime 清单、完整许可证文本和 MPL-2.0 驱动源代码已随 macOS bundle 验证；非 Go 资产审查和支持版本政策仍待完成。

满足这些项并附上可复现记录后，才能将相应平台从候选转为支持。
