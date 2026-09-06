# Spec：SWE-bench 记忆内核实验臂（memory arm）

状态：已实现（见「实现与验证」）。背景：50 题 campaign 复盘发现 semantix 臂全程运行在**记忆内核关闭态**——语义库零写入、零检索、无学习曲线（顺序×步数相关性 -0.00），报告 §2.1 的两臂差异全部来自 agent 策略层而非语义层。本 spec 定义把记忆内核真正接入基准所需的四项修复、实验臂协议与可观测口径。

## 1. 根因链（四个独立缺陷，缺一即全链失效）

| # | 缺陷 | 位置 | 后果 |
|---|---|---|---|
| R1 | runner 生成的 config.toml 没有 `[semantix]` 段，`SemantixConfig.Enabled/Inject` 默认 false | `scripts/swebench/run_bench.py` SemantixAdapter.prepare | 内核桥整体短路（`bridge.Enabled()==false`） |
| R2 | `RenderTOML` 模板不渲染 `[semantix]` 段 | `harness/config/render.go` | 任何配置保存/迁移（如缺 `config_version` 触发的落盘迁移）**静默删除**该段——install.sh 开启的记忆在第一次配置保存后即消失；同进程二次加载读到已重写文件 |
| R3 | 会话镜像永远空：同步 run 从不发 `TurnDone`，headless 退出不经过 `Close`；且 `TurnStarted` 不带用户文本、sink 构造 `firstUserText` 恒空 | `harness/agent/run_loop.go`、`harness/semantix/{bridge,sink}.go` | 镜像 JSONL 0 字节且永无 user 行 → `semantix extract` 无输入，Prompt 切片不可能产生 |
| R4 | 内核计数不进 `--metrics` | `harness/cli/run_metrics.go` | 「记忆臂真的注入了」与「记忆臂冷跑」不可区分，实验不可判读 |

另有两项 issue 遗留（#428-B，已随本分支补完）：`--ablate` 此前只覆盖 harness 侧模块、不控制内核（且 `full-fold` 模块无消费者，已移除）——现新增 `kernel` 消融模块，`--ablate kernel` 在 boot 时强制关桥，单进程内即可做记忆消融；L3 判定链只部署在 gateway，agent 路径不经过 `DecideL3`（仍未变）。

## 2. 修复设计

### R1/切片库按 repo 共享（runner）

- 新 flag `--semantix-memory on|off`（默认 **on**）。on 时 config.toml 写入
  `[semantix] enabled/inject/budget=4096/project_dir/sessions_dir`；off 为消融孪生臂（不写段）。
- **每实例独立 `SEMANTIX_HOME`**（`semantix-home/inst/<iid>/`）：会话镜像、stats、projects 天然按实例隔离。Project 库只在同 repo 内共享：`project_dir` 指向 `semantix-home/kernel/<owner>__<repo>/`（库文件 `<owner>__<repo>/.semantix/project.db`），杜绝跨 repo 检索污染。
- 每实例结束后 runner 以 `semantix extract --input <mirror> --scope project --project-db <repo-store> --project <owner/repo>` 收割切片；extract 结果尾部写入该实例 metrics 的 `raw.extract`，实际 repo/store 写入 `raw.semantix_repo` / `raw.semantix_project_dir`。
- memory-on 调度按 repo 分批：同 repo 保持冻结子集选中顺序、在一个 worker 内串行，因此 append journal 始终单写；不同 repo 的独立 store 可并行。缺失或不安全的 repo 标识直接失败，不共享 fallback 库。memory-off 与其他 adapter 保持既有实例级并行。
- 新 flag `--semantix-kernel-bin`（默认 `bin/semantix`）。

### R2（config 渲染往返）

`RenderTOML` 增加 `[semantix]` 段渲染：布尔恒打印，可选字段非零才打印、否则注释示例（house style）。既有 render 往返测试覆盖。

### R3（镜像管线）

