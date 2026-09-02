# Issue #447：重复工具调用归因合同

> 状态：P1 测量基线（2026-09-02）
>
> 跟踪 Issue：[#447](https://github.com/Gnosil/semantix/issues/447)

## 1. 定义

一次已完成工具调用的签名为：

```text
SHA-256(provider-visible tool name + NUL + canonical JSON arguments)
```

同一 run 中第一个已完成签名不是重复；之后每个相同签名计一次 `repeated_tool_calls`，并按工具名计入 `repeated_tool_calls_by_name`。

参数 JSON 会先解析并重新序列化，因此对象 key 顺序不同不制造假差异。解析失败时使用 trim 后原始字节做摘要。工具名属于签名的一部分，相同参数但不同工具不是重复。

## 2. 数据流

1. 完整 `ToolDispatch` 提供 ID、provider-visible name 和 args；partial dispatch 不参与；
2. metrics sink 只保存 ID → `(name, digest)`；
3. 对应 `ToolResult` 到达后，签名才进入 completed 集合；
4. 已完成集合中已有该签名时累计 repeat；
5. result 后删除 pending ID；未完成/取消的 dispatch 不计调用或重复。

原始参数不进入 metrics JSON，也不长期保存在重复计数器。现有 `tool_calls_by_name` 继续统计所有已完成调用；新字段只统计首次之后的同参重复。

## 3. 解释边界

`repeated_tool_calls` 是轨迹信号，不自动等价“有害”：

- 同一测试在修改前后各跑一次可能是必要验证；
- 同一路径在内容变化后重读可能是有效确认；
- 相同命令在外部状态变化后重试可能合理。

因此该字段用于 A-D 配对、异常实例筛选和后续 fuse 输入。熔断必须再结合新证据增量、工具结果、修改/回滚、时间顺序与注入 slice ID，不能仅凭 repeat > 0 拒绝历史。

## 4. 输出

`run --metrics`：

```json
{
  "tool_calls": 12,
  "tool_calls_by_name": {"read_file": 5, "grep": 4, "bash": 3},
  "repeated_tool_calls": 3,
  "repeated_tool_calls_by_name": {"read_file": 2, "grep": 1}
}
```

SWE runner 将两个字段原样规范化到每实例 `metrics.jsonl`；`report.py` 聚合 total 和 per-name map，Markdown 显示 `tools/repeat`。

## 5. 验证

- canonical JSON key 顺序得到同一签名；
- 相同工具/参数第二次完成计 1；
- 不同参数不计重复；
- 不同工具名不计同签名；
- dispatch 本身不计完成或重复；
- legacy metrics 缺字段时聚合为 0/空 map；
- malformed/negative per-name counters继续 fail closed。

```powershell
go test ./harness/cli -run 'TestMetricsSinkAttributesRepeatedToolArguments|TestMetricsSinkAccountsToolCallsAndRetries' -count=1
python -m unittest scripts/swebench/test_metrics_attribution.py -v
```
