# SWE-bench Verified harness comparison（semantix / claude-code / codex / dsh × DeepSeek）

同一模型（DeepSeek）× 不同 harness 跑 SWE-bench Verified，统一采集四类指标：

| 指标 | 来源 |
| --- | --- |
| **resolve rate（分数）** | 官方 `swebench.harness.run_evaluation`（Docker） |
| **token 消耗**（input / output） | 各 harness 原生遥测或线级计量代理 |
| **缓存命中率** | `cache_hit / (cache_hit + cache_miss)`，取 provider 上报的 cache token 计数 |
| **耗时** | runner 计的单实例墙钟 + harness 自报 API 时长（raw 内） |

成本按 DeepSeek 价目表（cache-hit / cache-miss / output 三价）由命中/未命中 token 数折算，另保留 harness 自报成本作对照。

## 各 harness 的接入与计量方式

| harness | 模型接入 | token/缓存计量 |
| --- | --- | --- |
| `semantix` | `semantix-agent run -p`，OpenAI 协议直连 `api.deepseek.com`。provider key **只从 Semantix 全局 `.env` 解析**（不读进程环境变量），adapter 自动写入 `$SEMANTIX_HOME/.env` | 原生 `--metrics` JSON（`cache_hit_tokens`/`cache_miss_tokens` 即 DeepSeek 上报计数） |
| `claude-code` | `claude -p`，走 DeepSeek 的 Anthropic 兼容端点 `api.deepseek.com/anthropic` | 结果 JSON 的 `usage`（`cache_read_input_tokens` = 命中） |
| `codex` | `codex exec --json`，默认 **Responses API**（DeepSeek 原生支持 `/v1/responses`），当前版 codex 直接可用。legacy chat 协议需 codex ≤0.80.0（`--codex-bin` + `--codex-wire-api chat`），且 DeepSeek 对 chat 多轮 tool 消息序校验严格，容易 400——不建议 | codex 自身 token 上报不可靠，经 `count_proxy.py` 计量代理旁路记录 provider 返回的 usage（含缓存字段） |
| `dsh` | DeepSeek Harness（`npm i -g @deepseek-ai/dsh`）headless profile；`DSH_PERMISSION_MODE=danger-full-access` 免审批。**dsh 的 node fetch 不识别 HTTPS_PROXY**，在代理沙箱中必须经计量代理中转（adapter 自动处理） | 首选 `count_proxy.py` 线级 usage；回退解析 `$DSH_HOME/sessions/**/session.jsonl.zstd` 的 usage 事件（`cacheReadTokens` = 命中） |
| `custom` | 任意 CLI，`--custom-spec spec.json` 描述命令模板与 usage 抽取 | spec 内 `usage_file` / `usage_regex` |

四条 adapter 均已通过 `mock_provider.py` 冒烟（本地双协议 mock，无需 key / 无需出网），指标管线验证过：token、命中率、成本、patch 提取、preds.jsonl 全链路正确。

## 前置条件

1. **DEEPSEEK_API_KEY**：`export DEEPSEEK_API_KEY=sk-...`（远程环境请配到环境变量，容器里没有你本机的 `~/.reasonix/.env`）。
2. **出网放行**（远程沙箱的 egress policy 需允许，否则全部 403）：
   - `api.deepseek.com` —— 所有 harness 的模型调用
   - `huggingface.co` + `cdn-lfs.huggingface.co` —— 拉取 SWE-bench Verified 数据集
   - Docker Hub（`registry-1.docker.io`、`auth.docker.io`、`production.cloudfront.docker.com`）或 ghcr（`ghcr.io`、`pkg-containers.githubusercontent.com`，Epoch 预构建镜像）—— 官方评测镜像
   - `github.com` —— 任务仓库 checkout（一般已放行）
3. **Docker daemon** 运行中（评测阶段需要；~120GB 磁盘跑全量，50 实例子集约 10-20GB）。
4. `pip install swebench zstandard datasets`；`go build -o bin/semantix-agent ./cmd/semantix-agent`。

