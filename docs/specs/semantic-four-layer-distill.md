# Spec：四层语义知识提炼（distill）与消费准入

> 日期：2026-09-01。判级：**Spec-Required**（跨 ≥3 包、>300 行、新增 CLI flag）。
> 背景：[`swe-pilot-two-arm.md`](../reports/swe-pilot-two-arm.md) 与 ablate 对照（[`swebench-harness-comparison.md`](../reports/swebench-harness-comparison.md) §5.2）复算结论——注入超额成本 ~80% 来自行为漂移（弱信号切片诱导多探索），turn 级流水账对独立任务是弱信号。本 spec 把库的供给侧从「流水账」升级为四层结构化知识，并按证据收紧消费准入。

## 1. 目标与四层定义

把 session 流水账提炼为可直接喂给规划/执行的知识卡，每层独立可测：

| 层 | 产物 | 载体（不新增 SliceType） | 证据基础 |
|---|---|---|---|
| L-A repo 操作知识 | 「repo-ops」卡：验证命令（含 exit status）、已知坑 | `Context` / Project | 冷库流水账即可提炼 |
| L-B 子系统地图 | 概览卡（近重复 Context 合并） | 已落地 `ConsolidateContext`，本次**接线** | W4 已实现未接入维护路径 |
| L-C 任务分型 → plan 骨架 | 「plan-skeleton」卡：任务型 + 阶段序列 | `Memory` / Project，`Meta.TaskType` 填分型 | 类型级重复 >> 实例级；#259 ByType 已在 |
| L-D 实例级定位 | 「outcome」卡：任务摘要 + 改过的文件 + 验证命令 | `Memory` / Project | 高相似历史任务才有用，靠 L-C 同型准入约束 |

## 2. 变更清单

### 2.1 `kernel/slice/task_type.go`（新）

`ClassifyTask(text string) string` → `bugfix|feature|test-update|refactor|docs|investigate|general`。表驱动关键词（中英双语），首个 user 消息为输入；确定性、无 LLM。写入 distill 产物的 `Meta.TaskType`。

### 2.2 `kernel/slice/distill.go`（新）

`Distill(sessionJSONL []byte, meta SliceMeta) ([]*Slice, error)`。自带解析器（不动 `parseTranscript`——它丢弃工具结果行）：

- 解析 role 行（user/assistant + tool_calls）与工具结果行（`tool_call_id`/`name`/`content`），按 `tool_call_id` 去重（镜像双写是已知现象），命令 ↔ 结果配对。
- **repo-ops 卡**（L-A）：
  - Verified commands：bash 命令匹配测试/构建特征（`pytest`、`test`、`runtests`、`tox`、`go test`、`make`、`npm/yarn test`…），记录归一化命令 + 最近一次配对结果（`ok` / `exit N`）；
  - Known pitfalls：失败结果的稳定模式行（`blocked:`、`outside the writable roots`、`No module named`、`command not found`…），每条一行、去重计数。
- **plan-skeleton 卡**（L-C）：工具轨迹映射到 canonical 阶段（`reproduce`（写脚本+跑）/`locate`（搜索/读）/`edit`（改非测试文件）/`verify`（verifyBoundary 复用）），输出 `plan(task=<type>): stage → stage …`；同会话内相邻重复阶段折叠计数。
- **outcome 卡**（L-D）：任务摘要（首个 user 消息压缩 ≤240B）+ `Edited:`（来自结果行 `edited <path>` / `wrote N bytes to <path>`，去重）+ `Verified-by:`（最后一次成功的测试命令）+ `task=<type>` 标记。
- 内容首行带层标记（`Repo operations …` / `Plan skeleton …` / `Task outcome …`），BM25 词法可检索；ID 仍为内容 hash（与 extractor 同 `newSlice` 路径，沿用 sanitize/admission 既有写入口）。

### 2.3 `cmd/semantix/extract.go`：两个 flag

- `--distill`：extract 产物之外追加 `Distill` 产物（默认关，行为不变）；
- `--consolidate`：本次 extract 完成后对目标库跑 `ConsolidateContext`（默认阈值），把 L-B 接进日常提炼路径（此前只挂在 `gc`）。

### 2.4 `kernel/inject/inject.go`：`Injector.TaskType`（L-C/L-D 准入）

新字段 `TaskType string`（空 = 现状不变）。非空时：`Memory` 类型候选若内容携带 `task=<t>` 标记且 `t != TaskType` → drop（计入 `Dropped`）。其他类型不受影响。理由：outcome/skeleton 卡跨任务型注入正是「实例级定位在低重复场景误导」的失败模式，同型准入是最小对策。

### 2.5 `harness/semantix/bridge.go`：消费准入收紧

- 注入前对 query 跑 `ClassifyTask`，填 `Injector.TaskType`；
- ByType 收紧（#268/W0 实测：工具名级与结果级切片跨任务无区分度、93.4% 伪命中）：`tool_pattern` 与 `result` 的 zone 阈值抬到不可达（恒 Miss），即 pilot 报告下一步 (1) 的「T/R 缺省不注入」。`prompt` 保留（gateway-m1 重复 query 场景有正面证据）、`context`/`memory` 保持默认。

### 2.6 不做（明确出范围）

plan-first 规划步（消费端架构）、gateway 侧 distill 接线、hybrid 检索接桥、per-instance 成本断路器、zone 小库保护。各自独立成题。

## 3. 契约影响

