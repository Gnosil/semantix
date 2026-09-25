# SWE-bench Verified × DeepSeek：semantix-agent 与主流 harness 对比（基建 + 现有公开数据）

> 日期：2026-08-24 · 更新：2026-08-27 · 状态：**实测完成**。四 harness × 冻结 50 题子集 + semantix ablate 对照臂已全部生成并通过官方 Docker 评测，结果见 §5；§2–§3 保留立项时的公开数据背景。

## 1. 结论速览（2026-08-27 实测）

- **semantix 与 dsh 并列第一（48/50 = 96%），但 semantix 便宜一半**：$1.68 vs $3.43（谷时价），input token 少 39%（89.9M vs 146.8M），缓存命中率更高（98.5% vs 96.8%）。claude-code 紧随其后（47/50，$1.71）。
- **harness 效应实测 62pp**：同模型、同 prompt、同子集下，codex 0.80 仅 34%（17/50）——其中 31 题交了空 patch（wire 层已由计量代理修复到零协议拒绝，空 patch 是 codex 自身行为）；只看它交出非空 patch 的 19 题，17 题通过（89%），说明瓶颈在 harness 不在模型。这把 §3.4 文献里「换 harness 波动 27pp」的结论在受控条件下放大复现了。
- **ablate 对照（匹配前缀 10 题）**：全量 semantix 与 `--ablate all` 都是 9/10（同挂 `psf__requests-2317`），但 ablate 成本低 32%（$0.0286/题 vs $0.0420/题）、步数少 30%——n=10 不足以触及能拉开差距的难题尾部（预算哨兵在 10/50 处截断了该臂）。
  > **归因更正（2026-09-07，Issue #447 §2）**：本轮 runner 尚未写入 `[semantix]`（memory wiring 于 2026-08-28 `d40bd4e` 才修通，`SemantixConfig.Enabled` 默认 false），且当时 `--ablate all` 不含 `kernel` 消融（`9b655b5` 才加入）。因此 64 vs 45 步只能说明**完整 harness 栈与 harness 侧消融存在调用差异**，不能归因于 L2 记忆内核；原文「记忆内核是纯开销」的表述作废。记忆因果对照统一使用 `--semantix-memory on|off`，见 `docs/reports/swe-pilot-two-arm.md`。
- **`psf__requests-2317` 五臂全挂**，是子集中唯一的全员未解题——instance 级难度/环境问题的典型样本。
- 绝对分数（94–96%）显著高于 §3 的公开锚点，与「2026 权重对 2024 基准存在训练暴露」的猜想一致；**本报告的可比性主张只在 harness 维度**（模型、prompt、子集、评测器全部受控），不对绝对分做 SOTA 宣称。

## 2. 本仓库已有的实测锚点（semantix-agent + DeepSeek）

来自 `artifacts/local-deploy/`（2026-08-22，`deepseek-v4-flash`，V28 变体，单实例官方 Docker 评测）：

| 指标 | django__django-13195 |
| --- | ---: |
| 官方评测 | **resolved 1/1** |
| 墙钟 | 537.7 s |
| input tokens | 1,708,054（其中 cache read 1,677,824 → **命中率 98.2%**） |
| output tokens | 24,525 |
| 成本 | $0.0692 |

以及 `docs/reports/semantix-v16-v28-prompt-stack.md` 的 V16/V24.1/V28 多变体小样本（3–5 实例，100% 档，均带 token/耗时/成本）。样本过小不构成基准，但证明**本仓库的链路（无头运行 → 官方评测 → 全指标）此前已端到端跑通**。

## 3. 公开数据汇总（检索日 2026-08-24；均附来源与可信级）

### 3.1 dsh 是什么

**dsh = DeepSeek Harness**（`github.com/deepseek-ai/deepseek-harness`，npm `@deepseek-ai/dsh`），2026-08-13 开源的官方插件化 agent 运行时（Cordis 插件框架，「一切皆插件」），MIT，developer preview。预设 Standard / PTC / **Minimal**（基准专用：仅 bash + file-replace 两工具）/ Creative 四模式；DeepSeek 自报的 agent 基准（Terminal-Bench 2.1 = 87.9 等）即用 Minimal 模式。**dsh 的 SWE-bench Verified 成绩官方与社区均未发布**（开源仅 11 天）。

### 3.2 模型与价目（对成本折算的直接输入）

