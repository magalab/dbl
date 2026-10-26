# ADR 0008: Redis adapter 与有界键浏览

- 状态：阶段 4 首版实现
- 日期：2026-10-25

## 决策

- Redis 使用 `github.com/redis/go-redis/v9 v9.17.3`，通过 `context.Context` 执行命令，连接层实现通用生命周期和独立 `connection.KeyValue` 能力；Redis 不实现也不伪装 SQL capability。
- 首版只连接单个 host/port endpoint，可设置 ACL 用户名、系统凭据库密码引用、非负 DB index 和 TLS。集群、哨兵、SSH 隧道、自定义 CA 与客户端证书暂不支持。默认 `verify-full` 使用系统根证书并验证主机名；`verify-ca` 仅验证信任链；`require` 跳过证书验证；`disable` 关闭 TLS。Redis 不提供 `prefer`，以免不明确地回退明文。
- 键浏览使用 Redis `SCAN` 游标与 `MATCH`，可选 `TYPE` 筛选，每页请求数量上限 100；不调用阻塞式 `KEYS`。前缀输入会追加 `*`，用户提供 glob 时直接传递。类型筛选需要 Redis 6.0+；该选项过滤每页返回内容，但完整迭代仍需扫描 keyspace。
- 读取 string/hash/list/set/zset/stream。hash/set/zset 使用各自的游标扫描；list 使用有界 `LRANGE` offset；stream 使用反向 ID 游标，翻页时只读取下一页。单个展示值最多保留 1 MiB，string 用 `STRLEN` + `GETRANGE` 避免完整取回超长值。Hash/list/set/zset/stream 命令仍需接收 Redis 返回的整页后才能在客户端截断超长单项。
- 首版写入支持 string、hash field、list append、set member、zset member/score、stream field；TTL 可设，集合写入在 TTL 留空或为 0 时保留已有 TTL，而 string 的 0 会移除已有 TTL 并改为永不过期。删除只支持单键。profile 的只读状态会阻止适配器写入；每次写入和删除都先在原生确认框展示环境、连接、目标 key 和操作影响。写入框包含转义后的值预览（最多 4 KiB）和字节数，并按类型说明覆盖/追加/成员更新及 TTL 结果。
- Adapter 和 UI 共用 1 MiB 值/字段上限；超限输入在构造确认预览之前拒绝，字段/值均按有界转义文本显示。
- 不对 Redis 原始值写日志；详情只有用户选中键后才读取，连接期间 SCAN 和详情读取均在异步任务内使用超时 context。

## 依据与验证

- Redis 官方 Go 客户端指南推荐使用 go-redis/v9 并演示 context 和单节点连接：[Redis Go client guide](https://github.com/redis/redis-doc/blob/master/docs/connect/clients/go.md)。
- Redis 官方 SCAN 文档说明 TYPE 选项及其从 6.0.0 开始提供，且不降低完整迭代的扫描工作量：[SCAN command](https://redis.io/docs/latest/commands/scan/)。
- go-redis 官方仓库记录了连接池、单机/集群客户端和 SCAN 等 API：[redis/go-redis](https://github.com/redis/go-redis)。
- `internal/redis` 的 miniredis 用例覆盖游标扫描、取消、六种类型读取与写入、stream ID 分页、超长值截断、TTL、只读拒绝和 TLS 配置；主 UI 测试覆盖 Redis profile 字段与连接/测试入口。
- 2026-10-26 在临时 Redis server 8.10.2 上通过两条真实服务路径：明文单机，以及 ACL 用户/密码配合自建测试 CA 的 `verify-full` TLS。后者通过 `SSL_CERT_FILE` 将临时 CA 加入 Go 系统根加载，验证凭据 vault 读取、TLS hostname/链验证、SCAN、SCAN TYPE、TTL、值读取、写入、删除和清理。其他 ACL 权限策略、目标生产 CA 和实际 UI 连接流程仍待验收。

## 限制

- Redis ACL 可以在服务器侧提供更强的命令权限控制；应用 `只读` profile 当前主要在 UI/adapter 层拦截写操作，不能替代 Redis 服务端 ACL。
- Redis 的 list offset 深分页仍受 Redis `LRANGE` 复杂度影响；超长集合元素虽限制 UI 保留量，网络响应本身仍可能包含超长字段。
- cluster/sentinel 拓扑、keyspace notifications/watch、TTL 单独修改、批量删除、二进制编辑和用户自定义 CA/mTLS 需要后续设计。