- **wire 契约零变更**：无新 SliceType、无新 event Kind、无落盘 schema 变更（新卡是常规 Slice 内容）；
- CLI 仅加法（两个默认关的 flag）；
- bridge 行为变更两处（TaskType 准入、T/R 恒 Miss），均 fail-open：分型失败 → `general` → M 卡同型准入退化为「只准 general 型」，检索/库不可用路径不变。

## 4. 测试

- `task_type_test.go`：表驱动分型（中英、边界、默认 general）；
- `distill_test.go`：真实镜像形状 fixture（role 行 + kind 事件行 + 双写工具结果行）→ 三种卡的内容断言、去重、配对正确性、空输入/无命中返回空；
- `inject` TaskType 准入测试（同型放行、异型 drop、空 TaskType 现状不变）；
- `extract` flag 测试（--distill 追加产物、--consolidate 触发合并）；
- bridge 测试：T/R 不再进注入块、TaskType 传递。

## 5. 验收

`go build ./... && go test ./kernel/... ./cmd/... ./harness/semantix/` 全绿；`semantix extract --distill --consolidate` 对 pilot 会话镜像产出三类卡且 `semantix search` 可检回。


## 6. 当前实现补记（2026-09-18，memory-flow 修复）

> **2026-09-24 更新**：本节描述的是 2026-09-18 时点的行为。此后 S2（`2af62ed6`）撤除了 score/coverage/margin/来源数/runner-up/最小库六项默认准入否决，S6（`a17ba134`）移除了 task 标签默认否决（标签改为 L2 历史元数据）；本节涉及"门槛不变/仍需通过 margin 与来源数准入"的表述仅作历史记录。当前准入以 [issue-447-memory-flow-repair SPEC §9](issue-447-memory-flow-repair.md) 为准：类型/origin/freshness/zone/预算保留，六项代理否决不再默认启用。

上文保留 2026-09-01 的历史设计与当时准入说明；以下记录当前实现，不把修复后的行为追记为原有行为。

- **四层载体不变**：L-A repo-ops / L-B consolidation 使用 `Context`，L-C plan-skeleton / L-D outcome 使用 `Memory`；仍由 `Extract`、`Distill`、`ConsolidateContext`、既有 Store 与 Injector 串接，不新增 SliceType 或模型提炼步骤。
- **观察不等于成功**：repo-ops 标题现为 `Observed commands`。只有 host `verification=passed` 才记为 `ok`；`failed`、文本中的 `exit N` 和 `unknown` 分别保留各自含义，缺失结果不构成成功证据。工具输出中的成功字样和 assistant 自述不提升验证状态。
- **Result 与 outcome 共用验证判定**：保留独立 assistant final 和实际工具结果顺序；`type=tool` 与 `role=tool` 都可读取。`Verified-by` / Result verified 依赖最新 mutation 之后的 host 验证；后续 mutation、失败或未知测试、未完成的测试调用撤销此前成功。host 显式 `not_verification` 优先于命令名猜测，普通读取不会冒充新测试。
- **来源与合并**：新增可选 `Meta.source_sessions`，旧 `SourceSession` 仍为主来源，旧记录缺字段仍可读。Context 只在 scope、origin、revision、dependency、verification 等 metadata 兼容时按原相似度规则合并，并保留实际来源的去重集合。CLI 对相同内容的兼容重复提取先调用 `RetainExtractionHistory` 再 `Put`，保留已观察来源及使用反馈；Store.Put 的替换语义不变。它不把一条来源复制成两份证据，也不按同一 session 折叠不同事实。
- **自动提取来源**：`semantix extract --origin session-auto` 明确标记自动 runner 产物，自动链路与 `--distill --consolidate` 一并传入；手工提取省略 `--origin` 仍默认 `user-curated`。CLI 只接受这两种取值，来源由调用链如实传递，不以提高信任等级争取准入；新二进制的 `extract --help` 已核实该参数和默认值。
- **共享候选资格与来源下限**：完整库 BM25 分数保留，由共享 `BuildHits` 在 type/status/task/freshness/origin 资格判断后取至多五个候选；原前五拒绝仍有 reason，诊断用 slice ID 关联来源。score、coverage、margin、来源数和预算门槛不变。Bridge 此次补接原 Issue #279 的 `session-auto` floor；旧版这里曾漏接线，import/legacy 仍存储但不进入 strict/shadow 准入，不能把“库中有卡”或普通检索命中当作已注入。
- **消费边界**：当前 bridge 的既有 allowlist 含 `Context`、`Memory` 和仅 host-verified 的 `Result`；未验证 Result 仍受拒绝。四层知识不意味着四类 Slice 无条件注入，仍需通过任务型、来源、freshness、相关性、margin 与预算准入，具体见 [L2 准入策略的当前实现补记](semantix-l2-admission-policy.md#10-当前实现补记2026-09-18memory-flow-修复)。
- **验证含义**：E2E 使用两个历史 session 的真实 HarnessSink 镜像，经生产提取、提炼、合并、存储和后续 Agent.Run 到 provider 请求；历史工具验证与 provider 记录器分别证明证据和传输，不能据此声称真实模型收益。原 orchard Result/outcome 近分轨迹保留为歧义拒绝反例，未通过改词、去重或降低门槛制造通过。

完整的本次实现范围、生命周期/验证修复、兼容性与未达成项见 [Issue #447 memory-flow 修复 SPEC](issue-447-memory-flow-repair.md)，实测结果见[实验报告](../reports/issue-447-memory-flow-repair.md)。
