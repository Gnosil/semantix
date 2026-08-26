# SWE-bench Verified × DeepSeek：semantix-agent 与主流 harness 对比（基建 + 实测进行中 + 公开数据）

> 日期：2026-08-24 · 更新：2026-08-26 · 状态：**semantix 臂前半（25/50）已完成并通过官方评测（§2.1）；其余臂待 DeepSeek 账户充值后续跑**（campaign 幂等可续，`scripts/swebench/`）。

## 2.1 实测（进行中）：semantix-agent × deepseek-v4-flash，冻结 50 题子集之前 25 题

单轮、`--preset balanced`、`--workers 4`、bash 沙箱 off（四臂统一最大权限约定）、谷时段为主。官方 `swebench.harness.run_evaluation`（Docker，Epoch ghcr 镜像）评测，0 个评测错误：

| 指标 | 数值 |
| --- | ---: |
| **官方解决率** | **22 / 25（88%）** |
| 缓存命中率（provider 上报聚合） | **98.4%** |
| input tokens（合计 / 单实例均值） | 48.0M / 1.92M |
| output tokens（合计） | 688K |
| 墙钟（均值 / 中位） | 294s / 243s |
| 成本（合计 / 单实例 / 每解决一题） | $0.96 / $0.038 / **$0.044** |

未解决 3 题：`psf__requests-2317`、`django__django-13297`、`matplotlib__matplotlib-26208`（patch 非空但未过测试）。注意事项：n=25 为冻结子集的处理序前半（受余额中断，非独立抽样）、单轮次、无置信区间；与 §3 公开数字比较时须注意子集差异。原始数据：`scripts/swebench/results/c50-semantix/`（metrics.jsonl + 分批评测报告 + partial-eval-summary.json）。

过程中发现并修复的两个方法学坑（详见 #400 与 `scripts/swebench/README.md`）：基准 prompt 的 "Do NOT modify existing test files" 被 semantix 任务策略的子串匹配放大为全局禁写；容器缺 bwrap 时 bash 工具被整体拒绝（其余三臂均无沙箱运行，故统一 `[sandbox] bash="off"`）。

## 1. 结论速览

- **实测基建已交付**：`scripts/swebench/` 提供统一 runner，同一 prompt、同一冻结子集下驱动 `semantix-agent`、`claude-code`、`codex`、`dsh` 四个 harness 跑 DeepSeek，统一采集 resolve rate、input/output token、**缓存命中率**（provider 上报 cache token 计数）、墙钟耗时与折算成本。四条 adapter 已用本地双协议 mock 全链路冒烟通过（无 key 环境下验证了计量与 patch 管线）。
- **本容器当前跑不了真实轮次**，三个硬阻塞均为环境侧：① 容器内无 `DEEPSEEK_API_KEY`（`~/.reasonix/.env` 在用户本机）；② egress policy 403 拦截 `api.deepseek.com`；③ 同样拦截 `huggingface.co`（数据集）与 Docker Hub/ghcr blob CDN（官方评测镜像）。解锁后按 §4 协议执行即可。
- **公开数据的核心事实**（详见 §3）：DeepSeek 官方从未发布过 SWE-bench 的 token/成本/耗时明细；同一模型换 harness 分数可差 ~27pp（Claw-SWE-Bench）；dsh（2026-08-13 开源的 DeepSeek Harness）至今没有公开的 SWE-bench Verified 成绩——**我们的实测会是第一批同模型四 harness 的完整四指标对照**。

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

## 4. 实测协议（环境解锁后执行）

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

## 5. 产物索引

- 统一 runner：`scripts/swebench/`（README 含完整用法、公平性设计与已知边界）
- 冒烟证据：四 harness 对 mock 端点各 1 实例，token/命中率/成本/patch 链路全部按预期落账（semantix 2,400 in · 85.3% hit；claude-code 1,200 in · 85.3%；codex 经计量代理 1,200 in · 85.3%；dsh 1,200 in · 85.3%——mock 的设定值，证明解析正确）
- 本仓库历史锚点：`artifacts/local-deploy/`、`docs/reports/semantix-v16-v28-prompt-stack.md`

## 6. 来源

官方：DeepSeek V4 技术报告 arXiv:2606.19348；V3.2 报告 arXiv:2512.02556；api-docs.deepseek.com（Anthropic 兼容端点、价目、agent 集成页）；github.com/deepseek-ai/deepseek-harness。
独立：vals.ai/benchmarks/swebench；Epoch AI（swe-bench-verified 页 + ghcr 镜像集）；HAL（hal.cs.princeton.edu，arXiv:2510.11977）。
论文：Claw-SWE-Bench arXiv:2606.12344；MSR arXiv:2604.22750；框架横评 arXiv:2511.00872；XFlow arXiv:2606.14790；Skywork-SWE arXiv:2506.19290。
说明：检索经由受限沙箱代理完成，部分站点数字来自搜索摘录与二级转载，已按官方自报/独立/社区分级标注；发布前建议在无限制网络下复核 vals.ai 与 swebench.com 榜单原值。