## 跑一轮

```bash
cd scripts/swebench

# 0) 数据集（一次性）
python3 -c "from pathlib import Path; from common import fetch_dataset; fetch_dataset(Path('data/swebench_verified.jsonl'))"

# 1) 生成 patch（四个 harness 各一轮；--sample 50 --seed 固定可复现子集）
python3 run_bench.py --harness semantix    --model deepseek-v4-flash --dataset data/swebench_verified.jsonl --sample 50 --workers 4
python3 run_bench.py --harness dsh         --model deepseek-v4-flash --dataset data/swebench_verified.jsonl --sample 50 --workers 4
python3 run_bench.py --harness claude-code --model deepseek-v4-flash --dataset data/swebench_verified.jsonl --sample 50 --workers 4
python3 run_bench.py --harness codex       --model deepseek-v4-flash --dataset data/swebench_verified.jsonl --sample 50 --workers 4 \
    --codex-bin ~/.local/codex080/node_modules/.bin/codex

# 2) 官方评测（每个 run 目录一次）
python3 evaluate.py --run-dir results/<run_id> --dataset data/swebench_verified.jsonl --max-workers 4

# 3) 汇总对比表
python3 report.py --runs results/semantix.* results/dsh.* results/claude-code.* results/codex.*
```

冒烟自检（无 key、无出网，验证管线本身）：

```bash
python3 mock_provider.py --port 8139 &
python3 run_bench.py --harness semantix --model deepseek-v4-flash \
  --dataset <smoke.jsonl> --run-id smoke --openai-base http://127.0.0.1:8139 \
  --semantix-bin ../../bin/semantix-agent --semantix-kernel-bin ../../bin/semantix
```

### semantix 记忆内核臂（`--semantix-memory`，默认 on）

`--semantix-memory on`（默认）把记忆内核真正接入基准：config 写入 `[semantix]`
enabled/inject；**每实例独立 `SEMANTIX_HOME`**（并发归属无竞态），所有实例的
`project_dir` 指向**真实 repo 独立切片库**
（`semantix-home/kernel/<owner>__<repo>/`）；每实例结束后 runner 跑
`semantix extract` 把该会话蒸馏进库，后续同 repo 实例即可检索注入（跨实例
记忆闭环，需 `go build -o bin/semantix ./cmd/semantix`）。`off` 为消融孪生臂
（内核关闭，等价旧行为）。记忆对照请用这对臂，**不要用 `--ablate`**（它只关
harness 侧模块，不触碰内核）。判读字段（metrics `raw`）：
`semantix_inject_turns` / `semantix_inject_bytes` / `semantix_reuse_hits` /
`semantix_fuse_turns` / `semantix_rejected_slices` / `raw.extract`（每实例入库量）。
设计与根因：`docs/specs/swebench-memory-arm.md`。

#### 调用来源归因字段

Semantix 的 `steps` 是所有已计费模型调用总数，不等同于 executor 主循环轮数。
runner 会把原生 `usage_by_source` 规范化进每实例 `metrics.jsonl`：

| 字段 | 含义 |
|---|---|
| `steps` | 所有来源的已计费模型调用总数 |
| `model_calls_by_source` | 原生完整来源表；未知的新来源原样保留 |
| `executor_calls` / `planner_calls` / `subagent_calls` / `compaction_calls` | 四个主要来源的稳定便捷计数 |
| `other_model_calls` | classifier、title、capability-router、recovery-reviewer、goal-evaluator 及未来来源之和 |
| `source_call_total` | `model_calls_by_source` 的调用数总和 |
| `source_call_delta` | `steps - source_call_total`；当前完整 metrics 预期为 0，旧记录可能非 0 |
| `provider_retries` | provider 传输重试事件数；没有 Usage 事件时不计入 `steps` |
| `compactions` | compaction 尝试次数；与实际产生 Usage 的 `compaction_calls` 不同 |
| `tool_calls_by_name` | 已完成工具调用按 canonical tool name 汇总 |
| `repeated_tool_calls` | 同一 provider-visible 工具名 + canonical JSON 参数在首次完成后的重复次数 |
| `repeated_tool_calls_by_name` | 上述重复按工具名拆分；只保留参数摘要，不写原始参数 |
| `semantix_fuse_turns` | 已有 loop/progress guard 清除本轮历史块的次数（每 user turn 最多一次） |
| `semantix_rejected_slices` | 熔断时被关联并累计 `Rejected` 的注入 slice 数 |

