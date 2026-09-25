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

- 当前类型资格为 Context、Memory、host-verified Result；Prompt、ToolPattern、probation Result 仍拒绝。**边界**：该资格保证是 Harness strict/shadow 路径的；gateway 的 L2 注入器未配置同一 allowlist（`AllowedTypes==nil`，见 §9.2"路径之间契约不一致"行），Prompt/ToolPattern 在 gateway 侧仍可进入 system 块——本批不改动 gateway，两路径的对齐是后续独立工作。
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

### 6.1 旧库重收割配方（逐个真实会话执行）

本批没有原地迁移旧库。一个只含 Prompt/ToolPattern、缺真实来源或缺版本信息的库，应用 S2 后仍可能全部被现有类型/来源/freshness 条件拒绝。需要从原始会话重新提取；将旧卡直接改成 Context、补造 session/origin 或把历史 revision 改成当前 HEAD，并不是供给修复。

有原始 Harness 会话镜像时，在**单独输出库**离线重新提取。SWE 历史与交互式历史都建议启用 `--distill --consolidate`，而非只对 SWE 启用。每个会话分别设置下列变量，再执行一次；命令使用已有 CLI 参数，hash embedder 不调用远端模型：

```sh
semantix extract --input "$SESSION_JSONL" \
  --db "$REPLAY_DB" --scope project \
  --session "$SOURCE_SESSION" --project "$SOURCE_PROJECT" \
  --base-commit "$SOURCE_REVISION" --origin session-auto \
  --embedder hash --distill --consolidate
```

- `SESSION_JSONL`、`SOURCE_SESSION`、`SOURCE_PROJECT`、`SOURCE_REVISION` 必须分别来自该真实会话的镜像路径、会话 ID、项目标识和观察时版本，不从当前任务猜测，也不为凑来源数量拆分或伪造会话。
- **ObservedCommit 是会话当时观察到的版本**（上面的 `SOURCE_REVISION`，经 `--base-commit` 写入现有 `BaseCommit` 字段），不是执行重收割时的当前 HEAD。不要用此时的 `git rev-parse HEAD` 补填未知历史版本；缺失证据就保持未知，且不声称重提取后已具备注入资格。
- `REPLAY_DB` 指向新建的离线检查目录，同项目的真实会话逐个写入该输出库，原库不覆盖；`session-auto` 仅对应实际自动生成的 Harness 镜像，外部导入材料不靠改标签取得该身份。
- 只有恢复了对应来源工作区、且掌握真实相关文件路径时，才从该来源工作区运行并追加 `--fingerprint "$SOURCE_RELATIVE_PATHS"`。现有 CLI 把这一组 Deps 赋给所有产物，仍不是逐卡归属。没有原始证据时保持未知。
- 不传 `--l3-safe`；重提取不授予直接答案复用能力。先查看真实产物类型、来源、验证状态及拒因，再决定使用；四层 flags 不保证任意短会话都产生可用知识。

上述命令是离线重提取配方，不是已执行的用户真实库迁移；本批未改历史实验、用户库或原始轨迹。

### 6.2 freshness 的现行运行后果

以下是 Harness 在候选到达 freshness 检查时的拒绝 reason；更早的类型、状态等检查也可能先拒绝它。

| 运行条件 | reason / 后果 |
|---|---|
| 当前工作区提交未知，例如非 Git 工作区或 HEAD 解析失败 | `current_commit_unknown`；历史卡不注入 |
| 当前提交已知，但卡片 `BaseCommit` 未知 | `commit_unknown`；历史卡不注入，即使存有 Deps 也不绕过来源版本缺失 |
| 当前提交和 `BaseCommit` 均已知且不同，卡片没有 Deps | `stale_commit`；原本合格的卡在 HEAD 变化后失格 |

**无 Deps 且绑定旧提交的合格卡，在下一次提交使 HEAD 改变后即被拒绝，不论这次提交是否只改无关文档。** 若库中所有原本合格的卡都满足此条件，L2 会再次零注入；检索命中或重提取成功不消除这一后果。有真实 Deps 的跨提交卡仍需通过依赖校验与其它资格检查，不保证一定注入。S2 撤除的六项默认门槛不包含 freshness，§6.1 的重收割配方也不改变该契约。

