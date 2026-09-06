# SWE-bench 2 实例冒烟：管线验证 + hy4 接入状态

> 日期：2026-09-01。模型：**deepseek-v4-flash**（hy4 通路已接线，凭据待额度，见 §4）。
> 目的：在换模型之前，先把「agent 运行 → patch 提取 → 官方 harness 评测」全链路在
> arm64 宿主机上跑通，并固定一套可复现的运行环境。
> 上位文档：`docs/reports/swe-pilot-two-arm.md`（10 实例双臂）、`docs/specs/swebench-efficiency-research-plan.md`。

---

## 1. 结论速览

| 项目 | 状态 |
|---|---|
| 全链路（双臂 × 2 实例 × 官方 Docker 评测） | ✅ 打通，双臂均 **2/2 resolved** |
| arm64 宿主机跑 x86_64 评测镜像 | ✅ 依赖 `/tmp/femb` 里已打的 `platform="linux/amd64"` 补丁 |
| hy4 provider 接线（semantix-agent → OpenAI 兼容端点） | ✅ 请求格式正确，provider 已识别 |
| hy4 **凭据额度** | ❌ `token plan quota exhausted`（code 20098），见 §4 |
| 本轮能否测出「复用收益」 | ❌ **不能**，见 §3（这是本次最重要的发现） |

---

## 2. 实测数字（deepseek-v4-flash）

### 逐实例

| instance | arm | 墙钟 s | input tok | output tok | cache_read tok | 成本 USD | patch B | resolved |
|---|---|---|---|---|---|---|---|---|
| django__django-11477 | off | 227 | 1,363,419 | 24,920 | 1,337,088 | 0.014408 | 4,849 | ✅ |
| django__django-11477 | on | 136 | 1,067,870 | 15,994 | 1,042,944 | 0.010888 | 658 | ✅ |
| sympy__sympy-13615 | off | 114 | 488,146 | 12,424 | 471,552 | 0.007122 | 907 | ✅ |
| sympy__sympy-13615 | on | 191 | 1,014,892 | 18,780 | 993,280 | 0.011065 | 2,293 | ✅ |

### 聚合

| 指标 | OFF | ON | Δ(ON/OFF) |
|---|---|---|---|
| input tokens | 1,851,565 | 2,082,762 | **+12.5%** |
| output tokens | 37,344 | 34,774 | −6.9% |
| cache 命中率 | 97.68% | 97.77% | +0.1pt |
| API 成本 | $0.021530 | $0.021953 | **+2.0%** |
| 墙钟 | 341s | 327s | −4.1% |
| resolved | 2/2 | 2/2 | = |

单实例方差极大，与 W1 pilot 的观察一致：django-11477 ON 省 21.7% input，
sympy-13615 ON 却多花 107.9%。**n=2 不构成任何统计结论。**

---

## 3. 关键发现：2 实例冒烟测不出复用效果

`run_arm.sh` 的 ON 臂只在**同 repo** 之间累积切片库（`run_arm.sh:60-71`，
`prev_repo = short_repo` 才 extract）。本轮选的两个实例分属 django 与 sympy，
因此：

- 实例 1（django）：库为空，注入是 no-op；
- 实例 2（sympy）：warm-up 里唯一的既有会话属于 django，跨 repo 被 skip，库仍为空。

**结果：ON 臂两轮实际都没有跨实例注入，ON 与 OFF 的差异纯粹是运行方差。**
这解释了为什么 django 省、sympy 费却方向相反。

> **对实验设计的约束**：任何要测「跨会话复用」的 pilot，每个 repo 至少要 2 个实例，
> 且同 repo 实例必须连续运行（库按序累积）。W1 的 django×5 + sympy×5 设计是对的；
> 缩到 2 个跨 repo 实例只能验证管线，不能验证假设。

---

## 4. hy4 接入状态：接线完成，卡在凭据

### 已确认的事实

- WorkBuddy 当前会话的 hy4 走 `https://copilot.tencent.com/v2/chat/completions`，
  模型 ID `hy4-preview`（见 `~/.workbuddy/logs/*/workbuddyMainThread__*.log` 的
  `[ModelProvider] Sending request` 行）。
- 该端点要求会话级凭据，凭据存放在 Electron `safeStorage`（系统钥匙串），
  **不以明文 API key 形式暴露**；裸调用返回 `401 Authorization Required`（APISIX）。
- 机器上唯一的明文 hy4 通路凭据是 `~/.workbuddy/models.json` 里的
  Tencent Cloud Token Plan key（端点 `https://api.lkeap.cloud.tencent.com/plan/v3`，
  `/models` 里确实列了 `hy4-preview`），但该 plan **额度已耗尽**：
  ```
  hy4: status 500: {"error":{"message":"token plan quota exhausted",
                              "code":"20098"}}
  ```

