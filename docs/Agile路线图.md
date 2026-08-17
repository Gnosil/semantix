# Agile 路线图（Agile 1-3）

> 日期：2026-08-14 · 状态：规划（v1） · 唯一事实来源：本文件 + `docs/reports/harness-refactor-blueprint.md`
> 目标：把散落在 M0 任务单元化 / m0-gate / harness-refactor-blueprint 三处的执行规划，收拢为 **3 个 Agile** 的统一路线图：每个 Agile 的目标、范围、验收、当前状态、完成定义（DoD）。

---

## 0. 术语与编号体系

- **Agile** = 一次可交付产品的迭代周期（团队节奏单位），每个 Agile 有明确的 DoD。
- **P0-P5（README）** = 产品功能阶段（设计视角）；**Agile** = 执行节奏。映射见 §6。
- **U 编号**（GitHub issue 前缀，全仓库唯一）：
  - **M0 · U1-U10**（已全部完成）：kernel 骨架/切片/BM25/fork 适配/Gate
  - **M1 · U11-U18**（U11-U18 已实现，验收见各 issue）：真实数据/embedding/fork 闭环/evolve/L3/成本仪表。m0-gate 建议清单里的 U19/U20（灰色地带、LLM judge）已作为 M1 一部分实现，**编号作废不再使用**
  - **M2 · U19-U27**（2026-08-14 起，issue #113-#121）：CLI v2 产品化（命令树/config/--json/completion/doctor/install/gc/serve）——✅ 全部完成（含补强 U28-U36）
  - **GW 编号**（网关线，先例 #133 M2-GW1）：Semantix Gateway（New API 套壳）独立编号，不占 U 序列
  - **Agile 2 · U37-U43**（2026-08-17 起）：H2/H3/H5 资源编排批次，契约 spec 见 `docs/specs/h2h3-resource-orchestration.md`

---

## 1. Agile 1 — 首个可下载 agent（品牌化产品）

> 目标（一页化）：**reasonix+semantix 完整 bundle 可下载、安装即用；跨会话复用可被用户感知（命中数/节省成本可视化）**。

### 范围与阶段（蓝图 H1-H4）

| 阶段 | 内容 | 状态 |
|---|---|---|
| M0（U1-U10） | kernel 骨架 + 事件契约 / 切片提取器 + BM25 / fork 适配 / M0-Gate | ✅ 全部完成，Gate 有条件通过 |
| M1（U11-U18） | 真实数据验证 / embedding / fork 闭环 / evolve / L3 / 成本仪表 | 🚧 已实现，遗留 #58 |
| H1 接入 | fork Reasonix 挂载 U7/U8（事件旁路 + 注入工具），跨会话闭环真实会话跑通 | ✅ 合入（issue #64 closed） |
| H2 资源层 / H3 编排 / H4 UI 重构 | ResourceLayer / sched 编排 / Semantix Design TUI+桌面 | ⏳ 归入 Agile 2（H2/H3），H4 可并行 |

### 当前状态

- M0 ✅；M1 中 **#58 真实数据命中率 ≥70% 是唯一 P0 遗留门禁**（30 天窗口，`semantix verify` 回放）
- v0.3.1 已发布（bundle + QUICKSTART + agent-skill + 官网）
- v0.4.0 已发布（CLI v2 完整版：命令树 / config / --json 信封 / completion / doctor / install / gc / serve，4 平台资产）
- v0.4.1 已发布（CLI 可用性修复：doctor --json 信封 version 与发布版本一致 #169 + zoneIcon 同名冲突修复 #172，4 平台资产，见 `docs/releases/v0.4.1.md`）
- CLI v2（M2/U19-U27）是本 Agile 的补强，不阻塞 DoD

### DoD（完成定义）

- [ ] v1.0 发布：单二进制 bundle（reasonix+semantix）安装即用
- [ ] #58 真实数据命中率 ≥70%（verify 回放）
- [ ] 复用可视化在 TUI/桌面端可见（命中切片数 + 节省成本 + 来源会话）

---

## 2. Agile 2 — 自进化闭环（kernel 调配 harness）