## 7. 后续问题，不在本 PR 中伪装完成

- 逐卡“断言 → 实际来源工具证据 → 必要依赖”归属。需要真实跨题正负例，不能仅凭卡片名称、TaskType 或 body 关键词省略 Deps。
- 区分“某版本上观察到的历史事实”和“适用于当前任务的建议”，不将前者直接当可迁移保证。
- freshness 仍可产生 `current_commit_unknown` / `commit_unknown` / `stale_commit`（条件见 §6.2）；无 Deps、绑定旧提交的合格卡在 HEAD 变化后失格，甚至使整库 L2 再次零注入。逐卡适用性另按 §9.5 设计；不采用最近 N 提交窗口回退，理由见 §9.3。
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
- **无 Deps 卡的最近 N 提交窗口回退：不采用。** 提交距离不是必要依赖未变的证据：一个提交就能改变关键行为，许多无关文档提交也可能完全不影响历史。任意 N 既会放过已失效卡，也会拒绝仍相关的旧卡；它只是另一个未经校准的代理阈值，没有解决“历史观察”与“当前适用断言”的区别。本批保留现行 freshness 与显式拒因，代价是 §6.2 所述 HEAD 变化后的失格；后续按 §9.5 用逐卡来源、必要依赖和跨提交正负例独立验收，不以窗口或伪造 Deps 掩盖缺口。
- **分解契约，先减法再修接线：采用。** 先撤默认六项否决，保留原排序/预算/边界；查询污染、异步代际和生命周期按独立缺陷处理；卡片适用性与交付统计各自单独验收。不引入新模型、Jev、额外评分器或策略框架。

**预期与代价：** 合格的单条/同源/近分相关历史可以回到 provider 上下文，但词汇相似、语义不适用的历史也可能更多。离线正例证明“能进去”，负例守住明确无关及权限边界；这不等于质量提升，更不等于任务正确率会提高。

### 9.4 本批可执行顺序（每步测试通过再 commit）

#### S0 — 设计与口径

- [x] 本设计先行提交；`semantix-l2-admission-policy.md` 的旧阈值说明已标成历史。
- 检查：`git diff --check`；确认源码未混入此 commit，实验材料没有进 index。
- Commit：`docs(memory): specify L2 admission repair and rollback steps`。

#### S1 — 共享关闭路径

文件：`harness/semantix/bridge.go`、`harness/semantix/bridge_close_test.go`。

- [x] 先验证关闭后调用 `InjectDetailed` 的回归在原代码失败；再以同一 closing 锁登记读取，复用 `statsWG`；保持 store.Close 先于 WG.Done。
- [x] 检查已开始读取能够退出、关闭后不重开库；实际 Agent/provider 用例重复运行验证清理。不宣称阻断所有其它 Bridge API 或 join 整个异步任务。
- 关键代码顺序：`Lock → closing? return → statsWG.Add(1) → Unlock → defer statsWG.Done → kernelIndex → defer closeSliceStore`。
- 验收：`go test ./harness/semantix -run '^TestBridgeAdmissionAfterClose$' -count=1 -v`；Linux race 与实际 Agent 用例再检验。
- Commit：`4f9817b2` — `fix(memory): join injection reads before bridge shutdown`。

#### S2 — 撤除六项默认代理门槛

文件：`harness/semantix/bridge.go`、`admission_defaults_test.go`、`candidate_starvation_test.go`、`reuse_test.go`、`revision_sources_test.go`、`harness/agent/memory_flow_e2e_test.go`。

- [x] 原实现观察单例、单来源、无 runner-up、佐证同分、低 raw score、长查询误拒；无重叠负例保持无注入。
- [x] 删除六项 Bridge 设置及只为来源数量门槛存在的 helper；不修改 Kernel 显式可选参数。
- [x] 真实 transcript → 提取/提炼 → Store → Bridge → Agent → recordingProvider；同源 Result/outcome 近分仍可交付，off/shadow 请求保持无块，user-role/来源/精确预算保持。
- 验收：`go test ./harness/semantix ./kernel/inject -count=1`；`go test ./harness/agent -run '^TestMemoryFlow(CorroboratingResultAndOutcomeToProvider|InvoiceHistoryToProvider)$' -count=3 -v`。
- Commit：`2af62ed6` — `fix(memory): remove uncalibrated default admission vetoes`。

