# 开发与验证

Go 版本和依赖版本以 [go.mod](../go.mod) 为准。SQLite 保持纯 Go 构建；构建和常规测试使用 `CGO_ENABLED=0`。

## 本地运行与打包

```sh
make run
make build
make package-macos
make package-windows
```

`make build` 输出 `build/dbl`。`make package-macos` 使用与 go.mod 一致的 MyGo CLI，生成 macOS arm64 的 ad-hoc 签名安装包。`make package-windows` 需在 Windows amd64 主机运行，生成 `build/windows-amd64/` 下的应用和 NSIS 安装器；MyGo 会准备 NSIS 构建依赖。正式 Windows 发布仍由 [GitHub Actions](github-actions.md) 构建和上传。

## 常规检查与集成验证

下文的通过记录和性能数字来自历史验证，不代表当前工作树或最新发布包的验证结论。

以下集成用例会写入测试数据库，请使用隔离的临时服务。各命令中的数据库密码均为示例测试值。


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

2026-10-26 在 Apple M1 / macOS 15.8.1 / Go 1.27.3 上复测：10,000 行 × 128 字节 SQLite 扫描，批次 128/512/1,024 分别约 2.980/3.001/2.985 ms、429.47/426.56/428.82 MB/s、2.09/2.11/2.11 MB 分配；10,000 行索引排序约 1.36 ms，混合整数/浮点排序约 1.51 ms，均约 82 KB、3 次分配。只测后端扫描/索引排序，不含 UI 绘制；命令和完整数据见 [阶段 0 ADR](decisions/0001-stage-0.md)。macOS 原生窗口、菜单与文件面板已通过可见 app bundle 实际启动；桌面 SQL 查询、结果网格和 CSV 导出已端到端操作验证。中文输入法和屏幕阅读器仍待验收。


## 如何理解历史验证记录

本页保留此前 README 中的集成验证和基准记录。这些记录对应当时的代码、依赖和环境，不代表当前工作树或最新安装包已通过同样检查。依赖升级后需按改动重新验证；历史 Linux、Intel Mac 和 Windows arm64 构建记录不属于当前发布范围。

当前发布范围与验收要求见 [发布准备清单](release-readiness.md)，实现取舍见 [技术决策记录](decisions/)。