> 目标：从"复用工具"升级为**资源大脑**——kernel 能调配 harness 的资源（工具并发/预算/模型 tier），prefetch + evolve 接入闭环，**命中率与成本随使用持续改善（自进化曲线）**。

### 范围（蓝图 H2/H3/H5）

| 项 | 内容 |
|---|---|
| H2 ResourceLayer | ToolRegistry 可挂起/恢复/并发分组 + BudgetController（token/成本配额）+ 资源目录上报 |
| H3 编排 | kernel/sched 落地：意图 → 资源分配指令下发 + 反馈闭环（观测 → 决策 → 执行 → 进化） |
| H5 预取/进化 | kernel/prefetch（Planner + MatrixPrefetcher + Runner）+ evolve 接入编排闭环 |

### 当前状态（2026-08-17 更新）

**kernel 侧 MVP 已随 M1-U18b 提前落地**（合入 main，commit d8b0558）：

- ✅ `kernel/sched.RuleDecider`（并行分组 + behavior gate + 模型 tier + prefetch hints）
- ✅ `kernel/prefetch`（Planner 离线转置矩阵 / MatrixPrefetcher 在线 hit-waste 学习 / Runner 串行只读执行 + 结果落 Result 切片）
- ✅ `kernel/evolve` MVP（独立已实现）

**战略决策（2026-08-17，Song）**：停止在 DeepSeek-Reasonix fork 仓库改动。Reasonix agent 系统
**vendor 进本仓 `harness/`**（MIT + attribution），在集成分支 `harness-integration` 上与 kernel
**进程内**结合，换 Semantix Design 视觉。`patches/` 模式废弃（U13c 成果作迁移对照表）。
这是蓝图 §6「停止跟随上游」风险项的正式落地；#158 桌面端随之重定位到本仓（等 U38 后续作）。

**fork 侧已验证的能力**（U13c/#123，`patches/semantix-sched-prefetch.patch`）：sched 本地移植、
并行分组、注入预热——按 spec §5 迁移进 `harness/`，移植副本删除（`kernel/sched` 成唯一实现）。

**issue 批次 U37-U43**（合体路线，契约 spec `docs/specs/h2h3-resource-orchestration.md` 先审后写）：

| U | 内容 | 阶段 |
|---|---|---|
| U37 | Harness 合体 + 资源编排契约 spec 评审（C0 vendor 方案 + ResourceCatalog + RoundPlan 扩展） | 门禁 |
| U38 | C0：vendor Reasonix agent 系统进 `harness/`（模块改写 + 构建 + 冒烟） | 合体 |
| U39 | C5：Semantix Design 视觉基线（主题 token + U33 复用面板迁移进程内化） | H4 |
| U40 | C1/C2：kernel 进程内接线（Decider 直连 + ResourceCatalog + SuspendTools 执行点） | H2/H3 |
| U41 | C3：BudgetController 阶梯降级（70/90/100 三阈值） | H2 |
| U42 | 调度演示 + 可量化收益报告（DoD 证据） | H3 |
| U43 | C4：prefetch/evolve 闭环接线 + 自进化曲线报告（DoD 证据） | H5 |

### 验收标准（蓝图 §5）

- [ ] 工具可挂起/恢复 + 预算配额生效（H2）
- [ ] 调度演示：kernel 决策改变 harness 行为 + 可量化收益（H3）
- [ ] 命中率/成本随使用提升曲线（H5，自进化证据）

### 前置依赖

- #58 真实数据达标（Agile 1 门禁）
- H1 fork 闭环稳定（已合入）
- CLI v2 的 `--json`/稳定契约（M2，作为调度指令的传输面）

---

## 3. Agile 3 — 多 harness 生态

> 目标：Semantix 成为**跨 coding agent 的自进化层**——Future Agent Integrations 表中的候选变为正式接入（shipped），第三方可低成本贡献 adapter。

### 范围

