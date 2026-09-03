# SWE pilot 三臂对照：四层蒸馏 vs main 现状 vs 无注入

> 日期：2026-09-03。模型：deepseek-v4-flash（SEMANTIX_HOME=/tmp/semantix-run/home-deepseek）。
> 实例：`/tmp/semantix-run/pilot/`（SWE-bench Verified 子集，django×5 + sympy×5，同 repo 连续运行）。
> 上位文档：`docs/specs/semantic-four-layer-distill.md`（实现）、
> `docs/reports/swe-pilot-smoke2-hy4-ready.md` §3（实验设计约束）、
> `docs/superpowers/plans/2026-09-02-four-layer-distill-landing.md`（执行计划）。

---

## 1. 结论速览

| 结论 | 证据 |
|---|---|
| **当前 main（#454 后）在本 pilot 上注入暴露为 0** —— strict 门槛把 L2 注入通道实质关死 | 两个 ON 臂的 kernel 事件镜像只有 SliceHit(kind=5，Reuse 面板检索统计)，无一条 SliceInject(kind=6)；三臂 input tokens 几乎相同（无注入块开销） |
| W1 口径的步数/成本膨胀（40→60 步、+32% 成本）**在 main 现状已不存在** —— 代价是复用收益同时归零 | 三臂步数 23.1 / 20.4 / 21.6（方差内持平，配对差 higher/lower 各半）；成本 $0.006-0.007 持平 |
| 四层蒸馏的**供给侧质量已验证**，但零暴露下消费端收益无法兑现 | e2e：`extract --distill --consolidate` 对真实会话产出 repo-ops / plan-skeleton / outcome 三类卡且 BM25 检回 top-hit（见 spec §5 验收） |
| **下一步是门槛校准，不是继续收紧** | 每 repo 前 2 题被 MinSourceSessions=2 拦（冷启动保护）；第 3-5 题被 MinScore=0.70 + MinTopMargin=0.15 + RequireRunnerUp 拦（跨任务 BM25 相关性天然低）。bridge 注释已预告 per-scale threshold calibration follow-up |

## 2. 实验设计

三臂串行共享 `/tmp/semantix-run/repos/`，臂间清空 repo 侧 `.semantix` 库；全程 `caffeinate -i`：

| 臂 | 二进制 | 供给侧 | 语义 |
|---|---|---|---|
| OFF | 分支版 | — | 注入关，基线 |
| ON-old | origin/main（4a5a2ff2，#454 后） | 流水账 extract | **main 现状**：#454 strict 准入（Context/Memory 白名单 + 五门槛） |
| ON-new | 分支版（b2c00b88+） | `--distill --consolidate` 四层卡 | strict 准入 + TaskType 同型门 + 四层蒸馏供给 |

关键参数：`MAX_STEPS=80`（W1 用 40 —— cap 太低会截断膨胀信号）、`AGENT_TIMEOUT_SECS=1500`、每 repo 5 实例连续运行（smoke2 §3：跨会话复用要求同 repo ≥2 实例）。

**与 W1（+32% 成本 / 40→60 步口径）不同代**：W1 跑在 #454 之前的老代码上；本轮 ON-old 已是收紧后的 main。三臂回答「main 现状还剩多少膨胀」+「四层蒸馏在其上的增量」。

### 步数口径修正

result JSON 的 `num_turns` 是死计数：headless 路径上 `event.TurnDone` 从不触发（`harness/cli/run_output.go` 的 `turns++` 挂在 TurnDone 上），W1 metrics 里的 0/1 全是 `turns==0 && !isError → 1` 的 fallback 痕迹。本轮 turns 统计该 run 写入 `$SEMANTIX_HOME/projects/<slug>/sessions/` 的会话 JSONL 中 `role=assistant` 行数（`run_arm.sh` 快照-差集法），即真实 LLM 轮次。

## 3. 实测数字（三臂均为 caffeinate 保护下的干净重跑）

### 逐实例步数

| instance | OFF | ON-old | ON-new |
|---|---|---|---|
| django__django-11477 | 40 | 17 | 20 |
| django__django-14373 | 8 | 13 | 9 |
| django__django-14434 | 18 | 20 | 26 |
| django__django-15987 | 19 | 14 | 13 |
| django__django-16819 | 42 | 57 | 63 |
| sympy__sympy-13615 | 21 | 20 | 18 |
| sympy__sympy-16886 | 6 | 6 | 7 |
| sympy__sympy-19637 | 22 | 7 | 19 |
| sympy__sympy-20916 | 28 | 26 | 22 |
| sympy__sympy-24562 | 27 | 24 | 19 |

### 聚合（n=10）

| 指标 | OFF | ON-old | ON-new |
|---|---|---|---|
| 步数均值 | 23.1 | 20.4 | 21.6 |
| input tokens 均值 | 494,311 | 467,308 | 476,528 |
| 成本均值 USD | 0.006 | 0.007 | 0.006 |
| 墙钟均值 s | 99.1 | 104.3 | 88.3 |
| 超时(rc=124) | 0 | 0 | 0 |

