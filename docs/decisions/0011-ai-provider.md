# ADR 0011: Streaming AI providers

Updated: 2026-10-29

Multiple named OpenAI-compatible providers can be added, edited, removed and selected in the assistant. Each saves its own endpoint, model and opaque reference to an encrypted local API key. Legacy single-provider settings migrate automatically. Selection persists across restarts.

Send submits directly to the selected provider without a mandatory payload preview or second confirmation. Task, template and context options are collapsed by default. SQL, errors, schema and database metadata are attached only when explicitly selected for that request. Context selections reset after request preparation. Prompts and responses remain in memory and can be cleared.

Regenerate reuses the previous messages with the currently selected provider. Requests and provider switching are disabled while a request is running or provider edits are unsaved. Generated content is a draft; inserting SQL never executes it and database mutation confirmation remains required.

API keys stay in the encrypted credential vault, never preferences JSON. Replacing or removing a provider cleans up credentials only if no other provider references them. Remote endpoints require HTTPS; HTTP is allowed only for localhost. URL credentials, queries, fragments and redirects are rejected. Requests are bounded and streams are cancellable.

Supported responses use choices[].delta.content SSE. Vendor-specific protocols, tool calls and multimodal responses are not supported. Real cloud traffic and desktop visual acceptance still require manual verification.

Focused checks cover legacy migration, independent provider persistence, selected-account regeneration, canceling edits, default hidden request internals, expandable options, context opt-in and the underlying HTTP/SSE provider behavior.
