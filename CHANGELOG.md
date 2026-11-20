# Changelog

## [0.1.6] - 2026-11-20

- Add Redis instance metrics and an etcd cluster dashboard with lease management and key-binding workflows.
- Improve SQL result browsing with page-size selection, page jumps through the bounded result stream, a fixed row-number column, and export actions in result context menus.
- Add staged table edits with a configurable confirmation before submission.
- Improve connection and database overview panels, keep query settings with each workspace, and clarify connection switching in the workspace tabs.
- Keep database/schema selection with each query workspace, improve connection overview tabs, and update database driver dependencies.

## [0.1.5] - 2026-11-19

- Simplify the update dialogs and add space before the version message.
- Save interface settings automatically and restore the last saved values if a write fails.
- Let users show and hide saved connection passwords without changing the saved credential.

## [0.1.4] - 2026-11-18

- Show the existing rounded-square application icon with more title spacing in the update dialog.
- Offer update check intervals of 1, 2, 6, 12, and 24 hours; legacy values fall back to 24 hours.
- Add an optional Windows setting to keep DBL in the system tray when its window closes.
- Show prominent connection progress and retry details when a Quick Start connection fails.

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