#### S3 — 查询取真实任务，不取 runner 包装

文件：`harness/semantix/query.go`、`query_test.go`。修改所有 clean/build 入口共用的任务正文提取，不让两者规则分叉。

- [x] 先以实际 testbed 前缀加正文为回归，同时覆盖 `Issue:` 与 XML；正文带路径、Expected/Actual、多行描述，三个边界内的相同正文和无外壳输入生成相同检索投影。
- [x] 负例：包装的 runtime 路径及操作要求不变成 intent/path/error/test；正文中非边界的 `Issue:` 字样不被截断；保留无外壳的普通多行输入。
- [x] 仅识别已知完整 testbed 前缀、开头的 Issue 标签或明确 git-checkout 外壳；正文内部 Expected/Actual/Requirements 不作通用截断。XML 也保留完整正文词义，不再只留标题加提取字段；因此正文里的普通叙述仍可能参与 BM25，这是避免丢需求的明确取舍。error 正则大小写只作用于异常名分支；test 识别命名格式而非裸单词。沿现有 tokenizer，不另造 query 模型。
- 验收：`go test ./harness/semantix -run 'Test(Clean|Build)RetrievalQuery' -count=1 -v`，再跑整个 Bridge 包。
- Commit：`a66ea1d7` — `fix(memory): separate issue content from runner query framing`。

#### S4 — 异步预取绑定发起任务

文件：`harness/agent/agent.go`、`prefetch_feedback.go`、`prefetch_feedback_test.go`。

- [x] 在启动处捕获 `turn := a.semantixTurn.Load()`，与 input 一同传给异步结果；结果不再在完成时取得新 turn。
- [x] 旧 turn 结果不得覆盖当前 turn 的有效结果；延迟结果仍按原 turn 记废弃反馈。
- [x] channel/既有事件同步构造 A 发起 → B 开始 → A 完成，断言 B 请求没有 A 历史、新缓存不被旧缓存替换；保留原 prefetch 命中/浪费统计语义。
- 验收：`go test -race ./harness/agent -run 'Prefetch|InjectWarm|RetrievalInput' -count=1`，再跑全 Agent 包；不通过 sleep 或删除断言掩盖竞态。
- Commit：`110cd235` — `fix(agent): keep speculative memory within its originating turn`。2026-09-24 ZCode 第二轮审查通过，无阻塞缺陷；本轮保持这三个文件不变。

#### S5 — 合并验证与独立回退

- [x] Linux 原生工具链执行 `go test -race ./harness/semantix ./kernel/... ./harness/agent -count=1`、boot 记忆生命周期定向回归与 vet；Windows 失败如实记录，不以 Linux 通过伪装 Windows 全套通过。
- [x] 用 Git 原版本与独立副本验证原行为恢复；把 observation-only 新测试再放进恢复副本，必须复现目标失败；工作副本继续保留修复。
- [x] 生成并重开本地 `MODIFIED_FILE.tar.gz`、`DIFF_FILE.patch`、`VERIFICATION.txt`、可执行 `ROLLBACK.sh`，记录每个命令的输入、实际输出与 exit。
- [x] 更新本文步骤状态并提交；本地提交链可逐项 revert，不 squash 掉失败/纠正记录。

### 9.5 已发现但本批不假装修好的两项契约工作

**逐卡适用性与 task 误拒：** 后续最小方向是让普通 L2 历史携带“当时观察到”的来源/版本/核验状态，不要求其等同直接答案；只有声明当前可用的具体结果承担当前证据要求。但现有 Context/Memory 含实例路径和 outcome，不能仅凭类型名跳过 Deps。先用真实轨迹把每条断言连回工具证据，分开可迁移操作与实例结论；对同子系统跨行动、同标签不同问题、相关和无关文件变化分别做正负例。缺归属时保留未知，不伪造依赖，不批量清空。L3 保持独立验证。此设计须另一个可独立回退的实现步骤，不能混进 S2。