- `deepseek-chat` / `deepseek-reasoner` **已于 2026-07-24 退役**；现役 `deepseek-v4-flash`（284B MoE / 13B 激活）与 `deepseek-v4-pro`（1.6T MoE / 49B 激活），均 1M 上下文。
- 2026-08-16 起分峰谷计价（峰=01:00–04:00、06:00–10:00 UTC，价格 ×2）。谷时每 1M token：Flash 命中 $0.007 / 未命中 $0.22 / 输出 $0.66；Pro 命中 $0.022 / $0.66 / $1.98。**缓存命中比未命中便宜 ~31×**——这正是「命中率」必须作为一级指标的原因。
- Claude Code 接 DeepSeek 的官方路径：`ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic` + `ANTHROPIC_AUTH_TOKEN=<DeepSeek key>`（api-docs.deepseek.com 有专页）。Codex 官方集成页存在，但社区对其是否需要 Responses 桥接说法不一；实测用 codex 0.80.0（chat 协议末版）直连。

### 3.3 SWE-bench Verified：DeepSeek 分数按 harness 分列

| harness / 评测方 | 模型 | 分数 | token/成本/耗时 | 可信级 |
| --- | --- | ---: | --- | --- |
| DeepSeek 内部脚手架（≈今日 dsh Minimal） | V3.1 → V3.1-Terminus → V3.2-Exp | 66.0 → 68.4 → 67.8 | 未发布 | 官方自报 |
| 同上 | V4-Pro（4 月 preview，Max thinking） | **80.6** | 未发布 | 官方自报（arXiv:2606.19348） |
| 同上 | V4-Flash（Max） | **79.0** | 未发布 | 官方自报 |
| vals.ai 中立 bash-only harness（全 500 题） | V4-Pro-0813 | **96.4 ±0.8（第 2，仅次 Claude Opus 5 的 97.0）** | **$0.02/题**（Opus 5 为 $1.29/题） | 独立 |
| Claude Code 作 harness | V3.2 | 72–74（官方鲁棒性组） | 未发布 | 官方自报 |
| mini-swe-agent（bash-only） | V4-Flash | 77.4 | 未发布 | 论文（arXiv:2606.14790） |
| OpenHands | V3 / V3-0324 / R1 | 32.4 / 38.8 / 34.0 | 未发布 | 论文（arXiv:2506.19290） |
| Codex CLI | 任意 DeepSeek | **无公开数据** | — | — |
| dsh | 任意 | **无公开数据** | — | — |

> ⚠️ 「80.6」与「96.4」都在流传：前者是 4 月 preview 权重 + 官方脚手架，后者是 0813 权重 + vals.ai 中立 harness——**checkpoint 与 harness 都不同，不可同列一栏**。这恰是本项目坚持「同模型同子集换 harness」的动机。

### 3.4 harness 敏感性与 token/成本研究（方法学依据）

- **Claw-SWE-Bench**（arXiv:2606.12344，350 题多语言子集，非全量 Verified）：固定模型换 harness，Pass@1 波动 **27.4pp**；OpenClaw × V4-Flash 得 70.3%、总成本 $8.2、**缓存命中率 98.5%**（其表格同时报 Pass@1 / 总成本 / 输入输出 token / 轮数 / 命中率 / 墙钟中位——与我们的指标集同构）。
- **HAL**（Princeton，ICLR 2026）：SWE-bench Verified 全量一轮中位 $163；单题成本 $0.08（DeepSeek R1）→ $32.00（Opus 4.1 High），脚手架显著影响精度与成本。
- **MSR/Stanford**（arXiv:2604.22750）：agent 任务 token 消耗约为 chat 的 1000×，**input token 主导成本**，同题重复跑 token 可差 30×。
- **框架横评**（arXiv:2511.00872）：仅 harness 差异即可让单题 token 从 186K 拉到 3,486K（~19×）。
- Terminal-Bench 2.1 同模型对照：V4-Pro-0813 在 dsh Minimal 自报 87.9，在独立 Terminus 2 参考 harness 仅 54.68——harness 效应普遍且巨大。

### 3.5 参照系（其他模型 × 原生 harness，2026-08 口径）

Claude Opus 5：97.0（vals.ai，$1.29/题）；Claude Fable/Mythos 5：95–95.5（聚合站转载官方数）；GPT-5.3-Codex：85；对比 V4 系 $0.02–0.04/题的量级差 — 成本轴上 DeepSeek 领先一至两个数量级，分数轴上取决于 harness 口径。

## 4. 实测协议（已于 2026-08-27 按此执行，结果见 §5）

