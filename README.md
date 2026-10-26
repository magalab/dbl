# DBL 数据工作台

当前实现已覆盖阶段 0–5 首版代码及阶段 6 的 SQL AI 草稿路径：六类数据源均有独立 capability，MongoDB/etcd 有专属工作区，AI provider 可配置并流式返回。连接配置支持开发/预发/生产环境标记、最近连接记录；所有可写 SQL 连接上的写语句及风险不明确的语句执行前都会显示环境、连接、当前选中对象、风险估计和完整 SQL 并要求确认。查询历史默认仅驻留内存；用户可在历史面板选择将最近 100 条记录保存在本机，并随时清除。PostgreSQL 17/MySQL 8.4 的真实服务集成，以及 Redis 8.10.2、MongoDB 8.0.34、etcd 3.7.3 的单节点集成都已验证；Redis ACL + verify-full TLS、MongoDB SCRAM + TLS 加密也已通过。目标 etcd 版本与 UI、AI 云 provider 和数据源桌面 bundle 交互仍待验收。阶段 7 已添加原生打包配置、应用图标、changelog 和发布阻塞清单；正式签名、干净环境安装/卸载及支持矩阵验收尚未完成。各阶段遗留的中文输入法、辅助技术和跨平台密钥库验收仍需继续。

## 运行

需要 Go 1.27.1 或更新版本。依赖下载可用后运行：

```sh
CGO_ENABLED=0 go run .
```

macOS 的应用菜单、Linux/Windows 的“帮助”菜单均提供“关于 DBL”，显示应用版本与 MyGo 版本。窗口中包含可拖动分栏、窄窗口自动隐藏的检查器、MyGo SQL 代码编辑器（含格式化按钮）、虚拟列表和结果表，以及最多 16 个互相独立的查询工作区标签。标签分别保留 SQL 文本、已读结果、排序和会话历史；格式化器保留字符串、标识符和注释内容，不解析或验证 SQL 方言。SQLite 连接由标签共享。切换有未读结果的标签会停止该标签的后续读取并保留已读部分。SQLite 原生文件选择打开/新建、路径输入、关闭、可选只读连接、对象与列浏览、SQL 查询、取消、续读、单元格/行复制、CSV/JSON 导出和会话查询历史也已接入；会话内历史可按工作区清空并随退出清除，另可在历史面板选择本机持久保存。错误先显示摘要，用户可展开技术详情；已识别的数据库服务错误只显示错误码或通用类别，不回显服务端原文和可能包含的键值。数据库对象按 200 项 keyset 页面加载；查询批次默认最多 128 行且有约 8 MiB 的字节目标；结果窗口最多保留 10,000 行、约 64 MiB 的估算行数据，显示当前来源行范围。超过单行缓存上限的行会计数并明确提示未保留。结果表支持按列排序，但只重排当前保留窗口，不改变游标读取顺序，也不表示完整查询结果已排序。完整导出会在只读连接上重跑生成当前结果窗口的 SQL，而不是编辑器后来修改的文本；结果来源文件与当前连接不一致时会禁用导出。CSV/JSON 流式写入私有临时文件，完成并同步后才原子发布；取消或失败会清理临时文件，发布时拒绝覆盖已有目标。重新执行可能因非确定性 SQL 或并发写入而得到不同数据。

本机偏好保存最近 SQLite 文件路径，连接 profile 支持分组、收藏、只读和复制。SQLite profile 可测试/打开；PostgreSQL/MySQL 支持 TLS、SQL 工作区和数据库/Schema 选择（切换后重连）；PostgreSQL 当前 schema 决定未限定对象名的 search_path，选择共享 schema 前应确认其创建者可信；SQLite 固定使用 main schema；Redis 支持 SCAN、类型读取、TTL 和基础写入；MongoDB 支持数据库/集合、find、基础 pipeline、分页文档与 diff 后写入；etcd 支持多 endpoint、TLS/认证、前缀/精确读取、revision/lease TTL 元数据、watch 和删除预览。Redis 不调用 `KEYS`。Mongo 聚合禁止 `$out`/`$merge`；etcd 删除默认精确键，前缀删除需显式开启并先查看数量。etcd mTLS 客户端证书和私钥进入系统凭据库，自定义 CA 使用 PEM 路径；MongoDB 支持 PEM 自定义 CA 并追加到系统根证书，暂不支持 mTLS 客户端证书。AI 面板支持配置 OpenAI-compatible SSE endpoint、model 与 vault API key，提供按任务分类的提示模板、多行提示编辑器和按上次已审核请求重新生成，并生成/解释 SQL、生成 Mongo filter/pipeline 草稿及提供 Redis/etcd 文字建议。上下文逐项 opt-in，发送前展示实际 payload；文档、键值和配置值不发送。AI 草稿不自动执行，插入 SQL 后执行前展示连接、语句和粗略风险分类并要求确认。MongoDB/Redis/etcd 已通过本机服务集成；Redis ACL + verify-full TLS、MongoDB SCRAM + TLS 加密及 profile 私有 CA verify-full 路径也已通过。目标 etcd 版本与 UI、AI 云 provider 与各数据源桌面 bundle 交互尚未完整验收。配置位于用户配置目录下的 `dbl/settings.json`；Unix 权限是 `0700`/`0600`，Windows ACL 行为仍需验证。配置只保存非敏感元数据与不透明凭据引用；正式应用 bundle 重启后的引用/授权行为，以及 Windows/Linux 后端仍待验证。