**交付观测与反馈：** 后续在 finalized request/provider 接口边界计实际引用，复用既有 turn 与事件；组装/预取保持独立计数。先覆盖未消费预取、interceptor 删除/拒绝、预算退出/取消、重试及多请求复用；再覆盖本地 HTTP recorder 与 Responses incremental 请求的 history 延续。`Injected` 现有多调用者语义不得只改 Harness 一处；若持久化写入失败必须显式报告，不声称已保存。模型采用与独立成绩收益仍需另一层证据。不得用本批合成 recordingProvider 代替这些验收。

### 9.6 失败即回退的执行规则

- 一个步骤动手前确认 index 为空、记录前一个 commit；只暂存该步骤文件/代码块。
- 测试红灯先区分“预期回归 RED”与“实现后的失败”。实现失败时保存日志与 diff 到 `/lab/`，将**该步骤拥有的改动**恢复到前一个成功状态，再修改设计重做。不用清空全工作树的 reset/clean，不覆盖其它人的文件。
- 若失败代码已提交，使用 `git revert <该步commit>` 保留审计，再重新实现；不改写已发布历史。
- 原版本原样也失败的环境问题保留双方日志，修运行环境或改用能验证同一断言的平台；不跳过断言、不改通过判定。
- Git 提交与普通测试足够记录版本和回退，不增加哈希门禁、冻结合约、付费试跑或新的批次准入层。

### 9.7 本批交付检查点

| 独立步骤 | 本地提交 | 已验收范围 |
|---|---|---|
| 设计先行及实际 runner 输入修正 | `408d0218`、`7c942d79` | 旧策略标为历史；L2/L3、门槛类别、观测层级与回退顺序分开 |
| 注入读取生命周期 | `4f9817b2` | Close 后不再打开检索；S1-only overlay 验证，不依赖后续门槛修改 |
| 六项默认误拒 | `2af62ed6` | 单例、同源、近分、长查询等正例与无重叠负例；真实 provider 接口回归 |
| 任务 query 投影 | `a66ea1d7` | 实际 testbed 前缀、XML/Issue/普通多行、正文语义、正则误识别 |
| 异步预取代际 | `110cd235` | 可控交错复现旧任务进入新请求；修复后旧结果不覆盖当前缓存/请求 |

本批已经完成定向 RED/GREEN、相关 Kernel/Bridge/Agent 的 Linux race、boot 生命周期及 vet，以及独立 Git 副本的修复通过→恢复原字节→观察测试重现失败。Windows 全包历史失败仍保留在本地证据中，不表述为全平台全量通过。回退脚本最初遇到 Git Bash `/tmp` 与 `/c` 的同目录别名，未写文件即退出；改用原生目录身份比对后完成回退，仍只接受指定独立副本。

源码和测试仍保留修复；本地提交未自动 push、合并或发布。**这只是 S0–S5 的完成，不是第9.5节适用性/交付契约工作的完成，更不是实际模型收益证明。** `task_mismatch`、版本/依赖误拒及组装统计的语义问题继续单独处理，不用这批正例掩盖剩余断点。

### 9.8 旧库、重提取与仍可能零注入的运行条件

重收割配方统一见 §6.1；不在这里维护另一份命令。

freshness 的运行条件与三种版本拒因见 §6.2，后续范围见 §7 / §9.5；最近 N 提交窗口回退不予采用的设计决策见 §9.3。


### 9.9 S6 — 任务标签保留描述作用，不再默认否决相关历史

用户已要求继续已批准的分步修复。目标仍是让来源明确、项目内、预算内且相关的 L2 历史供模型重新核查，不把它升级为直接答案或绕过 L3。按 systematic-debugging / writing-plans / TDD 执行，先提交本节设计再修改行为。

**根因与调用范围：** Harness 的普通、degraded 和 warm-prefetch 都经过 `Bridge.injectResult`，此处向 `Injector.TaskType` 传入 `ClassifyTask(query)`。分类是带优先级的关键词匹配；`taskAdmits` 只解析 Memory 卡正文首行标签，未标记卡及 Context/Result 不受约束。相同代码问题仅因当前动作从“修复”换成“排查”或“修测试”，就会硬拒历史，而相同标签并不能证明相关。全仓调用检查只有 Harness 默认配置此字段；Kernel 显式 API 的调用者仍可请求过滤。