配对步数差：ON-old−OFF 均值 −2.7（3 高/6 低）；ON-new−OFF −1.5（4 高/6 低）；ON-new−ON-old +1.2（5 高/5 低）。全部在方差内 —— **臂间无系统性差异**，与零注入自洽。django-16819 三臂均为最重题（42/57/63），是题目难度而非注入效应。

## 4. 关键发现

### 4.1 注入暴露为 0：主结论的证据链

ON 臂 kernel 事件镜像（`run_arm.sh` 的 `sessions_dir`，阅后即焚、仅存活最后一题）中只有 `{"kind":5, "data":{"layer":"L2","slice_ids":[…5 个候选]}}` —— kind=5 是 `Bridge.Reuse()` 的检索统计（Search 命中 5 候选，事关 reuse 面板显示），**不代表注入**；注入事件 SliceInject（kind=6）零出现。旁证：三臂 input tokens 几乎一致（注入若发生，每请求应携带最多 4KB 的 [semantix-reuse] 块）。

拦截归因（per #454 门槛）：每 repo 第 1-2 题 `MinSourceSessions=2` 必拦（冷启动保护）；第 3-5 题候选需同时过 AllowedTypes{Context,Memory} + MinScore 0.70 + MinCoverage 0.25 + MinTopMargin 0.15 + RequireRunnerUp + zone 分类 —— SWE 场景各题 issue 互不相同，跨任务 BM25 相关性低，实测无一候选全过。

### 4.2 宿主机睡眠污染了首轮数据（方法论教训）

首轮三臂跑于夜间，Mac 多次睡眠：ON-old 三题 elapsed 达 7518/8725/11524s，cache TTL 过期重建（单题 cache_creation 460k tokens vs 正常 ~15k）令 token/cost 严重虚高；ON-new 的 django 段同样横跨睡眠。三臂全部在 `caffeinate -i` 下重跑后数据干净。教训固化进 §5 复现步骤。附：会话文件名时间戳为 UTC，与 CST 日志时间对照时差 8 小时。

### 4.3 num_turns 死计数（harness 既有 bug，建议独立开题）

见 §2。TurnDone 事件在 headless 路径不发射，`num_turns` 自 W1 起从未真实工作过。

## 5. 复现步骤

```bash
# 二进制（分支版 + main 版，worktree 编译）
cd /Users/song/semantix/semantix
go build -o /tmp/semantix-agent-new ./cmd/semantix-agent && go build -o /tmp/semantix-new ./cmd/semantix
git worktree add /tmp/wt-main-bin origin/main --detach
(cd /tmp/wt-main-bin && go build -o /tmp/semantix-agent-old ./cmd/semantix-agent && go build -o /tmp/semantix-old ./cmd/semantix)
git worktree remove /tmp/wt-main-bin

# 三臂（臂间清库；caffeinate 防睡眠）
clean() { rm -rf /tmp/semantix-run/repos/{django,sympy}/.semantix; }
ENV="MAX_STEPS=80 AGENT_TIMEOUT_SECS=1500 SEMANTIX_HOME=/tmp/semantix-run/home-deepseek PILOT_DIR=/tmp/semantix-run/pilot"
clean; env $ENV BIN=/tmp/semantix-agent-new KBIN=/tmp/semantix-new \
  caffeinate -i bash scripts/experiments/swe_pilot/run_arm.sh off /tmp/semantix-run/arm3-off
clean; env $ENV BIN=/tmp/semantix-agent-old KBIN=/tmp/semantix-old \
  caffeinate -i bash scripts/experiments/swe_pilot/run_arm.sh on /tmp/semantix-run/arm3-on-old
clean; env $ENV BIN=/tmp/semantix-agent-new KBIN=/tmp/semantix-new EXTRACT_FLAGS="--distill --consolidate" \
  caffeinate -i bash scripts/experiments/swe_pilot/run_arm.sh on /tmp/semantix-run/arm3-on-new
clean
```

## 6. 待办 / 缺口

1. **门槛校准 spec（下一个主线）**：per-scale zone/门槛校准，目标 = 蒸馏卡在受控误注率下真实注入；建议附带注入暴露率作为一级实验指标（本轮教训：不测暴露率会把「没注入」误读成「注入无害」）。
2. **注入暴露率可观测性**：mirror 阅后即焚使前 9 题证据丢失；建议 `run_arm.sh` 保留全部 mirror 或 bridge 侧累计 SliceInject 计数进 result。
3. TurnDone 事件修复（独立题）。
4. resolved 官方评测（femb + Docker，smoke2 §5.4；报告 submitted=10）—— 零注入下三臂 resolved 差异预期为方差，优先级降。
5. n=10 方差大；扩样本用 `scripts/swebench/subsets/verified-50-seed20260824.txt`（swe50），在门槛校准后跑才有信息量。