MongoDB 集合检查器会按需读取索引，只展示名称、key definition、unique/sparse 标记和 TTL 属性，不提供索引写入或删除操作。

AI 的提示、请求预览和回答只在运行期间保留，可用“清空 AI 内容”立即清除；未保存 API key 有单独清除入口。
回答可通过“复制 AI 回答”复制到系统剪贴板。

PostgreSQL/MySQL/MongoDB profile 支持附加 PEM 格式自定义 CA，并与系统根证书合并；只可与 verify-full/verify-ca 配合。客户端证书仅 etcd 支持。

Redis 键浏览器支持按 string/hash/list/set/zset/stream 类型筛选，并通过 `SCAN TYPE` 延续游标分页；该服务端选项要求 Redis 6.0 或更新版本。

## 验证

```sh
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go test -run '^$' -bench BenchmarkSortRowIndices10K -benchmem ./internal/sqlstream
CGO_ENABLED=0 go test -run '^$' -bench BenchmarkSQLiteScan10K ./internal/sqlite
```

`go test ./...`、`go test -race ./...`、`CGO_ENABLED=0 go vet ./...`、`CGO_ENABLED=0 go build ./...` 均已通过，且 darwin/amd64、linux/amd64+arm64、windows/amd64+arm64 纯 Go 交叉构建通过。PostgreSQL 17.11、MySQL 8.4.13、MongoDB 8.0.34、Redis 8.10.2 与 etcd 3.7.3 的单节点集成用例均已通过（etcd client 锁定 v3.6.16）；PostgreSQL/MySQL profile 私有 CA + verify-full、Redis ACL + verify-full TLS、MongoDB SCRAM + TLS 加密和 MongoDB profile 私有 CA + verify-full 路径也已通过。AI provider 已有 payload 安全、SSE 分块和取消等自动化测试；真实云 provider 抓包与 UI 人工验收仍未完成。

要运行 PostgreSQL 集成用例，请使用专用的临时数据库。用例会创建并清理一个唯一的 `dbl_it_*` schema：

```sh
docker run --rm -d --name dbl-test-postgres \
  -e POSTGRES_USER=dbltest -e POSTGRES_PASSWORD=dbltest-password \
  -e POSTGRES_DB=dbltest -p 127.0.0.1:15432:5432 postgres:17-alpine
DBL_TEST_POSTGRES_HOST=127.0.0.1 DBL_TEST_POSTGRES_PORT=15432 \
  DBL_TEST_POSTGRES_DATABASE=dbltest DBL_TEST_POSTGRES_USERNAME=dbltest \
  DBL_TEST_POSTGRES_PASSWORD=dbltest-password \
  go test ./internal/postgresql -run '^TestPostgreSQLIntegration$' -v
# For a TLS-enabled service with a server certificate matching DBL_TEST_POSTGRES_HOST,
# use these settings instead of the plaintext settings above.
DBL_TEST_POSTGRES_HOST=127.0.0.1 DBL_TEST_POSTGRES_PORT=15432 \
DBL_TEST_POSTGRES_DATABASE=dbltest DBL_TEST_POSTGRES_USERNAME=dbltest \
DBL_TEST_POSTGRES_PASSWORD=dbltest-password DBL_TEST_POSTGRES_TLS_MODE=verify-full \
DBL_TEST_POSTGRES_CA=/path/to/test-ca.pem \
  go test ./internal/postgresql -run '^TestPostgreSQLIntegration$' -v
docker stop dbl-test-postgres
```

MySQL 集成用例可用专用的临时数据库运行。用例会在该数据库中创建并清理唯一的 `dbl_it_*` 表：

