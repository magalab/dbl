# GitHub Actions 与直接分发

公共仓库：https://github.com/magalab/dbl

当前发布目标仅为 macOS arm64 和 Windows amd64。草稿发布任务使用 Ubuntu runner 汇总产物，不构建 Linux 应用。

应用通过 GitHub Releases 直接分发，各平台不考虑应用商店。macOS 使用 ad-hoc 签名，不进行 Developer ID 签名或 Apple 公证；下载后的首次打开可能需要用户在系统设置中允许。

## 持续检查

`.github/workflows/ci.yml` 在 main 推送、Pull Request 或手动触发时运行，并供发布流程复用。在 macOS arm64 和 Windows amd64 上检查 Go 格式、模块校验、编译、vet 和单元测试，始终设置 `CGO_ENABLED=0`。需要真实数据库服务的集成用例保持 opt-in，不由此流程提供服务。

## 构建与发布

`.github/workflows/release.yml` 支持手动构建，以及推送 `v*` 标签后构建。所有构建先通过 CI 检查；Go 版本从 go.mod 读取，MyGo CLI 使用 go.mod 中同一模块版本。

| 目标 | 交付内容 |
|---|---|
| macOS arm64 | ad-hoc 签名的 DMG，检查签名及二进制架构 |
| Windows amd64 | NSIS Setup 安装器 |

每个目标独立构建并上传 Actions artifact，保留 14 天。手动构建只上传 artifact；标签构建在全部成功后创建或更新对应的草稿 Release，并附上 SHA256SUMS。已有正式发布的 Release 不会被覆盖。Windows 安装器依赖由 MyGo CLI 自动准备；无须配置 Apple 签名证书或公证凭据。

`resources/icon.png` 和 `resources/icon-macos.png` 是构建图标输入，通过 `resourcesExclude` 排除独立文件复制；Windows 应用与安装器的图标仍由 MyGo 内嵌。Windows 安装目录保留 `DBL.exe`、`Uninstall.exe`、`.mygo-files` 安装文件清单、`LICENSE`、`THIRD_PARTY_NOTICES.md` 和 `THIRD_PARTY_LICENSES/`。安装与覆盖升级行为仍需 Windows 实机验收。

发布步骤：

1. 更新 mygo.json 的 version 和 CHANGELOG.md，提交并推送 main。
2. 创建并推送与 version 一致的标签，例如 version 为 `0.1.0` 时推送 `v0.1.0`。标签不匹配时构建失败。
3. 等待 Release workflow 完成，在草稿 Release 下载并验收安装包、许可资源和校验和。
4. 完成验收后手动发布草稿 Release；正式稳定版发布后自动更新 Homebrew tap。

## Homebrew tap

`.github/workflows/homebrew.yml` 参考 Meow 的发布方式，使用仓库 secret `HOMEBREW_TAP_TOKEN` 检出并提交到 `magalab/homebrew-tap`。Token 需要该 tap 的 contents 写入权限。

正式稳定版 Release 发布后，流程下载 `dbl-<version>-arm64.dmg` 和 SHA256SUMS，核对校验和，然后生成或更新 `Casks/dbl.rb`。Cask 声明仅支持 macOS 15+ arm64，安装 `DBL.app`。草稿和预发布版本不更新 tap；为避免旧版本重试导致回退，只允许当前最新稳定版。流程失败时可在 Actions 的 Homebrew Tap 页面手动输入同一 tag 重试。

首次正式发布且 tap 更新成功后，可运行：

```sh
brew install --cask magalab/tap/dbl
```

该流程提供安装包构建和下载，不配置应用内自动更新。发布前的实际桌面操作、安装卸载和各平台凭据库验收仍按 release-readiness.md 执行。
