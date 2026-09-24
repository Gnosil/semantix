<div align="center">

# Semantix

### 给 AI 编程 agent 的跨会话记忆——和一份小得多的账单

**语义缓存 · 自适应调度 · 投机预取 · 跨会话学习**

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg?style=flat-square)](./LICENSE)
[![Version](https://img.shields.io/badge/release-0.7.3-blue?style=flat-square)](https://github.com/Gnosil/semantix/releases)
[![GitHub stars](https://img.shields.io/github/stars/Gnosil/semantix?style=flat-square&logo=github)](https://github.com/Gnosil/semantix/stargazers)
[![GitHub contributors](https://img.shields.io/github/contributors/Gnosil/semantix?style=flat-square&logo=github)](https://github.com/Gnosil/semantix/graphs/contributors)
[![Website](https://img.shields.io/badge/website-semantix.ensureok.ai-168b6d?style=flat-square)](https://semantix.ensureok.ai)

[English](./README.md) · **简体中文** · [快速上手](./docs/QUICKSTART.md) · [技术总览](./docs/TECHNICAL-OVERVIEW.zh-CN.md) · [官网](https://semantix.ensureok.ai)

</div>

<br/>

> **编程 agent 在会话结束时丢失全部上下文；厂商的前缀缓存又仅在提示词逐字节一致时命中——靠近开头的一处改动，即令其后内容全部失效。**
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

固定版本 / 架构：`... | sh -s -- v0.7.2 arm64`。其他安装方式与完整命令参考见 [docs/QUICKSTART.md](./docs/QUICKSTART.md)。

## 两种形态

**`semantix-agent` —— 完整 agent。** 一个 CLI 编程 agent，记忆内核随包内置：提取、检索、注入与自进化闭环在启动时已接好，内置约 50 家端点的 provider 预设。

**内核 —— 挂载至现有 agent。** 设计上与 harness 解耦，集成面只是一小组 hook（工具注册、消息拦截、会话导出 / 事件旁路）。三种接法：

| 接入方式 | 集成成本 | 适用对象 |
| --- | --- | --- |
| **Agent skill** | `semantix install --target claude-code` | Claude Code |
| **工具注册** | 两个工具 schema（`semantix_lookup` / `semantix_inject`） | 自研 / 私有 agent |
| **网关** | 一个 base URL，无需改动代码 | 任何 OpenAI 兼容客户端 |

全程 fail-open：内核异常时，agent 回退至常规执行路径。

## 跨会话记忆

同一项目的构建约定、测试布局、服务启动参数，在每个新会话中都需重新建立。Semantix 从已结束的会话中提取可复用**切片**——任务模式、项目知识、工具序列、已验证结果——存入本地评分库，并在遇到相似任务时重新注入。每条命中均标注其检索 zone（🟢 hit · 🟡 grey · ⚪ miss）与来源会话——真实的 `semantix search` / `dashboard` / `verify` 输出见[复用可视化实录](./docs/TECHNICAL-OVERVIEW.zh-CN.md#复用可视化)。

命中、未命中与人工纠正均回流至切片评分与检索阈值，库的精度随使用提升，而非仅随体量增长；按类型分化的淘汰策略优先清除过期结果，保留项目知识。

## 缓存命中与成本

厂商的前缀缓存为**字节精确匹配**：仅当本次提示词与上次逐字节一致时命中，靠近开头的单个字符差异即令其后内容全部失效。

该失效模式的影响普遍被低估。Claude Code 在 system 提示词头部写入逐请求变化的计费标记；一方端点在服务端剥离该标记，第三方端点不剥离，因而在第三方端点上，该行导致缓存自首个 token 起即失效。前缀清洁中间件所依据的规格，将命中率差异归因于这一个 header，记录为 **133 倍**；关闭剥离后缓存开销升至 **4–5 倍**。`semantix-gateway` 默认执行剥离。

设计目标是使厂商缓存可命中，而非绕开它：

- **字节稳定注入** —— 检索结果按 ID 排序而非按分数排序，使语义相近的请求生成逐字节一致的前缀
- **前缀清洁** —— 剥离逐请求变化的归属标记，并将工具数组按名称规范化排序，消除客户端枚举顺序对前缀的干扰
- **厂商感知** —— 厂商能力表、按厂商区分的缓存有效期（DeepSeek 落盘 24 小时，Anthropic 5 分钟 ephemeral 窗口）、预算感知的 `cache_control` 断点放置、按模型价格表
- **L3 结果复用** —— 已验证结果直接返回，不产生模型调用，fail-closed

### 逐厂商适配

缓存行为取决于承载模型的服务栈，而非模型本身；其差异幅度足以要求逐栈适配。

| 端点 | 稳定轮次的 prompt 缓存命中 | 数字来源 |
| --- | --- | --- |
| DeepSeek | **99.8%** | 厂商回报的 cache token 计数 |
| GLM | **97.5%** | 一周专项实测；遥测上报天花板约 97.6% |

两者均为**单轮 prompt token 命中率**——`命中 / (命中 + 未命中)`，取自厂商返回的 cache token 计数，非估算值。`semantix-agent` 在状态栏同时显示当前轮次命中率与会话累计均值，该数值可在任意负载上直接核验。

为期一周的 GLM 专项实测显示：同一模型置于不同托管栈之后，缓存寿命相差数倍——某栈前缀保持 1–8 分钟，真实过期落在 `(8, 12]` 分钟；另一栈 120 秒内维持 96–98%，至 301 秒降至 28%。这是 TTL 表按厂商区分而非采用单一全局值的原因，也解释了 GLM 命中率遥测约 **97.6%** 的上报天花板——尾部不足一块的部分不计入 cached。

方法与原始数据：[docs/reports/glm-spike-week.md](./docs/reports/glm-spike-week.md) · [docs/reports/glm-p0-1-prefix-audit.md](./docs/reports/glm-p0-1-prefix-audit.md)。

缓存之外，调度器学习工具使用模式，将可并行的调用并行化，并在模型等待期预取只读上下文。

### 实测

- 合成回放对照实验中节省 **79.8%** 成本（[docs/reports/m0-cost-comparison.md](./docs/reports/m0-cost-comparison.md)）
- 4 个会话的演示库 **80% 缓存命中率（L3/L2）**——一屏 `semantix dashboard` 仪表盘的真实输出见[复用可视化实录](./docs/TECHNICAL-OVERVIEW.zh-CN.md#复用可视化)
- `semantix verify` 回放门禁要求相关性 **≥ 70%**；用真实用户会话验证命中率是当前的 v1.0 门禁 —— [#58](https://github.com/Gnosil/semantix/issues/58)

**以上是回放 / 演示测量，不是生产基准。** 完整证据链和全部技术细节见 [docs/TECHNICAL-OVERVIEW.zh-CN.md](./docs/TECHNICAL-OVERVIEW.zh-CN.md)。

## 30 秒体验

已用上方一行命令装好？从历史会话提取切片并复用 —— 这就是记忆内核在工作：

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search  --query "修复 go 测试失败" --db .semantix/project.db
semantix inject  --query "修复 go 测试失败" --db .semantix/project.db   # L2 注入块
semantix verify  --session <会话目录> --project demo                    # 回放门禁（≥70%）
semantix dashboard                                                      # 一屏复用仪表盘
```

想改用源码构建（Go 1.26+）：`go build -o semantix ./cmd/semantix && go build -o semantix-agent ./cmd/semantix-agent`。完整命令参考与配置见 [docs/QUICKSTART.md](./docs/QUICKSTART.md)。

## 集成

| Agent / 助手 | 接入路径 | 状态 |
| --- | --- | --- |
| Semantix Agent | 内置捆绑：`[semantix] enabled=true` + `semantix_lookup` 工具 | ✅ v0.3.0 起随包发布 |
| Claude Code | 工具注册（`semantix_lookup` / `semantix_inject` schema） | ✅ 已文档化（`agent-skill/tools/`） |
| LangChain 应用 | 两个 hook 的中间件（消息改写 + 会话提取） | ✅ 已文档化（[报告](./docs/reports/langchain-middleware.md)） |
| 自研 / 私有 agent | 会话绕行：export / 事件旁路 / 直接调用 | ✅ 已文档化（`agent-skill/hooks/session-bypass.md`） |
| 任何 OpenAI 兼容客户端 | 前面架 `semantix-gateway`，改 base URL | ✅ 已随包发布 |

Cursor、Windsurf、Codex CLI、Gemini CLI、Copilot agent 模式、Cline / Continue / Aider 均可经上述某一路径接入内核，**无一需要修改内核**。优先级依据实际使用情况确定；具体集成需求以 [issue](https://github.com/Gnosil/semantix/issues) 跟踪（模板：`.github/ISSUE_TEMPLATE/integration_request.yml`）。

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
