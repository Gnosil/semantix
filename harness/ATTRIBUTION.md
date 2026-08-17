# Harness Attribution

`harness/` 是从 DeepSeek-Reasonix vendor 进本仓的 agent 系统（合体路线：
`docs/specs/h2h3-resource-orchestration.md` §0/§3，2026-08-17 决策）。

## 上游

- 原始项目：[esengine/DeepSeek-Reasonix](https://github.com/esengine/DeepSeek-Reasonix)（MIT，`main-v2` 分支）
- 经由 fork：Gnosil/DeepSeek-Reasonix
- **脱钩基线 commit：`bf0d8594`**（fork `feat/h4-branding` 的干净基线，
  即「fix: restore semantix_lookup builtin registration」）
- 上游许可证原文：[LICENSE.upstream](./LICENSE.upstream)（MIT）

自该基线起本目录与上游**正式脱钩**：上游后续修复不再自动同步，
重大修复按需手工 cherry-pick 并在本文件追加记录。

## 搬运清单（U38，2026-08-17）

**依据**：`cmd/reasonix` 编译闭包分析——`internal/` 90 个顶层包中 85 个在闭包内，
`internal/boot` 单独闭包即 84 个包（依赖图致密，入口级裁剪收益 ~5 包但需改造骨架，
违背蓝图「骨架不动」原则），故采用近全量 vendor。

| 内容 | 处置 | 理由 |
|---|---|---|
| `internal/`（88 包）→ `harness/internal/` | ✅ 搬 | 编译闭包 + 测试辅助（`testenv`/`worktree`/`appidentity` 虽在闭包外但被测试与 Tier B 契约引用，保留避免断链） |
| `internal/desktoplauncher` | ❌ 裁 | 桌面启动器，闭包外，desktop module 本期不搬 |
| `internal/semantix` | ❌ 裁 | 跨进程时代的 kernel 桥（CLI 子进程 sink/inject/sched 移植），由进程内接线替代（spec §4，U40 落地） |
| `docs/`、`release-notes/` → `harness/docs`、`harness/release-notes` | ✅ 搬 | `go:embed` 包，闭包内 |
| `cmd/reasonix` → `harness/entry` + `cmd/semantix-agent` | ✅ 改造 | 顶层命令无法 import `harness/internal`（Go internal 可见性），以公开 `entry` 包作接缝 |
| `desktop/`（独立 go module） | ❌ 不搬 | spec §3：#158 时再取 |
| `sdk/`、`npm/`、`tools/`、`benchmarks/`、`prod_test/` 等 | ❌ 不搬 | 闭包外 |

**Import 改写**：`reasonix/internal/...` → `semantix/harness/internal/...`、
`reasonix/docs|release-notes` → `semantix/harness/docs|release-notes`（纯前缀替换）。
字符串字面量中的 `reasonix/...`（worktree 分支名模板、ACP vendor method 名等）
按 Tier B 不改清单保留（改动 = 独立迁移 spec，见 `docs/specs/h4-branding.md`）。

**品牌与视觉**：本次 vendor 保持上游原样（二进制自报 `reasonix`）；
Semantix 品牌换皮由已验证的 H4 patch 在 U39 套用（`git apply --directory=harness`）。
