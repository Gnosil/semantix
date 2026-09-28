# Harness L2 persist placement（只追加的注入块）

## 状态

已实现（`harness/agent/run_loop.go`、`sampling_request.go`、`progress_guard.go`、`compact.go`；测试 `harness/agent/semantix_persist_test.go`）。默认仍为旧行为（`placement = "ephemeral"`），新行为需显式开启：

```toml
[semantix]
mode = "strict"
placement = "persist"
```

## 背景

旧行为（ephemeral）：每个 provider 请求都在当前 user 消息前临时插入本轮的 `[semantix-reuse]` 块，块不写入 canonical transcript。本地 overlay 测量显示：

- 下一个用户轮开始时，旧块从历史中间消失，前缀从旧块位置起失配，即使两轮的块字节完全相同；
- 无块的轮次不追加 policy 句，system 前缀随之变化，整段失配；
- 轮中追加的 user 消息（steer、nudge）让块移位。

在 provider 前缀缓存下，这些都是对已缓存前缀的改写：之后的整段历史按缓存写入价重付。

## 设计（placement = "persist"，仅 strict 模式）

1. **嵌入而非插入**：本轮块在 user 消息写入 canonical transcript 之前组装，拼在该消息 `Content` 的头部；`RawContent` 保留用户原文（标题、预览、rewind 继续使用原文）。之后每个请求原样回放，不再调用 `prependSemantixHistory`。纯追加，因此 projection 的 `CoveredPrefixHash` 保持有效，rewind/fork 把块和 user 消息当作一个整体。
2. **policy 句是会话常量**：strict + persist 时每个请求都在第一条 system 消息末尾带固定的信任政策，无论本轮有没有块，system 前缀不再随轮次切换。shadow/off 不受影响，仍与无 L2 请求逐字节相同。
3. **晚到的 prefetch 丢弃**：本轮 user 消息写入之后才到的块无法在不改写历史的前提下放入，直接丢弃，下一轮重新检索。
4. **熔断改为追加撤回**：loop guard 触发时不能删除已持久化的块，改为追加一条 `[semantix-retraction]` user 消息点名撤回的切片；该前缀列入 `SyntheticUserPrefixes`，不计为用户轮次。
5. **compaction 摘要剔除块**：`renderTranscript` 在把 user 消息交给摘要器前去掉 `[semantix-reuse]…[/semantix-reuse]` 区段，防止不可信的历史证据被洗成“用户写的摘要”。

## 取舍

- 块会在上下文里累积（旧轮次的块继续可见），上下文更长、compaction 更早；换来的是块只按写入价付一次，之后按读取价。
- 旧块的内容对后续轮次仍可见，这改变了模型看到的信息；是否影响任务质量需要真实运行的 ON/OFF 对照来验证。
- 本规格修订了 `semantix-l2-history-authority-and-budget.md` §2 中“不写回 canonical session”与“shadow/off 没有正文时不追加政策”两条，修订范围仅限 strict + persist。

## 验证

- `TestPersistPlacementIsAppendOnly`：两个真实用户轮经 `a.Run`，第二轮请求按消息逐条扩展第一轮请求；每个请求都带 policy 句；transcript 中 user 消息以块开头而 `RawContent` 不含块。同一夹具在默认 ephemeral 下不是纯追加（回归对照）。
- `TestPersistFuseAppendsRetraction`、`TestStripSemantixReuseForSummary`。
- `go test ./harness/... -race`：除 3 个在 root 用户下 chmod 失效的既有测试外全部通过（与基线相同）。
