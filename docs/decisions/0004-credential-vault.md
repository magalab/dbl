# ADR 0004: 系统凭据库封装

- 状态：进行中
- 日期：2026-10-25

## 决策

- `internal/credentials` 定义可注入的 backend 和 `Vault` API；配置持久化层只应保存 `v1:` 格式的不透明引用，不应保存 secret。
- 系统 backend 复用锁定版本 `github.com/egoist/mygo v0.6.2` 的默认 `mygo.Secrets` service，不再增加额外 keyring 依赖。MyGo 为 macOS Keychain、Windows Credential Manager 和 Linux Secret Service 提供统一 API；默认 service 按应用标识隔离，开发版和正式版使用不同空间。无可用系统凭据服务时调用失败，不回退到明文文件；不通过 `Secrets.Service` 覆盖默认隔离。
- 单个系统密钥库条目遵循 MyGo 的 `MaxSecretSize`（2,560 字节）。不超过此值的 secret 继续使用 `v1:` 单项引用；较大 secret（总计最多 64 KiB）以 `v2:` 引用分块保存，每块均不超过系统条目限制，manifest 记录长度和 SHA-256 校验值。写入 manifest 前不会返回引用；读取时校验块长度和整体摘要；删除时先删块、最后删 manifest，以便失败后重试。空值、超出 64 KiB 的 secret 和格式错误引用在调用 backend 前拒绝。backend 错误对调用者统一折叠为通用错误，避免错误文本泄露 secret 或环境细节。
- 删除不存在的引用视为成功。写入失败时尝试删除可能由 backend 部分创建的条目，防止留下不可达 secret；清理本身是尽力而为。
- MyGo `Secrets` API 不接收 `context.Context`。凭据调用可能等待系统解锁或授权，应用必须在后台任务中执行，不能阻塞 UI 线程。

## 验证

- fake backend 单元测试覆盖不透明引用、存取/删除、输入校验、backend 错误脱敏、nil backend 失败关闭和写入后端晚失败的清理。
- 凭据测试还覆盖 64 KiB 最大逻辑 secret 的分块往返、v1 引用兼容、分块写入部分失败后的清理、缺失块拒绝加载和完整删除。TLS 文件导入最多读取 64 KiB + 1 字节后即拒绝，不会无界加载本地文件。
- 当前单元测试通过临时 MyGo API stub 编译运行；这只验证 wrapper 行为，不验证真实操作系统密钥库。
- `tmp/keychain-smoke` 使用正式应用名和 bundle identifier，分成三个独立进程阶段，并以 32 KiB payload 强制走 `v2:` 分块路径；macOS app 已构建、bundle ID/签名/arm64 已检查，但正式 app 的 Keychain 进程重启路径尚未运行，以避免自动处理潜在授权提示。
- Linux Secret Service 已在一次性 Debian 12 arm64 容器中用 session D-Bus 和 GNOME Keyring 实测：同一 `com.magalab.dbl` harness 分三次独立进程完成 32 KiB v2 store、load+delete、再确认 `ErrNotFound`。所有值均为确定性临时数据，不打印且不落到 key 文件；容器结束后 keyring 随容器销毁。该验证不替代非 root 桌面用户、发行版登录会话或 UI prompt 的现场确认。Windows Credential Manager 尚未实测。

## 限制

- SQLite profile 不含 secret。PostgreSQL、MySQL、Redis、MongoDB、etcd profile 及 AI provider 的凭据均通过 vault 引用读取；各 profile 删除时会在不再有其他 profile 引用该凭据后尝试清理。跨进程/平台证据仍不完整。
- macOS 的 OS 锁定/授权提示、正式签名 app 对 `com.magalab.dbl` service 的跨进程访问和应用重启后的引用可用性尚未实测；Linux 容器中的 GNOME Keyring 跨进程验证已通过，但实际桌面会话/发行版差异仍待确认；Windows Credential Manager、密钥轮换和卸载后的 secret 清理尚未实测或定义。
- 本 wrapper 不承诺清除运行时内存中的 secret 副本。
