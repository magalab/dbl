# Repository guidance

## Go structure

- Keep the module root limited to `main.go`, which delegates startup to `internal/app`.
- Put the desktop workbench, UI, app-level state, and their focused tests in `internal/app`. Keep these cohesive files in the `app` package when they share the workbench state; split by responsibility rather than arbitrarily by line count.
- Put reusable database protocols, persistence, credentials, and safety boundaries in their matching `internal/` packages. Avoid importing the app UI package from these lower-level packages.
- Keep package-level helpers unexported unless another package needs them. Avoid adding dependencies between UI files through new global state.
- Keep package-level helpers unexported unless another package needs them. Avoid adding dependencies between UI files through new global state.

## Product behavior and safety

- Keep Chinese and English strings in the localization layer. Any new visible UI text must have both translations.
- Use the shared icon helpers for icon-only actions and provide localized labels/tooltips for accessibility.
- Prefer icons for buttons wherever the action is clear from its context; keep localized accessible labels/tooltips, and use color or icon changes to show toggled state.
- Store API keys, database secrets, and TLS private material only through the encrypted local credential vault. Preferences may keep opaque credential references, never secret values. Keep the vault directory and key file restricted to the current OS user.
- AI requests must remain opt-in, show the actual payload before sending, and never execute generated SQL or data changes automatically.
- Preserve read-only protections and explicit preview/confirmation flows for database mutations.
- Keep SQLite builds pure Go (`CGO_ENABLED=0`); do not add a CGO-only SQLite dependency.

## Changes and verification

- Format Go changes with `gofmt`.
- Use `go build ./...` for a repository compile check. Add or run focused tests when a behavior change needs them; integration tests may require local database services.
- Keep documentation aligned with user-visible behavior and shortcuts.