`report.py` 的 Markdown 表用 `calls E/P/S/C/O` 显示
executor/planner/subagent/compaction/other；JSON 输出保留完整来源表和工具表。

#### Shadow retrieval 实验臂

`--semantix-retrieval-mode off|shadow|strict`（默认 `strict`）控制开启内核后的
L2 路径。`shadow` 会检索、执行相同的 zone/清洗/预算判定并记录 `kernel_cache`
诊断，但不向 provider 消息添加复用块；因此可作为 A/B 臂 B，与
`--semantix-memory off` 做请求字节不变量检查，再用其分数分布标定后续门禁。

#### Repo 隔离与调度

memory-on 时 runner 按数据集 `repo=owner/repo` 分组。同 repo 实例严格保持冻结
子集中的选中顺序并在同一 worker 内串行；不同 repo 才使用 `--workers` 并行。
非法或缺失 repo 标识会在调度前失败，不会落入共享的 unknown 库。每实例
`raw.semantix_repo` / `raw.semantix_project_dir` 记录实际归属，便于审计。

#### Issue #447 A/B/C 核心配对矩阵

`memory_matrix.py` 默认按 repetition 内 A→B→C 串行执行，避免跨臂的
provider/CPU 竞争；每个 repetition/arm 使用独立 state/work 目录，同时复用同一
`--ids` 顺序。传入 `--legacy-semantix-bin` 时才追加 D；D 只用于复现旧策略，
不属于判断当前修复是否优于 memory-off 的核心实验：

所有 CLI 路径会在 runner 启动时解析为绝对路径。这样 agent 切换到每个实例的
workspace 后，仍会读取同一个 state/config/credential 目录并把 metrics 写回预期的
results 目录；从 `scripts/swebench` 传入相对路径与绝对路径语义一致。

| 臂 | 配置 | agent binary |
|---|---|---|
| A | `memory=off`, `retrieval=off` | 当前版本 |
| B | `memory=on`, `retrieval=shadow` | 当前版本 |
| C | `memory=on`, `retrieval=strict` | 当前版本（P0 门禁） |
| D（可选） | `memory=on`, `retrieval=strict` | P0.4 前的 legacy all-type 版本 |

```bash
python3 memory_matrix.py \
  --dataset data/swebench_verified.jsonl \
  --ids subsets/verified-50-s20260824.txt \
  --model deepseek-v4-flash --repetitions 3 --workers 4 \
  --semantix-seed-dir state/issue447-frozen-seed \
  --semantix-bin ../../bin/semantix-agent \
  --semantix-kernel-bin ../../bin/semantix

# 对生成的每个 run 完成官方 evaluate.py 后：
python3 memory_matrix_report.py \
  --manifest results/issue447-memory.matrix.json \
  --format md --out results/issue447-memory.report.md
```

首次单实例端到端预跑及其因果边界见
[`docs/reports/issue-447-memory-matrix-pilot.md`](../../docs/reports/issue-447-memory-matrix-pilot.md)。

`--semantix-seed-dir` 接收一个冻结的 repo-store 根目录，内部结构与 runner 的
`kernel/<owner>__<repo>/` 一致。矩阵会在 B/C（以及显式启用的 D）第一次启动时
分别复制同一份 seed，
A 不读取 seed；断点续跑通过 `.seed-source.json` 识别已经完成的复制，不会覆盖运行中
新提取的切片。若目标 state 已有数据但没有 seed 标记，runner 会直接报错，避免把
未知历史与冻结语料混在一起。发布实验结果时应同时保存 seed 生成命令、输入 session
列表和 repo 顺序。