```sh
docker run --rm -d --name dbl-test-mysql \
  -e MYSQL_ROOT_PASSWORD=root-test-password \
  -e MYSQL_DATABASE=dbltest -e MYSQL_USER=dbltest \
  -e MYSQL_PASSWORD=dbltest-password \
  -p 127.0.0.1:13306:3306 mysql:8.4
DBL_TEST_MYSQL_HOST=127.0.0.1 DBL_TEST_MYSQL_PORT=13306 \
  DBL_TEST_MYSQL_DATABASE=dbltest DBL_TEST_MYSQL_USERNAME=dbltest \
  DBL_TEST_MYSQL_PASSWORD=dbltest-password DBL_TEST_MYSQL_TLS_MODE=disable \
  go test ./internal/mysql -run '^TestMySQLIntegration$' -v
# For a TLS-enabled service with a server certificate matching DBL_TEST_MYSQL_HOST,
# use these settings instead of the plaintext settings above.
DBL_TEST_MYSQL_HOST=127.0.0.1 DBL_TEST_MYSQL_PORT=13306 \
DBL_TEST_MYSQL_DATABASE=dbltest DBL_TEST_MYSQL_USERNAME=dbltest \
DBL_TEST_MYSQL_PASSWORD=dbltest-password DBL_TEST_MYSQL_TLS_MODE=verify-full \
DBL_TEST_MYSQL_CA=/path/to/test-ca.pem \
  go test ./internal/mysql -run '^TestMySQLIntegration$' -v
docker stop dbl-test-mysql
```

Redis 集成用例需要一个可写的专用 Redis DB；只创建并删除本测试生成的唯一 `dbl_it_*` 键：

```sh
DBL_TEST_REDIS_ADDR=127.0.0.1:6379 DBL_TEST_REDIS_TLS_MODE=disable \
  go test ./internal/redis -run '^TestRedisIntegration$' -v
```

ACL/TLS 环境可另外设置 `DBL_TEST_REDIS_USERNAME`、`DBL_TEST_REDIS_PASSWORD`、`DBL_TEST_REDIS_DB` 和 `DBL_TEST_REDIS_TLS_MODE`（支持 `verify-full`、`verify-ca`、`require`、`disable`）。

## Redis 与 MongoDB 集成验证

请连接隔离的本机服务。Redis 用例只创建并删除唯一 `dbl_it_*` 键；MongoDB 用例只创建并删除唯一数据库。MongoDB 用例还检查索引元数据、查询/聚合/替换和 `$out` 写入阶段拦截。

```sh
DBL_TEST_REDIS_ADDR=127.0.0.1:6379 DBL_TEST_REDIS_TLS_MODE=disable \
  go test ./internal/redis -run '^TestRedisIntegration$' -v
DBL_TEST_MONGODB_ADDR=127.0.0.1:27017 DBL_TEST_MONGODB_TLS_MODE=disable \
  go test ./internal/mongodb -run '^TestMongoDBIntegration$' -v
# Strict TLS with a private CA; server certificate must include the endpoint hostname/IP SAN.
DBL_TEST_MONGODB_ADDR=127.0.0.1:27017 DBL_TEST_MONGODB_TLS_MODE=verify-full \
DBL_TEST_MONGODB_CA=/path/to/test-ca.pem \
  go test ./internal/mongodb -run '^TestMongoDBIntegration$' -v
# Hostname mismatch test: point localhost to a TLS server whose certificate lacks a localhost SAN.
DBL_TEST_MONGODB_TLS_MISMATCH_ADDR=localhost:27017 DBL_TEST_MONGODB_CA=/path/to/test-ca.pem \
  go test ./internal/mongodb -run '^TestMongoDBTLSHostnameMismatchIntegration$' -v
```

2026-10-26 本机验证 Redis 8.10.2 的明文路径和 ACL + 自建测试 CA `verify-full` TLS 路径；MongoDB 8.0.34 验证明文路径及 SCRAM 凭据 + `tlsMode=require` 加密路径。MongoDB `require` 不验证服务端证书。Profile `TLSCAPath` + `verify-full` 已用测试 CA 验证证书链和 hostname，hostname 不匹配时拒绝。etcd revision compaction 错误路径已集成验证；目标版本、目标生产证书与桌面 UI 仍待验收。

## etcd 集成验证

使用隔离的 etcd 服务运行集成用例；用例会创建并清理唯一前缀下的 key，并覆盖分页、租约 TTL 读取与到期删除、watch 取消与 revision compaction 错误、删除边界和只读保护。认证/TLS/mTLS 服务可设置 CA、客户端证书/私钥和用户名/密码：

