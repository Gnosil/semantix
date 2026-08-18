# Semantix — 快速上手（Quickstart）

> 自进化 Agent Kernel：把 coding agent 的中间产物切片化、跨会话复用，
> 降低成本并让 kernel 逐步获得调配 harness 资源的能力。
> 架构细节见 `docs/reports/harness-refactor-blueprint.md` 与 `docs/Agent-Infra-架构设计.md`。

## 安装

### 方式一：GitHub Release（推荐）

**完整产品（v0.3.0+）**：`semantix-agent-v0.3.0-<platform>.tar.gz`——reasonix（coding-agent harness）+ semantix（记忆内核）+ 示例配置 + 安装脚本：

```bash
tar -xzf semantix-agent-v0.3.0-darwin-arm64.tar.gz
cd semantix-agent-v0.3.0-darwin-arm64
./semantix-install.sh v0.3.0   # 安装两个二进制 + 配置
reasonix --config reasonix.toml   # 启动完整 agent
```

**仅内核**：从 [Releases](https://github.com/Gnosil/semantix/releases) 下载对应平台二进制：

| 平台 | 文件 |
|---|---|
| macOS Intel | `semantix-v0.3.0-darwin-amd64` |
| macOS Apple Silicon | `semantix-v0.3.0-darwin-arm64` |
| Linux amd64/arm64 | `semantix-v0.3.0-linux-{amd64,arm64}` |
| Windows amd64/arm64 | `semantix-v0.3.0-windows-{amd64,arm64}.exe` |

```bash
chmod +x semantix-v0.3.0-darwin-arm64
sudo mv semantix-v0.3.0-darwin-arm64 /usr/local/bin/semantix
```

校验：`shasum -a 256 -c SHA256SUMS.txt`

### 方式二：源码构建

```bash
git clone https://github.com/Gnosil/semantix.git && cd semantix
go build -o semantix ./cmd/semantix   # 需要 Go 1.26+
```

## 30 秒体验

```bash
# 1. 从历史会话提取切片（Reasonix/Claude Code 风格 JSONL：每行 {role,content,tool_calls}）
semantix extract --input session.jsonl --db .semantix/project.db --project demo

# 2. 语义检索（三模式）
semantix search --query "修复 go 测试失败" --db .semantix/project.db
semantix search --query "修复 go 测试失败" --retriever vector
semantix search --query "修复 go 测试失败" --retriever hybrid

# 3. L2 注入块（会话 B 复用会话 A 的切片）
semantix inject --query "修复 go 测试失败" --db .semantix/project.db
# → [semantix-reuse] ... [/semantix-reuse]

# 4. 离线回放验证（M0-2：命中率 ≥70% 达标）
semantix verify --session <会话目录> --project demo > eval.tsv
# 逐行标 ✅/❌，命中率 = ✅/总行数
```

## 命令参考

命令树按四组组织，`semantix help` 按组列出全部命令；每个子命令
`semantix <command> --help` 查看其全部 flags。

**kernel 运维**

| 命令 | 用途 | 关键参数 |
|---|---|---|
| `extract` | 会话 JSONL → 语义切片入库 | `--input` `--db` `--scope` `--project` |
| `search` | 检索切片 | `--query` `--retriever bm25\|vector\|hybrid` `--limit` `--json` |
| `verify` | 离线回放命中率验证（门禁） | `--session` `--holdout` `--db` `--strict` |
| `eval` | 检索策略比较（单阈值 vs 三段） | `--set` `--train-frac` `--tau-*` |
| `eval-judge` | LLM judge 真实性评估（门禁） | `--stub` `--audit` `--min-consistency` |
| `usage` | 成本节省统计 | `--db` `--evolve-db` |
| `lookup` | semantix_lookup 工具（JSON） | `--query` `--limit` `--scope` |
| `inject` | L2 注入块（规范序/预算截断） | `--query` `--budget` `--k` |

**产品与管理**：`doctor` 健康检查（db / config / embedder / judge，任一 FAIL 退出码 3）、
`install` 一键安装、`completion` 生成 shell 补全脚本（bash / zsh / fish，加载方式见下文）、
`init` `config` `version` 已实现。

`semantix install` 按 `agent-skill/` 现有文档（SKILL.md + tools/ + hooks/ + config/ + scripts/）
落盘到目标 harness，幂等可重跑，`--uninstall` 精确移除安装的文件：

| 目标 | 默认落盘位置 | 说明 |
|---|---|---|
| `reasonix` | `~/.semantix/agent-skill/` | fork 已内置集成；落参考文档 + 打印 `[semantix] enabled=true` 配置步骤 |
| `claude-code` | `~/.claude/skills/semantix/` | Claude Code agent skills 目录，重启后生效 |
| `custom` | `--dir` 必填 | 任意目录；`--source`/`SEMANTIX_SKILL_DIR` 指定 agent-skill 源 |

```bash
semantix install --target claude-code          # 安装到 ~/.claude/skills/semantix/
semantix install --target custom --dir ./agent  # 自定义目录
semantix install --target claude-code --uninstall   # 卸载（仅移除 install 记录的文件）
```

**维护**：`export`（JSONL 备份）`import`（恢复）`gc`（评分 + 清理 + 上限淘汰）已实现。`gc` 默认重算价值权重并按 `store.max_slices`（默认 5000）归档超限切片到 `<db>.archive.jsonl`（`import` 可还原）；`--retention-days` / `--min-weight` / `--max-slices` / `--no-rescore` / `--no-archive` / `--dry-run`。

**服务模式**：`serve` `watch` 为规划中命令（执行会报 unknown command）。

**退出码契约**（所有命令统一）：

| 码 | 语义 |
|---|---|
| 0 | 成功 |
| 1 | 运行错误（IO、db、检索失败） |
| 2 | 用法错误（未知命令、flag 非法） |
| 3 | 门禁未达标（`verify --strict`、`eval-judge` 一致性） |

## Shell completion（bash / zsh / fish）

`semantix completion bash|zsh|fish` 输出对应 shell 的补全脚本，覆盖全部子命令、
主要 flag 及枚举值（`--scope`、`--retriever`、`--embedder`、`--stub`、
`--judge-protocol`）。加载方式：

```bash
# bash：追加到 ~/.bashrc
source <(semantix completion bash)

# zsh：追加到 ~/.zshrc（需已启用 compinit）
autoload -Uz compinit && compinit
source <(semantix completion zsh)

# fish：追加到 ~/.config/fish/config.fish
semantix completion fish | source
```

脚本由命令树注册表直接生成（与 `semantix help` 同一真源），新增命令或 flag
后重新执行一次即可刷新。

## 配置

复制 `semantix.example.toml` 为 `semantix.toml` 并按需修改（当前 CLI 参数优先）。

## 安全约定

- 切片库文件权限 `0600`、目录 `0700`（原子写 + 防 symlink）
- 所有输出经消毒（ANSI/C1 剥离、TSV 公式注入防护、注入块标记转义）
- 零第三方运行时依赖（单二进制）；检索默认 `hybrid`（BM25 + 哈希 embedding）

## 反馈

- 缺陷/建议：[Issues](https://github.com/Gnosil/semantix/issues)
- 架构与路线：`docs/reports/m0-gate.md`、`docs/reports/harness-refactor-blueprint.md`