**最小方案：** 撤除 Bridge 默认 `TaskType` 赋值，不新增开关、不改变提炼分类和卡片正文、不重写 Kernel 显式 `TaskType` 语义。原 BM25、eligible K=5、zone、origin、类型/Result 状态、项目 scope、版本/依赖校验、清洗、user-role 与精确字节预算保持。代价是跨任务类别且词法相似的历史可以作为参考进入；不保证语义正确或模型采用。相同任务标签但无词法重叠的负例仍不注入。

**文件边界：** `harness/semantix/bridge.go`、`admission_defaults_test.go`、`candidate_starvation_test.go`；仅同步 `kernel/inject/inject.go`、`kernel/slice/task_type.go`、`kernel/slice/distill.go` 中原来将可选过滤描述成默认保证的注释。本轮不编辑他人正在修改的 `memory_flow_e2e_test.go` 或其它文档改动。

**执行与验收：**
- [x] 先添加 `TestBridgeTaskLabelsAreDescriptive`：同问题跨 investigate/bugfix/test-update、真实 runner 外壳、normal/degraded、strict/shadow/off；先见 `task_type_mismatch` 的 RED。负例覆盖同标签无重叠、跨提交缺 Deps、未验证 Result，保持其它边界。
- [x] 删除共享 Bridge 默认赋值。将旧 candidate-window 的“task mismatch 阻断”用例改为“跨类别相关卡参与同一个 BM25/K 窗口”，保留 stale 分支、完整分数/元数据/五槽/阴影模式断言。Kernel 显式 task gate 的现有测试原样通过。
- [x] 运行 `go test ./harness/semantix ./kernel/inject ./kernel/slice -count=1`，再运行 Linux `go test -race ./harness/semantix ./kernel/... ./harness/agent -count=1`、`go vet ./harness/semantix ./kernel/... ./harness/agent` 和项目 `go test ./... -count=1`；全部输出保留，任何非本改动造成的失败也明确列出。
- [x] 独立 Git 副本运行相同正负例：原代码失败、修改通过、回退代码后失败，主工作树保留修复；更新本节状态后分步提交。

**回退：** 一个步骤失败先保留 diff/输出，只恢复本步骤拥有的代码，再调整方案。已提交的错误使用 Git revert。独立副本回退只恢复本节文件，不触及原实验、真实记忆库、他人文档或其它未提交改动。

**尚未覆盖：** 本节解决 task 标签代理误拒，不解决 §9.5 的逐卡依赖、freshness 或真实 provider 交付记账；不批量删除 Deps、不伪造版本、不重新解释既有 Injected 统计，不追加付费评测。


#### S6 验收修正：补齐既有中文任务标签断言

第一次整包验证发现 `tasktype_admission_test.go:TestBridgeGatesMemoryCardsByTaskType` 仍按旧默认行为要求拒绝跨类别卡；这是本步骤漏列的契约测试，不是通过降低安全断言来绕过失败。已先保存失败日志与六文件 diff，并恢复本步骤代码，再补充文件范围：该中文用例改为两条同相关度历史均保留来源和原标签；原 ToolPattern/未验证 Result 拒绝断言不变。新目标回归的 12 个标签误拒已在原代码复现，首次修改后目标通过。整包同时出现 Windows TempDir RemoveAll / Access denied，原始输出保留；使用原生 Linux 对相同断言复核，仍不表述为 Windows 全包通过。


**S6 已验收：** 设计 `aac310da`，补齐既有中文断言的计划纠正 `4ef94afc`，实现 `a17ba134`。原代码正例出现 `task_type_mismatch`；修改后普通/降级与 strict/shadow/off 的正负例通过。精确 Git 源码副本通过项目 `go test ./... -count=1`、相关 Kernel/Bridge/Agent race 与 vet；独立评审无阻塞发现。独立副本回退后，同一观察测试重新失败，恢复旧默认误拒；原文件复核后留存，工作树继续保留修复。首次失败后六文件回滚、原始 Windows 失败、所有命令输出与回退均保存在本步骤 `/lab/` 证据，不伪称 Windows 全包通过。