矩阵启动前先执行 fail-closed 预检；它逐 repo 检查 store 是否存在、library 是否达到
strict 的最小规模、同类型来源 session 是否达到门槛，以及可注入切片是否携带
`base_commit`。退出码 `0` 才进入真实评测，退出码 `3` 表示 seed 尚未就绪：

```bash
python3 validate_memory_seed.py \
  --seed-dir state/issue447-frozen-seed \
  --dataset data/swebench_verified.jsonl \
  --ids subsets/verified-50-s20260824.txt \
  --json-out results/issue447-seed-validation.json
```

runner 从 session mirror 的文件参数中提取仍位于 workspace 内的普通文件，并在 extraction
时写入 SHA-256 dependency fingerprint 与当前 `base_commit`。strict retrieval 在提交不同
且没有依赖证明、依赖缺失/越界/变化或 provenance 缺失时拒绝该切片；共享 `project_dir`
不再被误当作实例的 git workspace。

需要历史策略对照时，显式传入 `--legacy-semantix-bin`；该 binary 应固定构建自
`cb5e9cc`（repo 隔离已合并、strict 仍为旧全类型策略），不能用 harness
`--ablate all` 代替。矩阵 manifest 保存每个 run 的完整
命令、state/work/run 路径；重复执行会沿用 `run_bench.py` 的按实例续跑能力。

报告严格按 `(repetition, instance_id)` 与 A 配对；任一臂缺实例立即报错。输出
resolved、executor calls、steps、input tokens、工具总数和 read/search/test 工具族、
wall、cost、retry、注入次数/字节的 absolute 与相对 A 的 median/P75/P90。接入
`repeated_tool_calls` 后，Markdown 显示 `Δ repeats median/P75/P90`；接入熔断后还显示
`Δ fuses median/P75/P90`，JSON 同时保留 rejected slice 数和重复 read/search/test
工具族。旧 metrics 缺少重复字段时维持 unattributed，不会把“未采集”伪装成 0。
重复调用本身仍只是轨迹信号；只有 harness 已有 loop/progress guard 判断为持续无进展时，
才移除当前 turn 的历史并记录 `SliceReject(reason=loop_guard)`。

## 方法学要点（对比公平性）

- **同一 prompt 模板**（`common.PROMPT_TEMPLATE`）喂给所有 harness；system prompt 保持各 harness 原生（那正是 harness 差异的一部分）。
- **同一冻结子集**（`--sample N --seed S` 或 `--ids` 文件），同一模型、同一时段（DeepSeek 2026-08-16 起分峰谷计价，跨时段跑会引入成本噪声；成本按 off-peak 表折算，峰时 ×2）。
- patch 提取统一为 `git add -A && git diff --cached`（工作区全部改动，含新文件）。
- 切片库（`project_dir`）仅在同 repo 实例间共享；repo 之间物理隔离。如需无记忆对照，用 `--semantix-memory off`，或换 `--run-id` 清空 state。
- 单实例 exit code 不作成败信号（semantix 有已知的非零退出怪癖），以 diff 非空 + 官方评测为准。
- 每实例默认 2400s 超时；超时/崩溃记为 error，patch 照常提取（可能为空）。

## 已知边界

- codex 0.80.0 是 chat 协议末版；其原生 usage 恒为 0（`raw.usage_total` 保留作证），线级 `wire_usage` 才是权威计数。
- claude-code 的 `total_cost_usd` 按 Anthropic 价目计算，对 DeepSeek 端点无意义，仅存 `cost_native` 作对照；统一成本一律按 DeepSeek 价目由 token 折算。
- dsh 处于 developer preview（0.1.x），会话格式可能变；解析器只依赖 `assistant/chunk` 的 usage 事件。
- `deepseek-chat` / `deepseek-reasoner` 已于 2026-07-24 退役；用 `deepseek-v4-flash` / `deepseek-v4-pro`。
