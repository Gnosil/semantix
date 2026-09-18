# Issue #447：在既有 Kernel 框架内修复记忆供给与消费链路

状态：**Draft PR / 正确性修复待评审**。日期：2026-09-18。基于 `ea1af530`。

关联：[#447](https://github.com/Gnosil/semantix/issues/447)、[四层提炼设计](semantic-four-layer-distill.md)、[L2 准入策略](semantix-l2-admission-policy.md)、[本次实验报告](../reports/issue-447-memory-flow-repair.md)。本 SPEC 描述本 PR 已实现的内容；未完成项单列，不以实验通过数代替设计验收。

## 1. 目标和不变项

修复真实任务到检索输入、会话镜像到知识卡、来源与反馈持久化、候选选择、运行期资源生命周期的断点。保留现有链路：

```text
Agent 原始任务 → Bridge query → Project BM25 → Injector 资格/准入
  → 带来源的低权威 user-role 历史 → provider → 工具执行/host 收据
  → HarnessSink 会话镜像 → Extract + Distill + ConsolidateContext
  → 原 FileStore / 使用反馈 / evolution → 后续同项目任务
```

不替换 Kernel，不新增记忆架构、SliceType、检索算法或模型提炼服务；不降低相关性、来源数、freshness、权限或项目隔离要求。不把普通 hit、提取成功、非零注入、模型采用和任务收益混为一谈。

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
- 保留 Project 最小库 5、类型至少 2 个独立来源 session、BM25 0.70、coverage 0.25、top margin 0.15、必须有 runner-up、4096B 实验预算。
- type/status/task/freshness/origin 资格先于 eligible 候选窗口和相对分母；不重新训练或缩放 BM25，不按相似措辞合并竞争候选来抬 margin。
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
| 独立副本还原后重现原缺陷、主工作副本保留修改 | 已完成离线回退验证，见实验报告 |
| 正式 SWE 可确认的注入 → 采用 → 收益 | **未达成**；后29题正 bytes+sliceIds 日志0，前7题观测不完整 |
| 本次36题证明最终提交版本显著提升 | **未证明**；两段构建且最终 shell 修复未参加该轮 |

## 7. 后续问题，不在本 PR 中伪装完成

- 逐卡“断言 → 实际来源工具证据 → 必要依赖”归属。需要真实跨题正负例，不能仅凭卡片名称、TaskType 或 body 关键词省略 Deps。
- 区分“某版本上观察到的历史事实”和“适用于当前任务的建议”，不将前者直接当可迁移保证。
- 更完整的 shell 嵌套/动态形式验证需要独立设计；本次只声称所列 AST 回归形状被覆盖。
- 小型配对观察到传输及采用迹象，但正式 SWE 没有收益因果证据；是否继续评测需另行决定，不自动追加付费任务。

## 8. 回退与评审顺序

运行级回退沿用 `off` / `shadow`，不删除历史库，不修改已完成实验。代码回退使用 Git revert 对本 PR 提交操作；旧库兼容限制见第5节。本次独立副本回退不是对用户真实库执行迁移或删除。

建议按镜像/验证 → 提炼/持久化 → 检索/准入 → boot/截断 → runner → 实验限制的顺序评审。Draft 不自动关闭 #447，也不代表合并或正式发布。
