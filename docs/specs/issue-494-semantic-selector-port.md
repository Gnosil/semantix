# Issue #494 — L2 语义选择器移植（semantic selector port）

> 状态：**草案，待批准**（Spec-Required：改 `kernel/inject` 导出函数与 reason 语义、改 Bridge API、新增 `[semantix.selector]` 配置键与事件字段、新增跨仓 wire 契约 `semantix.scorer/v1`、触及第三方外发边界）。
> 日期：2026-09-21。别名：`docs/specs/semantix-l2-selector-port.md`（姐妹 spec 以此名引用本文，两名指同一文档，最终路径由 owner 定）。
>
> 关联 Issue：[#494](https://github.com/Gnosil/semantix/issues/494)（本文对象；@Allenllii 有条件批准 P0+P1，四条件见 §1.3）、[#447](https://github.com/Gnosil/semantix/issues/447)（上位问题）、[#490](https://github.com/Gnosil/semantix/issues/490)（trace 导出）、[#491](https://github.com/Gnosil/semantix/issues/491)（`judge_protocol=local`）、[#492](https://github.com/Gnosil/semantix/issues/492)（L3 台账回流）、[#493](https://github.com/Gnosil/semantix/issues/493)（语义规范化，独立研究）。
> 关联 PR：[#488](https://github.com/Gnosil/semantix/pull/488)（本文 kernel 基线）、[#478](https://github.com/Gnosil/semantix/pull/478)（查询修复 + `issue-447-admission-calibration.md`）、[#460](https://github.com/Gnosil/semantix/pull/460)（本地检索模型，拆分处置见 §7.6）。
>
> 上位文档：`docs/specs/semantix-l2-admission-policy.md`（策略合同，本文修订其 §4）、`docs/specs/issue-447-admission-calibration.md`（回放证据）、`docs/specs/semantix-memory-negative-transfer.md`（根因树）、`docs/specs/evolution-invariants.md`（I-1..I-5）。
> 姐妹 spec（本文引用其词汇，不重复定义）：`docs/specs/semantix-l2-lifecycle-hygiene.md`（§3 的现在落地部分）、`docs/specs/trace-export-schema.md`（=#490，`semantix.trace/v1`）、`docs/specs/scorer-wire-contract.md`（`semantix.scorer/v1`，S4 以 Draft 发布）、`docs/specs/issue-447-per-card-deps-and-freshness.md`（S2 供给 spec）。
>
> 公共/私有边界：模型权重、数据集、评估数字、校准阈值、训练配方、标注计划、供应商适配与条款笔记只在私有仓 `Gnosil/semantix-models`。**本文不含上述任何内容，也不含实验数字**（CLAUDE.md：实验数字只在 `/lab`）。§1 结论的数字依据在本地工作树 `wt-447-calib/lab/issue-447-calibration/`（gitignored，**不在任何 PR 中**；§8 S0 的备份步骤使其可复现）与私有仓 `data/raw/`。本文及公共仓代码、配置、事件、测试**不出现 #494 中的供应商名、模型名、条款名与条款编号**；下文统称"第三方后端"。
>
> 审批权限：#494 的有条件批准来自 #488 作者；**owner (Gnosil) 尚未批准任何内容**。本文每一节、§1 的 P0 结论、对 condition 2 的援引、以及对 #494 已批文本的两处偏离（§10 D1）都需 owner 批准。

## 0. 一句话结论

并入既有系统的不是某个供应商，而是它所代表的能力："在共享资格校验之后、共享组装之前，对合格候选做经校准的语义选择"。本文把这一能力拆成：`kernel/inject` 内一个纯本地 seam（§2）、harness 侧一个 `RelevanceSelector` 接口与 Bridge 生命周期合同（§3）、一个公共 HTTP 契约 `semantix.scorer/v1`（§4，同时覆盖 #491 与 #460）、以及第三方后端的默认关闭与结构性隔离（§6）。

- **现在**（零花费、零外发）：合并 #488 → rebase #478；生命周期卫生 PR；`CandidateDecision.Eligible` 附加字段；`semantix.trace/v1`（=#490）；`semantix.scorer/v1` 作为公共 Draft spec（无 Go client）；供给修复 spec。
- **G6 之后**（本地模型离线胜过词法规则后）：kernel seam + `RelevanceSelector` + `internal/scorer` 客户端 + `local-shadow`；#460 Go 部分与 #491 `ScoreJudge` 一起在 scorer/v1 上重新落地。
- **第三方后端**：公共仓零标识；私有仓中至多是"封存的、仅评估的比较臂"，在 owner/legal 书面澄清条款前一次也不调用；永远不是 teacher。**本计划在第三方调用数为 0 时具有完整价值。**
- **P0 结论**：approval condition 2 触发（§1）。#494 P1 的 mock 接线推迟到 G6，由 owner 批准（§10 D1，默认项）。

## 1. P0 结论：供给/资格是绑定门，不是相关性选择

结论级陈述；漏斗计数、反事实准入数、token 级取证与证据脆弱性说明在 `wt-447-calib/lab/issue-447-calibration/`（本地、gitignored）与私有仓 `docs/l2-selector-plan.md` §1.4，不在本文。

### 1.1 生产顺序回放（先取 top-5 再过类型门，修 F1 查询结构化缺陷后）

- 没有任何候选决策到达词法门（`runner_up_missing` / `top_margin_low` / `below_min_score` / `coverage_low` / zone 从未被触达）；候选全部死于 type / Result probation 与 freshness（legacy 供给 → `commit_unknown`；runner 供给 → `dependency_changed`，因为会话级 Deps 把触及文件绑到每张卡）。
- #488 自述的新一轮运行与此一致：strict 诊断无一条正 bytes+sliceIds 注入事件；逐 reason 计数未公布（§8 S1 列为请求项）。
- 按 #494 §2 自己的合同（"无合格候选时直接返回原有拒绝原因，请求数为 0"），**任何语义选择器在这批语料上会被调用 0 次**。

### 1.2 反事实回放（freshness 豁免 + 先过类型门再取 top-5 + `--distill --consolidate` 供给）

- **coverage 是绑定词法门**；`MinScore` 在 BM25 标度上是死条款（合格候选绝对分远高于 0..1 假定，整段网格无影响）；margin / runner-up / 来源数为二阶。
- 放宽 coverage 后放行的卡以与当前 issue 无关的 task-outcome 卡为主（重合来自停用词与裸数字）→ **reject 方向存在语义选择器的空间**。
- 语料中没有任何主题相关的任务对 → **rescue 方向（措辞不同、真实匹配）在此语料不可测**。

### 1.3 对 #494 approval condition 2 的意义

condition 2 原文："P0 结论优先于 P2 花费：若 P0 显示有效候选主要被 Deps/来源/类型拦截，先修供给问题，本 Issue 的 [第三方] 部分暂缓；不为验证 [第三方] 而放宽任何校验。"

结论：**condition 2 触发**。排序变为：供给修复（§8 S2）先于任何选择器；P1 mock 接线推迟到 G6；P2 真实调用无限期暂缓且受 §6 约束。不为选择器制造候选而放宽 freshness、类型或来源门。

其余三条件在本文的落点：condition 1（kernel 纯本地）→ §2.3 `TestInjectPackageImportsExcludeNet` 机械化；condition 3（按次审批预算与外发范围）→ §6.3；condition 4（基线为最终合并的 #488）→ §7.5、§8 S1，且 §8 S1b 中触及 `kernel/inject` 与 `bridge.go` 的部分以 "#488 已合并到 main" 为进入门，不在未合并基线上实现。

证据脆弱性：有效步数少、候选与卡片数少、单标注者内容判读、原始输入已删。§8 S0 要求立即备份 lab 目录。

## 2. `kernel/inject` seam：Eligible → LexicalSelect → Assemble

基线 = 最终合并的 #488。#488 的实际 kernel / Bridge 增量：eligible window + rank cap（`inject.go` pass-1）、origin 门移到 window / margin 之前、Bridge 接线 `MinOrigin: slice.OriginSessionAuto`、全库 `idx.Search(cleanedQuery, len(projectSlices), …)`、诊断按 ID join、`readGitHead` 回退 `git rev-parse`。（`stale_commit` 早已在 origin/main，不属于 #488 增量。）G6 落地；类型与函数名现在冻结。

### 2.1 类型与函数（`kernel/inject/inject.go`，无新增 import）

```go
type EligibleCandidate struct { Slice *slice.Slice; Score, Coverage float64; Rank int; Sanitized string /* sanitize+escapeMarker 输出：既是组装字节，也是唯一允许外发的字节 */ }
type EligibleSet struct { Query string; Candidates []EligibleCandidate /* ≤K，BM25 序，ID tie-break；post-window 拒绝已移出 */; Rejected []CandidateDecision /* 含原 top-K 拒绝与 post-window 拒绝，供诊断 */; EligibleCount int /* == len(eligibleScores)：window 谓词通过的 hit 数，含随后被 post-window 门移出者 */; Top1, TopMargin float64; LibrarySize int; CandidateKey string /* 有序合格 ID 以 "," 连接；无内容哈希 */ }
type SelectDecision struct { Keep bool; Grey bool; Zone zone.Zone; Reason string }
type Selection struct { Source string /* "lexical" | "external" */; ByID map[string]SelectDecision }
func (in *Injector) Eligible(query string, hits []slice.Hit) *EligibleSet
func (in *Injector) LexicalSelect(set *EligibleSet) Selection
func (in *Injector) Assemble(set *EligibleSet, sel Selection, budget int) (*Injection, error)
// BuildHits = Eligible → LexicalSelect → Assemble(budget=in.Budget)；Build 不变
```

- `Injection` 加一个附加字段 `Selector string`（`"lexical"` | `"external"`）。
- kernel 不含任何执行 I/O 的接口；`Selection` 是纯数据。`BuildHits` / `Build` 签名不变；`cmd/semantix/lookup.go:167` 与 `gateway/pipeline.go:109` 不动。
- 这是资格与组装两个阶段之间的一条 seam：`Eligible` 是资格阶段的出口，`Assemble` 是组装阶段的入口，`LexicalSelect` 是二者之间的默认（in-kernel）选择器；外部选择器只能替换 `LexicalSelect` 的输出，不能碰另外两段。

### 2.2 Eligible 谓词与 window 语义

- **window 谓词**（决定谁占 K 槽、谁进 eligibleScores / Top1 / TopMargin）**逐字复用 #488 pass-1**：`h.Slice != nil && admissionTypeEligible && taskAdmits && freshnessReason == "" && Origin.Level() >= MinOrigin.Level()`。`in.K>0 && ((eligible && len(eligibleScores)>=K) || (!eligible && rank>=K))` → 跳过；K=0 不封顶；ineligible 且 rank≥K 的 hit 不进入诊断（#488 语义）。
- **不含** `library_too_small`、`type_sources_too_few`、`sanitized_empty`：它们不影响 window 与 Top1/TopMargin。
- **post-window 拒绝**（在 `Eligible` 内、window 之后、按此序）：`nil_slice`、`type_not_allowed`、`result_probation`、`origin_below_floor`、freshness 六种、`library_too_small`、`type_sources_too_few`、`task_type_mismatch`、`sanitized_empty`。它们把候选从 `Candidates` 移到 `Rejected`，不改 window 成员、`EligibleCount` 与 Top1/TopMargin。因此 `len(Candidates) <= EligibleCount`，且 `TopMargin==0` 不足以区分"只有一个 window 成员"与"两个同分"——所有依赖 window 计数的门只看 `EligibleCount`。
- `LexicalSelect` 的 runner-up 门定义为 `RequireRunnerUp && set.EligibleCount < 2`（复现 #488 `len(eligibleScores) < 2`），**不是** `len(set.Candidates) < 2`。反例（golden fixture）：hit A（Context，分 5.0，window 合格，其类型来源会话 <2）+ hit B（Memory，分 4.0，全门通过），`RequireRunnerUp=true`、`MinTopMargin=0`：#488 admitted={B}；若以 `len(Candidates)` 判 runner-up 则 B 被误拒。
- 同一谓词提前到 S1b-lifecycle（#488 合并后）以附加字段 `CandidateDecision.Eligible bool`（json `eligible,omitempty`）落地，镜像到 `event.RetrievalCandidate.Eligible` 与 `eventwire`；测试 `TestCandidateDecisionEligibleIndependentOfFirstHitReason`、`TestEligibleFalseForEveryWindowGate`（表：`type_not_allowed`、`result_probation`、`origin_below_floor`、`current_commit_unknown`、`commit_unknown`、`stale_commit`、`path_missing`、`dependency_path_invalid`、`dependency_changed`、`task_type_mismatch`）、`TestEligibleTrueForPostWindowRejects`（`library_too_small`、`type_sources_too_few`、`sanitized_empty` 时 `Eligible=true`）、`TestWireRetrievalMirrorsEveryField`（反射）。

### 2.3 什么保持逐字节一致，什么改变

| 项 | 结论 |
|---|---|
| admitted SET、Top1、TopMargin、zone 判定、`Text` 字节、admitted IDs | 与 #488 **逐字节一致**；golden `TestEligibleSelectAssembleMatchesBuildHitsGolden`（#488 memory_flow_e2e + 既有 inject fixtures）；新增 fixture：sanitized-empty 的 top hit、too-few-sources 的 top hit、**too-few-sources top hit + 一张干净 runner-up（`RequireRunnerUp=true`、`MinTopMargin=0`，§2.2 的 A/B 反例）及其 sanitized-empty 孪生**、K-window 溢出（合格数 >K）、K=0 |
| 多门失败候选的 first-hit `Reason` | **改变**：`task_type_mismatch` 与 `sanitized_empty` 前移到软门之前 → `admissionVersion=2`；同 PR 修订 `semantix-l2-admission-policy.md` §4 判断顺序表 |
| kernel import 集 | `TestInjectPackageImportsExcludeNet`：`go/parser` 遍历 `kernel/inject` import，拒绝 `net`、`net/http`、`os/exec`（approval condition 1 机械化） |

### 2.4 Assemble 与 post-wait 重校验

- Bridge 在调用 `Assemble` 前用既有 `readGitHead` **重新读取 HEAD 并覆盖 `Injector.CurrentCommit`**（`freshnessReason` 以 build 时捕获的 `CurrentCommit` 比较，仅重跑函数检测不到 HEAD 变化）。HEAD **每 turn 只读一次**：在选择器响应到达后（或词法路径进入 `Assemble` 前）读入 memo 槽，full / degraded / warm 的三次 `Assemble` 共享同一值——`readGitHead` 在 HEAD 非松散 ref（packed refs、worktree）时回退 `git rev-parse` 子进程（2 s 超时），不能每次 `Assemble` 都跑。
- `Assemble` **按 `set.Candidates` 的存储顺序**（BM25 降序、ID tie-break，与 `bm25.Search` 输出一致，即 BuildHits 的 hit 序）迭代，对每个候选查 `sel.ByID`：未 kept → `selector_not_selected`；kept 但 ID 不在 set → `selector_unknown_id`，丢弃；kept → 重跑 `freshnessReason`（Deps 指纹 + 新 HEAD）→ 既有 exact-byte budget（`formatSliceItem` 输出，按 hit 序累加）→ canonical sort → provenance 行保留 BM25 `score=%.4f`。budget 按 hit 序累加是 §2.3 逐字节一致的前提，绝不按 `ByID`（map）或其他顺序迭代。
- `sel.Source=="external"`：`Grey=false` 恒成立；`CandidateDecision.Zone/Score` 为词法原值。
- `Selection` 与 budget 无关；`Assemble` 依赖 budget；full / degraded / warm 共享一次 `Selection`、各自组装。
- 测试：`TestAssembleDetectsHeadChangeDuringWait`、`TestAssembleRevalidatesDepsAfterSelection`、`TestAssembleDropsUnknownSelectedID`、`TestExternalSelectionNeverSetsGreyZoneOrScore`、`TestAssembleHonoursDegradedBudget`、`TestAssembleIteratesCandidatesInHitOrder`、`TestHeadReadOncePerTurn`。
- 成本：S7 前先度量 `freshnessReason` / `fingerprint.Verify` 对每个 hit 每 turn（含 warm）的耗时，以及 `readGitHead` 在 packed-refs / worktree 仓库上的耗时，再决定 `Assemble` 二次校验的范围。

### 2.5 Reason v2 与 fallback 闭集（`admissionVersion=2`）

- `CandidateDecision.Reason` v2 = v1 全部 + `selector_not_selected`、`selector_unknown_id`、`selector_not_judged`（memo 应用到交集时未见过的卡）、`selector_card_too_large`（`Sanitized` 超单卡上限，本地标 `insufficient_evidence`，不发送）。
- `selectorFallbackReason` 闭集：`turn_identity_missing`、`backend_info_unavailable`、`backend_egress_mismatch`、`backend_version_mismatch`、`backend_changed`、`backend_declined`、`protocol_error`、`transport_error`、`timeout`、`request_too_large`。
- `selectorOutcome=discarded` 的原因记入同一 `selectorFallbackReason` 字段：`new_turn`、`cancelled`、`closed`、`fused`、`freshness_lost`。

## 3. Harness 选择器接口与 Bridge 生命周期合同

### 3.1 接口（`harness/semantix/selector.go`，`harness/semantix` 保持 leaf）

```go
type RelevanceSelector interface {
    Info(ctx context.Context) (BackendInfo, error)
    Select(ctx context.Context, in SelectInput) (SelectOutput, error)
}
type SelectInput struct { QueryText string; SanitizeVersion string; Cards []SelectCard }
type SelectCard struct { Alias string /* c1..c5 */; Type, Layer, TaskType, Text string; Verified bool }   // 无 Project/Stats/Weight/Deps/paths/feedback
type BackendInfo struct { ID, ModelVersion, TemplateVersion, CalibrationID, Egress, OutputLicense string; Tasks []string; Judgements map[string][]string; MaxCandidates, MaxRequestBytes int }
type LabelP struct { Label string; P float64; Probs map[string]float64 }
type Verdict struct { Applicability LabelP; Extra map[string]LabelP }
type SelectOutput struct { Backend BackendInfo; Verdicts map[string]Verdict /* alias → */; Usage SelectorUsage; Status string /* ok|declined */; DeclineReason string }
type SelectorUsage struct { InputTokens int; CostUSD float64; Billed string /* known|unknown */ }
```

- `Bridge.Config.Selector SemantixSelectorConfig`、`Bridge.Config.SelectorClient RelevanceSelector`（boot 注入；nil ⇒ 行为等同 `bm25`）。`harness/selectorhttp/adapter.go` 把 `internal/scorer.Client` 适配为 `RelevanceSelector`；boot 用 `netclient.NewHTTPClient` 构造。
- v1 选择规则：keep ⇔ `Applicability.Label=="applicable" && P >= min_p`；其他 judgement 只进诊断。
- `TestSelectorCardHasOnlyAllowlistedFields`（反射）：`SelectCard` 字段集精确等于上述六项。

### 3.2 Bridge API 与调用流程

- `type InjectRequest struct { Query string; Turn int64; Degraded bool; Origin string /* "sync" | "warm" */ }`；`func (b *Bridge) InjectTurn(ctx context.Context, req InjectRequest) InjectResult`；`InjectResult.SelectorOutcome`。`InjectDetailed` / `InjectDegradedDetailed` 保留为薄包装（Turn=0，**仅供既有测试**；CLI 不经 Bridge——`cmd/semantix/lookup.go` 直接调用 `inject.Injector.Build`）。agent 侧非测试调用点恰好三处（Bridge 自身的 `Inject`/`InjectDegraded` 包装除外）：`harness/agent/run_loop.go`（同步 full / degraded）与 `harness/agent/agent.go` `startInjectWarm`，全部改走 `InjectTurn`。`(*Bridge).InvalidateTurn(turn int64, reason string)`。
- 流程：`collect := { open store; Search(全库); Eligible; LexicalSelect; 复制 EligibleSet; closeSliceStore }` → 若 `strategy==bm25 || mode==off || len(Candidates)==0` → `Assemble(lexical)` 返回（0 请求、既有拒因）→ 若 `Turn==0` → 词法路径 + `selectorFallbackReason=turn_identity_missing`（非静默）→ 否则取 memo → 派发 → 校验 → 映射为 `Selection` → 读 HEAD 一次入 memo → `Assemble`（调用方自身 budget）。
- 谁派发：`strategy=local`（enforce）只在同步 `beginRunTurn` 路径派发（#494 §6.2 定义），warm 只读 memo。`strategy=local-shadow`：派发**异步**、在 Bridge 拥有的 per-turn ctx 上，绝不延迟 provider 请求；verdict 到达后以 `turn+candidateKey` 为键写入同一 `RetrievalDiagnostics`（补记，不新增 event op）；同一 memo / 单飞代码路径。shadow 延迟证据不可转移到 enforce，enforce 的同步等待成本在 S8 单独度量。
- **shadow 下的 selector 字段定义**（任何 `*-shadow` strategy）：`selectorRequested=<strategy>`、`selectorActual="bm25"`（provider 可见输出永远是词法 Selection）、`selectorOutcome` 记 verdict 的到达状态、`selectedIds` 为 would-select；trace 行 `provenance.influenced_by` 列出**被咨询**的每个 backend（含 shadow），`model_influenced` 仍按 `selector_actual` 判定（§7.1）。

### 3.3 生命周期合同（可测需求）

每条需求给出编号、陈述与验收测试名；测试复用既有单测 / mock / httptest，不新增 CI 平台。

| # | 需求 | 测试 |
|---|---|---|
| L-1 turn 身份 | `beginRunTurn` 在 `Add(1)` 后传 `a.semantixTurn.Load()`；`startInjectWarm` 在 `go func()` **之前**捕获 `turn := a.semantixTurn.Load()` 并用它盖 `prefetchedInjectResult.Turn`（修 `agent.go:3121` 完成后才盖 turn）。每个 agent 调用点都传捕获的 turn；`Turn==0` 不静默。 | `TestInjectWarmStampsTurnCapturedAtSpawn`、`TestEveryAgentCallSitePassesCapturedTurn`（枚举恰好三处：`run_loop.go` 同步 full、同步 degraded、`agent.go` `startInjectWarm`；`go/parser` 断言 `harness/agent` 无其他 `InjectDetailed`/`InjectDegradedDetailed` 调用）、`TestSelectorTurnZeroRecordsFallbackReason` |
| L-2 每 turn 单次派发 | memo 单槽 `turnSelection{turn int64; candidateKey string; done chan struct{}; cancel context.CancelFunc; outcome string; selection inject.Selection; verdicts; backend BackendInfo; usage; latencyMs}`；状态 inflight → selected \| empty \| abstain \| fallback(reason) \| discarded(reason)。key=(Bridge 实例, Turn)。同 turn 第二个调用者（sync、warm、provider 重试）等待 `done`（受自身 ctx 约束），永不重派。`candidateKey` 不一致 ⇒ 不重派，memo 应用到交集，未见卡 `selector_not_judged`。 | `TestSelectorSingleDispatchPerTurn_SyncThenWarmThenRetry`、`TestSelectorCandidateSetChangedNoRedispatch`、`TestBridgeOwnedByExecutorOnly`（`boot.go:1739` 只有 executor 拥有 Bridge；若未来 subagent 共享 Bridge，`InjectRequest` 需加 `AgentID` 并入 key，记为 v2 条件） |
| L-3 sync 与 warm 共享一次 Selection | full / degraded / warm 共享同一 `Selection`，各自按自身 budget `Assemble`。 | `TestDegradedAndFullBudgetShareOneSelection`、`TestPinnedBlockStableAcrossProviderRetry` |
| L-4 terminal-decision latch | `a.turn.selectionTerminal` **只**由 `InjectResult.SelectorOutcome != "none"` 置位；`startInjectWarm` 在 `injectBlock!=""||injectionFused||selectionTerminal` 时返回；`takePrefetch` 在 `selectionTerminal` 时拒绝预取块。**bm25 拒绝不置位**：bm25 同步 miss 必须保留 `sampling_request.go:111-112` 的 N12 prefetch 回退。 | `TestSelectorRejectNotReinjectedByWarm`、`TestBM25SyncMissKeepsPrefetchFallback`、`TestInjectWarmSkippedWhenBlockLockedOrFused` |
| L-5 迟到响应丢弃 | 接受条件 = memo.turn 仍为活槽 && `request_id` 回显 && 返回 alias 集合 == 发送集合 && 槽未 discarded。新 turn 驱逐槽并 cancel；`Bridge.Close` cancel 并等待 `selectorWG`；`InvalidateTurn` 由 `armLoopGuardPass`（`progress_guard.go:178`）与 `RecordInjectionReject` 调用。**被丢弃的响应永不触发词法回退。** | `TestSelectorLateResponseDroppedAfter_{NewTurn,Close,Fuse,Cancel}` |
| L-6 锁范围 | 无 FileStore 句柄 / flock、无 `b.mu` 跨越 HTTP；store 在 `collect` 结束即关闭。 | `TestSelectorCallDoesNotHoldStoreLease`（mock handler 内取 `LOCK_EX\|LOCK_NB` 必须成功） |
| L-7 timeout 与 ctx | `context.WithTimeout(min(caller ctx, Bridge per-turn ctx), timeout_ms)`；任何状态不重试（401 / 422 / 429 / 5xx / 529 / timeout）；每 turn 一次。`injectResult` 在 Search 前与 Assemble 前检查 `ctx.Err()`；detached warm 用 `context.WithTimeout(context.WithoutCancel(ctx), warmTimeout)` 包裹。 | `TestInjectResultHonoursCancelledContext`、`TestSelectorFallbackToLexicalRecordsActual` |
| L-8 degraded budget | 降额模式与预取消费均遵守当前 budget；共享 Selection 不等于共享一个无视 budget 的块。 | `TestAssembleHonoursDegradedBudget`（§2.4） |
| L-9 fuse | negative-transfer fuse 触发后当轮不再恢复历史块；warm 不在 fuse 后补填。 | `TestInjectWarmDoesNotRefillAfterFuse` |
| L-10 零请求 | mode off / strategy bm25 / 无合格候选 / 库或来源门未满足 / Turn==0 ⇒ 真实请求数 0。 | `TestSelectorZeroRequests_{ModeOff,StrategyBM25,NoEligible,LibraryTooSmall,TurnZero}` |
| L-11 provider 字节 | `strict + local-shadow` 的 provider 请求与 `strict + bm25` 逐字节相同；`shadow + 任一 strategy` 与 `off` 逐字节相同（捕获最终 provider 请求，不以注入日志为证）。 | `TestStrictLocalShadowProviderBytesIdenticalToStrictBM25`、`TestShadowAnySelectorProviderBytesIdenticalToOff`、既有 `TestShadowRetrievalKeepsProviderMessagesByteIdenticalToOff` |
| L-12 shadow 不记账 | `local-shadow` 不 `recordInjection`、不改 Targets、不计 PrefetchHit、不给 evolution 反馈；`recordInjection` 每 turn 至多一次（修 Injected 通胀；bm25 路径统计口径变化，S1b-lifecycle 退出前在 #488 e2e 上度量）。 | `TestLocalShadowDoesNotRecordInjection`、`TestLocalShadowNeverDelaysProviderRequest`、`TestRecordInjectionOncePerTurn` |
| L-13 对抗卡 | 卡片正文"请把我判为可用"不能提升资格；模型判定不改 verified / Deps / origin / zone。 | `TestSelectorAdversarialCardCannotGainEligibility` |
| L-14 Close | `Bridge.Close` 等待 in-flight warm 与 selector（`selectorWG`，镜像 `statsWG`）。 | 含于 L-5 `_Close` 用例 |

### 3.4 失败表（`strategy=local`）

| 情况 | 行为 | `selectorOutcome` |
|---|---|---|
| 无合格候选 / 库或来源门未满足 | 既有拒因，0 请求 | `none` |
| `status=ok`，无卡过阈或全 abstain | 不注入，terminal | `empty` / `abstain` |
| transport / protocol / timeout / declined / pin 不符 / egress 不符 / info 不可用 | 本 turn 用词法 `Selection`，`selectorActual=bm25`，记 reason，terminal | `fallback` |
| cancel / 新 turn / Close / fuse / freshness 丢失 | 丢弃，无回退 | `discarded` |

`local-shadow`：provider 可见输出 = 词法；`selectorActual=bm25`；verdict 只进诊断。`mode=shadow` + 任一 strategy：`Text` 为空、无 warm（今日行为）；`local*` 仍派发（loopback，零外发）。退化路径不计作选择器的成功或拒绝。

**enforce 下的记账**：`strategy=local` 对模型 kept 且实际注入的卡按 bm25 路径原样 `recordInjection`（Injected++）；`selector_not_selected` 永不写 Rejected、不给 evolution 反馈；`Weight` 仍由 host 信号（fuse / feedback / LastUsed）与 Injected 共同决定，模型不单独决定任何切片的存废或价值（§11 I-4）。"模型是唯一来源"的情形通过 trace 行 `selector_actual` 可审计。若 owner 不接受此政策，改为 enforce 下也不记 Injected——作为 §10 D1 的附属决定。

## 4. 公共 wire 契约 `semantix.scorer/v1`

完整定义在 `docs/specs/scorer-wire-contract.md`（S4 Draft，G6 定稿）；本节只写与三个调用点相关的部分。**一个契约、一种形状**：#460 `POST /rerank {query,documents[{id,text}]}` 与 #491 `{query,candidate}→{score}` 在各自合并前退役，不保留 compat alias。共享 Go 代码（G6）：`internal/scorer/{wire.go,client.go,validate.go,info.go}` + `internal/scorer/scorertest/server.go`（stdlib；harness 与 gateway 共用；kernel 不 import）。产物：`docs/contracts/scorer-v1.schema.json` + goldens `internal/scorer/testdata/scorer_v1/{req_l2_select,resp_ok,resp_declined,resp_missing_id,resp_dup_id,resp_bad_enum,resp_bad_probs,info_local,info_third_party}.json`。

### 4.1 `GET {base}/v1/info`（不携带用户数据）

```json
{"schema":"semantix.scorer/v1",
 "backend":{"id":"<opaque>","model_version":"...","template_version":"...","calibration_id":"...","egress":"none|third_party","output_license":"unrestricted|restricted_no_train"},
 "tasks":["l2_select","l3_reuse","rerank"],
 "judgements":{"l2_select":["applicability"]},
 "limits":{"max_candidates":5,"max_request_bytes":24576}}
```

- 未声明 `output_license` ⇒ 视为 `restricted_no_train`；未声明 `egress` ⇒ 视为 `third_party`（fail-safe）。**这些是后端自述值**：它们驱动客户端的拒绝路径与 trace writer 的丢弃，但不是身份证明；训练侧对 backend 身份的白名单在私有仓另行强制（§6.4）。
- `backend.template_version` = 后端实际套用的**模型侧输入模板**标识（不透明串；与 trace 行的 `slice_template_version` 是两个独立字段，§7.1）。
- `backend.calibration_id`：不透明串；**`"none"` 为保留值 = 未校准**，客户端在 `strategy=local`（enforce）下遇 `calibration_id=="none"` ⇒ `backend_version_mismatch` 回退（未校准后端只能服务 `*-shadow`）。
- `strategy=local*` 要求 `egress=="none"`，否则每 turn `backend_egress_mismatch`、0 次 score 请求。
- re-handshake：info 按 Bridge 惰性缓存；初次失败或响应 backend 块 ≠ 缓存（`backend_changed`，含 SIGHUP 热重载）时，允许每 turn 至多一次无用户数据的 re-probe，且只在本 turn 派发之前；re-probe 成功后若 `model_version` ≠ `expect_backend` pin ⇒ `backend_version_mismatch` 回退；re-probe 永不导致同 turn 第二次 score 请求。

### 4.2 `POST {base}/v1/score`

```json
{"schema":"semantix.scorer/v1","task":"l2_select","request_id":"<128-bit hex，非 session/turn 派生>","deadline_ms":800,
 "query":{"text":"<kernel/sanitize 输出>","sanitize_version":"..."},
 "candidates":[{"id":"c1","type":"context|memory|result","layer":"repo-ops|subsystem-overview|plan-skeleton|outcome|","task_type":"...","text":"<EligibleCandidate.Sanitized>","facts":{"verified":true}}],
 "judgements":["applicability"]}
```

```json
{"schema":"semantix.scorer/v1","request_id":"<echo>","status":"ok|declined","decline_reason":"unsupported_language|unsupported_task|over_budget|...",
 "backend":{...同 /v1/info...},
 "results":[{"id":"c1","score":0.83,"judgements":{"applicability":{"label":"applicable|inapplicable|insufficient_evidence","p":0.83,"probs":{"applicable":0.83,"inapplicable":0.12,"insufficient_evidence":0.05}}}}],
 "usage":{"input_tokens":1234,"output_tokens":0,"cost_usd":0.0,"billed":"known|unknown"}}
```

- 请求为闭合 Go struct，无 `map[string]any`。`task` 为开放字符串（`l2_select` | `l3_reuse` | `rerank` 已定义；未知 task ⇒ `status=declined/unsupported_task`；#493 未来 `equivalence` 无需改 schema）。
- **字段白名单即出境上界**：不含 project slug、真实 slice ID、store 路径、session ID、Stats、Weight、feedback、Deps、向量。`TestRequestFieldWhitelist` 断言精确 key 集。编译常量 `ScorerRequestFieldsVersion = "scorer-fields-v1"` 随白名单变更递增。
- 超单卡上限的卡不发送、本地 `selector_card_too_large`；总体 > `max_request_bytes` ⇒ 不派发、`request_too_large`。响应 `io.LimitReader` 64 KiB。
- `score` = 该 task 主正标签的校准概率（`l2_select`: applicable；`l3_reuse`: reuse_safe；`rerank`: relevant）。
- v1 只有 `applicability` 是强制且枚举冻结的 judgement；其他 judgement 名（added_value / conflict / use_role 等，#494 §4.2）不在 v1 冻结，backend 在 `/v1/info.judgements` 自声明，客户端原样记入 `Verdict.Extra`。
- 校验（任一失败 ⇒ `protocol_error`）：schema 相等；`request_id` 回显；结果 id 集合 == 发送 alias 集合；label ∈ 枚举；p∈[0,1]；probs 和 ∈ 1±0.02；backend 块 == info（否则 `backend_changed` 走 §4.1）；`model_version` == pin。响应中除 alias→(label,p) 外任何内容不进入 provider 请求。
- 版本：major 在 schema 串；v1 内附加可选字段、新 task 串、backend 自声明 judgement 为非破坏；双方忽略未知字段；改既有 judgement 枚举或字段语义 ⇒ `semantix.scorer/v2`。

### 4.3 各调用点失败语义（客户端返回 `(resp, err)`，永不吞错；策略在调用方）

| 调用点 | 消费者 | 失败语义 | 阈值所在 |
|---|---|---|---|
| `l2_select` | harness `Bridge`（§3） | 见 §3.4：fallback 到词法 / discarded；每 turn 一次、不重试 | `[semantix.selector] min_p` + `expect_backend`；**无公共默认**，校准值只在 `semantix-models/deploy/harness/selector-local.toml` |
| `l3_reuse` | #491；`gateway/score_judge.go` 实现 `judge.Judge`（在 `gateway/` 而非 `kernel/judge`；由既有 `CachedJudge` 包裹） | **FAIL-CLOSED**：error ⇒ Reject + JudgeError，不缓存；Confirm ⇔ `score >= judge_score_threshold`；`CachedJudge` key 含 `model_version` 与阈值 | `[cache] judge_score_threshold`（必填、无公共默认）、`judge_promote_threshold`（可选，实现 `VariantJudge`）；校准值只在 `semantix-models/deploy/gateway/*.toml` |
| `rerank` | #460 重落地 `gateway/rerank.go` | **FAIL-SOFT** 回内层顺序；`Hit.Score` 永不覆写，语义分放附加 `SemScore/SemValid`（同 `Lexical/LexicalValid` 模式）；zone / MinScore / margin 继续基于 BM25/fused；Go 侧自行排序、不信任服务端顺序；装饰器**不得**包裹 L3 共享的 index | `[retrieval] rerank_mode = "shadow"（默认）\| "order"`；**无** `score` 模式 |

`l3_reuse` 补充：`judge_protocol` 白名单加 `local`；local 下 `judge_api_key` / `judge_model` 可选；接线门从 `JudgeAPIKey != ""`（`gateway.go:144`）改为 base_url + protocol；`promote_consensus` 与 local 组合必须为 1，或设置 `judge_promote_threshold`，否则校验错误。理由：`gateway.go:166` 把 judge 包成 `judge.CachedJudge`，而 `CachedJudge` 实现 `VariantJudge`，其 `ConfirmSecondary`（`kernel/judge/cache.go:124-146`）在内层不是 `VariantJudge` 时回退到内层的普通 `Confirm`——所以 `kernel/cache/l3.go:496-499` 的 non-Variant 分支（`approve=false`）**不会被触达**，`promote_consensus=2` 会用同一个确定性分数"确认"两次（伪共识），而不是静默不 promote。`judge_promote_threshold` 让 `ScoreJudge` 自己实现 `VariantJudge`（更严的第二阈值），才构成第二视角；`usage.JudgeDecision` 附加 `score`、`backendId`、`modelVersion`、`protocol`、`cached`。相似度 cross-encoder ≠ 六维 reuse-safety rubric；验收跑 judge-audit.tsv 而非 QQP。eval-judge / calibrate / verify 三个 CLI 构造点接受 local。

### 4.4 目的地类别

- v1 唯一目的地类别 = loopback（`validateLoopbackURL`，明文 http 允许）+ `egress=none`。
- `remote` / `remote-shadow` 为保留值，v1 拒绝；未来单独 spec 的最低要求见 §6.3。本文不实现。

## 5. 配置键、加载期校验、事件字段

### 5.1 配置（`harness/config`）

```toml
[semantix]
mode = "off"            # off | shadow | strict ；未知值 => 加载错误（新行为）
grey_mode = ""          # "" | drop | audit ；新键（见下）；未知值 => 加载错误
trace_log = ""          # 可选；路径；0600；user-global pin；空=关

[semantix.selector]     # 整表 user-global pin（cfg.Secrets 模式，load.go:150-174）
strategy = "bm25"       # bm25 | local-shadow | local ；remote-shadow | remote 为保留值，v1 => 加载错误 "reserved"
base_url = ""           # local* 必填；必须通过 validateLoopbackURL
timeout_ms = 800        # 50..1000
max_request_bytes = 24576
min_p = 0.0             # 无公共默认；strategy=local 且 min_p==0 => 加载错误
expect_backend = ""     # "<id>@<model_version>"；strategy=local 必填
```

Go：`type SemantixSelectorConfig struct { Strategy string; BaseURL string; TimeoutMS int; MaxRequestBytes int; MinP float64; ExpectBackend string }`（toml tag 同键名）；`SemantixConfig.GreyMode string \`toml:"grey_mode"\``；`SemantixConfig.Selector SemantixSelectorConfig \`toml:"selector"\``；`SemantixConfig.TraceLog string \`toml:"trace_log"\``。

- **`grey_mode` 现状与落地**：origin/main 与 #488 的 `harness/config.SemantixConfig` **没有** `grey_mode` 键，`boot.go` 构造 `semantix.Config` 时不设 `GreyMode`；`semantix.Config.GreyMode` 只存在于 `bridge.go:45-50` 并在 `:311` 以 `AllowGrey: b.cfg.GreyMode == "audit"` 消费——`grey_mode=audit` 在当前基线**不可达**。决定：S1b-config PR 新增 `[semantix] grey_mode`（`SemantixConfig.GreyMode string \`toml:"grey_mode"\``，值 `""`/`drop`/`audit`）并经 `boot.semantixBridgeConfig(cfg)` 映射到 `semantix.Config.GreyMode`；实现即合并本地未推送的 wt-grey-mode 两提交（若已推送则 rebase 复用）。S2 的 audit 人群、D4 的 `FreshnessClass` audit-only 可达性都以此为前提。
- **加载期校验**：`validateSemantix()` 从 `Config.Validate` 调用；未知 `mode`、未知 `grey_mode`、未知 `selector.strategy` **同一 PR 内**全部变为加载期 ERROR（今日未知 `mode` 静默等于 `off`，`bridge.go:140-141`；release note 标注行为变化）。`RenderTOML` 打印每个键（`TestSemantixConfigRenderRoundTripAllKeys`，反射遍历 `SemantixConfig`）。落地纯函数 `boot.semantixBridgeConfig(cfg)`（`TestSemantixBridgeConfigMapsEveryField`，含 `GreyMode`）；不再有未接线键。
- **user-global pin**：`[semantix.selector]` 整表与 `trace_log` 为具备外发/持久化能力的键，`LoadForRoot` 整体恢复 `cfg.Semantix.Selector` 与 `cfg.Semantix.TraceLog` 为 user-global 值；project TOML 不能设置（`TestProjectTomlCannotSetSelectorOrTraceLog`）。
- 分期：S1b-config 只落 `grey_mode`、`strategy`（仅接受 `bm25`）、`trace_log` 与校验；其余键随 G6 启用（键名现在冻结）。
- 网关键（S7 与 #460/#491 重落地）：`[cache] judge_protocol += "local"`、`judge_score_threshold`、`judge_promote_threshold`；`[retrieval] rerank_base_url`、`rerank_timeout_ms`、`rerank_top_n`、`rerank_mode`。
- 测试：`TestSemantixUnknownModeIsLoadError`、`TestSemantixUnknownSelectorIsLoadError`。

### 5.2 事件 / 诊断字段（附加、`omitempty`；`harness/event/event.go` 与 `harness/eventwire/wire.go` 双写）

S1b-lifecycle 只加 `RetrievalCandidate.eligible`。以下名字现在冻结、G6 启用：

- `RetrievalDiagnostics` += `admissionVersion int`、`turn int64`、`candidateKey string`、`selectorRequested string`、`selectorActual string`（任何 `*-shadow` 与 fallback 下恒为 `bm25`）、`selectorOutcome string`（`none|selected|empty|abstain|fallback|discarded`）、`selectorFallbackReason string`（§2.5 闭集）、`selectorBackend string`、`selectorModelVersion string`、`selectorTemplateVersion string`、`selectorCalibrationId string`、`selectorEgress string`（`none|third_party`）、`selectorOutputLicense string`（`unrestricted|restricted_no_train`）、`selectorCalls int`（0|1）、`selectorLatencyMs int64`、`selectorUsage{inputTokens int; costUsd float64; billed string /*known|unknown*/}`、`selectedIds []string`（would-select；`finalOrder` 保持 provider-visible 语义）。
- `RetrievalCandidate` += `lexicalKeep bool`、`judgements map[string]{label string; p float64}`。
- **受限后端的 wire 封口**：当 `selectorOutputLicense == "restricted_no_train"` **或** `selectorEgress != "none"` 时，所有由后端 Output 派生的字段都不上 wire：`judgements` 省略、`selectedIds` 置空、`selectorOutcome` 只允许 `none|fallback|discarded`（不写 `selected|empty|abstain`）；`selectorBackend` / `selectorEgress` / `selectorOutputLicense` / `selectorCalls` / `selectorLatencyMs` / `selectorUsage` **保留**以便审计误申报。`TestRestrictedBackendLeavesNoDerivedFieldsOnWire`。trace writer 对同类行同样置空 `decision.selected_ids`（§7.1）。
- usage source：`UsageSourceMemorySelector = "memory-selector"`（`event.go:616-624` 常量块）；timeout ⇒ `billed=unknown`，永不记 0。
- 不新增 `kernel/event` Kind；不新增 `KernelCache op` 枚举值。诊断按 slice ID join（#488 已如此），永不按位置对 hits join；决策顺序确定性（BM25 序、ID tie-break）。
- 分层严格分开（#494 §7）：`retrieved → eligible → selected → provider-visible → adopted/unknown → task outcome`。

## 6. 第三方后端策略

### 6.1 原则

1. **默认关闭，永远不是默认值**：`strategy` 默认 `bm25`；`remote*` 在 v1 为加载错误；任何版本都不把 `remote*` 作为默认；不因检测到某个环境变量而自动启用。
2. **外发同意是按次审批，脱敏不是同意**（#494 approval condition 3）：每次真实第三方调用前，owner 书面批准预算与外发字段清单。
3. **公共仓零标识**：代码、配置、事件、spec、测试不出现供应商名、模型名、价格、URL。legal 说不时公共 diff 为零。
4. **第三方模型 Output 结构性排除于所有训练 / 标签导出**（§6.4）。
5. **法律姿态统一且保守**：在人读过实际条款、供应商书面答复前，所有第三方用途均 pending；任何 agent 不得注册账号或接受条款。

### 6.2 工程政策（不做法律结论）

一些托管模型服务的客户条款限制把其服务或输出用于模型蒸馏、模仿训练、或开发相似 / 竞争产品，并限制把服务作为独立服务再提供。具体条款是否允许"仅评估的比较"、以及本仓开发的本地 matcher 是否落入"相似 / 竞争产品"，**是 owner / legal 的判断，本文不给结论**；条款原文、编号、快照与解读笔记只在私有仓。在 owner / legal 书面澄清某个具体供应商之前，本文采取的保守工程政策：

| 用途 | 政策 |
|---|---|
| 蒸馏 teacher、soft / hard 标签源、hard-negative 挖掘、伪标签、主动学习采集、student 模型选择 / early stopping / 阈值调参、任何进入训练路径的列 | **禁止**（硬；§6.4 结构性排除） |
| 封存的仅评估比较臂：student vN 与阈值冻结打 tag 后，对冻结 eval-set 版本恰好跑一次；报告 agreement / kappa、false-admit / false-reject；永不反馈训练或选择；结果不公开发表 | **待 owner / legal 书面澄清后**方可（§8 S9） |
| 把第三方重新暴露在 `semantix.scorer/v1` 之后的适配器 | 只能 loopback、单操作者、不为他人部署、不发布；只在私有仓、且仅在 legal + condition 3 之后创建；**v1 运行时没有任何 strategy 能调到它**（`local*` 拒绝 `egress!=none`，`remote*` 为加载错误），其唯一调用者是私有仓的离线比较工具 |
| 运行时 `remote` 选择器 | v1 不实现；未来单独 spec（§6.3） |

供应商身份、价格、版本、条款快照与其笔记只在私有仓 `semantix-models/docs/` 与 `docs/approvals/<id>.md`。

### 6.3 外发同意机制与逐目的地字段清单

任何未来 `remote` / `remote-shadow` spec 至少包含：https + `api_key_env`（只存变量名，env 存在不自动启用）+ `egress_ack == "<backend.id>@<ScorerRequestFieldsVersion>"`（白名单变更即失效，`egress_consent_stale`）+ `spend_limit_usd` 默认 0（= 不调用）+ `CheckRedirect` 拒绝跨主机 + 逐目的地字段清单（与 loopback 清单分别测试）+ 可选 build tag 使默认发布二进制物理不可达第三方主机 + `docs/Security-安全设计.md` 出境章节。

condition 3 审批文件 `docs/approvals/<id>.md`（私有仓）记：approver、date、budget_usd_cap、max_requests、outbound_fields = `scorer-fields-v1`（即 §4.2 白名单：sanitized query text + sanitize_version + 逐卡 alias / type / layer / task_type / sanitized text / verified）、corpus_description、row_count、model pin、api_key_env（只存变量名）、terms_snapshot_sha；retries=0；cap 硬停；timeout ⇒ `billed=unknown`；owner 事先查看样例载荷（卡片含沙箱路径与命令行）。后端特有的门（语言、别名、批形状）写在审批文件而非本文。工具拒绝无 `--approval-id` 启动；env 中存在 key 本身不启动任何调用。

### 6.4 来源标记与训练防火墙

纵深防御，不宣称"结构性不可能"：wire 与 trace 两层都以后端**自述**的 `egress` / `output_license` 为键，一个误申报 `egress=none` 的 loopback 进程能穿过这两层；因此私有仓的训练入口另按 backend **身份**白名单（只接受私有仓 registry 自建 checkpoint 的 `backend_id@model_version`），公共侧保证每条诊断与 trace 行都记录后端自述值以便事后审计。

- **wire 层**：`backend.egress` / `backend.output_license` 由 `/v1/info` 与每个响应携带；未声明按 fail-safe 解释（§4.1）。`selectorBackend` / `selectorEgress` / `selectorOutputLicense` 逐诊断记录（含 shadow）；受限时派生字段封口（§5.2）。
- **trace 层**（`semantix.trace/v1`，§7.1）：`signals[].source.kind` 枚举**没有** `third_party` 值；writer 遇到 `backend.egress != "none"` 或 `output_license != "unrestricted"` 的 verdict 一律不写，并置空该行 `decision.selected_ids`（`TestTraceWriterDropsNonLocalSignals`）。`provenance.influenced_by` 列出被咨询的每个 backend（含 shadow）。`export-traces --purpose=training` 丢弃 `model_influenced=true` 的行，**并且**独立于 `model_influenced` 丢弃 `influenced_by` 含任何 `egress!=none` 或 `output_license!=unrestricted` 后端的行（`TestExportTrainingDropsRestrictedInfluencedRows`）。
- **私有仓层**：训练数据来源与许可类别在私有仓强制（目录白名单、逐行许可类别、backend 身份白名单、无 allow-flag 旁路）；公共导出器的保证只有上两条。细则不在本文。
- **硬顺序门**：S3 exporter 过滤 + S5 私有仓训练防火墙合并且绿**之前**，任何**其输出可能成为标签、分数或选择器判定的**第三方模型调用（teacher、合成 query 生成、比较臂、适配器）一律不得发生。agent 运行与 `--distill` 使用 owner 既有 provider 属既有实践，不受此门约束，但 S2 重提炼的费用与外发在 §10 D4 下明示。

## 7. 与 #490 / #491 / #492 / #493 / #460 / #478 / #488 的关系

### 7.1 #490 = `semantix.trace/v1`（`docs/specs/trace-export-schema.md`）

本文的基石，前置到 S3。一行 JSONL 一个决策事件：

- **record types** `kind` ∈ {`l2_agent`, `l2_gateway`, `l3_judge`, `promote_ledger`, `false_hit`, `verify`}。
- **grouping keys** `group{repo, base_commit, session, turn}`：repo slug 只在 `group` 用于按 repo/session 无泄漏切分，不进入 `candidates`。
- **字段**：`trace_id`（`sha256(kind|session|turn|at)`）、`build{version,commit}`、`sanitize_version`、`slice_template_version`（`slice-template-v1`；导出器套用的公共切片模板，与后端 `backend.template_version` 无关）、`admission_version`、`query{text,sha256,bytes,source ∈ {first_user,turn_user,replay}}`、`observations`（保留恒 null）、`candidates[]{id,type,layer,task_type,origin,verified,text,text_sha256,truncated,eligible,hard_reason,lexical{score,coverage,rank,zone,keep,reason},signals[]{name,label,p,probs,source{kind,backend_id,model_version,template_version,calibration_id}}}`、`decision{mode,selector_requested,selector_actual,selector_outcome,fallback_reason,selected_ids,final_order,provider_visible_ids,bytes,rerank_mode,judge{...}}`、`outcome{adopted,fuse,verify,false_hit,manual_reject,promote,task_result}`、`provenance{producer,model_influenced,influenced_by}`。
- **query 脱敏**：今日无任何日志脱敏 query；导出器 `semantix usage export-traces --purpose=training|evaluation` 是唯一的脱敏 joiner，对 query 文本自跑 `kernel/sanitize` 并盖 `sanitize_version`；`SanitizeVersion` 为空的旧 slice 重跑；路径归一化在 cmd 层；secret-pattern 后检失败的行丢弃并计数（`TestExportTracesNeverEmitsUnsanitizedQuery`、`TestExportTracesDropsRowsFailingSecretScan`、`TestExportTracesGroupsByRepoSession`）。
- **model_influenced** ⇔ `selector_actual != "bm25"` 或 `rerank_mode != "off"`；任何 `*-shadow` strategy 下 `selector_requested=<strategy>`、`selector_actual="bm25"`；`influenced_by` 列出该 turn **被咨询**的每个 backend id（含 shadow 与 fallback 前的尝试）。`--purpose=training` 丢弃 `model_influenced=true` 行（`TestExportTrainingDropsInfluencedRows`），并独立丢弃 `influenced_by` 含受限后端的行（§6.4）；`consensus_failed` 导出为 ambiguous。
- 生产者顺序：`--replay` producer 先行（产品化 `lab/issue-447-calibration/replay.py`，join / 脱敏核心放 `internal/trace/`）→ 运行时 `[semantix] trace_log`（`harness/semantix/trace_log.go`，user-global、0600、默认关、store 关闭后写、每 turn 一行、`TestTraceLogKeepsProviderMessagesByteIdentical`；视 D5）→ #460 `gateway/retrieval_events.go` 改造为 `l2_gateway` 生产者（记 rerank 前 BM25 分）→ `usage.jsonl` → `l3_judge` → #492 台账 → `promote_ledger` → `false_hit` / `verify`。
- #490 原文的"judge 日志 = 蒸馏软标签"加注：现网 judge 是二元 yes/no，无概率；第三方输出结构性排除；哪些信号可作训练标签是私有仓的政策。`docs/Security-安全设计.md` 同步：trace_log 是新持久化文本通道；导出物移往私有仓或 #492 对象存储是**另一次** owner 出境决策。

### 7.2 #491

`semantix.scorer/v1` 的第二个消费者（`task=l3_reuse`），不再有独立 `{query,candidate}` 形状；在 S7 随契约落地；过渡期用 issue 自述的 OpenAI 兼容 shim（零代码）。补齐 issue 遗漏：`judge_score_threshold`、fail-closed、确定性打分器的 consensus 规则、原始分入 `JudgeDecision`、`ScoreJudge` 在 `gateway/`（§4.3）。第三方后端永不作为 `judge_protocol`。

### 7.3 #492

item 2 = `promote_ledger` kind（positive = promote 且后续 verify pass；`consensus_failed` = ambiguous），在 `promote_ttl_seconds` 惰性删除前导出；items 1/3（对象存储布局、失效）独立设计。trace 行上传对象存储是 `Security-安全设计.md` 未允许的新数据流，需单独 owner 出境决策；restricted 内容永不上传。

### 7.4 #493

独立研究；未来可作 scorer/v1 第四消费者（`task=equivalence`，uncertain = reject = abstain 语义，无需改 schema）。共享两条约束：pinned 块文本与顺序 turn 内稳定；动态分数 / verdict 只进审计记录不进 provider 可见文本。本文不为其做工作。

### 7.5 PR #488 / #478：落地顺序与 `query.go` 冲突

- **#488 最先合并**（condition 4 基线）：eligible window 取代 #478 校准 spec P2；接线 `MinOrigin`；把 `Bridge.Close` 移出 `boot.build` 使诊断到达 wire。向 #488 作者提出的**请求**（非合并门）：strict 诊断按 reason 直方图放 `/lab`；说明全库 Search + 每 hit `freshnessReason` 的延迟；承认 `Injector.K` 现有两重含义。报告数字是否移出 `docs/reports/` 取决于 §10 D7（CLAUDE.md 的 `/lab` 规则与 CONTRIBUTING.md:34 的 `docs/reports/` 规则冲突未解前，不向贡献者提出搬迁要求）。**不向 #488 添加任何 selector 改动。**
- **#478 rebase 到合并后的 #488**。`git merge-tree` 显示：`harness/semantix/query.go` **无文本冲突但合并结果不编译**（#488 新增 `if !hasIssue { values[0] = signalBody }`，#478 把 `issue, hasIssue := taggedBody(...)` 换成 `if issue, ok := issueBody(body); ok` ⇒ `undefined: hasIssue`）；`harness/semantix/query_test.go` **文本冲突**。rebase 步骤：修 `query.go` 使之编译；手工合并 `query_test.go`，两个 PR 新增的 query 测试都必须存活；再按 §10 D3b 的语义决议补测试。两者对非 `<issue>` 提示拉向相反方向（#478 剥离模板 token；#488 整体 `signalBody`），**语义取舍是 owner 与 #488 作者的决定**（D3b），本文只给候选方案：`hasIssue` 取自 `issueBody()`；先剥离；剥离后 token 过少时回退整体 `signalBody`（`TestBridgeQueryNonIssuePromptDropsTemplateTokens`、`TestBridgeQueryIssueLabelPromptKeepsIssueBody`）。
- `issue-447-admission-calibration.md` 编辑：删 P2（被 #488 取代）；P1 freshness 分级并入 S2 供给 spec；P3（MinScore 0）、P5（暴露率一级指标）、P6（`--distill --consolidate` 默认供给）保留；P4 键保留但**撤回 `MinCoverage 0.10` 候选默认**（§1.2：放行的卡以无关 outcome 卡为主），coverage 改动等 S6 L1-vs-模型离线评估。合并后在新 main 重跑 replay 再基线：`MinOrigin` 接线与全库 Search 会改变哪些卡到达 freshness 门，reason 分布（含既有的 `stale_commit`）预期变化。

### 7.6 PR #460：拆分

**立即 request-changes**：它现在可合并进 Python 栈（`scripts/ml`、`uv.lock`、11 个 `.pyc`）、覆写 `Hit.Score`、以词法准入做标签（admitted = positive；rerank 开启后自强化）、且未校准 sigmoid 喂 `zone.ClassifyL3`（装饰器包裹 L3 共享 index，是独立的 L3 安全缺陷）。

| 去向 | 内容 |
|---|---|
| **留公共，S7 随 scorer/v1 重落地** | `gateway/rerank.go`（`task=rerank`、`rerank_mode shadow\|order`、附加 `SemScore`、Go 侧排序）；`gateway/retrieval_events.go`（`l2_gateway` 生产者、rerank 前 BM25 分、query 脱敏）；`gateway/config.go` 键 + `validateLoopbackURL`；`gc --score-params`（JSON schema，fitter 移出）；`docs/specs/local-retrieval-model.md` 裁剪为 I-1..I-5 + 指向 scorer/v1，"admitted=positive" 配方撤回；`cmd/semantix/export.go` 不得可作训练源——要么删除，要么输出带 `"export":"raw-dump"` 头且私有 builder 硬拒 |
| **移私有仓** | `scripts/ml`、`pyproject`、`uv.lock`、lab 脚本以 `git subtree split --prefix=scripts/ml` 导入 semantix-models（目标布局与改写清单在私有仓迁移文档）；删 11 个 `.pyc`；`synth_queries` 的第三方 LLM 出境走 §6.3 同一审批流程 |

## 8. 阶段计划

私有预注册数字（floor、margin、样本目标、延迟与校准目标）只在 `semantix-models/docs/selector-eval-plan.md`；公共 issue 与本文只写"达标 / 未达标"。

| 阶段 | 内容 | 进入门 | 退出 | Kill / 分支 | 时间盒 |
|---|---|---|---|---|---|
| S0 | owner 决策（§10）；#494 结论级评论（援引 condition 2、G6 再入门）；备份 `wt-447-calib/lab/issue-447-calibration` → semantix-models `data/raw/`（gitignored）；重写私有仓第三方模型分析文档 §4（删 teacher 角色）；#460 request-changes | 无 | §10 各项决策有答或明确搁置 | 无 | 1 周 |
| S1 | 合并 #488 → rebase #478（§7.5，含 `query.go` 编译修复与 `query_test.go` 手工合并）；删校准 spec P2；撤回 `MinCoverage 0.10` 候选默认；replay 工具移植到合并后 main 并重跑基线 | #488 owner 评审完成；D3b 有答 | `go build ./... && go test ./harness/semantix/... ./kernel/inject/...` 绿；replay 复现"硬门 100% 绑定"（新 reason 名） | 直方图（若作者提供）显示软门拒绝达到预注册比例 → 重开 P1 时机讨论（§1 结论削弱） | 1 周 |
| S1b-config | `[semantix]` 加载期校验、`grey_mode` 键 + `boot.semantixBridgeConfig` 映射、`[semantix.selector]` 表骨架、`trace_log` 键、user-global pin（§5.1）；只触及 `harness/config` 与 `boot.go`，**与 S1 并行** | spec 批准 | 配置测试全绿；`RenderTOML` 往返测试绿 | 无 | 1 周 |
| S1b-lifecycle | 生命周期卫生 spec + PR（§3.3 L-1/L-4/L-7/L-9/L-12/L-14 的今日可落部分：`InjectTurn` / turn 身份 / warm 守卫 / `recordInjection` 每 turn 一次 / ctx / Close）；`CandidateDecision.Eligible` bool（§2.2）；触及 `kernel/inject` 与 `bridge.go` | spec 批准；**#488 已合并到 main**（condition 4；在 #488 之上 rebase，不在未合并基线上实现） | §3.3 对应测试全绿；shadow 字节一致测试绿；bm25 路径 Injected 口径变化在 #488 e2e 上度量 | 无；评审停滞则只出 S1b-config | 1 周 |
| S2 | 供给 spec `issue-447-per-card-deps-and-freshness.md` + 实现（逐卡 Deps + `FreshnessClass`）；重提炼 pilot 库（费用与外发在 D4 明示）；shadow replay | S1 合并；S1b-config 合并（`grey_mode=audit` 可达）；spec 批准；D4 | 生产顺序下 **default 人群**与 **audit 人群**（`grey_mode=audit`）分别达到预注册合格候选 floor | 一轮后若幸存者绝大多数为 Deps-free repo-ops 卡且"同 project + verified command"确定性规则标注与人一致 → 关闭模型选择器线，把规则写进 #478 校准 spec | 3 周 |
| S3 | `trace-export-schema.md`（=#490）+ `--replay` producer 先行 + 事件 `eligible` 镜像；运行时 `trace_log` writer 视 D5 | S1 合并；spec 批准；可与 S2 重叠 | 脱敏测试绿；本地会话导出 JSONL 被私有仓摄取并按 repo/session 分组切分；trace_log 不改 provider 字节 | 无（#490 独立有价值） | 2 周 |
| S4 | 公共 Draft `scorer-wire-contract.md` + `docs/contracts/scorer-v1.schema.json` + goldens（无 Go client）；CONTRIBUTING 契约冻结规则；按 D7 的答案修订 CONTRIBUTING.md:34 / CLAUDE.md 使报告位置规则唯一 | 无；与 S1–S3 并行；D7 有答 | spec 处于 Draft-approved；私有仓 scorer 原型通过 goldens | 无 | 1 周 |
| S5 | 私有仓里程碑（内容见私有计划） | 可立即开始 | 私有仓定义的退出条件；对公共仓的可见结果 = 训练防火墙"绿"（硬顺序门的一半） | 无 | 2 周 |
| S6 | 私有仓里程碑：人工金标与离线臂（内容见私有计划） | S2 + S3 退出 | 私有仓预注册门"达标 / 未达标"；不足则所有结论限定 reject 方向 | 私有仓 kill：出规则不出模型 → 规则写进 #478 校准 spec | 4 周 |
| S7 (=G6) | 私有仓：student 训练、校准冻结、serving 达标（内容见私有计划）；然后公共仓：本文 §2–§3 kernel seam + `internal/scorer` + `local-shadow` 对 mock；#460 Go 重落地 + #491 `ScoreJudge` | 私有仓 S6 退出条件达标 | **G6 四条全满足（无数字）**：student 胜最优词法规则；真概率校准达标；serving 延迟达标；S2 floor 在新 shadow trace 上仍成立。#494 §9 测试矩阵对 mock 全绿 | 私有仓 kill：封存，保留 trace 与 eval set | 6 周 |
| S8 | 配对任务实验 `local` vs `bm25`；默认值单独评审 | S7 退出；`min_p` / `expect_backend` 在 dev 上校准并 pin；shadow 显示合格候选覆盖达标 | 报告选择质量、暴露率、pass / regress、总费用（含 fallback、0 候选） | 无 pass 增益且总费用 / 墙钟变差 → 默认保持 `bm25` | 3 周 |
| S9（可选） | 封存的第三方仅评估比较臂（§6.2） | owner / legal 书面澄清；`docs/approvals/<id>.md`；student vN 与阈值已冻结 | 私有报告 | legal 说不 → 目录永不创建，公共零 diff | — |

**硬顺序门**（范围见 §6.4）：S3 exporter 过滤 + S5 私有仓训练防火墙合并且绿之前，任何其输出可能成为标签、分数或选择器判定的第三方模型调用一律不得发生。

## 9. 非目标

- 不换检索器、不扩大 K、不重新提炼知识以"喂"选择器；rescue 方向（措辞不同、真实匹配）若为目标，属于私有仓 embedding recall 线，不属于本 selector。
- 不重开 Prompt / ToolPattern / 未验证 Result 的注入；模型"applicable"不提升 verified / Deps / origin / zone；模型"inapplicable"不删除、不降权任何 slice。
- 不做语义重排、跨卡去重、跨项目选择；第一版只改选中集合，保留 BM25 排序与 ID tie-break。
- 不在 kernel 新增网络客户端、凭据、跨进程服务；不新增插件平台或通用模型路由框架；不新增 Go 依赖。
- 不实现 `remote` / `remote-shadow`；不在公共仓放任何供应商 SDK、wire 形状、名称。
- 不把注入率当收益；不以"注入率太低"为由下调任何阈值（策略 spec §6）。
- 不在公共仓放实验数字（`/lab` only）、训练脚本、权重、数据、校准值。
- #495（Milvus compose profile + MCP 数据源）与本文完全正交：不涉及 kernel、scorer、trace、selector；不共享本文词汇。

## 10. Owner 决策（各附推荐默认）

| # | 决策 | 推荐 |
|---|---|---|
| D1 P1 时机 | 接受把 #494 P1（kernel seam + mock 接线）推迟到 G6，现在只做卫生 PR + `Eligible` bool？备选 P1-lite：现在切纯本地 `Eligible/LexicalSelect/Assemble` + 进程内 fake selector 测生命周期，无 HTTP client、无契约、无供应商名。同时确认对 #494 已批文本的两处偏离：§4.1 "无跨进程服务" → 允许 loopback 本地 scorer 进程；#494 §5 以供应商命名的 selector 值 → `bm25/local-shadow/local`。附属：enforce 下模型 kept 卡按 bm25 口径记 Injected（§3.4） | **是** |
| D2 法律 | 在读过实际条款并得到供应商书面答复前，第三方零调用；立即重写私有仓第三方模型分析文档 §4 删 teacher 角色。附带：是否书面询问供应商"仅评估比较"与"本地 matcher 是否属 similar product" | **是** |
| D3 PR 处置 | #460 立即 request-changes 并按 §7.6 拆分（subtree 保历史 vs squash）；合并顺序 #488 先、#478 由 owner rebase；向 #488 作者**提出**直方图请求；卫生 PR 是否交给该作者是向其提出的建议，不是计划项 | **是；subtree** |
| D3b `query.go` 语义 | 非 `<issue>` 提示下 #478 的模板 token 剥离与 #488 的整体 `signalBody` 二者取舍（§7.5 给出候选方案）；owner 与 #488 作者共同决定 | **先剥离，token 过少时回退 `signalBody`** |
| D4 freshness 策略 | 批准 S2 供给 spec 含逐卡 Deps + `FreshnessClass`（`content_bound` 默认 \| `path_bound` \| `repo_bound`；仅 `grey_mode=audit` 可达直至校准（前提：S1b-config 落地 `grey_mode` 键）；永不用于 Result 切片与 L3；永不为了给选择器制造候选而放宽）；Deps-free repo-ops 卡在默认模式是否可跨 commit 准入。附属：S2 重提炼 pilot 库要用 owner 既有 provider 对切片内容做一次 `--distill` 调用（费用在私有计划），owner 确认这属既有实践 | **批准 audit-only；默认模式改动待校准后单独评审** |
| D5 trace 放置与 trace_log | 导出物只在 `/lab` 或手工复制到私有仓 `data/raw/`（gitignored）；#492 对象存储不在范围；接受 opt-in、user-global、0600 的运行时 `trace_log`（含脱敏 query 与卡片文本）但推迟到有消费者时落地 | **是** |
| D6 标注与 teacher | 标注计划、teacher 政策与预注册数字在私有仓，owner 在那里批准 | **在私有仓决定** |
| D7 报告位置规则 | CLAUDE.md 的"`docs/reports/`、`docs/specs/` 不再新增实验类文档"与 CONTRIBUTING.md:34 的"报告放 `docs/reports/`"冲突：决定唯一规则并在 S4 修订相应文件；在此之前不向任何贡献者提出搬迁报告数字的要求 | **CLAUDE.md 规则胜，CONTRIBUTING.md:34 改为"结论级验收说明；数字放 `/lab`"** |

默认项（owner 可否决，不答即生效）：公共命名 `bm25|local-shadow|local`；`[semantix.selector]` 子表整体 user-global pin；未知 `mode` / `grey_mode` / `strategy` 变加载错误；v1 只 `applicability` 强制、其他 judgement 不冻结；project 不进 scorer 请求；shadow 异步派发；latch 只由 selector outcome 置位；#491 与 local 组合强制 `consensus=1` 或设 `judge_promote_threshold`；rerank 默认 `shadow`、无 `score` 模式；slice 模板契约移入公共 `docs/contracts/slice-template-v1.md`；报告只在 `/lab`；第三方适配器（若有）仅私有 loopback。

Spec 分级表（R1-R7 / E1-E2）在 origin/main 未找到；本文按惯例判级为 Spec-Required，owner 指认其位置。

## 11. evolution-invariants 逐条对照

- **I-1 原始层不可变：满足。** 选择器只读 `EligibleSet`，不写任何切片；"inapplicable" 不删不降权；trace 导出只读事件与切片。
- **I-2 固化高门槛：满足。** 任何模型进入 enforce 路径以 G6 四条为前提；模型产物只在私有仓；`expect_backend` pin 使模型切换钉在配置边界，rollback = 改回 `strategy=bm25`。
- **I-3 增量进化：满足。** `admissionVersion` 与 backend `model_version` / `calibration_id` 逐诊断记录；阈值只在私有 deploy TOML，绑定 `model_version + calibration_id`。
- **I-4 外部信号锚定：满足。** 训练侧：标签外生于被训模型且不含任何受限服务输出；词法准入结果只作审计列，永不作标签；`--purpose=training` 丢弃 `model_influenced=true` 与受限 `influenced_by` 行，切断自确认环；#460 "admitted=positive" 配方撤回。运行时侧（§3.4）：enforce 下模型 kept 卡按 bm25 口径记 Injected，`selector_not_selected` 永不写 Rejected，`Weight` 仍由 host 信号共同决定，模型不单独决定任何切片的存废或价值；shadow 不记账。
- **I-5 Weight 永不参与检索：满足。** `SelectCard` 与 scorer 请求白名单结构性排除 Stats / Weight / feedback / Deps；`Hit.Score` 永不覆写；zone / MinScore / margin 继续基于 BM25/fused。

## 12. 回滚

按影响面从小到大，每一步都是配置或单开关，不需回退数据：

1. `strategy=local` → `local-shadow`（provider 输入回到词法，诊断保留）；
2. `strategy=bm25`（0 请求；`SelectorClient` 可留空）；
3. `mode=shadow` / `off`（既有回滚路径）；
4. 代码退回用正常 `git revert`，不删除库、不覆盖原实验结果、不删 trace。

`admissionVersion=2` 的 reason 变化不可通过配置回滚；离线回放必须用事件中的 `admissionVersion` 选择判断顺序，不用新顺序覆盖历史记录。

## 13. 验证命令

```bash
go test ./kernel/inject -count=1
go test ./harness/semantix ./harness/eventwire ./harness/event -count=1
go test ./harness/config -run 'TestSemantix|TestRenderTOMLRoundTrips' -count=1
go test ./harness/agent -run 'TestShadow|TestStrictLocalShadow|TestSelector|TestInjectWarm|TestRecordInjection|TestBM25SyncMiss' -count=1
go test ./internal/scorer/... -count=1
go test ./kernel/inject -run TestInjectPackageImportsExcludeNet -count=1
git diff --check origin/main...HEAD
```