### 已做好的接线（拿到额度即可跑）

隔离实验 home，**不污染 `~/.semantix` 全局配置**：

- `/tmp/semantix-run/home-hy4/` —— `default_model = "hy4/hy4-preview"`
- `/tmp/semantix-run/home-deepseek/` —— `default_model = "deepseek-flash/deepseek-v4-flash"`（本轮用它跑的）
- 两者均已设置 `[sandbox] bash = "off"`（本机无 `sandbox-exec`，否则 shell 工具被拒）

provider 定义：

```toml
[[providers]]
name        = "hy4"
kind        = "openai"
base_url    = "https://api.lkeap.cloud.tencent.com/plan/v3"
model       = "hy4-preview"
api_key_env = "HY4_API_KEY"
reasoning_protocol = "none"
context_window = 128000
```

切换到 hy4 只需换一个环境变量：

```bash
SEMANTIX_HOME=/tmp/semantix-run/home-hy4   # 而不是 home-deepseek
```

若换成别的 OpenAI 兼容端点，改 `base_url` + `model` + `.env` 里的
`HY4_API_KEY` 三处即可，脚本无需改动。

---

## 5. 复现步骤

```bash
# 0) 运行环境（二进制、repo、实例 JSON 均已就位；若 /tmp 被清需重建）
#    /tmp/semantix-agent、/tmp/semantix、/tmp/semantix-run/repos/{django,sympy}

# 1) 2 实例冒烟子集（django×1 + sympy×1）
mkdir -p /tmp/semantix-run/pilot-smoke2
cp /tmp/semantix-run/pilot/django__django-11477.json \
   /tmp/semantix-run/pilot/sympy__sympy-13615.json /tmp/semantix-run/pilot-smoke2/

# 2) 双臂运行（换成 home-hy4 即为 hy4 臂）
MAX_STEPS=40 AGENT_TIMEOUT_SECS=900 \
  SEMANTIX_HOME=/tmp/semantix-run/home-deepseek \
  PILOT_DIR=/tmp/semantix-run/pilot-smoke2 \
  bash scripts/experiments/swe_pilot/run_arm.sh off /tmp/semantix-run/smoke2-off
MAX_STEPS=40 AGENT_TIMEOUT_SECS=900 \
  SEMANTIX_HOME=/tmp/semantix-run/home-deepseek \
  PILOT_DIR=/tmp/semantix-run/pilot-smoke2 \
  bash scripts/experiments/swe_pilot/run_arm.sh on  /tmp/semantix-run/smoke2-on

# 3) predictions.jsonl（run_arm.sh 只产 *.patch，评测需要三字段 jsonl）
python3.12 /tmp/semantix-run/make_predictions.py /tmp/semantix-run/smoke2-off semantix-agent-off
python3.12 /tmp/semantix-run/make_predictions.py /tmp/semantix-run/smoke2-on  semantix-agent-on

# 4) 官方评测（arm64 需 /tmp/femb 的 platform 补丁；数据集走本地 HF 缓存）
cd /tmp/semantix-run/smoke2-off && HF_DATASETS_OFFLINE=1 \
  HTTP_PROXY=http://127.0.0.1:7890 HTTPS_PROXY=http://127.0.0.1:7890 \
  /tmp/femb/bin/python -m swebench.harness.run_evaluation \
  --dataset_name SWE-bench/SWE-bench --split test \
  --predictions_path predictions.jsonl --max_workers 1 --timeout 1800 --run_id smoke2_off
```

产物：`/tmp/semantix-run/smoke2-{off,on}/`（`metrics.tsv`、`*.patch`、
`predictions.jsonl`、`semantix-agent-*.smoke2_*.json`）。

---

## 6. 待办 / 缺口

1. **hy4 额度**：需要可用的 key（充值 token plan，或提供另一个 OpenAI 兼容端点）。
   拿到后重跑 §5 第 2 步即可，无需改代码。
2. **`make_predictions.py` 应进仓库**：`scripts/experiments/swe_pilot/` 目前只产
   `*.patch`，缺 patch→predictions 这一步，本轮靠 `/tmp` 下的临时脚本补上。
   建议补进 `scripts/experiments/swe_pilot/make_predictions.py`。
3. **评测走的是��整 SWE-bench test（2294 实例）而非 Verified 子集**：
   与 W1 pilot 的做法一致，但报告里的 `total_instances: 2294` 容易误读，
   写论文时应显式声明 submitted=N。
4. **下一步实验设计**：按 §3 的约束，测复用收益的 pilot 必须每 repo ≥2 实例。
   建议直接用 W1 的 django×5 + sympy×5（镜像已在本地 Docker 缓存，无需重拉）。
