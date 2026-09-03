# Semantix L2 严格准入策略

> 状态：P0 基线 + P1.1 结构化 query（2026-09-03）
>
> 跟踪 Issue：[#447](https://github.com/Gnosil/semantix/issues/447)
>
> 适用路径：`harness/semantix` → `kernel/inject`

## 1. 目标与边界

本策略用于阻止弱相关历史在证据不足时进入 agent 上下文。它解决的是“是否允许注入”，不是用 BM25 分数证明历史一定正确。

严格模式遵循四条规则：

1. 检索命中不等于允许注入；
2. 小库、单一来源或排序不稳定时宁可 miss；
3. 仅允许语义强度较高的 `Context` 和 `Memory`；
4. 每个拒绝都返回稳定 reason code，shadow 与 strict 使用同一套判断。

`off` 不检索、不注入；`shadow` 运行完整清洗、检索和准入判断，但不改变 provider request；`strict` 只注入通过全部门禁的候选。

## 2. 查询处理

Bridge 不再直接把完整 benchmark prompt 交给 BM25，而是生成确定性的低权威 token 投影。P1.1 在 P0 模板清洗上继续提取：

1. 删除 `<execution-policy ...>...</execution-policy>`；
2. 存在 `<issue>...</issue>` 时只使用 issue 正文；
3. 从 runner 外壳识别 repo，从 issue 标题识别 intent；
4. 确定性提取 workspace path、代码 symbol、error code/exception、test name 和 import dependency；
5. URL 本身不参与 path/symbol 提取，避免链接路径伪装成 workspace 证据；
6. structured 模式只把 intent 和这些高信息字段投影给 BM25，正文叙述和固定 benchmark 外壳不参与检索；
7. 没有结构化信号时保留原 P0 清洗结果，记录 `lexical_fallback/no_structured_signals`；清洗后为空仍 fail closed。

事件继续只保存完整 query 与最终检索 query 的 SHA-256、字节数和 token 数，不复制完整 prompt；同时在 `queryStructure` 记录被选中的 intent/repo/path/symbol/error/test/dependency 及 fallback reason，使检索输入可解释。

## 3. 生产默认值

| 门禁 | 默认值 | 作用 |
|---|---:|---|
| 可注入类型 | `Context`, `Memory` | Prompt、ToolPattern、Result 仅诊断 |
| Project 最小切片数 | 5 | 避免冷启动小库强制产生胜者 |
| 候选类型最小来源会话数 | 2 | 避免单次会话自我强化 |
| 最低 BM25 绝对分 | 0.70 | 排除只有相对排名、没有绝对相关性的候选 |
| 最低 query coverage | 0.25 | 要求候选覆盖足够多的当前任务 token |
| 最低 eligible top-1/top-2 margin | 0.15 | 排除两个候选难以区分的情况 |
| runner-up | 必须存在 | 单个 eligible 候选不能自动通过 |

Margin 只在 allowlist 内的候选之间计算，避免一个不可注入的高分 Prompt 改变 Context/Memory 的准入结论。

## 4. 判断顺序和 reason code

每个候选按下列顺序评估；先命中的拒绝条件成为该候选的稳定 reason：

| Reason | 含义 | 运维动作 |
|---|---|---|
| `type_not_allowed` | 类型不在生产 allowlist | 保持 shadow；先提升切片质量/验证状态 |
| `library_too_small` | Project 库小于 5 | 等待积累，不通过降低 score 绕过 |
| `type_sources_too_few` | 该类型不足 2 个独立来源会话 | 增加独立成功样本 |
| `score_low` | BM25 绝对分低于 0.70 | 检查 query 清洗或历史是否真正相关 |
| `coverage_low` | query coverage 低于 0.25 | 检查是否只命中了通用词 |
| `runner_up_missing` | allowlist 内没有第二候选 | 保持 miss，避免单候选强制胜者 |
| `top_margin_low` | eligible top-1 与 top-2 差小于 0.15 | 候选歧义过高，等待更多证据 |
| `grey_disabled` | 候选只进入 grey 且未启用 audit | 保持拒绝 |
| `budget_exhausted` | 当前预算无法容纳候选 | 缩小或压缩切片，不突破预算 |

通过全部门禁后，事件记录 `admitted`；shadow 模式随后将最终 decision 标为 `withheld` / `shadow_mode`，strict 模式才生成注入正文。

## 5. 示例

### 5.1 小库中的高分 Prompt

即使 Prompt 与当前任务共享大量 `fix/test/repository` 词，它仍先被 `type_not_allowed` 拒绝；若库总量不足 5，其他候选还会得到 `library_too_small`。这避免“第一名”被误解为“足够相关”。

### 5.2 两个接近的 Context

两个允许类型候选分别为 1.10 和 1.02，margin 为 0.08。即使二者都超过绝对分和 coverage，top-1 仍以 `top_margin_low` 拒绝，因为当前证据不能稳定区分它们。

### 5.3 强证据 Context

Project 至少有 5 个切片，Context 来自至少 2 个独立 session；top-1 为 1.40、top-2 为 0.80，coverage 为 0.50。该候选通过默认门禁；strict 注入，shadow 仅记录。

## 6. 校准流程

默认阈值是保守止损线，不应直接按“注入率太低”下调。阈值调整必须基于冻结样本的配对实验：

1. 使用同一 manifest、模型、prompt、repo 内顺序和运行环境；
2. 比较 memory off、shadow、strict BM25 和旧宽松策略；
3. 按 reason code 输出候选分布；
4. 同时检查 resolved、executor calls、重复 read/grep/test、输入 token 和成本；
5. 只有 pass@1 非劣且中位调用数/P75-P90 工具轮数不增加时才扩大准入；
6. 每次只改一个阈值并保留上一组事件用于离线重放。

覆盖率不是成功指标。大量 `library_too_small` 或 `runner_up_missing` 是冷启动期的预期行为。

## 7. 观测与回放

`RetrievalDiagnostics` 提供：

- mode、library size、repo、base commit；
- query before/after 摘要；
- query strategy、结构化字段和 fallback reason；
- candidate ID/type/source session；
- score、coverage、eligible top margin；
- admitted/rejected/withheld 与 reason；
- strict 模式的 injected bytes、message role 和最终顺序。

离线回放必须使用事件中的原始数值与策略版本，不能用新的阈值反推旧 decision 后覆盖历史记录。

## 8. 回滚

出现回归时按影响面从小到大回滚：

1. 单次实验改为 `shadow`，保持诊断而不改变模型输入；
2. 单次运行改为 `off`，回到无记忆基线；
3. 保留 reason 分布、provider request hash 和逐实例调用指标，用于定位是 query、候选质量还是阈值问题。

不要通过重新开放 Prompt/ToolPattern/未验证 Result 或允许单候选自动通过来恢复注入率。

## 9. 验证命令

```powershell
go test ./kernel/inject -count=1
go test ./harness/semantix ./harness/eventwire -count=1
go test ./harness/config -run TestRenderTOMLRoundTrips -count=1
go test ./harness/agent -run TestShadowRetrievalKeepsProviderMessagesByteIdenticalToOff -count=1
git diff --check upstream/main...HEAD
```