1. **子集**：`--sample 50 --seed 20260824` 冻结 50 题（全量 500 题四 harness ≈ 2000 次 agent 运行，先 50 题定量再决定是否扩全量）。
2. **四轮生成**：semantix（`deepseek` preset balanced）、dsh（headless）、claude-code（anthropic 端点）、codex 0.80（chat + 计量代理），同 prompt 模板、同模型 `deepseek-v4-flash`、单实例 2400s 超时、`--workers 4`。
3. **评测**：官方 `swebench.harness.run_evaluation`（Docker，Epoch ghcr 预构建镜像优先）。
4. **汇总**：`report.py` 出四指标对比表；成本按谷时价折算并标注运行时段；semantix 另跑一条 `--ablate all` 对照臂隔离记忆内核的贡献。
5. **发布口径**：区分「resolve rate（官方评测）」与「无空 patch 率」；缓存命中率一律为 provider 上报 cache token 之比，不用估算值。

### 待用户解锁的清单

| # | 事项 | 动作 |
| --- | --- | --- |
| 1 | DeepSeek API key | 环境变量 `DEEPSEEK_API_KEY`（容器内没有本机的 `~/.reasonix/.env`） |
| 2 | 出网放行 | `api.deepseek.com`、`huggingface.co`、`cdn-lfs.huggingface.co`、Docker Hub CDN（或 `ghcr.io` + `pkg-containers.githubusercontent.com`） |
| 3 | （可选）全量磁盘 | 全 500 题评测镜像 ~30GiB（Epoch 去重集）+ 运行空间 |

## 5. 实测结果（2026-08-27）

### 5.1 主表：四 harness × 冻结 50 题 + ablate 对照臂

模型 `deepseek-v4-flash`，同一 prompt 模板，`--workers 4`，单实例超时 2400s；成本按谷时价折算（命中 $0.007 / 未命中 $0.22 / 输出 $0.66 每 1M）；缓存命中率 = provider 上报 cache_hit ÷ 总 input。resolve 均为官方 `swebench.harness.run_evaluation`（swebench 5.0.2，Docker Hub `swebench/sweb.eval.x86_64.*` 预构建镜像）结论。

| harness | resolved | resolve % | 空 patch | 平均墙钟 | input tok | output tok | 命中率 | 成本 (USD) | $/题 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| **semantix**（deepseek preset, balanced） | **48/50** | **96.0%** | 0 | 258s | 89.9M | 1.15M | **98.5%** | **$1.68** | $0.034 |
| dsh（headless） | **48/50** | **96.0%** | 0 | 201s | 146.8M | 2.11M | 96.8% | $3.43 | $0.069 |
| claude-code（anthropic 兼容端点） | 47/50 | 94.0% | 0 | 235s | 94.2M | 0.87M | 97.6% | $1.71 | $0.034 |
| codex 0.80（chat + 计量代理） | 17/50 | 34.0% | **31** | 868s | 250.9M | 3.74M | 99.7% | $4.41 | $0.088 |
| semantix `--ablate all`（截断臂，n=10） | 9/10 | 90.0% | 0 | 236s | 15.0M | 0.21M | 98.6% | $0.29 | $0.029 |

未解题明细：semantix = dsh = {`psf__requests-2317`, `sympy__sympy-13798`}；claude-code = {`psf__requests-2317`, `astropy__astropy-14598`, `django__django-13297`}；codex 非空 19 题中 17 过，挂 {`psf__requests-2317`, `astropy__astropy-14598`}，另 31 题空 patch（其中 3 题 runner 记录 agent 异常退出）。`psf__requests-2317` 为五臂共同未解。

### 5.2 ablate 对照（匹配前缀，同 10 题）

预算哨兵在余额 ¥5.04 时截断 ablate 臂于 10/50（该臂无跨实例记忆，前缀合法）。与全量 semantix 在**同 10 题**上对齐：

| 臂 | resolved | input | output | 命中率 | 成本 | $/题 | 平均步数 | 平均墙钟 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| semantix 全量 | 9/10 | 22.8M | 286k | 98.5% | $0.420 | $0.0420 | 64 | 309s |
| semantix ablate | 9/10 | 15.0M | 206k | 98.6% | $0.286 | $0.0286 | 45 | 236s |

两臂同挂 `psf__requests-2317`。结论：这 10 题（多为全员可解的中低难度）上 `--ablate all` 相对全量栈少花 47% token / 32% 成本；能否在难题尾部回本需扩样本验证，n=10 不构成否定性结论。

