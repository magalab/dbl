# 发布准备与支持矩阵

状态：发布被阻塞；此文件记录候选范围和必须完成的证据，不构成平台支持承诺。

## 候选矩阵

| 平台 | 候选版本 | 架构 | 状态 |
|---|---|---|---|
| macOS | 12+ | arm64 | 2026-10-26 针对连接模态面板、设置面板和快捷键改动重建 `/tmp/dbl-after-modal/darwin-arm64/DBL.app`；bundle ID `com.magalab.dbl`、版本 `0.1.0`、最低系统版本 12.0、自定义图标正确，ad-hoc codesign 校验通过，107 个资源与源目录逐文件哈希一致。未启动此最新 bundle，故连接弹窗、设置和快捷键仍只有 UI tester 证据；此前旧测试窗口曾通过 SQLite、PostgreSQL、MySQL profile 基础连接/查询及 SQL 编辑器 smoke，不能替代最新 bundle 验收。自定义 CA 表单、AI、生产环境提示尚无可见 bundle 验收；数据库集成已覆盖 PostgreSQL/MySQL 私有 CA + verify-full、MongoDB/Redis/etcd 单机及认证/TLS；正式签名、公证、干净安装/卸载未验证 |
| macOS | 12+ | amd64/universal | MyGo 生成 Universal `.app` 和 `.dmg`；主程序包含 arm64 + x86_64，ad-hoc codesign 与 `hdiutil verify` 通过，DMG 内 107 个资源哈希匹配；Intel 硬件启动仍待验证 |
| Linux | GTK 3 发行版 | amd64、arm64 | 两架构 deb 与 tar.gz 均已生成；Debian 12 arm64 与 OrbStack/QEMU amd64 容器安装 deb 后，应用均在 Xvfb 下运行至少 8 秒；107 个资源哈希匹配。arm64 容器通过 deb 卸载和普通用户 tar 完整安装/启动/卸载：应用文件移除、配置/数据库哨兵文件保留。GNOME Keyring/D-Bus 在正式 app ID 下通过 32 KiB `v2:` 分块凭据的三进程 smoke。原生桌面/登录会话、其他发行版与目标硬件仍待验证 |
| Windows | Windows 10/11 | amd64、arm64 | 两架构 exe 与 NSIS Setup 安装器均已生成；打包目录 107 个资源哈希与源一致；目标系统安装运行、ACL、凭据库、签名和卸载待验证 |

SQLite 使用 `modernc.org/sqlite v1.61.0`，保持 `CGO_ENABLED=0`。MyGo 固定 `v0.6.2`。MongoDB、etcd 和 AI 的版本/协议见 `docs/decisions/`。

## 构建

需要 Go 1.27.1+；MyGo CLI 可安装为独立工具：

```sh
go install github.com/egoist/mygo/cmd/mygo@v0.6.2
CGO_ENABLED=0 mygo build -platform darwin/arm64,windows/amd64,windows/arm64,linux/amd64,linux/arm64
```

构建会将 MyGo 原生 UI 打包为各平台安装产物，图标源文件为 `resources/icon.png`（1024×1024）。macOS 开发者签名/公证、Windows 签名、自动更新密钥和发布仓库尚未配置。签名私钥、更新签名密钥只能来自 CI secret 或受控密钥库，不得写入 `mygo.json`、源码或发布日志。Go runtime 依赖已生成版本化许可清单，完整上游许可/通知及 MPL-2.0 MySQL 源码已放入 MyGo 自动打包的 `resources/`；最新 darwin/arm64 app bundle 已核对包含 105 个许可/通知/源码文件，与源目录逐文件 SHA-256 一致。DBL 自身许可证仍未声明，非 Go 打包资产和法律审阅仍待完成。

macOS 从“DBL”应用菜单、Linux/Windows 从“帮助”菜单打开“关于 DBL”面板；面板显示打包版本、DBL 名称和 MyGo 版本，未打包的开发运行显示“开发版”。

## 阻塞验收项

已识别数据库服务端错误的详情文本脱敏已由单测覆盖；这不替代 AI provider 的真实请求抓包、各平台凭据库检查或桌面交互安全验收。

- 在干净的 macOS/Linux/Windows 环境安装、启动、更新策略演练和卸载，验证配置与数据库文件保留策略。
- 逐个平台确认密钥库读写/删除、设置文件权限、mTLS 私钥生命周期和应用退出后的连接/watch 关闭。Linux Debian 12 容器中的 GNOME Keyring/D-Bus 已通过正式 app ID 对 32 KiB `v2:` 分块凭据进行三进程存取删除 smoke；实际桌面会话和其他发行版仍待验证。macOS 正式 bundle ID 的 32 KiB `v2:` 跨进程 harness 已构建但尚未运行，正式签名身份和重启后引用可用性仍待验证；Windows Credential Manager 尚未实测。
- 对 SQLite、PostgreSQL、MySQL、Redis、MongoDB、etcd 的关键路径做真实服务验收；Redis、MongoDB 与 etcd 明文单节点已验证；PostgreSQL/MySQL profile 私有 CA + verify-full、Redis ACL + verify-full TLS、MongoDB verify-full 证书验证与 SCRAM + TLS 加密路径已验证。PostgreSQL/MySQL 基础 profile 保存/测试/连接/查询已在 macOS 测试窗口分别对 PostgreSQL 17.11 和 MySQL 8.4.13 完成 trust/无密码认证、无 TLS smoke；MySQL `verify-full` + profile CA 路径的真实 TLS 集成也已通过，PG/MySQL CA 输入框和严格 TLS 提示有 UI tester 覆盖。PG/MySQL TLS profile 可见端到端流程与凭据 vault UI 生命周期、MongoDB/etcd/AI UI 人工验收及 etcd 目标部署版本仍缺证据；revision compaction 与短 lease 到期行为已有集成测试。
- 对 AI provider 抓包确认默认请求不含 SQL/错误/schema/真实值；勾选时显示准确发送内容；测试取消、重定向拒绝、失败提示和 key 清理。
- 完成中文输入法、键盘焦点、屏幕阅读器/辅助技术、大字号、窄窗口和高对比度检查。
- 执行签名和自动更新密钥恢复演练；崩溃恢复策略已定为不恢复未保存的 SQL 草稿/结果、设置以原子文件更新保留。设置包测试已在文件创建、写入、同步、关闭及替换边界通过子进程主动退出；另以操作系统强制终止子进程覆盖替换前/替换后，重启只读到完整旧值或完整新值。真实掉电/存储故障注入、Windows ACL/替换运行时验收仍待完成。Go runtime 清单、完整许可证文本和 MPL-2.0 驱动源代码已随 macOS bundle 验证；DBL 自身许可证选择、非 Go 资产审查和支持版本政策仍待完成。

满足这些项并附上可复现记录后，才能将相应平台从候选转为支持。
