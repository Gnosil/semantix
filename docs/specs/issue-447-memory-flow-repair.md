# Issue #447：在既有 Kernel 框架内修复记忆供给与消费链路

状态：**Draft PR / 正确性修复待评审**。日期：2026-09-18。基于 `ea1af530`。

2026-09-24 本地后续修订：先按第9节提交设计，再逐步提交修复；本地提交不等于更新远端 Draft PR。历史实验使用旧条件，不作为这些修订的效果验证。

关联：[#447](https://github.com/Gnosil/semantix/issues/447)、[四层提炼设计](semantic-four-layer-distill.md)、[L2 准入策略](semantix-l2-admission-policy.md)、[本次实验报告](../reports/issue-447-memory-flow-repair.md)。第1–8节记录原 PR 及本地最小修订；第9节记录完整链路复核、决策和分步状态。计划、实现与验证分别标注，不以实验通过数代替设计验收。

## 1. 目标和不变项

修复真实任务到检索输入、会话镜像到知识卡、来源与反馈持久化、候选选择、运行期资源生命周期的断点。保留现有链路：

```text
Agent 原始任务 → Bridge query → Project BM25 → Injector 资格/准入
  → 带来源的低权威 user-role 历史 → provider → 工具执行/host 收据
  → HarnessSink 会话镜像 → Extract + Distill + ConsolidateContext
  → 原 FileStore / 使用反馈 / evolution → 后续同项目任务
```

不替换 Kernel，不新增记忆架构、SliceType、检索算法或模型提炼服务；保留 BM25/zone、freshness、权限与项目隔离。撤除未经校准的库规模、来源数量和额外词法准入代理条件，而非将它们全部视为安全边界。不把普通 hit、提取成功、非零注入、模型采用和任务收益混为一谈。

## 2. 故障 → 最小修改 → 验收

| 故障或缺口 | 本 PR 修改 | 主要回归 |
|---|---|---|
| 检索使用 provider 能力包装而非原始任务 | retrieval / reuse / prefetch 共用 `turnInput`；普通多行 query 保留完整任务语义 | `retrieval_input_test.go`、`query_test.go` |
| 一个 turn 的多个 assistant 消息被拼接，工具结果按 dispatch 而非完成顺序写出 | HarnessSink 保留消息边界、实际完成顺序和 host verification/mutation；忽略 partial/refreshed 预览；封口一次；不镜像 reasoning | `sink_rounds_test.go` |
| Kernel 遥测被反向解析为用户 Prompt | Extract 不把 hit/inject/evolution 事件当用户消息 | `telemetry_extractor_test.go` |
| 文本成功、缺失结果或旧成功被误当当前验证 | Extract 与 Distill 共用验证判断；最新 mutation 后才可验证；后续失败、未知或未完成测试撤销旧成功；显式 `not_verification` 优先 | `extractor_verification_test.go`、`distill_verification_test.go` |
| `pytest ... \| tail` 等 shell 后段吞掉测试失败 | 复用现有 shell AST 与 `mask_exit`；预执行检查与两个收据消费入口一致拒绝被掩盖的验证状态 | evidence / agent 的 `pipeline_exit_regression_test.go`；control recovery fixture 改用未掩盖退出码的 vitest，保留自动恢复断言 |
| 既有四层产物未在 SWE runner 中启用 | 自动提取传 `--distill --consolidate --origin session-auto` | `test_repo_isolation.py`、`extract_provenance_test.go` |
| 相似卡跨来源/版本混合，重复提取丢反馈 | 仅 metadata 兼容的 Context 合并；保留真实来源集合和 stats；兼容重复提取保留历史；Store.Put 仍替换 | `consolidate_provenance_test.go`、`source_sessions_store_test.go`、CLI provenance 回归 |
| 前五名全是不合格类型，合格候选永远看不到 | 全库同一 BM25 分数；共享 BuildHits 先资格再取最多五个；原前五拒绝保留诊断，按 ID 关联来源 | `candidate_starvation_test.go`、`inject_test.go` |
| 单条匹配历史、单一来源、相互佐证的近分卡或长任务说明被额外条件整体否决 | Bridge 不再启用库规模、来源数、runner-up、分差、额外 BM25 分数线与 coverage 下限；沿既有 zone 排序、选取与预算组装 | `admission_defaults_test.go`、`memory_flow_e2e_test.go` |
| 异步预取与 Bridge.Close 交错，关闭后仍打开记忆库 | 共享 injectResult 在同一关闭锁下登记进行中的读取；Close 等待注入读取及统计写入完成，关闭后的注入直接返回 | `bridge_close_test.go`、实际 provider 传输回归 |
| 自动来源被当手工来源，Bridge 来源下限未接线 | CLI 默认仍 `user-curated`；runner 显式 `session-auto`；Bridge 使用既有 `MinOrigin=session-auto` | provenance / candidate 回归 |
| packed refs/worktree 读不到当前 HEAD | 复用 gitcmd 查询真实 HEAD，失败仍 unknown，不用旧卡 revision 代替 | `revision_sources_test.go` |
| Build 成功返回就 Close Bridge，运行时诊断和注入反馈关闭 | 失败构建自行清理；成功构建将生命周期交给 controller/runtime，结束时关闭 | `semantix_lifecycle_test.go`，覆盖 Build / BuildRuntime / extension |
| `finish_reason=length` 被当正常 final | 在原任务预算内有界续做；保留取消、步数、预算、readiness，连续三次无工具进展截断停止 | `truncated_final_test.go` |
| 不完整显式路径集合提供部分 freshness 证明 | 补齐现有路径键；缺失/目录/越界/symlink/歧义路径使整组不给 Deps | `test_extraction_fingerprints.py` |

最后的 shell 验证修复发生在 36 题运行结束后；本 PR 含它，实验构建不含它。不得把实验成绩标成当前最终代码的复测结果。

## 3. 四层知识的实际语义

| 层 | 既有载体 | 本次修复 | 不代表什么 |
|---|---|---|---|
| L-A repo 操作知识 | Context / repo-ops | `Observed commands` 区分 host passed、failed、文本 exit、unknown | 历史命令不等于在当前版本必然正确 |
| L-B 子系统概览 | Context / ConsolidateContext | 接线并校验 metadata 兼容，保留真实来源 | 不是新增语义总结模型；本轮实际合并 groups=0 |
| L-C 任务型计划 | Memory / plan-skeleton | 沿既有轨迹阶段提炼，复用验证边界 | 相同 task type 不是跨版本适用性证明 |
| L-D 实例结果 | Memory / outcome，及既有 Result | 最后 mutation 后的 host 验证决定 Verified-by/verified | 模型自述、命令 exit 0 或历史通过不自动赋予当前有效性 |

四层仍是确定性规则提炼。没有实现“自动理解每条知识的必要依赖”；当前会话级 Deps 仍可能把通用知识绑定到过多文件。

## 4. 准入与信任保持

- 当前类型资格为 Context、Memory、host-verified Result；Prompt、ToolPattern、probation Result 仍拒绝。
- Bridge 撤除 Project 最小库 5、类型至少 2 个独立来源 session、BM25 0.70、coverage 0.25、top margin 0.15 和必须有 runner-up 六项默认阻断。不新增六个配置开关；Kernel Injector 的既有可选字段及显式启用测试保持兼容。
- type/status/task/freshness/origin 资格先于 eligible 候选窗口和相对分母；不重新训练或缩放 BM25，不合并卡片来制造高分差。来源集合及反馈仍持久化，移除来源数量门槛不等于删除来源证据。
- 既有 BM25/zone 选择、最多五条与精确字节预算保留。单一候选或同分候选不再整体否决；这也意味着词汇相似但语义不适用的历史仍可能通过，不声称已具备语义裁判。当前实验预算仍为 4096B。
- `BuildHits(K=0)` 保持不截断兼容；Bridge 设 K=5。原前五被拒候选的诊断保留，不承诺每一个全库低排名拒绝都逐条记录。
- `off` 不检索；`shadow` 只诊断，provider 消息与 off 一致；`strict` 只注入通过全部检查的历史，历史消息仍低权威。
- 自动与手工来源如实标注。import/legacy 仍可保存和查看，但不达 Bridge 的来源下限。
- 现有 TLS、鉴权、工具权限、只读支持文件、仓库隔离不放宽。显式路径检查不是对 shell/free-text 所有隐式依赖的完整证明。

## 5. 接口、持久化和兼容性

1. CLI 新增 `extract --origin user-curated|session-auto`；省略时保持手工默认。既有 distill/consolidate flags 不改全局默认，仅 SWE 自动调用启用。
2. `SliceMeta.source_sessions` 为可选字段；旧 `SourceSession` 保留，计数取非空去重并集。旧记录缺新字段仍能读，FileStore 对集合深拷贝，避免调用者修改存储中的来源。
3. 无新事件 Kind、provider role 或数据库迁移；session JSONL 保留既有形状，但修正消息及工具完成顺序。反馈通过既有 stats / evolution 路径。
4. 保持 Store.Put 的替换语义，只在提取入口保留相同内容且 metadata 兼容的使用反馈。
5. 旧读者可读原字段，但旧写者再次写入可能丢弃其不认识的可选来源集合；不声称双向无损降级。

提交前全量回归还修正一处测试时序前提：`harness/memory/memory_test.go` 为 legacy 文件显式设置晚于已保存记录的 mtime，保留原排序/作用域断言；生产 memory 逻辑未改，不依赖 sleep 或文件系统时钟精度。

## 6. 验收状态

| 条件 | 状态 |
|---|---|
| 正负例：真实镜像 → 生产提取/提炼 → Store → Bridge → Agent → provider；CLI 接线另有回归，非手写卡绕过 | 已有回归覆盖 |
| Build 成功后的诊断、Injected 持久化、evolution 生命周期 | 已有实际 boot 回归覆盖 |
| 错误/未知/后续 mutation 不提升 verified；掩盖 exit 不作为验证证据 | 已有回归覆盖 |
| off/shadow 保持消息边界；strict 不放宽信任和 freshness | 已有回归覆盖 |
| 撤除六项额外默认阻断；同源 Result/outcome 不因近分而互相否决；实际 provider 消息仍低权威 | 本地回归覆盖；不是新一轮模型收益实验 |
| 独立副本还原后重现原缺陷、主工作副本保留修改 | 已完成离线回退验证，见实验报告 |
| 正式 SWE 可确认的注入 → 采用 → 收益 | **未达成**；后29题正 bytes+sliceIds 日志0，前7题观测不完整 |
| 本次36题证明最终提交版本显著提升 | **未证明**；两段构建且最终 shell 修复未参加该轮 |

## 7. 后续问题，不在本 PR 中伪装完成

- 逐卡“断言 → 实际来源工具证据 → 必要依赖”归属。需要真实跨题正负例，不能仅凭卡片名称、TaskType 或 body 关键词省略 Deps。
- 区分“某版本上观察到的历史事实”和“适用于当前任务的建议”，不将前者直接当可迁移保证。
- 类型、验证状态、task tag、版本与依赖检查继续独立审查误拒。撤除六项默认阻断不等于证明这些检查全部正确，也不等于所有历史零注入已解决；需要分别识别错误供给、正确拒绝和过粗适用性绑定。
- 更完整的 shell 嵌套/动态形式验证需要独立设计；本次只声称所列 AST 回归形状被覆盖。
- 小型配对观察到传输及采用迹象，但正式 SWE 没有收益因果证据；是否继续评测需另行决定，不自动追加付费任务。

## 8. 回退与评审顺序

运行级回退沿用 `off` / `shadow`，不删除历史库，不修改已完成实验。代码回退使用 Git revert 对本 PR 提交操作；旧库兼容限制见第5节。本次独立副本回退不是对用户真实库执行迁移或删除。

建议按镜像/验证 → 提炼/持久化 → 检索/准入 → boot/截断 → runner → 实验限制的顺序评审。Draft 不自动关闭 #447，也不代表合并或正式发布。

## 9. 2026-09-24 第一性原理复核与分步执行

**执行方式：** 使用 writing-plans / executing-plans 在现有隔离分支逐项执行；每个可独立验收的步骤一个本地 commit。本文先提交，代码步骤随后提交。新实验输入、输出、失败记录与回退证据只保存在 gitignored `/lab/issue-447-admission-repair-20260924/`；不启动付费模型调用，不修改旧实验，不自动 push 或合并。

### 9.1 首先明确系统到底在判断什么

1. **L2 是历史参考，不是答案缓存。** `harness/agent/sampling_request.go` 把历史放在 user-role，固定系统说明要求模型对照当前任务、源码和工具结果重新验证；后面仍调用 provider。L2 应决定“这段来源明确、相关且预算内的历史值得交给模型核查吗”，不是“这段历史能否无需验证地回答当前任务”。
2. **L3 是直接答案复用。** `kernel/cache/l3.go` 与 `gateway/pipeline.go` 能绕过上游调用直接返回内容，其模型/上下文/依赖/L3Safe 检查服务于另一种正确性要求。L2 改动不修改 L3 的检查或把 L2 hit 宣称为可直接返回的答案。
3. **BM25 是相关性排序，不是事实验证。** 库更大、来源更多、第一二名差更大，都不是相关历史存在的必要条件。两条近分历史可能相互佐证，不应仅因难分高低就两条都拒绝。
4. **可信来源不等于当前适用。** 来源、项目和消息权限是边界；任务标签与词法分差只是代理信号；历史版本/依赖是事实来源。三类信息不能统称“安全阈值”，也不能为了产生注入统一删除。
5. **成功必须分层证明。** 生成卡片、检索命中、组装历史块、最终请求包含历史、provider 接口调用、远端实际接收、模型采用、独立评分收益，是不同的事实。前一项通过不推出后一项。

### 9.2 已核对的全链路与根因

```text
原始用户任务
  → Bridge.buildRetrievalQuery
  → Project FileStore / 全库同分 BM25
  → 类型/来源/任务/版本依赖资格 → eligible K → zone → 精确预算
  → InjectDetailed 组装历史块 / 同步或异步预取
  → sampling_request：投影、interceptor、预算 → provider 接口
  → 工具与 host 收据 → HarnessSink
  → Extract / Distill / Consolidate → FileStore
  → 反馈 / evolution → 后续检索
```

| 问题 | 已有源码证据 | 结论与处理边界 |
|---|---|---|
| 六项后加代理条件叠加造成误拒 | `Bridge.injectResult` 设置 library/source-count/runner-up/margin/score/coverage；单例及同源佐证卡可在进入模型前全被否决 | 删除 Bridge 默认设置，不添加六个新开关；Kernel 既有显式可选参数保持兼容 |
| query 仍包含 runner 外壳 | `query.go` 仅识别 XML `<issue>`，本轮实际 runner 使用固定 testbed 环境前缀后接问题正文；相关 #478 修复还覆盖独立 `Issue:`；error 正则的 `(?i)` 泄漏到大写错误码分支，test 正则匹配裸 test/testbed | 在共享 query 投影识别已知任务边界，修正正则范围；保留普通多行及 Expected/Actual 正文，不用“任意冒号行”截断任务 |
| 会话依赖被当作每张卡的必要依赖 | `run_bench.py` 汇总整段镜像路径，`cmd/semantix/extract.go` 将同一 meta.Deps 交给各层提炼，`extractor.go`/`distill.go` 复制到每卡 | 一个无关文件变化可能淘汰通用操作知识；不能据此判断整条历史为假，也不能把 Deps 全清空当修复。见9.5 |
| L2 的历史适用性与直接复用有效性混用 | `inject.go:freshnessReason` 对 Context/Memory/Result 共用当前版本检查 | 当前代码仍保持检查；必须先区分“历史观察”与“可迁移断言”，再有针对性改变，不能把撤六项门槛称为 freshness 已修好 |
| task 标签的硬否决不等于相关性判断 | `task_type.go` 的词匹配分类有优先级；`inject.go:taskAdmits` 看正文首行，未标记卡反而不受约束 | “调查同一个 bug”可能需要 bugfix 历史；相同 task 标签也可能完全不相关。独立正负例评估，不把标签当权限边界；本批先不改变此接口 |
| 组装被误叫注入并计入反馈 | `bridge.go:recordInjection` 在返回文本前更新 Injected；`agent.go:startInjectWarm` 即使结果最终没用也调用它 | 现存 Injected / SliceInject 只证明组装，不证明 provider 交付；报告必须纠正口径。CLI lookup / gateway 也写同一字段，见9.5，不局部重定义后假称全局一致 |
| 上个任务的预取被盖上新任务编号 | `startInjectWarm` 先捕获旧 input，goroutine 完成后才 Load 当前 turn；`takePrefetch` 只核对 turn | 启动时一起捕获 input 与 turn；旧代结果不得覆盖新代缓存；用可控同步回归，而非 sleep |
| Close 后的预取重新打开库 | `startInjectWarm` 脱离原 context；共享 `injectResult` 原来没有生命周期登记 | 同一锁下检查 closing 并登记注入读取；Close 等待读取及统计写入退出，不声称 join 了整个预取 goroutine |
| 路径之间契约不一致 | gateway L2 仍将块拼进 system，且未配置 Harness 同一套准入；L3 的 verified 是依赖有效性，不等同 host 测试通过 | 本轮仅更改 Harness；不把它宣传为 gateway 或 L3 全链路修复，也不借此扩大到未经验证的全局策略 |

原始 `eceb763f` 的 Harness 并非“完全没有筛选”：已有 BM25、zone、sanitize 和预算。本次不恢复旧版超预算特例，不取消权限/项目隔离，也不将原始方案神化为已证明最佳。

### 9.3 方案取舍

- **整体回到旧版：不选。** 会同时撤掉真实来源、消息权限、精确预算、host 收据与缺陷修复，难以归因。
- **六个阈值调低或全部参数化：不选。** 仍要求无关代理信号为历史参考背书，只是把误拒藏进另一套默认值。
- **分解契约，先减法再修接线：采用。** 先撤默认六项否决，保留原排序/预算/边界；查询污染、异步代际和生命周期按独立缺陷处理；卡片适用性与交付统计各自单独验收。不引入新模型、Jev、额外评分器或策略框架。

**预期与代价：** 合格的单条/同源/近分相关历史可以回到 provider 上下文，但词汇相似、语义不适用的历史也可能更多。离线正例证明“能进去”，负例守住明确无关及权限边界；这不等于质量提升，更不等于任务正确率会提高。

### 9.4 本批可执行顺序（每步测试通过再 commit）

#### S0 — 设计与口径

- [x] 本设计先行提交；`semantix-l2-admission-policy.md` 的旧阈值说明已标成历史。
- 检查：`git diff --check`；确认源码未混入此 commit，实验材料没有进 index。
- Commit：`docs(memory): specify L2 admission repair and rollback steps`。

#### S1 — 共享关闭路径

文件：`harness/semantix/bridge.go`、`harness/semantix/bridge_close_test.go`。

- [ ] 先验证关闭后调用 `InjectDetailed` 的回归在原代码失败；再以同一 closing 锁登记读取，复用 `statsWG`；保持 store.Close 先于 WG.Done。
- [ ] 检查已开始读取能够退出、关闭后不重开库；实际 Agent/provider 用例重复运行验证清理。不宣称阻断所有其它 Bridge API 或 join 整个异步任务。
- 关键代码顺序：`Lock → closing? return → statsWG.Add(1) → Unlock → defer statsWG.Done → kernelIndex → defer closeSliceStore`。
- 验收：`go test ./harness/semantix -run '^TestBridgeAdmissionAfterClose$' -count=1 -v`；Linux race 与实际 Agent 用例再检验。
- Commit：`fix(memory): join injection reads before bridge shutdown`。

#### S2 — 撤除六项默认代理门槛

文件：`harness/semantix/bridge.go`、`admission_defaults_test.go`、`candidate_starvation_test.go`、`reuse_test.go`、`revision_sources_test.go`、`harness/agent/memory_flow_e2e_test.go`。

- [ ] 原实现观察单例、单来源、无 runner-up、佐证同分、低 raw score、长查询误拒；无重叠负例保持无注入。
- [ ] 删除六项 Bridge 设置及只为来源数量门槛存在的 helper；不修改 Kernel 显式可选参数。
- [ ] 真实 transcript → 提取/提炼 → Store → Bridge → Agent → recordingProvider；同源 Result/outcome 近分仍可交付，off/shadow 请求保持无块，user-role/来源/精确预算保持。
- 验收：`go test ./harness/semantix ./kernel/inject -count=1`；`go test ./harness/agent -run '^TestMemoryFlow(CorroboratingResultAndOutcomeToProvider|InvoiceHistoryToProvider)$' -count=3 -v`。
- Commit：`fix(memory): remove uncalibrated default admission vetoes`。

#### S3 — 查询取真实任务，不取 runner 包装

文件：`harness/semantix/query.go`、`query_test.go`。修改所有 clean/build 入口共用的任务正文提取，不让两者规则分叉。

- [ ] 先以实际 testbed 前缀加正文为回归，同时覆盖 `Issue:` 与 XML；正文带路径、Expected/Actual、多行描述，三个边界内的相同正文和无外壳输入生成相同检索投影。
- [ ] 负例：包装的 runtime 路径及操作要求不变成 intent/path/error/test；正文中非边界的 `Issue:` 字样不被截断；保留无外壳的普通多行输入。
- [ ] 仅识别已知完整 testbed 前缀、开头的 Issue 标签或明确 git-checkout 外壳；正文内部 Expected/Actual/Requirements 不作通用截断。XML 也保留完整正文词义，不再只留标题加提取字段；因此正文里的普通叙述仍可能参与 BM25，这是避免丢需求的明确取舍。error 正则大小写只作用于异常名分支；test 识别命名格式而非裸单词。沿现有 tokenizer，不另造 query 模型。
- 验收：`go test ./harness/semantix -run 'Test(Clean|Build)RetrievalQuery' -count=1 -v`，再跑整个 Bridge 包。
- Commit：`fix(memory): separate issue content from runner query framing`。

#### S4 — 异步预取绑定发起任务

文件：`harness/agent/agent.go`、`prefetch_feedback.go`、现有 prefetch 测试文件。

- [ ] 在启动处捕获 `turn := a.semantixTurn.Load()`，与 input 一同传给异步结果；结果不再在完成时取得新 turn。
- [ ] 旧 turn 结果不得覆盖当前 turn 的有效结果；延迟结果仍按原 turn 记废弃反馈。
- [ ] channel/既有事件同步构造 A 发起 → B 开始 → A 完成，断言 B 请求没有 A 历史、新缓存不被旧缓存替换；保留原 prefetch 命中/浪费统计语义。
- 验收：`go test -race ./harness/agent -run 'Prefetch|InjectWarm|RetrievalInput' -count=1`，再跑全 Agent 包；不通过 sleep 或删除断言掩盖竞态。
- Commit：`fix(agent): keep speculative memory within its originating turn`。

#### S5 — 合并验证与独立回退

- [ ] Linux 原生工具链执行 `go test -race ./harness/semantix ./kernel/... ./harness/agent -count=1`、boot 记忆生命周期定向回归与 vet；Windows 失败如实记录，不以 Linux 通过伪装 Windows 全套通过。
- [ ] 用 Git 原版本与独立副本验证原行为恢复；把 observation-only 新测试再放进恢复副本，必须复现目标失败；工作副本继续保留修复。
- [ ] 生成并重开本地 `MODIFIED_FILE.tar.gz`、`DIFF_FILE.patch`、`VERIFICATION.txt`、可执行 `ROLLBACK.sh`，记录每个命令的输入、实际输出与 exit。
- [ ] 更新本文步骤状态并提交；本地提交链可逐项 revert，不 squash 掉失败/纠正记录。

### 9.5 已发现但本批不假装修好的两项契约工作

**逐卡适用性与 task 误拒：** 后续最小方向是让普通 L2 历史携带“当时观察到”的来源/版本/核验状态，不要求其等同直接答案；只有声明当前可用的具体结果承担当前证据要求。但现有 Context/Memory 含实例路径和 outcome，不能仅凭类型名跳过 Deps。先用真实轨迹把每条断言连回工具证据，分开可迁移操作与实例结论；对同子系统跨行动、同标签不同问题、相关和无关文件变化分别做正负例。缺归属时保留未知，不伪造依赖，不批量清空。L3 保持独立验证。此设计须另一个可独立回退的实现步骤，不能混进 S2。

**交付观测与反馈：** 后续在 finalized request/provider 接口边界计实际引用，复用既有 turn 与事件；组装/预取保持独立计数。先覆盖未消费预取、interceptor 删除/拒绝、预算退出/取消、重试及多请求复用；再覆盖本地 HTTP recorder 与 Responses incremental 请求的 history 延续。`Injected` 现有多调用者语义不得只改 Harness 一处；若持久化写入失败必须显式报告，不声称已保存。模型采用与独立成绩收益仍需另一层证据。不得用本批合成 recordingProvider 代替这些验收。

### 9.6 失败即回退的执行规则

- 一个步骤动手前确认 index 为空、记录前一个 commit；只暂存该步骤文件/代码块。
- 测试红灯先区分“预期回归 RED”与“实现后的失败”。实现失败时保存日志与 diff 到 `/lab/`，将**该步骤拥有的改动**恢复到前一个成功状态，再修改设计重做。不用清空全工作树的 reset/clean，不覆盖其它人的文件。
- 若失败代码已提交，使用 `git revert <该步commit>` 保留审计，再重新实现；不改写已发布历史。
- 原版本原样也失败的环境问题保留双方日志，修运行环境或改用能验证同一断言的平台；不跳过断言、不改通过判定。
- Git 提交与普通测试足够记录版本和回退，不增加哈希门禁、冻结合约、付费试跑或新的批次准入层。
