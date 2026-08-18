# Semantix 事件契约（Event Contract）

> 对应 `kernel/event` 包。事件是 kernel 与 harness（Reasonix 等）之间的**唯一观测通道**：kernel 通过它们驱动语义切片提取、缓存统计、调度与进化信号。

**Wire 稳定性**：JSON 字段名是契约的一部分，不得更改。新增事件只能追加 Kind（`KindCount` 前插入），不得重排/删除既有 Kind 序号。

---

## 1. Event 信封（wire 格式）

```json
{
  "kind": 1,
  "session_id": "sess-abc123",
  "turn": 3,
  "at": "2026-08-08T12:00:00.000Z",
  "data": { "...payload..." }
}
```

| 字段 | 类型 | 说明 |
|---|---|---|
| `kind` | int | 事件类型（下表编号） |
| `session_id` | string | 会话 ID（跨会话复用的聚合键） |
| `turn` | int（omitempty） | 会话内 turn 序号 |
| `at` | RFC3339 | 事件时间 |
| `data` | object（omitempty） | 各 Kind 的 payload |

---

## 2. 事件全集（12 种）

| Kind | 序号 | 语义 | Payload 字段 | 触发方 |
|---|---|---|---|---|
| `TurnStarted` | 0 | 一个用户 turn 开始 | （无，SessionID/Turn 在信封） | harness 会话 |
| `Usage` | 1 | 模型调用 token 用量，含缓存命中明细 | `prompt_tokens`, `cache_hit`, `cache_miss`, `completion_tokens`, `cost_usd` | harness 模型层 |
| `ToolDispatch` | 2 | 一个工具调用被派发 | `call_id`, `name`, `args`(raw), `read_only` | harness 工具执行器 |
| `ToolResult` | 3 | 一个工具调用结果 | `call_id`, `ok`, `latency_ns`, `err_msg`(omitempty) | harness 工具执行器 |
| `ToolRoundEnd` | 4 | 一批工具执行汇总 | `dispatched`, `succeeded` | harness 工具执行器 |
| `SliceHit` | 5 | 语义缓存命中（L1 字节 / L2 语义注入 / L3 结果复用） | `layer`("L1"\|"L2"\|"L3"), `slice_ids`, `scores`(omitempty) | kernel 缓存层 |
| `SliceInject` | 6 | L2 稳定切片注入（按规范序） | `slice_ids`(canonical order), `bytes` | kernel 注入器 |
| `SliceReject` | 7 | 注入污染被拒绝（用户编辑/回滚） | `slice_id`, `reason`(omitempty) | kernel 验证层 |
| `PrefetchHit` | 8 | 投机预取被使用 | `targets` | kernel 预取器 |
| `PrefetchWaste` | 9 | 投机预取未被使用 | `targets` | kernel 预取器 |
| `Compact` | 10 | 上下文压缩（snip/prune/summary） | `trigger`("snip"\|"prune"\|"summary"), `before_tokens`, `after_tokens` | harness 压缩器 |
| `EvolutionTick` | 11 | 进化参数快照更新 | `params`(raw JSON) | kernel 进化引擎 |

**不变量**：
- `SliceHit.Layer ∈ {L1, L2, L3}`；`Compact.Trigger ∈ {snip, prune, summary}`。
- `ToolResult.latency_ns` 为纳秒（Go `time.Duration` 的 wire 编码）。
- `SliceInject.SliceIDs` 必须保持规范序（确定性注入，见架构设计 §4.2）。

---

## 3. Reasonix 事件映射表

Semantix 的 adapter 监听 Reasonix（或任意 harness）并翻译为上述契约：

| Semantix 事件 | Reasonix 对应点 | 备注 |
|---|---|---|
| `TurnStarted` | 会话/轮次开始（session 载入、新 turn 组装） | turn 边界 = 语义切片提取的切分点 |
| `Usage` | 模型流式调用后的 usage 汇总（provider 返回） | 含前缀缓存命中统计（`cache_hit`/`cache_miss`） |
| `ToolDispatch` | 工具执行器 `executeBatch`/`executeOne` 派发前 | 每个工具调用一条 |
| `ToolResult` | 工具结果回填会话前 | `ok=false` 时携带 `err_msg` |
| `ToolRoundEnd` | 一轮工具批执行完（`handleToolRound`） | 汇总计数 |
| `Compact` | 上下文压缩触发（snip 0.6 / prune 0.8 / force 0.9） | 阈值可配置 |
| `SliceHit` / `SliceInject` / `SliceReject` | （无对应） | **semantix 新增**：语义缓存闭环 |
| `PrefetchHit` / `PrefetchWaste` | （无对应） | **semantix 新增**：投机预取闭环 |
| `EvolutionTick` | （无对应） | **semantix 新增**：自进化闭环 |

> Reasonix 自身的工具修复（`NormalizeMessages`）、双模型会话（Coordinator）与三级压缩阈值是 harness 内部机制；semantix kernel **不干预**这些机制，只消费其可观测事件——保持 kernel 与 harness 解耦（架构设计 §2.2）。

---

## 4. 消费方

| 事件 | 消费方 |
|---|---|
| `TurnStarted` / `ToolDispatch` / `ToolResult` / `ToolRoundEnd` | 切片提取器（turn 切分 + T-Slice n-gram） |
| `Usage` | 成本统计、缓存收益核算 |
| `SliceHit` / `SliceInject` / `SliceReject` | 切片评分（命中/污染信号 → 进化引擎）。现状（2026-08-16）：切片评分原料由 store 统计回写直接采集（`slice.ApplyStats`，lookup/inject/gateway 四挂点），这三个事件仍零生产者，留给 harness 闭环接线 |
| `PrefetchHit` / `PrefetchWaste` | 预取策略调参 |
| `Compact` | 压缩统计 |
| `EvolutionTick` | 参数持久化 + 审计 |