本步骤已解除 Harness 默认的 task 标签否决；Kernel 调用者显式设置 `TaskType` 的过滤仍原样存在。§9.5 的逐卡依赖/freshness 与交付记账依然未完成；其它检查仍可导致整批候选被拒绝。未改变旧实验、真实库、收费条件，未进行模型调用，未推送或更新远端 PR。其它进行中的文档及 invoice 测试改动没有混入本步骤提交和独立验收副本。


### 9.10 S7–S9 — 已授权的剩余闭环修复（2026-09-24）

用户要求按同一 workflow 连续完成：本地 SPEC 先提交，红灯复现，最小实现，验证后分步 commit；实现失败先保存并恢复该步骤拥有的改动再调整。继续原 worktree，不改旧实验/真实库，不追加付费调用，不自动推送或修改远端 PR。

**第一性原理：** L2 提供有来源的历史观察，L3 才声称直接复用当前答案。一次会话访问的所有文件并非每条历史观察的当前依赖。当前 extractor/distiller 将同一 Deps 复制到所有卡，Injector 又把它们当当前有效结论；于是“以前运行过某命令/改过某文件”随任意文件改变而消失。相反，任意自然语言 Result 的真实依赖并不能从词匹配证明完整，仍须保守核验，不伪造逐文件归属。

#### S7：逐卡声明历史观察，而非按类型豁免

- 仅确定性 extractor 的 repeated-tool Context，以及 Distill 的 repo-ops / plan-skeleton / outcome 模板卡，由生产者标记 `SliceMeta.Historical=true`。标记描述“过去发生过”，不声明现在仍适用。Prompt、ToolPattern、任意最终 Result 不继承输入的 historical 标记，默认/旧库保持 snapshot 语义。
- Kernel 仅对带该标记、Context/Memory、origin 至少 session-auto、非空 source session/base commit 的卡使用历史语义。仍要求当前项目/版本可识别，仍检查依赖路径语法；保留原 source/commit/Deps 值作来源审计，绝不全库清空或改写历史指纹。历史卡不读取当前文件以证明过去事件，因此相关或无关文件变化/删除不会把真实过去变成假过去；输出明确 `applicability=historical_observation; revalidate_before_use`。
- 其它卡不变：当前结果依赖变化/缺失/越界/symlink、来源不明、未验证 Result 继续拒绝；Result 即使误带 historical 也不豁免，L3 验证完全不变。旧库需可信原轨迹重新提取才获得新分类，不自动升级旧记录。
- 这不是将 Context/Memory 全部放行，也不把模板观察称为通用真理。收敛复用维持 metadata 一致规则，来源聚合不抹掉版本/指纹边界。
- 文件：`kernel/slice/{slice,extractor,distill}.go`、`kernel/inject/inject.go` 和相邻测试。测试覆盖四种模板真实抽取→存储重开→跨提交/文件变更的注入；原文本 Result、未知/旧卡、假标记/来源、无关词、项目、预算、清洗与 L3 负例。

#### S8：组装不是交付；反馈只认真正进入 provider 的历史

- `InjectDetailed`/degraded/prefetch 是候选选择与组装；它们仅输出诊断，不提前增长持久 `Injected`/LastUsed。CLI `inject` 只输出文本，同样没有 provider 交付；保持输出形状，取消提前记入 SliceInject/Injected。
- Agent 在 finalized request（含 interceptor、预算、角色投影）之后调用 provider，只有 `Stream` 成功返回非空 channel 且最终 user-role 内容仍包含本 turn 的完整 host-owned block，才记 `SliceInject`/Injected；取消、预算退出、删除/改写/拒绝、未消费预取、provider 同步失败不记。每个 user turn 的同一 block 只记一次，重试和多轮复用不放大演化权重。
- 这项计数证明 provider 接口接受请求，不等于模型采用/收益，也不等于第一字节生成成功。内建 HTTP provider 的接受点在成功响应头之后；用本地 HTTP recorder 独立核实序列化内容。Responses stateful 延续可以沿用已经交付的历史，不必每轮重新传输；过期 ID 回退完整请求、删除历史后全量重放均应测试。
- 复用 Bridge 事件和 stats，不新增计数表/ID/hash/gate；持久化错误通过现有 KernelCache 诊断显式报告，不虚称保存。loop/progress guard 仍可熔断组装块，但仅对已交付 IDs 记录负反馈，信号仍不是因果证明。
- 同步修所有 Injected 写入口：Gateway 在成功 forward 的 2xx 后才记录；转换/网络/非2xx不记，CLI 无 provider 不记。历史已持久计数不篡改，报告注明旧计数含组装与新语义的版本边界。
- 文件：Bridge、Agent sampling/turn/fuse、Gateway pipeline、CLI lookup 与对应测试；Kernel event 注释同步语义。不改权限、TLS、上游、预算或 provider 重试策略。

