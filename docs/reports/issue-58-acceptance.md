# Issue #58 验收报告（第一轮）— M1-U11: 真实数据命中率验证

> 状态：**第一轮完成，未通过（50% < 70%）——样本不足，待积累更多真实会话后复测**。
> 对应 Issue：`#58 M1-U11: 真实数据命中率验证（verify 回放 ≥70%，M0-Gate 最后门槛）`。
> 门槛定义：`docs/reports/m0-gate.md` §3（真实数据命中率 ≥70%）。

## 1. 结论摘要

| 项 | 值 | 判定 |
|---|---|---|
| 回放 turn 数 | 4 | 样本过小，统计不具决定性 |
| 命中率（已标 ✅/总） | **2/4 = 50%** | ❌ 未达 70% 门槛 |
| zone 分布 | hit=2, grey=1, miss=1 | grey_ratio=25%（≤30% ✅） |
| 判定 | **样本不足，待积累真实会话后复测**（用户决策） | — |

## 2. 环境

| 项 | 值 |
|---|---|
| 日期 | 2026-08-13（第一轮） |
| semantix 二进制 | `C:\Users\liwen\go\bin\semantix.exe`（`go install ./cmd/semantix`） |
| 模型（数据产生方） | deepseek/deepseek-v4-flash（Reasonix fork 会话） |
| 数据来源 | `.semantix/sessions/boot-{1,2,5}.jsonl`——**HarnessSink（U7 事件旁路）真实旁路产物**，非合成数据 |
| 会话数 | 3（boot-1: 4 user 行 / boot-2: 1 user 行 / boot-5: 4 user 行） |
| verify 参数 | `--holdout 0.33`（前 67% 建库、后 33% 回放），默认 zone 阈值 |

## 3. 数据来源说明

- **唯一兼容数据**：HarnessSink 旁路的 boot-*.jsonl（扁平 `{content, role}` JSONL，
  与 verify `parseTurns` 完全兼容）。这是 Reasonix fork 挂载（H1，U7）的真实产物。
- **不兼容数据（已排除）**：历史 Reasonix 会话（`~/AppData/Roaming/reasonix/...`）
  为 events/recovery 格式（`{schema_version, messages:[...]}` 或含 system 注入包装），
  verify 无法直接解析；未使用，避免格式转换引入偏差。
- **数据积累机制**：HarnessSink 持续旁路当前真实会话（boot-5.jsonl 实时增长，
  437KB / 4 user 行），每完成一轮真实任务即新增训练/回放素材。

## 4. 标注规则

对每条回放行判定：**top1_content 是否与 query 属"之前做过类似"的任务**（M0 假设
"垂类任务产生相似中间产物"）。

- ✅：top1 与 query 描述同类操作/任务/流程（如延续任务、同系列验证型任务）
- ❌：top1 与 query 是不同 issue/不同任务，或 miss（无命中）

| # | query | top1_content | zone | 标注 | 依据 |
|---|---|---|---|---|---|
| 1 | 继续完成 | 继续 | hit | ✅ | 延续任务，同类操作 |
| 2 | M1-U16b 提升条目持久化 #68… | M1-U13b fork 端到端验证 #64… | hit | ❌ | 不同 issue/不同任务域（存储层 vs fork 验证） |
| 3 | 开工 | （空） | miss | ❌ | miss 无命中 |
| 4 | M1-U11 真实数据命中率验证 #58… | M1-U13b fork 端到端验证 #64… | grey | ✅ | 同为 M1 验证型任务，流程相似（跑验证→写报告） |

## 5. 第一轮判定与复测计划

- **判定**：命中率 50% 未达 70% 门槛；但样本仅 4 回放 turn、3 会话同属 semantix
  项目线（内容同质），**统计不具决定性**。按用户决策：**积累更多真实会话后复测**，
  不修改代码（verify 工具与 extractor 粒度保持现状）。
- **复测触发条件**：boot 会话旁路积累到 ≥6 个会话（或 10+ 回放 turn）后重跑
  `semantix verify --session tmp/verify-data --holdout 0.3`。
- **失败路径参考**（issue #58）：若样本充足后仍 <70%，先调 extractor 粒度
  （turn 级 → 子任务级）再测一轮；两轮不过触发止损评审。

## 6. 提交物

- TSV 输出：`tmp/verify-data/verify-annotated.tsv`（4 行 + 标注列）
- 本报告（docs/reports/issue-58-acceptance.md）
- 复测：数据积累后更新本报告并提交 issue #58

## 7. 遗留

1. 数据量不足是唯一阻塞——HarnessSink 旁路是自动的，无需额外动作，等待真实会话积累
2. 标注含主观判断（"是否之前做过类似"），标注规则已显式化，可复核
