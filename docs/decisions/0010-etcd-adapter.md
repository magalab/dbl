# ADR 0010: etcd v3 前缀与 revision 工作区

- 状态：阶段 5 首版实现；本机 etcd 明文及认证 + verify-full/mTLS 真实服务集成已通过，桌面人工验收待完成
- 日期：2026-10-26

## 决策

- 使用官方 `go.etcd.io/etcd/client/v3 v3.6.16`，实现独立 `connection.Configuration` capability。profile 支持多个逗号分隔 endpoint、用户名和系统凭据库密码引用；TLS 默认验证服务端证书和主机名，可通过 PEM CA 文件扩展信任根，并可将客户端证书/私钥加密存入系统凭据库。mTLS 已通过本机真实服务集成；UI 文件导入和正式应用凭据生命周期仍待人工验收。
- endpoint 可使用 `https://host:port` / `http://host:port`；裸 host 使用 profile 端口，缺省为 2379。TLS 模式与 endpoint scheme 必须一致，禁止将密码或凭据写进 endpoint URL。
- 前缀读取按 key 升序，每页最多 100 项，以续读 key 继续；值最多保留 1 MiB，并展示 create/mod revision、version 和 lease ID。选择绑定 lease 的键时再查询 `TimeToLive`，显示剩余 TTL 和授予 TTL；lease 过期或查询失败单独提示，不阻断键值读取。Watch 支持起始 revision、prefix/exact 模式、取消和有界 100 项 UI 缓冲。
- 写入需要非只读 profile，支持可选 lease ID。写入预览固定目标键、值和 lease，显示值预览及绑定/移除租约的影响；确认前若内容变化，必须重新预览。删除 UI 先读取影响数量，再以确认时捕获的目标键/前缀提交；默认始终使用选中的精确键，只有显式启用前缀模式才使用浏览前缀，并拒绝空前缀删除。预览计数不是事务快照，并发变更可能使实际删除数变化。

## 依据与限制

- etcd 官方 Go client 使用 context 传递请求期限并要求显式关闭 client：[etcd client/v3](https://github.com/etcd-io/etcd/blob/main/client/v3/README.md)。当前依赖锁定 v3.6.16。
- `internal/etcd/store_test.go` 覆盖 endpoint/TLS 组合、IPv4/IPv6、输入边界、关闭状态和 gRPC 错误分类；`internal/etcd/integration_test.go` 提供可选服务用例，覆盖分页续读、前缀计数/删除边界、租约 TTL、watch 事件及取消、只读保护。本机使用 etcd server v3.7.3 与项目锁定的 client v3.6.16 完成 `TestEtcdIntegration`（2026-10-26）。HTTP 单节点路径覆盖分页、计数、租约 TTL、watch 取消、删除边界和只读保护；HTTPS 路径使用自建 CA `verify-full`、客户端证书 mTLS 和用户名/密码认证，重复覆盖这些操作且通过。服务与临时证书/数据已清理。真实 revision compaction 和短租约到期后 key 消失均已由本机集成用例验证；目标部署版本和桌面 UI 仍待验证。
