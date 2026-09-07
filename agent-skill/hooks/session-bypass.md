# 会话旁路适配（把 agent 会话喂给 semantix）

semantix 的记忆从**会话记录**里提取。你的 agent 的会话只要是 JSONL
（每行一个 JSON 对象），就能喂给 `semantix extract`。

## 通用会话 JSONL 格式

```jsonl
{"role":"user","content":"我办案喜欢先用模板"}
{"role":"assistant","content":"好的","tool_calls":[{"id":"c1","name":"bash","arguments":{"cmd":"..."}}]}
{"role":"tool","tool_call_id":"c1","name":"bash","content":"输出"}
```

- `role`: `user` | `assistant` | `tool`
- `tool_calls`: assistant 行可带（工具调用序列 → ToolPattern 切片）
- 行序 = 时间序；容忍坏行（跳过）

## 三种接入方式（按侵入度排序）

### A. 会话后导出（零侵入，推荐起步）

你的 agent 每次会话结束后，把会话记录转成上述 JSONL 存到
`~/.semantix/sessions/<sessionID>.jsonl`，然后：

```bash
semantix extract --input ~/.semantix/sessions/<sessionID>.jsonl --scope user --project <业务域>
```

（也可加 `--fingerprint <模板/案卷路径>`：这些文件一变，相关记忆自动失效。）

### B. 事件旁路（实时，semantix-agent 参考实现）

Semantix Agent 已在 H1 挂载 `HarnessSink`
（`internal/semantix/sink.go`）：harness 事件流 → turn 级 JSONL
自动写 `~/.semantix/sessions/`，配置 `[semantix] enabled=true` 即开。

其他 agent 框架：找到自己的"事件/回调"点，用同样思路写一个 sink——
只依赖稳定事件子集（turn 开始 / 文本 / 工具调用 / 工具结果 / turn 结束）。

### C. 直接调用（无会话文件时）

如果你的 agent 不落盘会话，可在关键节点主动调：
```bash
printf '%s\n' '{"role":"user","content":"<本轮任务>"}' \
  '{"role":"assistant","content":"<本轮产出>"}' | semantix extract --input - --scope user
```

## 沉淀时机建议

- 每次会话结束（batch）
- 或：任务完成且验证通过后（高质量记忆）
- 办案场景：结案后沉淀（含流程 + 经验总结），开庭/阅卷后沉淀偏好
