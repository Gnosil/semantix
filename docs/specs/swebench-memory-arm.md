# Spec：SWE-bench 记忆内核实验臂（memory arm）

状态：已实现（见「实现与验证」）。背景：50 题 campaign 复盘发现 semantix 臂全程运行在**记忆内核关闭态**——语义库零写入、零检索、无学习曲线（顺序×步数相关性 -0.00），报告 §2.1 的两臂差异全部来自 agent 策略层而非语义层。本 spec 定义把记忆内核真正接入基准所需的四项修复、实验臂协议与可观测口径。

## 1. 根因链（四个独立缺陷，缺一即全链失效）

| # | 缺陷 | 位置 | 后果 |
|---|---|---|---|
| R1 | runner 生成的 config.toml 没有 `[semantix]` 段，`SemantixConfig.Enabled/Inject` 默认 false | `scripts/swebench/run_bench.py` SemantixAdapter.prepare | 内核桥整体短路（`bridge.Enabled()==false`） |
| R2 | `RenderTOML` 模板不渲染 `[semantix]` 段 | `harness/config/render.go` | 任何配置保存/迁移（如缺 `config_version` 触发的落盘迁移）**静默删除**该段——install.sh 开启的记忆在第一次配置保存后即消失；同进程二次加载读到已重写文件 |
| R3 | 会话镜像永远空：同步 run 从不发 `TurnDone`，headless 退出不经过 `Close`；且 `TurnStarted` 不带用户文本、sink 构造 `firstUserText` 恒空 | `harness/agent/run_loop.go`、`harness/semantix/{bridge,sink}.go` | 镜像 JSONL 0 字节且永无 user 行 → `semantix extract` 无输入，Prompt 切片不可能产生 |
| R4 | 内核计数不进 `--metrics` | `harness/cli/run_metrics.go` | 「记忆臂真的注入了」与「记忆臂冷跑」不可区分，实验不可判读 |

另有两项非本次范围的既有事实（见 issue）：`--ablate` 只覆盖 harness 侧模块、不控制内核（`full-fold` 模块无消费者）；L3 判定链只部署在 gateway，agent 路径不经过 `DecideL3`。

## 2. 修复设计

### R1/切片库共享（runner）

- 新 flag `--semantix-memory on|off`（默认 **on**）。on 时 config.toml 写入
  `[semantix] enabled/inject/budget=4096/project_dir/sessions_dir`；off 为消融孪生臂（不写段）。
- **每实例独立 `SEMANTIX_HOME`**（`semantix-home/inst/<iid>/`）：会话镜像、stats、projects 天然按实例隔离，`--workers` 并发下归属无竞态；**只有切片库共享**——所有实例的 `project_dir` 指向同一 `semantix-home/kernel/`（库文件 `kernel/.semantix/project.db`）。
- 每实例结束后 runner 以 `semantix extract --input <mirror> --scope project --project-db <shared>` 收割切片（`threading.Lock` 串行化：库的 append journal 是单写者）；extract 结果尾部写入该实例 metrics 的 `raw.extract`。
- 新 flag `--semantix-kernel-bin`（默认 `bin/semantix`）。

### R2（config 渲染往返）

`RenderTOML` 增加 `[semantix]` 段渲染：布尔恒打印，可选字段非零才打印、否则注释示例（house style）。既有 render 往返测试覆盖。

### R3（镜像管线）

- `TurnStarted` 事件携带该轮用户文本（`run_loop.go`）；`HarnessSink` 在 TurnStarted 用它填 `first`。
- 新增 `HarnessSink.EndTurn()` / `Bridge.EndTurn()`：flush 并关闭当前轮；`Agent.emitReuse`（所有 Run 返回路径必经）调用之。TurnDone 语义不变（同步 run 依旧不向 UI 流发 TurnDone）。

### R4（可观测）

- 新 Notice 码 `semantix_inject`（Detail `{"bytes":n}`），每用户轮注入块非空时发一次。
- `RunMetrics` 新增：`semantix_inject_turns` / `semantix_inject_bytes` / `semantix_reuse_hits` / `semantix_reuse_savings_usd`（omitempty，旧读者无感）。metricsSink 消费 `semantix_inject` 与既有 `semantix_reuse` Notice。

## 3. 实验臂协议（替代旧 §4「--ablate all 隔离记忆内核」的错误设想）

- **记忆对照 = `--semantix-memory on` vs `off` 两臂**（同子集、同模型、同 preset）。`--ablate` 不用于记忆对照（它只关 harness 侧 planner/subagent/evidence/harness-memory/compaction）。
- on 臂按 preds.jsonl 处理序天然形成课程：判读时报告 (a) 全臂 resolve；(b) 注入覆盖率 `semantix_inject_turns>0` 的实例占比；(c) 处理序前/后半 resolve 与步数差（学习曲线）；(d) 每注入字节的边际成本。
- 由于处理序影响 on 臂（先跑的题喂后跑的题），跨臂公平性以「同 50 题整体」比较；更严谨的顺序敏感性留待多 seed 重复。

## 4. 实现与验证

- 代码：上述四点全部落地（本 PR）。`go build ./...`、`go vet`、touched 包 `-race` 测试绿（唯一失败 `TestSaveToUnwritableUserSymlinkTargetPreservesLink` 为 root 容器环境既有问题，与改动无关）。
- 端到端冒烟（mock provider，2 实例串行）：实例 1 `extracted=2 stored=2`、无注入（冷库，符合预期）；实例 2 提取入库并 **`semantix_inject_turns=1 / semantix_inject_bytes=3252`**——跨实例记忆闭环打通。`--semantix-memory off` 回归旧行为（无 extract/注入）。

## 5. 已知边界

- extract 的切片为浅统计提取（Prompt/Context/ToolPattern/Result）；LLM 蒸馏式经验（ReasoningBank/AWM 路线）不在本 spec 范围。
- agent 路径检索为 BM25-only（`bridge.kernelIndex` 硬编码），嵌入/混合检索与 L3 接入 agent 路径均为后续工作。
- 镜像不含 reasoning 与 tool 输出全文截断策略沿用 sink 现状。