| 项 | 内容 |
|---|---|
| adapter 铺开 | `semantix install` 多 target（Claude Code / OpenAI Codex / Cursor / Gemini CLI / 自定义） |
| serve/watch | CLI U27 常驻服务（unix socket + JSON-RPC，与 CLI flag 同构） |
| 协议标准化 | 子进程协议 → JSON-RPC 演进路径，协议版本化 |
| 生态 | 第三方 adapter 贡献模板 + CI 门禁 + 验收报告 |

### 验收标准

- [ ] ≥3 个 harness 正式接入（非候选状态）
- [ ] 同一切片库跨 harness 共享（命中不受前端切换影响）
- [ ] 第三方 adapter 有贡献模板与 CI 门禁

### 当前状态

- 接入路径已文档化：`agent-skill/`（SKILL.md + tools/ + hooks/）、CLI v2 #125 install（M2-U25）
- 候选清单：README "Future Agent Integrations" 表

---

## 4. 最终形态

> **一句话：自进化的 agent kernel 层——任何 coding agent 装上 Semantix 后，每一次会话都让下一次会话更快、更便宜。**

| 维度 | 形态 |
|---|---|
| 技术 | 单 kernel 多 harness（"one kernel, many harnesses"）；CLI 为统一调用面，serve 为常驻加速；kernel 保持独立 module、零 harness 代码耦合 |
| 产品 | Agile 1 品牌化 bundle（reasonix+semantix）→ Agile 3 独立 kernel 层 + adapter 生态 |
| 指标 | L2 命中 ≥40% / 组合缓存 ≥90% / 成本降 ≥50% / 命中率随使用上升（自进化曲线） |

---

## 5. 总览表

| Agile | 目标 | 当前状态 | DoD 摘要 |
|---|---|---|---|
| 1 | 首个可下载品牌化 agent | M0 ✅ · M1 遗留 #58（唯一 P0 门禁）· CLI v2 ✅（U19-U36）· TUI 可视化 ✅（U33/#168）· 桌面端 #158 | v1.0 发布 + 命中率 ≥70% + 复用可视化 |
| 2 | 自进化闭环（kernel 调配 harness） | 🚧 kernel 侧 MVP ✅（M1-U18b）· 2026-08-17 转合体路线（harness vendor 进本仓，集成分支 `harness-integration`）→ 批次 U37-U43 已建，spec 待审 | 调度演示 + 自进化曲线 |
| 3 | 多 harness 生态 | 路径已文档化；serve/watch ✅（U27/U36） | ≥3 harness 正式接入 |

**网关线（套壳，GW 编号，独立于 Agile 主线）**：GW1 ✅（#133，主干可运行、29 测试绿）。
剩余按 `docs/specs/newapi-gateway-design.md` §0 对账（2026-08-15 回写）：流式响应侧写记忆（GW2，
不修则真实流式流量不积累 L3）→ 部署产物 + healthz 探活（GW3）→ **§7 M0/M1 验收门实录**（GW4，
真实全链路 + 二次命中 + 成本节省 ≥30%，「套壳完成」的定义性门槛）→ Anthropic 适配（GW5，Spec-Required）
→ 配置键对账（GW6）/ 计量口径收尾（GW7）。

---

## 6. 与 README P0-P5 的映射

| README 功能阶段 | 归属 Agile |
|---|---|
| P0 Observability / P1 SSL / P2 Semantic Cache | Agile 1（已基本完成） |
| P3 Adaptive Scheduler / P4 Prefetch | Agile 2（kernel 侧 MVP 已落地 M1-U18b，harness 侧待 H2/H3） |
| P5 Evolution Loop（MVP 已做）+ 多 harness 接入 | Agile 2 尾 + Agile 3 |

---

## 7. 演进规则

1. **每 Agile 开工前**：更新本文件状态列 + 创建该 Agile 的 issue 批次（U 编号连续）；
2. **DoD 未达标不进入下一 Agile**（Agile 1 的 #58 是硬门禁）；
3. **编号防冲突**：新批次 U 编号前先 grep 本文件与全部 issue；
4. 蓝图（harness-refactor-blueprint.md）为技术细节权威，本文件为节奏/目标权威，冲突时以本文件为准并回改蓝图。