- `TurnStarted` 事件携带该轮用户文本（`run_loop.go`）；`HarnessSink` 在 TurnStarted 用它填 `first`。
- 新增 `HarnessSink.EndTurn()` / `Bridge.EndTurn()`：flush 并关闭当前轮；`Agent.emitReuse`（所有 Run 返回路径必经）调用之。TurnDone 语义不变（同步 run 依旧不向 UI 流发 TurnDone）。

### R4（可观测）

- 新 Notice 码 `semantix_inject`（Detail `{"bytes":n}`），每用户轮注入块非空时发一次。
- `RunMetrics` 新增：`semantix_inject_turns` / `semantix_inject_bytes` / `semantix_reuse_hits` / `semantix_reuse_savings_usd`（omitempty，旧读者无感）。metricsSink 消费 `semantix_inject` 与既有 `semantix_reuse` Notice。

### P0 调用来源归因（Issue #447）

`RunMetrics.steps` 已包含 executor、planner、subagent、compaction 和其他辅助模型调用，不能直接当作主循环步数。Go 侧 `usage_by_source` 保持权威来源；SWE-bench runner 将其规范化为：

- 完整 `model_calls_by_source` 映射，未知来源不丢弃；
- `executor_calls`、`planner_calls`、`subagent_calls`、`compaction_calls` 四个便捷字段；
- `other_model_calls`，汇总 classifier/title/capability-router/recovery-reviewer/goal-evaluator 与未来来源；
- `source_call_total` 和 `source_call_delta = steps - source_call_total`，用于发现漏归因；
- `provider_retries`、`compactions`、`subagent_runs`、`tool_failures` 和 `tool_calls_by_name`，用于区分模型调用、传输重试、压缩尝试、委派与工具行为。

旧 metrics 缺少这些字段时全部按 0 读取；`source_call_delta` 保留其 `steps`，明确表示无法追溯，而不是错误归入 executor。该变更只扩展观测 JSON 和报告，不修改 agent、provider request 或记忆注入行为。

## 3. 实验臂协议（替代旧 §4「--ablate all 隔离记忆内核」的错误设想）

- **记忆对照 = `--semantix-memory on` vs `off` 两臂**（同子集、同模型、同 preset）。`--ablate` 不用于记忆对照（它只关 harness 侧 planner/subagent/evidence/harness-memory/compaction）。
- on 臂在每个 repo 内按固定选中顺序形成课程：判读时报告 (a) 全臂 resolve；(b) 注入覆盖率 `semantix_inject_turns>0` 的实例占比；(c) repo 内处理序与 resolve/步数差；(d) 每注入字节的边际成本。
- `--workers` 只改变不同 repo 的完成交错，不改变任何 repo 内课程顺序。跨臂必须复用同一冻结子集与 repo 内顺序；更严谨的顺序敏感性留待多 seed 重复。

## 4. 实现与验证

- 代码：上述四点全部落地（本 PR）。`go build ./...`、`go vet`、touched 包 `-race` 测试绿（唯一失败 `TestSaveToUnwritableUserSymlinkTargetPreservesLink` 为 root 容器环境既有问题，与改动无关）。
- 端到端冒烟（mock provider，2 实例串行）：实例 1 `extracted=2 stored=2`、无注入（冷库，符合预期）；实例 2 提取入库并 **`semantix_inject_turns=1 / semantix_inject_bytes=3252`**——跨实例记忆闭环打通。`--semantix-memory off` 回归旧行为（无 extract/注入）。

## 5. 已知边界

- extract 的切片为浅统计提取（Prompt/Context/ToolPattern/Result）；LLM 蒸馏式经验（ReasoningBank/AWM 路线）不在本 spec 范围。
- agent 路径检索为 BM25-only（`bridge.kernelIndex` 硬编码），嵌入/混合检索与 L3 接入 agent 路径均为后续工作。
- 镜像不含 reasoning 与 tool 输出全文截断策略沿用 sink 现状。
