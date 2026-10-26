# ADR 0006: PostgreSQL adapter 与 TLS 策略

- 状态：进行中
- 日期：2026-10-25

## 决策

- PostgreSQL 使用 `github.com/jackc/pgx/v5 v5.11.0`，通过 `pgx/v5/stdlib` 接入现有 `database/sql` 查询流；使用 `pgx.ConnConfig`，不拼接用户字段构造 DSN。
- 连接密码只从 `credentials.Vault` 按 opaque reference 读取，并在连接配置及错误处理中屏蔽密码。连接请求有 10 秒超时，连接池限制为 5 条；只读 profile 设置 `default_transaction_read_only=on`。
- 默认 TLS 为 `verify-full`，使用系统根证书校验链和主机名。profile 可提供 PEM 自定义 CA，追加到系统根证书池；只有 `verify-full`/`verify-ca` 可使用该路径。另提供 `require`、`prefer`、`disable`，UI 解释它们不验证证书、允许明文回退或关闭加密。客户端证书暂不支持。
- Schema/表元数据按 schema、类型、名称使用 keyset 分页；列元数据按 schema/table 查询。用户 SQL 通过共享 `sqlstream` 流读取，遵循现有行数/字节上限和 context 取消机制。
- 只读 profile 对每条用户 SQL 启动 `database/sql` 只读事务；结果耗尽后提交，提前停止/取消时回滚。不能只依赖 `default_transaction_read_only` 会话默认值，因为同一会话的 SQL 可能尝试修改它。`sqlstream.Cursor.OnFinish` 为驱动适配器提供流结束钩子。
- PostgreSQL 导出使用新的只读连接、只读 session 默认值和只读事务，按流写 CSV/JSON。与 SQLite 一样，这是重新执行原 SQL，并不承诺与结果窗口具有相同快照。
- JSON/JSONB 列在 JSON 导出中按 JSON 结构保留，而不是作为二进制字节数组编码为 Base64。
- PostgreSQL 错误按 SQLSTATE 分类，避免依赖错误文本判断认证、权限、查询、取消和并发错误。

## 依据与验证

- 驱动版本和 API：[pgx v5.11.0](https://pkg.go.dev/github.com/jackc/pgx/v5@v5.11.0)、[`stdlib` database/sql adapter](https://pkg.go.dev/github.com/jackc/pgx/v5@v5.11.0/stdlib)。
- PostgreSQL TLS 参数语义参考：[PostgreSQL SSL Support](https://www.postgresql.org/docs/current/libpq-ssl.html)。
- 单元用例覆盖 TLS 配置、端口校验、连接参数、SQLSTATE 错误分类、凭据读取和错误文本脱敏；sqlstream 测试覆盖 adapter 的游标收尾钩子与 JSONB 结构化导出。
- 可选 `TestPostgreSQLIntegration` 覆盖临时 schema/表创建和清理、schema-aware 分页、列元数据、查询/取消后健康检查、只读事务写保护及 JSONB 导出。该用例已在 PostgreSQL 17 本机临时实例上通过；使用 profile `TLSCAPath`、自签 CA 和带 IP SAN 的证书验证 `verify-full` 握手。服务、证书及数据目录均已清理。
- 最终代码状态下 `GOPROXY=off go test ./...`、`go test -race ./...`、`go vet ./...`、`CGO_ENABLED=0 go build ./...` 和 darwin/amd64、linux/amd64、linux/arm64、windows/amd64、windows/arm64 交叉构建通过。OrbStack 镜像下载曾无进展，改用本机 Homebrew PostgreSQL 17 验证，测试后已卸载本次安装的服务和依赖。

## 限制

- MySQL adapter 已独立记录于 ADR 0007；PostgreSQL/MySQL 共用 SQL contract，但各自保留数据库元数据和事务实现。
- 客户端证书暂不支持；profile 自定义 CA 的 UI 文件路径尚未做可见桌面操作验收。
- PostgreSQL 结果字段映射由 `database/sql` 驱动类型决定；数组、复合类型等特殊值的展示策略尚未专门设计。
