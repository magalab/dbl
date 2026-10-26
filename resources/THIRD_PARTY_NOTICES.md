# DBL 第三方组件许可清单

生成日期：2026-10-26。范围为 MyGo 原生应用运行包在 darwin/arm64、darwin/amd64、linux/amd64、linux/arm64、windows/amd64、windows/arm64 上的 Go 依赖并集；版本来自当前 `go.mod` 模块解析结果。依赖许可证和通知文件随 `resources/THIRD_PARTY_LICENSES/` 分发。

本清单不为 DBL 自身代码选择许可证；仓库目前没有声明项目许可证，公开分发前仍需由项目所有者决定。

扫描使用 `github.com/google/go-licenses v1.6.0`。`modernc.org/libc`、`mathutil` 和 `memory` 的模块根许可证逐条核对后按 BSD-3-Clause 记录；扫描器低置信度分类不作为这三项的依据。MySQL 驱动 v1.10.1 使用 MPL-2.0，其锁定版本源文件随许可证文本保存在资源目录对应路径中。

| 导入包 | Go 模块 | 锁定版本 | 许可证 | 上游许可证来源 |
|---|---|---:|---|---|
| `filippo.io/edwards25519` | `filippo.io/edwards25519` | `v1.2.0` | BSD-3-Clause | [LICENSE](https://github.com/FiloSottile/edwards25519/blob/v1.2.0/LICENSE) |
| `github.com/cespare/xxhash/v2` | `github.com/cespare/xxhash/v2` | `v2.3.0` | MIT | [LICENSE](https://github.com/cespare/xxhash/blob/v2.3.0/LICENSE.txt) |
| `github.com/coreos/go-semver/semver` | `github.com/coreos/go-semver` | `v0.3.1` | Apache-2.0 | [LICENSE](https://github.com/coreos/go-semver/blob/v0.3.1/LICENSE) |
| `github.com/coreos/go-systemd/v22/journal` | `github.com/coreos/go-systemd/v22` | `v22.5.0` | Apache-2.0 | [LICENSE](https://github.com/coreos/go-systemd/blob/v22.5.0/LICENSE) |
| `github.com/dgryski/go-rendezvous` | `github.com/dgryski/go-rendezvous` | `v0.0.0-20200823014737-9f7001d12a5f` | MIT | [LICENSE](https://github.com/dgryski/go-rendezvous/blob/9f7001d12a5f/LICENSE) |
| `github.com/dustin/go-humanize` | `github.com/dustin/go-humanize` | `v1.1.0` | MIT | [LICENSE](https://github.com/dustin/go-humanize/blob/v1.1.0/LICENSE) |
| `github.com/ebitengine/purego` | `github.com/ebitengine/purego` | `v0.11.1` | Apache-2.0 | [LICENSE](https://github.com/ebitengine/purego/blob/v0.11.1/LICENSE) |
| `github.com/egoist/mygo` | `github.com/egoist/mygo` | `v0.6.2` | MIT | [LICENSE](https://github.com/egoist/mygo/blob/v0.6.2/LICENSE) |
| `github.com/go-sql-driver/mysql` | `github.com/go-sql-driver/mysql` | `v1.10.1` | MPL-2.0 | [LICENSE](https://github.com/go-sql-driver/mysql/blob/v1.10.1/LICENSE) |
| `github.com/go-text/typesetting` | `github.com/go-text/typesetting` | `v0.3.5` | BSD-3-Clause | [LICENSE](https://github.com/go-text/typesetting/blob/v0.3.5/LICENSE) |
| `github.com/gogo/protobuf` | `github.com/gogo/protobuf` | `v1.3.2` | BSD-3-Clause | [LICENSE](https://github.com/gogo/protobuf/blob/v1.3.2/LICENSE) |
| `github.com/golang/protobuf/proto` | `github.com/golang/protobuf` | `v1.5.4` | BSD-3-Clause | [LICENSE](https://github.com/golang/protobuf/blob/v1.5.4/LICENSE) |
| `github.com/google/uuid` | `github.com/google/uuid` | `v1.6.0` | BSD-3-Clause | [LICENSE](https://github.com/google/uuid/blob/v1.6.0/LICENSE) |
| `github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2/options` | `github.com/grpc-ecosystem/grpc-gateway/v2` | `v2.29.0` | BSD-3-Clause | [LICENSE](https://github.com/grpc-ecosystem/grpc-gateway/blob/v2.29.0/LICENSE) |
| `github.com/jackc/pgpassfile` | `github.com/jackc/pgpassfile` | `v1.0.0` | MIT | [LICENSE](https://github.com/jackc/pgpassfile/blob/v1.0.0/LICENSE) |
| `github.com/jackc/pgservicefile` | `github.com/jackc/pgservicefile` | `v0.0.0-20240606120523-5a60cdf6a761` | MIT | [LICENSE](https://github.com/jackc/pgservicefile/blob/5a60cdf6a761/LICENSE) |
| `github.com/jackc/pgx/v5` | `github.com/jackc/pgx/v5` | `v5.11.0` | MIT | [LICENSE](https://github.com/jackc/pgx/blob/v5.11.0/LICENSE) |
| `github.com/jackc/puddle/v2` | `github.com/jackc/puddle/v2` | `v2.2.2` | MIT | [LICENSE](https://github.com/jackc/puddle/blob/v2.2.2/LICENSE) |
| `github.com/klauspost/compress` | `github.com/klauspost/compress` | `v1.19.2` | Apache-2.0 | [LICENSE](https://github.com/klauspost/compress/blob/v1.19.2/LICENSE) |
| `github.com/klauspost/compress/internal/snapref` | `github.com/klauspost/compress` | `v1.19.2` | BSD-3-Clause | [LICENSE](https://github.com/klauspost/compress/blob/v1.19.2/internal/snapref/LICENSE) |
| `github.com/klauspost/compress/s2` | `github.com/klauspost/compress` | `v1.19.2` | BSD-3-Clause | [LICENSE](https://github.com/klauspost/compress/blob/v1.19.2/s2/LICENSE) |
| `github.com/klauspost/compress/snappy` | `github.com/klauspost/compress` | `v1.19.2` | BSD-3-Clause | [LICENSE](https://github.com/klauspost/compress/blob/v1.19.2/snappy/LICENSE) |
| `github.com/klauspost/compress/zstd/internal/xxhash` | `github.com/klauspost/compress` | `v1.19.2` | MIT | [LICENSE](https://github.com/klauspost/compress/blob/v1.19.2/zstd/internal/xxhash/LICENSE.txt) |
| `github.com/mattn/go-isatty` | `github.com/mattn/go-isatty` | `v0.0.24` | MIT | [LICENSE](https://github.com/mattn/go-isatty/blob/v0.0.24/LICENSE) |
| `github.com/ncruces/go-strftime` | `github.com/ncruces/go-strftime` | `v1.1.0` | MIT | [LICENSE](https://github.com/ncruces/go-strftime/blob/v1.1.0/LICENSE) |
| `github.com/redis/go-redis/v9` | `github.com/redis/go-redis/v9` | `v9.17.3` | BSD-2-Clause | [LICENSE](https://github.com/redis/go-redis/blob/v9.17.3/LICENSE) |
| `github.com/remyoudompheng/bigfft` | `github.com/remyoudompheng/bigfft` | `v0.0.0-20230129092748-24d4a6f8daec` | BSD-3-Clause | [LICENSE](https://github.com/remyoudompheng/bigfft/blob/24d4a6f8daec/LICENSE) |
| `github.com/xdg-go/scram` | `github.com/xdg-go/scram` | `v1.2.0` | Apache-2.0 | [LICENSE](https://github.com/xdg-go/scram/blob/v1.2.0/LICENSE) |
| `github.com/xdg-go/stringprep` | `github.com/xdg-go/stringprep` | `v1.0.4` | Apache-2.0 | [LICENSE](https://github.com/xdg-go/stringprep/blob/v1.0.4/LICENSE) |
| `github.com/youmark/pkcs8` | `github.com/youmark/pkcs8` | `v0.0.0-20240726163527-a2c0da244d78` | MIT | [LICENSE](https://github.com/youmark/pkcs8/blob/a2c0da244d78/LICENSE) |
| `go.etcd.io/etcd/api/v3` | `go.etcd.io/etcd/api/v3` | `v3.6.16` | Apache-2.0 | [LICENSE](https://github.com/etcd-io/etcd/blob/api/v3.6.16/api/LICENSE) |
| `go.etcd.io/etcd/client/pkg/v3` | `go.etcd.io/etcd/client/pkg/v3` | `v3.6.16` | Apache-2.0 | [LICENSE](https://github.com/etcd-io/etcd/blob/client/pkg/v3.6.16/client/pkg/LICENSE) |
| `go.etcd.io/etcd/client/v3` | `go.etcd.io/etcd/client/v3` | `v3.6.16` | Apache-2.0 | [LICENSE](https://github.com/etcd-io/etcd/blob/client/v3.6.16/client/v3/LICENSE) |
| `go.mongodb.org/mongo-driver/v2` | `go.mongodb.org/mongo-driver/v2` | `v2.9.2` | Apache-2.0 | [LICENSE](https://github.com/mongodb/mongo-go-driver/blob/v2.9.2/LICENSE) |
| `go.uber.org/multierr` | `go.uber.org/multierr` | `v1.11.0` | MIT | [LICENSE](https://github.com/uber-go/multierr/blob/v1.11.0/LICENSE.txt) |
| `go.uber.org/zap` | `go.uber.org/zap` | `v1.27.0` | MIT | [LICENSE](https://github.com/uber-go/zap/blob/v1.27.0/LICENSE) |
| `golang.org/x/crypto` | `golang.org/x/crypto` | `v0.58.0` | BSD-3-Clause | [LICENSE](https://cs.opensource.google/go/x/crypto/+/v0.58.0:LICENSE) |
| `golang.org/x/image` | `golang.org/x/image` | `v0.46.0` | BSD-3-Clause | [LICENSE](https://cs.opensource.google/go/x/image/+/v0.46.0:LICENSE) |
| `golang.org/x/net` | `golang.org/x/net` | `v0.61.0` | BSD-3-Clause | [LICENSE](https://cs.opensource.google/go/x/net/+/v0.61.0:LICENSE) |
| `golang.org/x/sync` | `golang.org/x/sync` | `v0.24.0` | BSD-3-Clause | [LICENSE](https://cs.opensource.google/go/x/sync/+/v0.24.0:LICENSE) |
| `golang.org/x/sys` | `golang.org/x/sys` | `v0.49.0` | BSD-3-Clause | [LICENSE](https://cs.opensource.google/go/x/sys/+/v0.49.0:LICENSE) |
| `golang.org/x/sys/unix` | `golang.org/x/sys` | `v0.49.0` | BSD-3-Clause | [LICENSE](https://cs.opensource.google/go/x/sys/+/v0.49.0:LICENSE) |
| `golang.org/x/text` | `golang.org/x/text` | `v0.43.0` | BSD-3-Clause | [LICENSE](https://cs.opensource.google/go/x/text/+/v0.43.0:LICENSE) |
| `google.golang.org/genproto/googleapis/api` | `google.golang.org/genproto/googleapis/api` | `v0.0.0-20260803160001-6ac0973c030d` | Apache-2.0 | [LICENSE](https://github.com/googleapis/go-genproto/blob/6ac0973c030d/googleapis/api/LICENSE) |
| `google.golang.org/genproto/googleapis/rpc/status` | `google.golang.org/genproto/googleapis/rpc` | `v0.0.0-20260803160001-6ac0973c030d` | Apache-2.0 | [LICENSE](https://github.com/googleapis/go-genproto/blob/6ac0973c030d/googleapis/rpc/LICENSE) |
| `google.golang.org/grpc` | `google.golang.org/grpc` | `v1.83.2` | Apache-2.0 | [LICENSE](https://github.com/grpc/grpc-go/blob/v1.83.2/LICENSE) |
| `google.golang.org/protobuf` | `google.golang.org/protobuf` | `v1.36.11` | BSD-3-Clause | [LICENSE](https://github.com/protocolbuffers/protobuf-go/blob/v1.36.11/LICENSE) |
| `modernc.org/libc` | `modernc.org/libc` | `v1.79.0` | BSD-3-Clause | [LICENSE](https://gitlab.com/cznic/libc/blob/v1.79.0/LICENSE) |
| `modernc.org/mathutil` | `modernc.org/mathutil` | `v1.7.1` | BSD-3-Clause | [LICENSE](https://gitlab.com/cznic/mathutil/blob/v1.7.1/LICENSE) |
| `modernc.org/memory` | `modernc.org/memory` | `v1.12.1` | BSD-3-Clause | [LICENSE](https://gitlab.com/cznic/memory/blob/v1.12.1/LICENSE) |
| `modernc.org/sqlite` | `modernc.org/sqlite` | `v1.61.0` | BSD-3-Clause | [LICENSE](https://gitlab.com/cznic/sqlite/blob/v1.61.0/LICENSE) |

SQLite 纯 Go 驱动另带有 `LICENSE-3RD-PARTY.md`；该文件以及 SQLite、libc、memory、mathutil 的模块级许可证文件均保留在随包资源中。
