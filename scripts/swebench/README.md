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
`project_dir` 指向**同一共享切片库**（`semantix-home/kernel/`）；每实例结束后
runner 跑 `semantix extract` 把该会话蒸馏进库，后续实例即可检索注入（跨实例
记忆闭环，需 `go build -o bin/semantix ./cmd/semantix`）。`off` 为消融孪生臂
（内核关闭，等价旧行为）。记忆对照请用这对臂，**不要用 `--ablate`**（它只关
harness 侧模块，不触碰内核）。判读字段（metrics `raw`）：
`semantix_inject_turns` / `semantix_inject_bytes` / `semantix_reuse_hits` /
`raw.extract`（每实例入库量）。设计与根因：`docs/specs/swebench-memory-arm.md`。

## 方法学要点（对比公平性）

- **同一 prompt 模板**（`common.PROMPT_TEMPLATE`）喂给所有 harness；system prompt 保持各 harness 原生（那正是 harness 差异的一部分）。
- **同一冻结子集**（`--sample N --seed S` 或 `--ids` 文件），同一模型、同一时段（DeepSeek 2026-08-16 起分峰谷计价，跨时段跑会引入成本噪声；成本按 off-peak 表折算，峰时 ×2）。
- patch 提取统一为 `git add -A && git diff --cached`（工作区全部改动，含新文件）。
- semantix 的 `SEMANTIX_HOME` 在 run 内共享——跨实例记忆/缓存正是被测对象；如需无记忆对照，用 `--ablate` 或换 `--run-id` 清空 state。
- 单实例 exit code 不作成败信号（semantix 有已知的非零退出怪癖），以 diff 非空 + 官方评测为准。
- 每实例默认 2400s 超时；超时/崩溃记为 error，patch 照常提取（可能为空）。

## 已知边界

- codex 0.80.0 是 chat 协议末版；其原生 usage 恒为 0（`raw.usage_total` 保留作证），线级 `wire_usage` 才是权威计数。
- claude-code 的 `total_cost_usd` 按 Anthropic 价目计算，对 DeepSeek 端点无意义，仅存 `cost_native` 作对照；统一成本一律按 DeepSeek 价目由 token 折算。
- dsh 处于 developer preview（0.1.x），会话格式可能变；解析器只依赖 `assistant/chunk` 的 usage 事件。
- `deepseek-chat` / `deepseek-reasoner` 已于 2026-07-24 退役；用 `deepseek-v4-flash` / `deepseek-v4-pro`。
