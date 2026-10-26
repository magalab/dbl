# ADR 0011: 可替换的流式 AI provider

- 状态：阶段 6 首版工作流已实现；云端请求抓包与桌面人工验收待完成
- 日期：2026-10-26

## 决策

- 定义独立 `assistant.Provider` 接口，首个实现通过标准库 HTTP 使用 OpenAI-compatible Chat Completions SSE，不把特定厂商 SDK 传入 UI。配置保存 endpoint、model 和系统凭据库 API key 引用；API key 不进入设置文件。
- 远程 endpoint 必须 HTTPS，只有 localhost 可使用 HTTP；禁止 URL 内凭据、query 和 fragment，禁止重定向。请求和响应均有大小上限，用户可取消流式请求。
- AI 面板支持 SQL 生成/解释/错误解释、Mongo filter/pipeline 草稿和 Redis/etcd 文字建议。SQL、错误、schema、Mongo 查询文本、Redis key 元数据和 etcd revision 元数据默认不发送；每一项都要逐项主动勾选，确认前显示完整 system/user payload。上下文合并不超过 64 KiB；文档/键/配置值永不由这些 context 开关发送。
- 请求预览、回答和提示仅保存在运行时内存。AI 面板提供一次性清空操作，移除提示、payload 预览、回答和勾选状态；未保存的 API key 可单独清除。清空不删除已放入 SQL/Mongo 编辑器的草稿或已保存 provider 设置。
- 生成结果可复制或放入相应 SQL/Mongo 编辑器，默认只显示为草稿。成功回答可按上次已审核的消息与 provider 快照重新准备请求；重发前再次展示相同 payload 并要求用户确认。AI 关闭或设置有未保存改动时禁用重发。放入 SQL 编辑器后，执行前还会展示目标连接、SQL 文本和粗略风险分类，并要求二次确认；编辑草稿后必须重新审阅。AI 不持有数据库连接或执行 capability。

## 限制

- Provider 仅兼容 `choices[].delta.content` 形式的 SSE；供应商专有事件格式、工具调用、多模态、重试/配额管理和本地模型不支持。Mongo 输出只是 JSON 草稿；Redis/etcd 仅给文字建议，没有命令执行路径。
- AI 请求提供 SQL、MongoDB、Redis、etcd 与错误排查的内置提示模板；模板插入后仍可在多行编辑器中修改。尚未对真实云 provider、取消网络请求和敏感数据发送做人工抓包验收。

## 自动化验证

- `internal/assistant/provider_test.go` 用真实 HTTP server 验证完整 JSON 请求体精确等于预期 model、stream 和调用者准备的 messages；API key 只出现在 Authorization header，不进入 JSON body。测试还覆盖 SSE 分块拼接、HTTPS/URL 配置约束、重定向目标不接收请求，以及 context cancel 能停止 HTTP 流。
- 根包 `main_test.go` 验证默认预览不包含 SQL、错误详情或 schema，生成请求仅停留在待审阅状态；显式 SQL 同意只影响本次准备的请求，随后自动清除同意标记。提示模板覆盖所有任务模式，可插入后编辑，AI 界面通过 headless 测试确认模板选择和多行编辑入口可见；重新生成复用上次已审核 payload、再次经过确认且在 AI 关闭时被拒绝。
- 这些用例覆盖代码路径，不替代真实云 provider 抓包和桌面交互验收。
