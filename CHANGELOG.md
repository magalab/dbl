# Changelog

## [0.1.3] - 2026-11-18

- Show the DBL application icon in the native update window.
- Move detailed settings explanations into localized hover tips.
- Arrange shortcuts in two columns and show the platform-specific modifier.

## [0.1.2] - 2026-11-18

- Add signed in-app updates for macOS arm64 and Windows amd64, with opt-in background checks and a toolbar shortcut.
- Add automatic SQL draft recovery and window startup mode / font preferences.
- Improve saved connection workflows and add a Windows packaging command.

## 0.1.1 - 2026-11-17

- Use concrete Windows file permissions for current-user-only ACLs.
- Retry initial distribution after the v0.1.0 Windows ACL check failed.

## 0.1.0 - Build attempt (not released)

- Add native SQL workspaces for SQLite, PostgreSQL, and MySQL.
- Add Redis key browsing, MongoDB document browsing, and etcd prefix/revision workflows.
- Add an optional streaming SQL assistant with explicit context selection and draft review.
- Add connection environment labels and production SQL confirmation.
- Support custom PEM CA bundles for PostgreSQL and MySQL TLS verification.
- Publish initial trial packages for macOS 15+ arm64 (ad-hoc signed) and Windows 10/11 amd64.
- Add GitHub Actions builds, SHA-256 checksums, and Homebrew tap publishing.
- Show affected row counts for direct DML statements without RETURNING.
- Desktop, accessibility, installation and platform-specific security acceptance remains tracked in `docs/release-readiness.md`.