> **归因更正（2026-09-07）**：该差异**不是** L2 记忆内核的开销——本轮记忆内核实际处于关闭状态（见 §1 更正注）。此处的 `steps` 是未拆分来源的总调用数，混合了 planner / subagent / compaction 等 harness 辅助调用；Issue #447 P0.1 起按来源拆分后，历史报告不再用未拆分 steps 做 memory 归因。

### 5.3 运行与披露（威胁效度清单）

- **时段**：生成轮 2026-08-27 10:18–16:05 UTC，全程谷时（峰段为 01:00–04:00、06:00–10:00 UTC）；成本表按谷时价折算，若按峰价 ×2。评测轮 16:30–20:15 UTC（评测不耗 API）。
- **semantix 产品修复**：正式轮前修复 `harness/taskpolicy`（commit `0b3da36`）——prompt 中「Do NOT modify existing test files」这类**限定对象**的禁改短语曾被误判为全工作区写冻结，导致 16/50 空 patch；修复后限定短语不再冻结、全局禁改与引用块剥离行为有回归测试覆盖。该修复属产品缺陷修正、对所有臂同一 prompt 生效，予以披露。
- **codex 协议桥**：codex 0.80 chat 线协议与 DeepSeek thinking 模式存在两处不兼容（assistant(tool_calls) 后被楔入文本/合成 user 消息；`reasoning_content` 不回传）。计量代理做了纯结构修复（消息排序归一 + reasoning 按 tool_call id 复注），修复后台账 4xx 拒绝为零、全部 turn.completed——**31 题空 patch 是 codex 在会话正常完成下自身不产出 patch**，非协议故障。
- **评测运维**：Docker Hub 匿名拉取限流（429）两次中断评测，另遇一次容器重启；评测驱动改为按 report.json 覆盖率断点续评 + 限流退避重试（commit `1661654`），最终五 run 缺口清零（`EVAL_ALL_DONE`）。逐实例评测幂等，中断不影响正确性。
- **防伪抽查**：抽 3 个 resolved 实例核对——模型 patch 与官方金 patch 均不同（如 django-11451：模型 13,251B vs 金 575B），FAIL_TO_PASS / PASS_TO_PASS 全绿，排除金 patch 泄漏与评测误判。
- **局限**：n=50 单 seed 单轮（无方差估计）；dsh 为 developer preview；codex 0.80 是 chat 协议末版（官方主推已转 Responses）；绝对分受 2026 权重对 2024 基准可能的训练暴露影响（§1 已述）。

## 6. 产物索引

- 统一 runner：`scripts/swebench/`（README 含完整用法、公平性设计与已知边界）
- 实测数据（容器内，未入库）：`scripts/swebench/results/{semantix,dsh,claude-code,codex,semantix-ablate}.deepseek-v4-flash.20260824/`——各含 `preds.jsonl`、`metrics.jsonl`、官方评测报告 JSON 与逐实例 `logs/run_evaluation/**/report.json`；冻结子集清单入库于 `scripts/swebench/subsets/verified-50-s20260824.txt`
- 冒烟证据：四 harness 对 mock 端点各 1 实例，token/命中率/成本/patch 链路全部按预期落账（semantix 2,400 in · 85.3% hit；claude-code 1,200 in · 85.3%；codex 经计量代理 1,200 in · 85.3%；dsh 1,200 in · 85.3%——mock 的设定值，证明解析正确）
- 本仓库历史锚点：`artifacts/local-deploy/`、`docs/reports/semantix-v16-v28-prompt-stack.md`

## 7. 来源

官方：DeepSeek V4 技术报告 arXiv:2606.19348；V3.2 报告 arXiv:2512.02556；api-docs.deepseek.com（Anthropic 兼容端点、价目、agent 集成页）；github.com/deepseek-ai/deepseek-harness。
独立：vals.ai/benchmarks/swebench；Epoch AI（swe-bench-verified 页 + ghcr 镜像集）；HAL（hal.cs.princeton.edu，arXiv:2510.11977）。
论文：Claw-SWE-Bench arXiv:2606.12344；MSR arXiv:2604.22750；框架横评 arXiv:2511.00872；XFlow arXiv:2606.14790；Skywork-SWE arXiv:2506.19290。
说明：检索经由受限沙箱代理完成，部分站点数字来自搜索摘录与二级转载，已按官方自报/独立/社区分级标注；发布前建议在无限制网络下复核 vals.ai 与 swebench.com 榜单原值。