#### S9：真实输入与全链验收

- 把已验证的 runner task-body 投影提到 Kernel 共用的纯文本 helper；Bridge 检索与 Distill 标题/描述标签共同使用，避免检索修了但新卡仍记录 testbed 外壳。普通多行任务语义保留；不修改真正发给 provider 的任务/权限指令。
- 项目完整 `go test ./... -count=1`，受影响包 Linux race 和 vet；原失败与修复输出都保存。使用隔离的精确 Git 源码副本，排除他人未提交改动。局部失败先回滚所属步骤，不擦掉他人改动。
- 独立副本：本批原 Git 代码 + 新观察测试应 RED，修复代码 GREEN，ROLLBACK 恢复原字节后观察测试重新 RED；工作副本继续保留修复。产物在 `/lab/issue-447-admission-repair-20260924/closure/`：MODIFIED_FILE.tar.gz、DIFF_FILE.patch、VERIFICATION.txt、可执行 ROLLBACK.sh，全部重开核验。
- 完成口径：链路软件契约可离线证明；显著成绩提升/模型采用不凭合成测试宣布。未迁移的旧库、真实模型相关性及显式调用者额外过滤条件仍如实列为使用前提，不将它们当成代码完成证明。

#### S9 完成状态（2026-09-24，ZCode 接力 codex 额度断点收尾）

- [x] 投影共享：`kernel/slice.TaskBody`（`47987b66`）+ Bridge 检索与 Distill 共用；task_body 三份测试入库。
- [x] 独立副本 RED/GREEN/ROLLBACK-RED 三态一致；closure 四产物（DIFF_FILE.patch / MODIFIED_FILE.tar.gz / ROLLBACK.sh / VERIFICATION.txt）已生成并重开核验，均在 `lab/issue-447-admission-repair-20260924/closure/`。
- [x] 批次受影响五包（kernel/slice、kernel/inject、harness/semantix、gateway、cmd/semantix）Windows 串行全绿；vet 通过。
- [x] Linux 原生 race 已补（当地时间 2026-09-24 晚、UTC 已跨入 09-25；用户授权后在 WSL Ubuntu 24.04 安装 go1.26.5）：`go test -race ./harness/semantix ./kernel/... ./harness/agent -count=1` 于 e7baff6f 全绿（含 invoice e2e 在 Linux 真实 python3 下通过），零数据竞争；输出存 `lab/issue-447-admission-repair-20260924/closure/LINUX-RACE.txt`。同日已移除 Windows python3/python Store 存根并在 D:\python 落位真实 python3.exe，Windows 侧 invoice e2e 亦通过。全量 `go test ./...` 中其余批次外包失败仍维持 pristine BASE 归因结论（symlink 权限/终端类，BASE 同样失败）；gateway e2e 在 Windows 整包模式下另有一类"Close 后文件重建"的 TempDir 清理竞态（断言全过、受害者轮换、复跑即绿、Linux race 下未见），judge 助手侧的 ingestWG join 已补（`04445ce1`），其余记录为存量待查。
- [x] 协作遗留同步落盘：round-2 测试清理 `204e8d9a`、文档 `5c8f4b7f`（F1/F2/F4 合并提交，S6–S9 提交交错所致，偏离四分步原计划的说明见 VERIFICATION.txt 第六节）。
