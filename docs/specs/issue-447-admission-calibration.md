# Issue #447 — L2 准入校准（per-scale threshold calibration）

> 状态：**草案，待批准**（Spec-Required：改准入语义与默认阈值、可能新增配置键）。
> 日期：2026-09-07。上位文档：`docs/specs/semantix-l2-admission-policy.md`（策略合同）、
> `docs/specs/semantix-memory-negative-transfer.md`（根因树）、`docs/reports/swe-pilot-three-arm-distill.md`（暴露=0 的 pilot）。
> 离线回放工具与逐候选数据：`lab/issue-447-calibration/`（本地，不入库）。

## 1. 要解决的问题

三臂 pilot（2026-09-03）证明当前 main 在 SWE pilot 上 L2 注入暴露为 0。报告把它归因于 `MinSourceSessions=2` 冷启动 + `MinScore 0.70 / MinTopMargin 0.15 / RequireRunnerUp`，并把「门槛校准」列为下一主线。

本 spec 先用离线回放核实**到底是哪一道门把通道关死的**，再据此决定校准什么。结论：**阈值层在这批数据上从未被触达**；关死通道的是它前面的三道门，而且其中一道（查询结构化）是实现缺陷而非策略选择。直接调阈值不会打开通道。

## 2. 回放方法

- 数据：pilot 的 agent 侧会话（`$SEMANTIX_HOME/projects/<slug>/sessions/*.jsonl`，每臂每题一份，`result.json.session_id` 对齐）、数据集 `base_commit`、两个仓库克隆。
- 课程：按 pilot 顺序逐题重建库——第 N 题的库 = 前 N-1 题会话的 `semantix extract` 结果；查询 = 该题会话的首条用户消息（与 bridge 收到的完全一致）。
- 供给侧两种：`legacy`（`run_arm.sh` 口径，无 `--base-commit`）与 `runner`（`run_bench.py` 口径，`--base-commit` + 触及文件 `--fingerprint`）；抽取两种：流水账（ON-old 臂）与 `--distill --consolidate`（ON-new 臂）。
- 判定：调用 main 上的 `Bridge` shadow 路径得到真实 `RetrievalDiagnostics`；再用同一 `inject.Injector.BuildHits` 跑门槛阶梯（逐级摘除 freshness → 来源会话 → runner-up/margin → score → coverage）和 2×2×3×5×3 网格；另加「先按类型过滤再取 top-5」变体。无模型调用，全程确定性，可重放。

## 3. 发现（门的绑定顺序）

| # | 门 | 观察 | 性质 |
|---|---|---|---|
| F1 | 查询结构化（P1.1） | swe_pilot prompt 没有 `<issue>` 标签，`intent` 取成了任务外壳第一行；`(?i)` 让 `pre-fix`/`four-digit` 命中 error code；裸词 `test` 命中 test name。结果结构化查询只剩模板 token（部分实例查询为纯模板），fallback 永不触发；coverage 是对模板 token 算的，L1 下的 admitted 全是模板重合的伪相似 | **实现缺陷，本 PR 已修**（Spec-Exempt：使实现符合策略 spec §2） |
| F2 | freshness（#466） | 跨 commit 课程下 100% 拒绝：legacy 供给 → `commit_unknown`；runner 供给 → `dependency_changed`（触及文件在相邻 base commit 间内容必变）。Issue P0.3 写的是「commit 不同时至少验证路径/符号仍存在，否则拒绝或降级 shadow」，实现是内容哈希相等 | 策略比 issue 更严，需决策 |
| F3 | 检索 K=5 在类型过滤之前 | legacy 供给下 top-5 全是 Prompt/ToolPattern/Result（`type_not_allowed`/`result_probation`），可注入类型进不了候选；先过滤再取 top-5 能让 Context/Memory 进入判定 | 结构问题，需决策 |
| F4 | 供给侧 | 流水账抽取的 Context（路径清单）在 K=50 内几乎检不到；`--distill` 卡片可检回（修 F1 后多在 rank 1–4） | 供给侧口径 |
| F5 | 阈值层 | eligible 候选 BM25 绝对分在 1–8 之间，`MinScore 0.70` 在 0–1.0 网格内对准入没有任何影响（BM25 标度上是死信，与 bridge 里对 zone 绝对地板的注释同一问题）；**coverage 0.25 是真正绑定项**（修 F1 后 0.25→0.10 使有准入的题从 1/8 变 7/8）；`type_sources_too_few` 只拦每 repo 前 2 题；margin/runner-up 影响次要 | 校准对象 |

逐候选 reason、K=50 排名、阶梯与网格明细见 `lab/issue-447-calibration/aggregate-fixed.log`。

## 4. 提案（逐项，需 Song 决策）

### P1 freshness 分级（回应 F2）

