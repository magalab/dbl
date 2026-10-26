# ADR 0003: 数据源连接契约与 mock 生命周期

- 状态：进行中
- 日期：2026-10-25

## 决策

- `internal/connection` 定义公共连接生命周期、健康检查、错误分类和关闭契约；SQL 能力单独由 `connection.SQL` 表达，不把 Redis/Mongo/etcd 强行映射为 SQL CRUD。
- Schema 对象和列元数据属于 domain contracts。对象页游标包含 schema/type/name，列查询接收 schema/table；SQLite、PostgreSQL 和 MySQL adapter 通过编译期断言满足连接与 SQL 能力接口。UI 持有能力接口而不是 `database/sql` 或具体驱动连接。
- `connection.Manager` 串行化连接替换、连接失败和断开状态，并为 UI/应用服务提供只读状态快照。
- workbench 持有应用生命周期 context；配置测试、数据库/对象读取、写操作、AI 和导出等限时后台任务及 etcd Watch 均从该 context 派生。关闭窗口时先取消生命周期 context，再取消当前能力专属任务并断开连接，避免未单独登记的异步任务在窗口关闭后继续访问服务。
- SQL capability 暴露可见数据库/schema 与当前命名空间。PostgreSQL 通过重连时的 pgx `search_path` startup 参数选择 schema，标识符按 PostgreSQL 双引号规则转义，UI 提示未限定名称按 search_path 解析并提醒共享 schema 信任风险；MySQL 通过 profile database 重新建立连接；SQLite 的 `main` 是固定工作区命名空间，不对任意 SQL 做隐式改写。命名空间变化必须中止活动读取、替换连接并清空旧结果/对象状态。
- `internal/connection/mock` 提供可控制延迟、连接失败和健康检查失败的 mock adapter，用于验证成功、失败、取消、健康检查及断开流程。
- SQLite、PostgreSQL 和 MySQL Store 实现健康检查、驱动错误分类和 SQL 能力；远程 schema/表/列元数据使用 schema-aware keyset 分页。
- 用户展开技术详情时，已识别的 SQLite/PostgreSQL/MySQL/Redis/MongoDB/etcd 服务端错误只显示驱动错误码或通用类别，不直接展示服务端原文，避免重复键值、键内容或连接身份随错误回显。应用自身生成的静态诊断仍保留。

## 验证

- `internal/connection` 单测覆盖初始/连接成功/连接失败/取消/断开状态，以及 mock 健康检查和通用错误分类。
- `internal/sqlite` 使用 `modernc.org/sqlite` 纯 Go 驱动执行真实文件集成测试；PostgreSQL/MySQL 各有配置、错误分类、凭据与错误脱敏单测和真实服务 opt-in 集成用例。
- UI headless tests 验证远程 profile 字段可访问、凭据引用不显示、SQLite/PostgreSQL/MySQL profile 的测试/连接入口，以及 MySQL 不再标记为待接入。mock manager 测试覆盖连接成功、失败、取消、健康检查与断开。
- 主包关闭生命周期测试覆盖已登记的 SQL/Redis/MongoDB/etcd/AI 任务和单独创建的 lifecycle 子任务；关闭后所有 context 都观察到 `context.Canceled`，活动连接进入断开状态。
- PostgreSQL 17.11 真实服务集成现验证可见 schema 枚举、当前 schema 和重连设置 `search_path` 后执行未限定表名查询；MySQL 集成用例覆盖可见 database/current database 发现，MySQL 8.4.13 真实服务也通过新增的可见 database/current database 命名空间断言。远程 profile 与命名空间选择的可见 app bundle 手动验收仍待完成。
- `internal/safeerror` 和主包回归测试以包含重复键值、用户名/数据库名和键值的模拟服务错误验证详情脱敏；另用真实 modernc SQLite parser error 验证不回显 SQL 标识符。该保护覆盖识别出的驱动错误类型；未知的应用/系统错误仍由调用方显示原文，需在新 adapter 接入时补充对应类型识别。

## 限制

- UI 已接入 SQLite、PostgreSQL 和 MySQL adapter；PostgreSQL 与 MySQL 均有一次性本机服务上的 opt-in 集成用例。SQL 工作区仍为三类 SQL 源共用同一单活动连接界面。
- SQL 错误状态分类仍使用驱动错误码与有限上下文规则。敏感详情脱敏由 `internal/safeerror` 对已知驱动错误类型统一处理；后续 adapter 必须将其服务端错误类型纳入识别，避免直接展示可能含值的原始消息。
- `internal/credentials` 已提供系统密钥库 wrapper，远程 SQL profile 可通过不透明引用读写凭据；正式应用 bundle 生命周期及 Windows/Linux 后端仍待验证。细节见 ADR 0004。
