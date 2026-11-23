<div align="center">

# Semantix

### 面向 AI 编程 agent 的跨会话记忆与工作复用

**语义缓存 · 自适应调度 · 投机预取 · 跨会话学习**

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg?style=flat-square)](./LICENSE)
[![Version](https://img.shields.io/badge/release-0.7.3-blue?style=flat-square)](https://github.com/Gnosil/semantix/releases)
[![GitHub stars](https://img.shields.io/github/stars/Gnosil/semantix?style=flat-square&logo=github)](https://github.com/Gnosil/semantix/stargazers)
[![GitHub contributors](https://img.shields.io/github/contributors/Gnosil/semantix?style=flat-square&logo=github)](https://github.com/Gnosil/semantix/graphs/contributors)
[![Website](https://img.shields.io/badge/website-semantix.ensureok.ai-168b6d?style=flat-square)](https://semantix.ensureok.ai)

[English](./README.md) · **简体中文** · [快速上手](./docs/QUICKSTART.md) · [技术总览](./docs/TECHNICAL-OVERVIEW.zh-CN.md) · [官网](https://semantix.ensureok.ai)

</div>

<br/>

> **新的编程会话经常重复项目探索和工具操作。厂商前缀缓存能复用匹配前缀的计算，但不会判断上一会话的经验现在是否相关、是否仍然有效。**
>
> Semantix 同时处理这两个问题：既是内置记忆内核的完整编程 agent，也是可挂载至现有 agent 的独立内核。

## 快速开始

**安装** —— 一行命令，macOS / Linux（arm64 / amd64）：

```bash
curl -fsSL https://raw.githubusercontent.com/Gnosil/semantix/main/agent-skill/scripts/install.sh | sh
```

装好 `semantix` + `semantix-agent` 到 `~/.local/bin`，默认开启跨会话记忆，并在该目录不在 `PATH` 时自动写入你的 shell rc。**使用** —— 在任意项目里启动 agent，当前文件夹即工作区：

```bash
cd ~/你的项目
semantix                 # 裸命令 → 在当前目录启动编程 agent（首次运行引导配置 provider / API key）
semantix search "..."    # 任意子命令 → 记忆内核（search / extract / inject / verify / usage）
```

固定版本 / 架构：`... | sh -s -- v0.7.3 arm64`。其他安装方式与完整命令参考见 [docs/QUICKSTART.md](./docs/QUICKSTART.md)。

## 两种形态

**`semantix-agent` —— 完整 agent。** 一个 CLI 编程 agent，记忆内核随包内置：提取、检索、注入与自进化闭环在启动时已接好，内置约 50 家端点的 provider 预设。

**内核 —— 挂载至现有 agent。** 设计上与 harness 解耦，集成面只是一小组 hook（工具注册、消息拦截、会话导出 / 事件旁路）。三种接法：

| 接入方式 | 集成成本 | 适用对象 |
| --- | --- | --- |
| **Agent skill** | `semantix install --target claude-code` | Claude Code |
| **工具注册** | 两个工具 schema（`semantix_lookup` / `semantix_inject`） | 自研 / 私有 agent |
| **网关** | 配置上游，并将客户端 base URL 指向网关 | OpenAI Chat Completions 兼容客户端 |

记忆集成采用 fail-open 设计：内核异常时，agent 回退至常规执行路径。直接复用结果采用 fail-closed：验证失败就拒绝缓存结果。

## 跨会话记忆

同一项目的构建约定、测试布局、服务启动参数，在新会话中经常需要重新建立。Semantix 从已结束的会话中提取可复用**切片**——任务模式、项目知识、工具序列与结果——存入本地评分库，并在遇到相似任务时注入相关切片。每条命中均标注其检索 zone（🟢 hit · 🟡 grey · ⚪ miss）与来源会话——真实的 `semantix search` / `dashboard` / `verify` 输出见[复用可视化实录](./docs/TECHNICAL-OVERVIEW.zh-CN.md#复用可视化)。

使用与反馈信号可用于更新切片评分和检索阈值；按类型分化的淘汰策略对可复用项目知识与短期结果采用不同处理。这些机制支持持续调优，精度是否提升仍需在目标负载上评测。

## 内核复用什么

Semantix 将跨会话记忆连接到三个层次的复用：

| 层级 | 当前机制 | 可减少的开销 |
| --- | --- | --- |
| **L1 · 厂商前缀缓存** | 网关前缀清洁与按厂商处理的缓存策略，帮助保留匹配前缀 | 重复的提示词计算，实际收益取决于上游缓存策略 |
| **L2 · 上下文复用** | 检索相关切片，在预算内组装带来源信息的上下文块 | 重复的项目探索与指令说明 |
| **L3 · 结果复用** | 相关性、隔离与有效性检查通过后，返回历史结果 | 当前的主要答案生成调用；灰区判定仍可能调用模型 |

这一方案的三个实现重点：

- **检查代码相关结果是否仍有效。** L3 通过修改时间与 SHA-256 指纹检查已记录的依赖文件；网关还执行会话上下文与模型隔离，并应用时效策略。无依赖记录的结果需要显式允许复用，网关入库默认关闭该选项。检查范围仅覆盖已记录依赖，外部状态和遗漏依赖仍需重新核验。见 [L3 决策实现](./kernel/cache/l3.go)。
- **将检索与复用准入分开。** Hit / Grey / Miss 分区指导准入：L2 默认丢弃灰区候选，可选审计模式；L3 的灰区候选必须通过 judge 判定，没有 judge 就拒绝复用。judge 批准后，仍需通过隔离与依赖检查。见 [L2 准入实现](./kernel/inject/inject.go)与[检索分区](./kernel/zone/zone.go)。
- **行为历史也参与优化。** 任务模式、项目上下文、工具序列、结果和记忆单元各有用途。在随包 agent 中，工具模式学习用于调度和投机只读预取，并接收执行结果反馈。这部分依赖 harness 集成；仅将客户端指向网关，不会接入工具调度器。见[内核桥接实现](./harness/semantix/bridge.go)。

默认检索路径为本地 BM25，无需向量数据库或 embedding 服务。CLI search 与网关也支持向量及混合检索；默认向量后端使用确定性的 token 哈希 embedding，模型 embedding 则需显式配置，调用 OpenAI 兼容 embedding 端点。网关配置模型后端时使用 HNSW 索引。哈希 embedding 的语义能力与训练得到的 embedding 模型不同。见[检索器构建](./gateway/retriever.go)与[配置参考](./docs/QUICKSTART.md)。

## 缓存命中与成本

厂商前缀缓存复用匹配的提示词前缀，token / 分块粒度、最小长度与有效期取决于服务栈。靠近开头的改动可能使后续大部分前缀无法复用。

`semantix-gateway` 默认清理第一条 system 消息头部已识别的归属 / 计费标记行，并按工具名称排序，在转发前减少两类前缀变化来源。实现与测试见[前缀清洁中间件](./gateway/sanitize.go)及[测试](./gateway/sanitize_test.go)，特定负载的观察记录见[前缀审计报告](./docs/reports/glm-p0-1-prefix-audit.md)。

设计目标是使厂商缓存可命中，而非绕开它：

- **确定性上下文组装** —— 通过准入的切片在各分区内按相关性分数排序，同分时按 ID 稳定排序。查询、分数或元数据变化仍可能改变注入字节，语义相近本身不保证前缀缓存命中
- **前缀清洁** —— 剥离逐请求变化的归属标记，并将工具数组按名称规范化排序，消除客户端枚举顺序对前缀的干扰
- **厂商感知** —— 厂商能力表、缓存控制策略与按模型价格表。网关可配置的逐厂商 L3 时效窗口属于本地复用策略，不控制上游前缀缓存寿命
- **L3 结果复用** —— 符合条件的结果直接返回，省去主要生成调用，fail-closed；可选灰区 judge 判定另有调用成本

### 逐厂商适配

缓存行为取决于承载模型的服务栈，而非模型本身；其差异幅度足以要求逐栈适配。

| 端点 | 稳定轮次的 prompt 缓存命中 | 数字来源 |
| --- | --- | --- |
| DeepSeek | **99.8%** | 厂商回报的 cache token 计数 |
| GLM（被测托管栈） | **97.5%** | 一周专项实测；观察到的遥测上报天花板约 97.6% |

两者均为观察到的**单轮 prompt token 命中率**——`命中 / (命中 + 未命中)`，取自厂商返回的 cache token 计数。它们是特定负载下的观察值，不代表保证命中率或记忆相关性。厂商上报缓存用量时，`semantix-agent` 在状态栏同时显示当前轮次命中率与会话累计均值。

为期一周的 GLM 专项实测显示：同一模型置于不同托管栈之后，缓存寿命相差数倍——某栈前缀保持 1–8 分钟，真实过期落在 `(8, 12]` 分钟；另一栈 120 秒内维持 96–98%，至 301 秒降至 28%。这些测量支持按服务栈分别标定。报告还记录了被测栈约 **97.6%** 的遥测上报天花板：尾部不足一块的部分未计入 cached。

方法与原始数据：[docs/reports/glm-spike-week.md](./docs/reports/glm-spike-week.md) · [docs/reports/glm-p0-1-prefix-audit.md](./docs/reports/glm-p0-1-prefix-audit.md)。

缓存之外，调度器学习工具使用模式，将可并行的调用并行化，并在模型等待期预取只读上下文。

### 证据与评测边界

- 合成演示的成本模型估算节省 **79.8%**，前提是可省去 80% 的重复工具步骤，并采用每步 token 估算值。这是成本模型示例，尚非生产账单降幅实测，假设见[报告](./docs/reports/m0-cost-comparison.md)
- 4 个会话的演示库 **80% 缓存命中率（L3/L2）**——一屏 `semantix dashboard` 仪表盘的真实输出见[复用可视化实录](./docs/TECHNICAL-OVERVIEW.zh-CN.md#复用可视化)
- `semantix verify` 默认回放判定使用**自动分类的 Hit 占比 ≥ 70%**。输出虽标为 `relevance`，该统计并非人工标注精确率；可选 `--labels` 校准支持基于标注的评测。真实会话相关性验证见 [#58](https://github.com/Gnosil/semantix/issues/58)

**以上是回放 / 演示测量，不是生产基准。** 完整证据链和全部技术细节见 [docs/TECHNICAL-OVERVIEW.zh-CN.md](./docs/TECHNICAL-OVERVIEW.zh-CN.md)。

## 30 秒体验

已用上方一行命令装好？从历史会话提取切片并复用 —— 这就是记忆内核在工作：

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search  --query "修复 go 测试失败" --db .semantix/project.db
semantix inject  --query "修复 go 测试失败" --db .semantix/project.db   # L2 注入块
semantix verify  --session <会话目录> --project demo                    # 回放判定（自动 Hit 占比 ≥70%）
semantix dashboard                                                      # 一屏复用仪表盘
```

想改用源码构建（Go 1.26+）：`go build -o semantix ./cmd/semantix && go build -o semantix-agent ./cmd/semantix-agent`。完整命令参考与配置见 [docs/QUICKSTART.md](./docs/QUICKSTART.md)。

## 集成

| Agent / 助手 | 接入路径 | 状态 |
| --- | --- | --- |
| Semantix Agent | 内置捆绑：`[semantix] enabled=true` + `semantix_lookup` 工具 | ✅ v0.3.0 起随包发布 |
| Claude Code | 工具注册（`semantix_lookup` / `semantix_inject` schema） | ✅ 已文档化（`agent-skill/tools/`） |
| Codex CLI | 可选的本地市场插件，通过 Hooks 读写项目记忆 | [安装与排查](./plugins/semantix-memory/README.zh-CN.md) |
| DeepSeek Harness（DSH） | 本地 Host 插件，自动召回和写入项目记忆 | 已验证 [DSH headless](./plugins/semantix-dsh-memory/README.md) |
| LangChain 应用 | 两个 hook 的中间件（消息改写 + 会话提取） | ✅ 已文档化（[报告](./docs/reports/langchain-middleware.md)） |
| 自研 / 私有 agent | 会话绕行：export / 事件旁路 / 直接调用 | ✅ 已文档化（`agent-skill/hooks/session-bypass.md`） |
| OpenAI Chat Completions 兼容客户端 | 前面架 `semantix-gateway`，配置上游并改 base URL | ✅ 已随包发布 |

其他 agent 可通过工具注册、受支持的网关请求或会话导出接入。兼容性取决于客户端协议和可用 hook；上述路径不意味着每个客户端都有经过测试的适配器，或都能获得随包 agent 的完整执行集成。具体集成需求以 [issue](https://github.com/Gnosil/semantix/issues) 跟踪（模板：`.github/ISSUE_TEMPLATE/integration_request.yml`）。

## 文档

| 文档 | 内容 |
| --- | --- |
| [ROADMAP.md](./ROADMAP.md) | 已发布 / 进行中 / 下一步（英文摘要，中文版为唯一事实来源） |
| [docs/TECHNICAL-OVERVIEW.zh-CN.md](./docs/TECHNICAL-OVERVIEW.zh-CN.md) | **从这里开始** —— 核心概念、三级缓存、调度与预取、复用可视化、模块地图、项目状态、安全设计 |
| [docs/QUICKSTART.md](./docs/QUICKSTART.md) | 安装、命令参考、shell 补全、配置 |
| [docs/Agent-Infra-架构设计.md](./docs/Agent-Infra-架构设计.md) | 完整架构设计（问题、分层、组件、风险、指标） |
| [docs/总体架构-流程树.md](./docs/总体架构-流程树.md) | 端到端流程树（含 mermaid） |
| [docs/Agile路线图.md](./docs/Agile路线图.md) | Agile 1–3 路线图与 DoD |
| [docs/Security-安全设计.md](./docs/Security-安全设计.md) · [SECURITY.md](./SECURITY.md) | 威胁模型与安全机制 |
| [agent-skill/SKILL.md](./agent-skill/SKILL.md) | 面向任意 harness 的自助集成 |
| [semantix.ensureok.ai](https://semantix.ensureok.ai) | 官网、产品文档与博客 |

## 社区

- 💬 [GitHub Discussions](https://github.com/Gnosil/semantix/discussions) — 问答、想法、展示
- 🐛 [Issue 列表](https://github.com/Gnosil/semantix/issues) — 认领 `good first issue` 入门
- 📖 [博客](https://semantix.ensureok.ai/blog) — 语义缓存与 agent 记忆层系列文章

## 参与贡献

Semantix 处于早期阶段。语义缓存、检索、调度、投机执行、评测方法、harness 适配器等方向均欢迎贡献，工作流见 [CONTRIBUTING.md](./CONTRIBUTING.md)（从 `main` 切分支，PR 需 `go vet` 与 `go test -race` 全绿）。中英文 issue / PR 均可。

**无需写代码的参与方式：** 以自有会话运行 `semantix verify`，将命中率结果提交至 [#58](https://github.com/Gnosil/semantix/issues/58)。社区汇总结果决定 v1.0 门禁是否通过。

架构假设均可质疑，验证它们本身即是这项工作的一部分。

## 许可与致谢

[MIT](./LICENSE)。[DeepSeek-Reasonix](https://github.com/esengine/DeepSeek-Reasonix)（MIT）。

---

<div align="center">

### Semantix

**每一次交互，都让下一次更便宜、更快、更聪明。**

</div>