| 情况 | 现状 | 提案 |
|---|---|---|
| 切片无 `base_commit` | `commit_unknown` 拒绝 | 保持拒绝（供给侧必须写 provenance） |
| commit 相同 | 通过 | 通过 |
| commit 不同、deps 路径全部存在、内容未变 | 通过 | 通过 |
| commit 不同、deps 路径全部存在、内容有变 | `dependency_changed` 拒绝 | **改为 grey**：仅在 `grey_mode=audit` 下带「未验证/依赖已变」头注入，默认仍拒绝；diagnostics 记 `dependency_changed_grey` |
| commit 不同、路径缺失或非法 | 拒绝 | 拒绝 |

理由：issue P0.3 的门槛是「路径/符号仍存在」；内容哈希相等在真实仓库演进（同 repo 不同 commit 正是 L2 的目标场景）下等价于关闭跨任务复用。grey 路径已有 budget/头注入合同（history-authority spec），不新增权威。**默认行为不变**，只给校准实验一个可测的档位。

### P2 类型感知检索（回应 F3）

`Bridge.injectResult` 改为 over-fetch（`K*4`，上限 50）后先按 `AllowedTypes` + Result probation 过滤，再取前 K 进入 `BuildHits`；diagnostics 新增 `retrievedBeforeFilter`。策略 spec §4 的判断顺序不变（type/probation 仍是第一道 reason），只是不再让不可注入类型消耗候选位。

### P3 绝对分门槛（回应 F5）

`MinScore` 默认从 0.70 改为 **0（关闭）**，相对置信由 zone 的 `score/top1` 承担；若要保留绝对地板，必须按库规模标定（per-scale），不能用 cosine 标度的常数。策略 spec §3 表同步。

### P4 coverage 与来源会话数（回应 F5）

- 不在本 spec 内直接下调 `MinCoverage`（策略 spec §6：不能因注入率低下调）。改为把 `MinCoverage`、`MinSourceSessions`、`MinTopMargin`、`RequireRunnerUp` 暴露为 `[semantix]` 配置键（默认值 = 现值），供 §5 实验逐个标定；配置键为新增 wire 面，随本 spec 一起审。
- 候选默认：`MinCoverage 0.10`（回放中打开 7/8 题的档位），**只有 §5 实验通过后才改默认**。

### P5 注入暴露率成为一级指标

- `scripts/experiments/swe_pilot/run_arm.sh`：不再阅后即焚 bridge 镜像（按题改名保留），并把 `SliceInject` 计数与 bytes 写进 `metrics.tsv`；
- `memory_matrix_report.py` 已有 `inject turns/bytes`，报告模板加「暴露率 = 有注入的 turn / 总 turn」。
- 任何「注入无害」结论必须附暴露率；暴露=0 时只能得出「未测」。

### P6 供给侧口径（回应 F4）

swe_pilot 与 run_bench 的抽取默认加 `--distill --consolidate`；流水账 Context 不再作为 L2 注入供给（仍可存库供诊断）。

## 5. 校准实验（按策略 spec §6 流程）

冻结 swe50 子集、同模型、同 repo 内顺序、`caffeinate`：

| 臂 | 配置 | 回答 |
|---|---|---|
| A | memory off | 基线 |
| B | shadow + P1–P3 + 候选默认 P4 | 暴露率/命中分布，不改模型输入 |
| C | strict + 同上 | 步数/token/resolved 配对差 |

每次只动一个阈值；记录每题 reason 分布、暴露率、`executor_calls`、`repeated_tool_calls`、resolved。先跑 B 看暴露率是否离开 0，再跑 C。

## 6. 验收

- B 臂暴露率 > 0 且 reason 分布可解释（不再被 F1–F3 单点关死）；
- C 对 A：pass@1 非劣；中位 `executor_calls`、P75/P90 工具轮数不升；每成功任务 input token 不升；
- 所有拒绝仍有稳定 reason code；`dependency_changed_grey` 只在 audit 出现；
- 回放工具能对新一轮 shadow 事件离线复算出与线上一致的 decision。

## 7. 回滚

按影响面：C→shadow → 恢复 `MinScore 0.70` → 关闭 over-fetch（`K` 直取）→ `dependency_changed` 回到硬拒绝。每一步都是配置或单开关，不需回退数据。

## 8. 不做

- 不重开 Prompt / ToolPattern / 未验证 Result 的注入；
- 不用「注入率太低」作为下调任何阈值的理由；
- 不把回放的 admitted 计数解释为收益——它只回答「门是否打开」，收益由 §5 的 C 臂回答。

## 9. 实现拆分（批准后）

1. P2 + P3（`harness/semantix/bridge.go`、`kernel/inject`，含 diagnostics 字段与测试）；
2. P1（`kernel/inject` freshness 分级 + grey 头，测试覆盖五种情况）；
3. P4 配置键（`harness/config` TOML 往返测试）；
4. P5/P6 脚本与报告；
5. 离线回放工具从 lab 提升为 `semantix replay`（另开 spec）。
