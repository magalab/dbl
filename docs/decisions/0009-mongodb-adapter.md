# ADR 0009: MongoDB 文档适配器

- 状态：阶段 5 首版实现
- 日期：2026-10-26

## 决策

- 使用官方 `go.mongodb.org/mongo-driver/v2 v2.9.2`，实现独立的 `connection.Documents` capability。首版连接单 endpoint，TLS 默认 `verify-full` 使用系统根证书；支持追加 PEM 自定义 CA、用户名、密码 vault 引用和认证数据库。副本集拓扑、mTLS 与 SRV URI 暂不作为 profile 输入。
- 文档和查询输入输出采用 MongoDB Extended JSON，保留 BSON ObjectID、日期、二进制和数值类型。find 和 aggregate 每次最多返回 100 条；文档编辑/插入最多 1 MiB，超长列表预览截断，完整详情单独读取。
- 选择集合时按需读取只读索引元数据，显示索引名、key definition、unique、sparse 和 TTL 秒数；最多读取 500 个索引，超出时明确报错，不提供索引修改/删除操作。
- 聚合 pipeline 禁止 `$out` 和 `$merge`，写入仅通过独立插入/替换 API。替换必须保留原 `_id`。UI 在提交前显示 diff 并要求显式确认，确认框标出环境、连接、数据库、集合和文档 ID；没有文档删除入口。
- profile 的只读状态由 adapter 层写入入口再次检查。连接、数据库枚举、查询、详情和写入均接受 context。

## 依据与限制

- 使用 MongoDB 官方 Go driver v2 的连接、context、cursor 和 aggregation API：[MongoDB Go Driver](https://www.mongodb.com/docs/drivers/go/current/)。
- 2026-10-26 在临时 MongoDB server 8.0.34 上通过明文、SCRAM + `tlsMode=require`，以及 profile `TLSCAPath` + `verify-full` TLS 集成。`require` 路径不验证服务端证书；严格 TLS 用自建 CA 和正确 IP SAN 验证成功，新增可选 `TestMongoDBTLSHostnameMismatchIntegration` 以 `localhost` 主机名不匹配验证拒绝。桌面 bundle 操作仍待验收。数据库/集合名称目前整批加载；无聚合结果中的 `_id` 会被拒绝显示为可编辑文档。
