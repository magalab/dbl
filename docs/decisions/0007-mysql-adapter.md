# ADR 0007: MySQL adapter 与 TLS 策略

- 状态：进行中
- 日期：2026-10-25

## 决策

- MySQL 使用 `github.com/go-sql-driver/mysql v1.10.1`，通过 `NewConnector`/`sql.OpenDB` 接入现有 `database/sql` 查询流。该驱动支持 MySQL 5.7+ 与 MariaDB 10.5+，要求 Go 1.24+，当前模块 Go 版本满足要求。
- 通过类型化 `mysql.Config` 设置地址、数据库、字符集、超时和 TLS；不将用户字段拼入 DSN。默认关闭多语句、LOCAL INFILE 和 cleartext password 认证选项，不开启驱动的明文 TLS 回退。
- 默认 TLS 为 `verify-full`，使用系统根证书校验链和主机名；可追加 PEM 自定义 CA。`verify-ca` 校验信任链但不校验主机名；`require` 加密但不验证证书；`prefer` 尝试 TLS 并允许服务器不支持 TLS 时退回明文；`disable` 关闭 TLS。自定义 CA 只允许 `verify-full`/`verify-ca`，界面展示相应风险提示。客户端证书暂不支持。
- Schema 代表 MySQL database。通过 `information_schema` 按二进制 schema/type/name keyset 分页读取 schema、table 和 view；列类型、默认值、主键、自动增长/生成信息从 `information_schema.columns` 读取。
- SQL 通过共享 `sqlstream` 以 context 取消和受控批次执行。只读 profile 的每条用户 SQL 在 `database/sql` 只读事务中运行；结果耗尽后提交，提前停止/取消时回滚。完整导出使用独立只读事务并按流写 CSV/JSON。
- 驱动错误优先按 MySQL errno 分类，并以 SQLSTATE 补充 query 类错误；取消和超时沿用 `context` 分类。
- 驱动许可为 MPL-2.0；v1.10.1 源码和许可证/通知已纳入 `resources/THIRD_PARTY_LICENSES/github.com/go-sql-driver/mysql/`，并在最新 macOS app bundle 中通过文件校验。

## 依据与验证

- 官方 Go 包资料：[go-sql-driver/mysql v1.10.1](https://pkg.go.dev/github.com/go-sql-driver/mysql@v1.10.1)。其中记录了 MySQL/MariaDB 版本支持、Go 要求、TLS 配置和 MPL-2.0 许可。
- 驱动官方文档：[README](https://github.com/go-sql-driver/mysql/blob/v1.10.1/README.md) 说明 `database/sql` context 取消、连接池参数与 TLS 模式。
- 单元用例覆盖 TLS 模式构造、默认端口、时间/字符集配置、errno/SQLSTATE 分类、凭据读取和密码错误脱敏。
- 原 `TestMySQLIntegration` 已在一次性 MySQL 8.4 服务上运行通过：profile `TLSCAPath`、自签 CA 和带 IP SAN 的证书完成真实 `verify-full` TLS 握手，并覆盖 schema/table 分页、列元数据、SQL 查询、直接只读写保护、JSON 结构导出、查询取消及取消后连接健康。本轮新增把 session 后续事务默认值改为 `READ WRITE` 后再次尝试写入的攻击用例，并已在临时 MySQL 8.4.13 服务上通过，确认后续显式只读事务仍拒绝写入且行数未变。MySQL 的 session 特征会影响后续事务，详见[官方 SET TRANSACTION 说明](https://dev.mysql.com/doc/refman/8.4/en/set-transaction.html)。临时服务、数据目录、包和依赖已清理。

## 限制

- MySQL/MariaDB 的认证插件、服务端版本和 TLS 配置各有差异；当前只验证 MySQL 8.4 的常见 TCP+TLS 连接模式。
- 客户端证书、Unix socket、SSH 隧道、云厂商 IAM/OIDC 等认证尚不支持；profile 自定义 CA 的 UI 文件路径尚未做可见桌面操作验收。
- 某些 MySQL 特殊列、空间/几何、位类型和非标准 JSON 表示仍未定义专属 UI 映射策略。
