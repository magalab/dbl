# ADR 0002: 本地偏好设置与 SQLite 连接配置

- 状态：进行中
- 日期：2026-10-25

## 决策

- 阶段 2 先落地 SQLite 连接配置 UI 子集：最多保存 100 个配置，支持名称、分组、收藏、文件路径和只读模式；另保存最近成功打开的 10 个路径。外观设置保存 `system`、`light` 或 `dark`，默认跟随系统。
- 持久化模型现统一为 `ConnectionProfile`，支持 SQLite、PostgreSQL 与 MySQL 的非敏感连接元数据；远程 profile 可保存 host、port、database、username、只读/收藏标志和可选不透明 credential reference。secret 本身不在该模型中。旧 `sqlite_profiles` 在加载时迁移并继续镜像写入，保留现有 SQLite 配置兼容性。
- 原生工作区支持 SQLite 配置的创建、编辑、复制、删除、选择、测试连接和打开；PostgreSQL/MySQL profile 均支持 TLS 配置、连接、测试以及共用 SQL 工作区。连接期间可从左侧栏展开配置管理。启动时恢复最近 SQLite 路径供文件选择使用，并展示已保存配置；恢复设置不会自动连接或创建文件。
- 偏好写入操作系统用户配置目录下的 `dbl/settings.toml`。旧版 `settings.json` 在首次启动时自动迁移。除连接元数据和外观模式外，还记录左右分栏宽度、检查器宽度与结果区高度；拖动停止 350 ms 后保存，避免每帧写盘。Unix 系统上配置目录权限为 `0700`，配置文件权限为 `0600`；Windows 通过受保护 DACL 仅授予当前用户完全控制，并使目录权限继承到子项。启动加载和写入时都会重新收紧目录及文件权限。写入采用同目录临时文件、文件 `fsync`，再原子替换；POSIX 上替换后同步父目录，Windows 使用 `MOVEFILE_WRITE_THROUGH`。
- 文件路径属于本机偏好，不包含数据库密码、证书私钥或 AI key。远程 profile 的密码输入使用密码控件，异步写入系统凭据库；设置文件只保存不透明引用。留空保留原密码，显式勾选后在保存时移除；删除 profile 时仅在无其他 profile 引用该凭据后清理 vault 条目。独立凭据 vault wrapper 及平台限制见 ADR 0004。
- 最近路径和配置写入在后台执行，并带单调递增 revision，防止较早启动的异步写入覆盖较新的设置。SQLite 打开期间，成功加载数据库对象后再保存最近路径。
- 查询历史单独 opt-in 持久化：默认关闭；打开后设置文件保存最多 100 条、每条 SQL 不超过 64 KiB 的记录，保留设置文件私有权限。错误详情不保存；关闭开关会删除历史。SQL 编辑器草稿、结果窗口和工作区标签不自动持久化，退出或崩溃后不恢复，避免未选择保存的 SQL/结果写入磁盘；已提交的配置和显式选择持久化的历史通过原子设置文件更新保留。

## 验证

- `internal/preferences` 测试覆盖首次启动空配置、最近路径去重/顺序/重载、SQLite 与远程 profile CRUD/复制/重复名称/上限、默认端口和非法配置拒绝、旧 SQLite profile 迁移、布局和外观的持久化/校验、加载时收紧已有配置目录/文件权限、较旧 revision 忽略、非法文件类型拒绝，以及查询历史默认关闭、限制、重启加载、关闭清除和跨设置并发写入合并。
- 当前包测试使用临时目录，不接触真实用户设置。
- 完整 MyGo 应用无法编译时，启动恢复和 UI 最近文件列表仍需在依赖可用后做端到端验证。

## 限制

- 远程 profile UI 保存配置元数据和凭据引用；实际密码保存在系统密钥库。PostgreSQL/MySQL 已通过连接 adapter 接入工作区。SQL 标签和结果不持久化；查询历史默认不持久化，用户可选择保存 SQL 历史。主题偏好已持久化并接入 MyGo appearance API。正式 DBL bundle 身份下的重启引用和授权提示仍需验收。
- Unix 权限在单测中检查；跨平台测试目前只证明包可编译，不证明 Windows 上的运行时 ACL 或替换已有配置文件语义。
- 配置文件损坏或版本未知时会在启动状态区报告错误；当前不自动删除、覆盖或尝试猜测恢复内容。SQL 草稿和结果窗口不做崩溃恢复；自动恢复最近编辑内容会把 SQL 持久写盘，因此暂不启用。设置更新已有临时文件、文件 `fsync`、原子替换和替换持久化处理；子进程在创建临时文件、写入、文件同步、关闭、完成替换等边界主动退出的回归用例确认重载后只见完整旧值或完整新值。Windows ACL 代码和断言已实现并交叉编译，实际 Windows 运行、突然强杀和掉电仍待验证。
- 路径可揭示用户本机目录结构，因此文件权限仍按私有配置处理。

## Windows ACL 依据

- Windows 实现使用 `SetNamedSecurityInfo` 设置受保护 DACL，并以 `OBJECT_INHERIT_ACE` 与 `CONTAINER_INHERIT_ACE` 让目录授权传递到文件和子目录。参考 [SetNamedSecurityInfo](https://learn.microsoft.com/en-us/windows/win32/api/aclapi/nf-aclapi-setnamedsecurityinfow) 和 [ACE inheritance rules](https://learn.microsoft.com/en-us/windows/win32/secauthz/ace-inheritance-rules)。本机只完成 Windows amd64/arm64 编译和 ACL 断言测试的交叉编译，仍需在 Windows 实机运行。