```sh
DBL_TEST_ETCD_ENDPOINT=http://127.0.0.1:2379 \
  go test ./internal/etcd -run '^TestEtcdIntegration$' -v
DBL_TEST_ETCD_ENDPOINT=https://127.0.0.1:2379 \
DBL_TEST_ETCD_CA=/path/to/ca.pem \
DBL_TEST_ETCD_CLIENT_CERT=/path/to/client.pem DBL_TEST_ETCD_CLIENT_KEY=/path/to/client-key.pem \
DBL_TEST_ETCD_USERNAME=dbltest DBL_TEST_ETCD_PASSWORD=secret \
  go test ./internal/etcd -run '^TestEtcdIntegration$' -v
```

2026-10-26 已使用 server v3.7.3 和项目 client v3.6.16 通过明文及用户名/密码 + 私有 CA verify-full + 客户端证书 mTLS 单节点验证；revision compaction 错误路径已集成验证；目标部署版本与 UI 仍需验收。

2026-10-26 在 Apple M1 / macOS 15.8.1 / Go 1.27.3 上复测：10,000 行 × 128 字节 SQLite 扫描，批次 128/512/1,024 分别约 2.980/3.001/2.985 ms、429.47/426.56/428.82 MB/s、2.09/2.11/2.11 MB 分配；10,000 行索引排序约 1.36 ms，混合整数/浮点排序约 1.51 ms，均约 82 KB、3 次分配。只测后端扫描/索引排序，不含 UI 绘制；命令和完整数据见 [阶段 0 ADR](docs/decisions/0001-stage-0.md)。macOS 原生窗口、菜单与文件面板已通过可见 app bundle 实际启动；桌面 SQL 查询、结果网格和 CSV 导出已端到端操作验证。中文输入法和屏幕阅读器仍待验收。

## 第三方许可

`THIRD_PARTY_NOTICES.md` 列出六个发布目标的 Go 运行依赖及版本；完整上游许可/通知文件随包放在 `resources/THIRD_PARTY_LICENSES/`。MySQL 驱动 MPL-2.0 源码也一并保留。DBL 自身许可证尚未由项目所有者决定，发布前仍需处理；该清单不代表完成法律审阅。

## 当前阶段限制

- SQLite 驱动固定为 `modernc.org/sqlite v1.61.0`，并按驱动 go.mod 固定 `modernc.org/libc v1.79.0`；纯 Go，无 CGO。依赖校验和已写入 `go.sum`，构建与测试可在当前模块缓存下以 `GOPROXY=off` 重现。
- PostgreSQL 驱动固定为 `github.com/jackc/pgx/v5 v5.11.0`，MySQL 驱动固定为 `github.com/go-sql-driver/mysql v1.10.1`；两者通过 `database/sql` adapter 接入。MySQL 驱动使用 MPL-2.0，其 v1.10.1 许可和源文件已随 MyGo 原生资源打包。SQLite 使用 `modernc.org/sqlite v1.61.0` 纯 Go 驱动；已接受其相对原生驱动的性能折损，并以本机代表性扫描基准作基线。
- 首发平台候选为 macOS 12+ arm64；MyGo 文档的打包默认最低 macOS 版本为 12.0。Linux x64/arm64（GTK 3）和 Windows 10/11 x64/arm64 为待验证目标，均不是已验证的支持承诺。
- 当前开发机为 macOS 15.8.1 arm64、Go 1.27.3。MySQL adapter 加入后的 `GOPROXY=off go test ./...`、`go test -race ./...`、`go vet ./...`、`CGO_ENABLED=0 go build ./...` 及 darwin/amd64、linux/amd64、linux/arm64、windows/amd64、windows/arm64 纯 Go 交叉构建均通过。PostgreSQL 17 与 MySQL 8.4 的一次性本机服务器集成用例及 `verify-full` TLS 握手均已通过。可见 app bundle 已检查 MySQL profile 的 TLS 选择和风险提示；真正连接/保存 profile 的桌面验收仍待做。中文输入法及屏幕阅读器验收未完成。
- 查询历史默认只保留本次运行的最近 100 条；在历史面板开启本机保存后，最近 100 条会写入私有设置文件，关闭时清除磁盘副本。
- 不返回结果集的 SQL 会显示执行完成，但尚未展示 DML 影响行数。
- `tmp/` 是本地工作目录，不属于应用交付内容。
