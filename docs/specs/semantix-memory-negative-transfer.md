# Semantix 记忆注入负迁移：多步、意图偏移与重复探索修复方案

> 状态：实施中（2026-09-02）
>
> 跟踪 Issue：[#447](https://github.com/Gnosil/semantix/issues/447)
>
> 范围：`semantix-agent` 的 L2 跨会话切片检索与注入路径、SWE-bench 记忆实验基座
>
> 目标：在保持 pass@1 非劣的前提下，阻止弱相关历史增加模型调用、工具轮次与输入 token。

## 1. 问题陈述

Semantix 的目标是通过跨会话语义复用减少重复定位和执行。但真实 SWE-bench memory ON/OFF pilot 中，ON 臂虽然保持质量非劣（10/10 vs 9/10），输入 token 增加 59.2%，API 成本增加 80.2%，且个别实例出现显著负收益。当前证据表明，弱相关切片可能让 agent 先验证历史路径，再回到当前问题，从而增加 `grep`、读取、测试和模型调用。

必须同时纠正一个历史归因：`docs/reports/swebench-harness-comparison.md` 的 64 vs 45 平均步数来自 2026-08-27 的 full vs `--ablate all` 实验；当时 runner 未写入 `[semantix]`，记忆内核实际关闭，`--ablate all` 也尚未包含 `kernel`。该数据证明完整 harness 栈存在额外模型调用，不能证明 L2 记忆造成了 19 步差异。记忆因果对照必须使用 `--semantix-memory on|off`。

## 2. 当前数据流

```text
用户任务全文
  → harness/semantix Bridge
  → Project scope BM25 top-5
  → zone.Default()：score/top1 + 绝对阈值
  → Prompt / Context / ToolPattern / Result / Memory 统一候选池
  → ID 排序组成 [semantix-reuse]
  → 作为独立 system message 锁定整个 user turn
  → 每个模型调用重复携带同一注入块
```

Agent 路径目前仍是 BM25-only；gateway 已有的 model embedding、hybrid 和 HNSW 没有进入该路径。

## 3. 根因假设

### H1：小库下的强制胜者

zone 使用 `score/top1`。对排名第一的候选，该比值恒为 1；BM25 只要因少量共享词得到超过绝对下限的分数，top-1 就会成为 Hit。小库里“第一名”只代表比其他少量候选更接近，并不代表足够相关。

固定 benchmark 模板进一步制造共享词：`repository`、`issue`、`root cause`、`fix`、`test`、`implement`。当前 tokenizer 没有 stopword 或模板剥离，可能实际匹配的是任务外壳而不是 bug 意图。

### H2：低质量切片统一进入高权威注入

- Prompt 是另一道任务的问题描述，不是已验证知识。
- ToolPattern 只有工具名，没有路径、参数、结果或成败；离线数据中跨项目命中率 93.4%，高于同项目 53.3%，说明它主要匹配通用工具序列。
- Result 是 agent 的最终自报，抽取发生在官方评测之前，失败结果也可能进入后续任务。
- Context 汇总的是重复访问路径和命令，不区分有效定位与探索死路。

这些切片目前可统一进入注入，且正文以 system role 发送。文档称其为“低权威参考”，协议层却给予 system 权威；正文又不展示 repo、commit、类型、验证状态、分数或时间，模型难以正确评估可信度。

### H3：跨 repo、跨 commit 污染

标准 `scripts/swebench/run_bench.py` 当前让整个 run 共用一个 `kernel/.semantix/project.db`，并使用常量 project slug `swebench`。BM25 只按 `Scope=Project` 过滤，不按真实 repo 过滤，因此不同仓库切片可以互相检索。

同一 repo 的不同 SWE-bench 实例还对应不同 base commit；L2 注入没有 commit、依赖哈希或文件存在性门禁，旧路径和旧 API 可能被带入当前任务。

### H4：错误提示在整个 turn 中持续锚定

注入在 user turn 开始时检索一次并锁定。这有利于前缀稳定，但错误切片也会在后续每个模型调用中持续出现。即使工具结果已经否定历史路径，模型仍可能继续验证其关联路径来调和 system 提示与当前证据。

### H5：额外调用放大成本

注入块本身通常只有几 KB；主要成本来自它触发的额外调用。每个额外调用会重新发送持续增长的 system、会话历史、工具调用和工具结果。旧 10 题 full/ablate 数据中，按平均上下文估算，额外调用可解释约 86.8% 的输入 token 差额。

## 4. 设计原则

1. **不确定时不注入**：miss 是正常退化路径，注入覆盖率不是成功指标。
2. **先隔离再排序**：repo、版本、类型和验证状态应先过滤，相关性排序不能替代边界约束。
3. **历史是证据，不是指令**：当前代码、工具结果和用户任务始终优先。
4. **按结果评价切片**：被检索或被注入不等于有价值，必须记录是否减少步骤或导致回退。
5. **区分主循环与辅助调用**：总 `steps` 必须拆为 executor、subagent、planner、compaction 等来源。

## 5. 修复方案

### P0：可信测量与立即止损

#### 5.1 Shadow retrieval

增加只检索、不注入的 shadow 模式，逐 turn 记录：

- 库大小、repo、commit；
- query 清洗前后文本摘要；
- top-k slice ID、类型、来源 session、验证状态；
- BM25 绝对分、query coverage、top-1/top-2 margin、zone；
- 最终是否注入及拒绝原因。

#### 5.2 Repo 隔离

标准 runner 改为每个真实 repo 独立 Project store：

```text
kernel/<owner>__<repo>/.semantix/project.db
```

同 repo 内按固定顺序串行，repo 之间可以并行，避免 `--workers` 让记忆课程由完成时序随机决定。

#### 5.3 类型 allowlist

生产默认仅允许：

- Context；
- Memory。

Prompt、ToolPattern 默认 shadow-only；Result 只有在具有成功验证或评测标记后才可注入。

#### 5.4 小库与绝对相关性门禁

第一版采用保守规则，具体数值通过 shadow 数据标定：

- Project 库或同类型来源会话不足时不注入；
- 最低 BM25 绝对分；
- 最低 query token coverage；
- top-1/top-2 最小 margin；
- 去除固定任务模板和通用工程 stopwords；
- 只有单个候选时不因缺少 top-2 自动通过。

#### 5.5 降低注入权威性

历史正文不再作为独立 system message。system 只声明固定政策：历史是不可信参考，冲突时以当前任务、当前代码和工具结果为准。切片正文作为带来源元数据的历史上下文发送，并明确要求先验证再使用。

#### 5.6 严格预算

移除 top-slice 超预算例外。任何单切片超过预算时进行确定性压缩或拒绝，保证 `budget` 是真实硬上限。

### P1：切片质量与负迁移反馈

#### 5.7 查询结构化

检索 query 从完整 prompt 改为任务意图、repo、路径、符号和错误码；固定 benchmark 外壳不参与检索。

#### 5.8 ToolPattern 升级

T-Slice 至少携带路径、查询词、命令族、验证边界和结果状态。纯工具名序列不进入生产注入。

#### 5.9 Result 成功提升

Result 初始为 probation；只有验证命令通过、无回滚，或外部评测 resolved 后才提升为 injectable。

#### 5.10 负迁移熔断

将 slice ID 与后续工具行为关联。连续出现重复读取、重复搜索、重复测试、无效路径或回滚时，当前 turn 停止使用该 slice，并累计 reject/waste，而不是仅累计 injected。

### P2：检索升级

在 P0/P1 门禁稳定后，再把 agent bridge 接到 hybrid + model embedding + HNSW。Embedding 用于提高召回和同义匹配，不能替代 repo、版本、类型和验证状态门禁。

## 6. 实验矩阵

在同一冻结子集、同模型、同 prompt、同 repo 顺序下运行：

| 臂 | 配置 |
|---|---|
| A | memory off |
| B | shadow retrieval，不注入 |
| C | repo 隔离 + C/M + 严格 Hit |
| D | 当前全类型注入策略 |
| E | C + hybrid/model embedding |

每实例必须报告：

- `executor_calls`、`subagent_calls`、`planner_calls`、`compaction_calls`；
- 主工具轮数、`tool_calls_by_name`、重复 read/grep/test；
- 注入 slice ID/type/source/repo/commit/bytes；
- input/output/cache token、成本、墙钟；
- resolved、回滚、污染事件。

## 7. 验收标准

1. pass@1 相对 memory-off 非劣；
2. 中位 executor calls 不增加；
3. P75/P90 工具轮数不显著增加；
4. 每成功任务输入 token 和成本下降；
5. 污染率不高于 5%；
6. 任何注入都能解释其 repo、版本、类型、来源和准入理由；
7. 小库、跨 repo、未验证 Result 和纯工具名 T-Slice 均有自动拒绝测试；
8. 文档不再用旧 full/`--ablate all` 数据归因记忆内核。

## 8. 建议实施顺序

1. 修正 benchmark 指标和历史文档归因；
2. 增加 shadow retrieval；
3. repo 独立 store + repo 内串行；
4. C/M allowlist + 小库/coverage/margin 门禁；
5. 历史正文降权 + 严格预算；
6. 跑 A–D 小样本并检查逐实例轨迹；
7. 接入成功提升和负迁移熔断；
8. 最后验证 hybrid/model embedding 的增量收益。

## 9. 实施进度

- P0.1 指标归因：已完成，调用来源与重复工具指标已进入逐实例记录；
- P0.2 Shadow retrieval：已完成，`off | shadow | strict` 及 provider-byte 不变性测试已落地；
- P0.3 Repo 隔离：已完成，采用真实 repo 独立 store 和 repo 内确定性串行；
- P0.4 严格准入：已实现 C/M allowlist、小库/来源会话/绝对分/coverage/margin/runner-up 门禁和 query 清洗；
- P0.5 历史正文降权、provenance、严格预算和 score-first 稳定排序：已实现；
- 后续：A-D 配对实验、Result 成功提升和负迁移熔断。

P0.4 的具体默认值、reason code、校准和回滚合同见 `docs/specs/semantix-l2-admission-policy.md`；
P0.5 的 provider 消息合同、完整字节预算口径和回滚步骤见
`docs/specs/semantix-l2-history-authority-and-budget.md`。

## 10. 相关材料

- `docs/reports/swe-pilot-two-arm.md`
- `docs/reports/swebench-harness-comparison.md`
- `docs/reports/w0-w4-efficiency-experiments.md`
- `docs/reports/w5-w6-ablation.md`
- `docs/specs/swebench-efficiency-research-plan.md`
- `docs/specs/swebench-memory-arm.md`
