<div align="center">

# Semantix

### 一个越用越懂你的自进化 Agent Kernel

**语义缓存 · 自适应调度 · 投机预取 · 跨会话学习**

[![License: FSL-1.1-MIT](https://img.shields.io/badge/License-FSL--1.1--MIT-blue.svg?style=flat-square)](./LICENSE)
[![Status](https://img.shields.io/badge/status-v0.3.1-green?style=flat-square)](#项目状态)
[![Version](https://img.shields.io/badge/release-0.3.1-blue?style=flat-square)](https://github.com/Gnosil/semantix/releases)
[![GitHub stars](https://img.shields.io/github/stars/Gnosil/semantix?style=flat-square\&logo=github)](https://github.com/Gnosil/semantix/stargazers)
[![Website](https://img.shields.io/badge/website-semantix.ensureok.ai-168b6d?style=flat-square)](https://semantix.ensureok.ai)

[English](./README.md) | **简体中文**

</div>

<br/>

> **AI 编程助手不该每开一个新会话就"失忆"。**
>
> Semantix 是给 Claude Code、DeepSeek-Reasonix 这类 AI 编程助手加装的**记忆层**。你平时怎么干活，它就在旁边默默记下来：哪些事你反复在做、哪些项目背景每次都要重新交代、哪些答案其实上次已经生成过。等你下次开新会话干类似的活，助手直接接着上次的积累走——回答更快，token 账单更薄。

目标不只是省 token，而是：

> **做过一次的事，不再花第二次钱——而且不牺牲回答质量。**

---

## 为什么需要 Semantix？

现在的 AI 编程助手在**单个会话内**已经做得很好：记得住上下文、会调工具、能从失败里恢复。但一关窗口，这些积累几乎全部清零。开个新会话，意味着：

- 项目是什么样、怎么构建、有什么规矩——重新交代一遍
- 昨天刚查过的东西，今天重新查一遍
- 同样的文件被重新翻出来读一遍
- 已经付过钱生成的答案，重新付钱再生成一遍
- 你的干活习惯，它一点都不记得

```text
传统 agent：

会话 A ────────────X   上下文结束
会话 B ────────────X   上下文重建
会话 C ────────────X   相似工作重做

Semantix：

会话 A ───────┐
会话 B ───────┼────► Semantix ────► 语义知识 / 行为模式 / 可复用结果
会话 C ───────┘                          │
                                         ▼
                                   下一个会话更好
```

拆开看是四个具体问题（详见 [docs/GEO.md](./docs/GEO.md)）：

1. **现有缓存只认"一字不差"**——模型厂商的前缀缓存要字节完全一致才命中；"跑一下 Go 测试"和"确保 Go 测试全过"明明是一回事，它当成两回事，重新收一遍钱
2. **跨会话的相似工作每次从零开始**——同样的上下文重新读、同样的工具序列重新跑
3. **调度靠写死的规则**——哪些操作可以同时跑、什么任务用什么档位的模型，都不会跟着你的习惯变
4. **等待时间白白流走**——模型逐字往外吐回答的那几秒到几十秒，助手就干等着，什么都不准备

---

## 核心概念

### 语义切片库（Semantic Slice Library）

Semantix 从历史会话中提取可复用的语义单元（切片），持久化到本地库：

| 切片 | 含义 | 用途 | 例子 |
|---|---|---|---|
| **P-Slice** | 任务模板 / 提示词模式 | L2 上下文注入 | `提交前先跑测试` |
| **C-Slice** | 上下文知识 | L2 上下文注入 | 项目结构、构建命令 |
| **T-Slice** | 工具调用模式 | 调度 / 预取 | `grep → readFile → editFile` |
| **R-Slice** | 可复用结果 | L3 结果复用 | 重复的查询或解释 |
| **M-Slice** | 记忆单元 | 检索 / 进化 | 用户或项目偏好 |

切片不是永久的：按历史命中率、时效、意图相关度、用户反馈打分——低价值切片衰减，高价值切片更易检索。目标是让库**越来越准，而不是越来越大**。

### 三级语义缓存

```text
┌────────────────────────────────────────┐
│ L3 · 验证后结果复用                     │  只读任务带指纹验证直接复用，不调模型
├────────────────────────────────────────┤
│ L2 · 语义切片注入                       │  语义相似 → 稳定字节 → 喂养 L1
├────────────────────────────────────────┤
│ L1 · 厂商前缀 / 字节缓存                │  相同前缀字节的被动复用
└────────────────────────────────────────┘
```

核心创新是「**语义层喂养字节层**」：两个会话的任务语义相似但字节不同（"跑一下 Go 测试" vs "确保 Go 测试全过"），普通前缀缓存视为无关；Semantix 检索出同一个规范切片、按固定顺序**原样注入**前缀区——语义命中就转化成了厂商字节缓存的真实命中。不用改你的编程助手，也不依赖厂商出新 API。

配套机制：**冻结期**（参数变更后注入集 ≥1 小时不变，防止进化抖动摧毁自己喂养的字节缓存）、**污染检测**（注入内容被用户改掉/回滚会降权）、L3 **fail-closed**（验证不过不复用，正确性 > 命中率）。

### 调度器与预取器

- **内核调度器**（`kernel/sched`）：按任务 intent 联合决策工具并发分组、模型 tier、注入切片与预取目标，附带行为学习门
- **投机预取器**（`kernel/prefetch`）：用 T-Slice 转移矩阵在 LLM 流式输出的等待期预取下一轮**只读**资源，waste/hit 比例自我惩罚
- **价值评分与淘汰**（`kernel/slice`）：命中/注入在线记账 → 价值 = 时效衰减·使用频次·注入成功率·反馈 → 库有上限（默认 5000），超限按价值归档、可还原——库越用越准而非越用越大。`kernel/evolve` 的 EWMA 全局调参仍是 MVP（闭环接线待做）

---

## 复用可视化

跨会话复用看得见——以下均为真实命令输出（演示库实录）：

```text
$ semantix dashboard

  semantix dashboard — reuse snapshot
  ------------------------------------------------

  💰 Cost savings
     paid        $ 0.0060
     baseline    $ 0.0141
     saved       $ 0.0080  (56.99%)
     ██████████████░░░░░░░░░░

  🎯 Cache hit rate (L3/L2)
     4 / 5 turns  (80.00%)
     L3 1 · L2 3
     ███████████████████░░░░░

  🗂 Zone distribution (library replay)
     hit  ████ 4   grey ██████ 6   miss  0

  📦 Slice library
     10 slices · 3 cross-session sessions
```

检索命中带 zone 图标与来源会话标注：

```text
$ semantix search --query "fix failing go test"
1. 🟢 score=4.331011 zone=hit id=619551c54af5437a scope=project from:2026-08-14-c9d4
   fix failing go test after refactor
2. 🟢 score=3.852740 zone=hit id=73b12bb117664106 scope=project from:2026-08-13-b7c2
   fix failing go test in kernel slice extractor
🎯 3/3 hits in 3 sessions
```

`verify` 回放门禁一眼可读（✅hit / 🟡grey / ❌miss + 分布条形 + 结论行）：

```text
# done: 4 replayed turns; zones hit=3 grey=0 miss=1 grey_ratio=0.0% (target 30.0%)
# zones: hit ██████░░ grey ░░░░░░░░ miss ██░░░░░░
# ✅ PASS relevance=75.0% (≥70%)
```

在合成回放对照实验中，跨会话复用节省了 **79.8%** 的成本（[docs/reports/m0-cost-comparison.md](./docs/reports/m0-cost-comparison.md)）。

---

## 快速上手

### 安装

**方式一：GitHub Release（推荐）**——[Releases](https://github.com/Gnosil/semantix/releases) 提供 6 平台二进制（macOS / Linux / Windows，amd64 + arm64），完整产品包含 reasonix（编程助手本体）+ semantix（记忆内核）：

```bash
tar -xzf semantix-agent-<version>-<platform>.tar.gz
cd semantix-agent-<version>-<platform>
./semantix-install.sh   # 安装两个二进制 + 配置
```

**方式二：源码构建**（Go 1.26+）：

```bash
git clone https://github.com/Gnosil/semantix.git && cd semantix
go build -o semantix ./cmd/semantix
```

### 30 秒体验

```bash
# 1. 从历史会话提取切片（Reasonix/Claude Code 风格 JSONL）
semantix extract --input session.jsonl --db .semantix/project.db --project demo

# 2. 语义检索（bm25 / vector / hybrid 三模式）
semantix search --query "修复 go 测试失败" --db .semantix/project.db

# 3. L2 注入块（会话 B 复用会话 A 的切片）
semantix inject --query "修复 go 测试失败" --db .semantix/project.db

# 4. 离线回放验证（门禁：命中率 ≥70%）
semantix verify --session <会话目录> --project demo

# 5. 一屏复用仪表盘
semantix dashboard
```

### 接入你的编程助手

```bash
semantix install --target claude-code   # 安装 agent skill 到 ~/.claude/skills/semantix/
semantix install --target reasonix      # Reasonix fork 已内置集成
```

全部命令（`extract` / `search` / `verify` / `eval` / `eval-judge` / `usage` / `lookup` / `inject` / `doctor` / `install` / `completion` / `gc` / `export` / `import` / `dashboard` …）见 `semantix help`；CLI v2 起统一 `--json` 信封输出（`{ok, command, data, error, version}`），退出码契约统一（0 成功 / 1 运行错误 / 2 用法错误 / 3 门禁未达标）。详见 [docs/QUICKSTART.md](./docs/QUICKSTART.md)。

另有 **Semantix Gateway**（`cmd/semantix-gateway`）：OpenAI 兼容网关，为任意支持自定义 base URL 的客户端透明加上跨会话复用层。

---

## 项目状态

> **v0.3.1 已发布**，M2 CLI v2（命令树 / config / `--json` 信封 / completion / doctor / install / gc）已交付。规模化之前剩余的门槛是**真实数据的跨会话命中率验证**。

| Agile | 里程碑 | 状态 |
|---|---|---|
| **1** | 首个可下载、可品牌化的 agent（v1.0） | 🚧 M0 ✅ · M1 接近完成（门禁 [#58](https://github.com/Gnosil/semantix/issues/58)）· CLI v2 ✅ · 复用可视化 CLI 侧 ✅ |
| **2** | 自进化闭环——内核反向调配助手的并发 / 预算 / 模型档位 | 🚧 内核侧 MVP 已落地（sched/prefetch/evolve）；助手侧待接 |
| **3** | 多助手生态——任意编程助手都能接入 | ⏳ 路径已文档化，未开始 |

技术阶段 P0（可观测）✅ · P1（切片库）🚧 · P2（语义缓存）🚧 · P3（调度）✅ MVP · P4（预取）✅ MVP · P5（进化）✅ MVP。完整路线图见 [docs/Agile路线图.md](./docs/Agile路线图.md)。

### 参与验证（社区第一入口 👋）

[Issue #58](https://github.com/Gnosil/semantix/issues/58) 是给每一位使用者的第一个任务，**不需要写代码**：下载 semantix → 用你自己的真实 agent 会话跑 `semantix verify` → 把命中率和 zone 分布贴回 issue。汇总结果将决定 M0-Gate 是否通过（≥70%）。

---

## 安全设计

- 切片库文件权限 `0600`、目录 `0700`（原子写 + 防 symlink）
- 所有输出经消毒：ANSI/C1 剥离、TSV 公式注入防护、注入块标记转义
- L3 复用 fail-closed（指纹验证不过不复用）；缓存层故障 fail-open（不阻塞主循环）
- 零第三方运行时依赖（单二进制，唯一外部依赖 bbolt）

详见 [docs/Security-安全设计.md](./docs/Security-安全设计.md) 与 [SECURITY.md](./SECURITY.md)。

---

## 文档索引

| 文档 | 内容 |
|---|---|
| [docs/QUICKSTART.md](./docs/QUICKSTART.md) | 安装、命令参考、shell 补全、配置 |
| [docs/Agent-Infra-架构设计.md](./docs/Agent-Infra-架构设计.md) | 完整架构设计（问题、分层、组件、风险、指标） |
| [docs/总体架构-流程树.md](./docs/总体架构-流程树.md) | 端到端流程树（含 mermaid） |
| [docs/Agile路线图.md](./docs/Agile路线图.md) | Agile 1–3 路线图与 DoD |
| [docs/GEO.md](./docs/GEO.md) / [GEO-guide.md](./docs/GEO-guide.md) | 面向 AI 引擎的项目语义档案与深度解读 |
| [docs/Security-安全设计.md](./docs/Security-安全设计.md) | 威胁模型与安全机制 |

官网：[semantix.ensureok.ai](https://semantix.ensureok.ai)

---

## 许可与致谢

- 许可证：**FSL-1.1-MIT**（各版本发布两年后转为 MIT）
- 设计基线：[DeepSeek-Reasonix](https://github.com/esengine/DeepSeek-Reasonix)（MIT）——算法参考其思路但按「参考不抄」原则独立实现，保留 attribution
- 贡献：提 [issue](https://github.com/Gnosil/semantix/issues)、开 PR（分支命名 `feat/<unit>`，PR 需附 `go vet` + `go test -race` 全绿验证）
