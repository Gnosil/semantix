# 四层蒸馏落地与三臂步数验证 · 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 `feat/semantic-four-layer-distill` 分支上按 spec 完成度 ~95% 的四层蒸馏实现补完、合入 main 前置状态，并用三臂 SWE pilot（off / on-旧行为 / on-新准入）实测注入步数膨胀是否被修复（用户口径：不带 kernel ~40 步，带 kernel ~60 步）。

**Architecture:** 供给侧四层知识卡（repo-ops / 子系统地图 / plan-skeleton / outcome）+ 消费侧 TaskType 同型准入 + tool_pattern/result 恒 Miss，全部已实现于当前分支未提交改动；本计划先保全再补缺（task_type_test.go）、e2e 验收、merge main，最后用新旧两份二进制在同一 pilot 上对照，归因「新准入是否拉回步数」。

**Tech Stack:** Go（brew 安装，PATH 需 `/opt/homebrew/bin`）、bash（scripts/experiments/swe_pilot/）、python3.12 + /tmp/femb（SWE-bench 官方评测，arm64 补丁已打）、deepseek-v4-flash（SEMANTIX_HOME=/tmp/semantix-run/home-deepseek）。

**Spec:** `docs/specs/semantic-four-layer-distill.md`（判级 Spec-Required，已存在并已按其实现；本计划新增部分——补测试 + 实验脚本开关——判级 Spec-Exempt）。实验设计约束来自 `docs/reports/swe-pilot-smoke2-hy4-ready.md` §3/§6 与 `docs/specs/swebench-efficiency-research-plan.md`。

## Global Constraints

- 仓库惯例：merge commit（不 rebase）；**绝不运行 `gh pr merge`**（分类器拦截，合并留给用户）。
- commit 尾注：`Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`。
- `.workbuddy/` 目录不提交、不删除、不动。
- 测试/实验输出必须 `tee` 落盘（flake 教训：禁止只 grep 管道丢失唯一失败样本）。
- 不做 spec §2.6 出范围项（plan-first、gateway distill、hybrid 接桥、成本断路器、zone 小库保护）；不顺手重构。
- 每次 Bash 前置 `export PATH="/opt/homebrew/bin:$PATH"`；工作目录 `/Users/song/semantix/semantix`。
- 实验烧钱量级（deepseek-v4-flash，单实例 $0.007-0.03）：三臂 × 10 实例 ≈ $0.5-1，已在授权范围内直接跑；若中途单臂成本超 $3 停下来报告。

---

### Task 1: 保全昨天的半成品为 commit

**Files:**
- Commit（modified）: `cmd/semantix/extract.go`, `harness/semantix/bridge.go`, `kernel/inject/inject.go`
- Commit（untracked）: `docs/specs/semantic-four-layer-distill.md`, `docs/reports/swe-pilot-smoke2-hy4-ready.md`, `kernel/slice/task_type.go`, `kernel/slice/distill.go`, `kernel/slice/distill_test.go`, `kernel/inject/tasktype_test.go`, `harness/semantix/tasktype_admission_test.go`, `cmd/semantix/extract_distill_test.go`, `scripts/experiments/swe_pilot/make_predictions.py`
- 不提交: `.workbuddy/`

**Interfaces:**
- Produces: 分支上完整的四层蒸馏实现 commit，后续任务的基线。

- [x] **Step 1: 确认全绿后提交**（build + 四包测试已于计划前验证通过）

```bash
cd /Users/song/semantix/semantix
git add cmd/semantix/extract.go harness/semantix/bridge.go kernel/inject/inject.go \
  docs/specs/semantic-four-layer-distill.md docs/reports/swe-pilot-smoke2-hy4-ready.md \
  kernel/slice/task_type.go kernel/slice/distill.go kernel/slice/distill_test.go \
  kernel/inject/tasktype_test.go harness/semantix/tasktype_admission_test.go \
  cmd/semantix/extract_distill_test.go scripts/experiments/swe_pilot/make_predictions.py
git commit -m "feat(distill): four-layer semantic distill + task-type admission (spec §2.1-2.5)

Implements docs/specs/semantic-four-layer-distill.md: ClassifyTask,
Distill (repo-ops / plan-skeleton / outcome cards), extract --distill
--consolidate flags, Injector.TaskType same-type admission, bridge
tool_pattern/result never-inject zones. Tests green in kernel/slice,
kernel/inject, cmd/semantix, harness/semantix.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

- [x] **Step 2: 验证提交完成**

Run: `git status -s`（预期仅剩 `?? .workbuddy/` 与本计划文件）；`git log --oneline -1` 显示上述 commit。

### Task 2: 补 `task_type_test.go`（spec §4 缺口）

**Files:**
- Create: `kernel/slice/task_type_test.go`
- 被测: `kernel/slice/task_type.go`（`ClassifyTask(text string) string`，precedence: test-update > bugfix > refactor > docs > feature > investigate > general）

**Interfaces:**
- Consumes: `ClassifyTask`、常量 `TaskTestUpdate/TaskBugfix/TaskRefactor/TaskDocs/TaskFeature/TaskInvestigate/TaskGeneral`。

- [x] **Step 1: 写表驱动测试**

```go
package slice

import "testing"

func TestClassifyTask(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		// Precedence: a failing test plus a fix is test-update, not bugfix.
		{"test-over-bugfix", "Fix the failing test in test_forms.py", TaskTestUpdate},
		{"test-zh", "这个断言失败了，帮我修测试", TaskTestUpdate},
		{"bugfix-en", "IndexError traceback when saving a form", TaskBugfix},
		{"bugfix-zh", "保存的时候报错崩溃了", TaskBugfix},
		{"refactor-en", "clean up the session manager and simplify it", TaskRefactor},
		{"refactor-zh", "把这个模块重构一下", TaskRefactor},
		{"docs-en", "update the README and docstring", TaskDocs},
		{"docs-zh", "补一下文档", TaskDocs},
		{"feature-en", "add support for streaming responses", TaskFeature},
		{"feature-zh", "新增一个导出功能", TaskFeature},
		{"investigate-en", "figure out the root cause of the slow query", TaskInvestigate},
		{"investigate-zh", "排查一下为什么这么慢", TaskInvestigate},
		// bugfix keys beat feature keys by order even when both appear.
		{"bugfix-over-feature", "add a guard so the crash stops", TaskBugfix},
		{"case-insensitive", "FIX THE BUG", TaskBugfix},
		{"empty", "", TaskGeneral},
		{"whitespace", "   \n\t", TaskGeneral},
		{"no-match", "hello there, how are things", TaskGeneral},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClassifyTask(c.in); got != c.want {
				t.Fatalf("ClassifyTask(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
```

- [x] **Step 2: 跑测试**

Run: `go test ./kernel/slice/ -run TestClassifyTask -v 2>&1 | tee /tmp/tt-test.log`
Expected: PASS（实现已在）。若有 case FAIL：先核对期望是否符合 spec §2.1 的 precedence 语义，是实现 bug 才改实现，是测试期望写错才改测试。

- [x] **Step 3: Commit**

```bash
git add kernel/slice/task_type_test.go
git commit -m "test(slice): table-driven ClassifyTask coverage (spec §4)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 3: spec §5 e2e 验收（extract --distill --consolidate → search 检回）

**Files:**
- 只读输入: `/tmp/semantix-run/smoke2-on/sessions/*.jsonl`（smoke2 的真实会话镜像；若目录空，改用 `/tmp/semantix-run/pilot-on/sessions/*.jsonl`；两者都没有则从 `kernel/slice/distill_test.go` 的 fixture 形状手写一个 12 行 JSONL 到 scratchpad）
- 产物: scratchpad 下的临时 `e2e.db`（不进仓库）

**Interfaces:**
- Consumes: Task 1 提交的 `semantix extract --distill --consolidate` CLI 行为。
- Produces: 验收证据（三类卡可检回），写进 Task 7 报告的「验收」一节。

- [x] **Step 1: 重编 kernel 二进制并跑 extract**

```bash
export PATH="/opt/homebrew/bin:$PATH"; cd /Users/song/semantix/semantix
go build -o /tmp/semantix-new ./cmd/semantix
S=$(ls /tmp/semantix-run/smoke2-on/sessions/*.jsonl 2>/dev/null | head -1)
DB=/private/tmp/claude-501/-Users-song--reasonix-global-workspace-semantix/a972d070-c3c7-42bd-adf7-6450ef17f145/scratchpad/e2e.db
/tmp/semantix-new extract --input "$S" --db "$DB" --session e2e-check --distill --consolidate 2>&1 | tee /tmp/e2e-extract.log
```

- [x] **Step 2: search 检回三类卡**

```bash
/tmp/semantix-new search --db "$DB" "Repo operations" 2>&1 | tee /tmp/e2e-search.log
/tmp/semantix-new search --db "$DB" "Plan skeleton" 2>&1 | tee -a /tmp/e2e-search.log
/tmp/semantix-new search --db "$DB" "Task outcome" 2>&1 | tee -a /tmp/e2e-search.log
```

Expected: 三个查询各至少检回一条对应层标记的卡（spec §2.2 内容首行带层标记，BM25 词法可检索）。任何一类为空 → 停：读 extract 日志与 distill.go 对应分支，修复后重跑本 Task。CLI flag 名以 `--help` 实际输出为准（若与 spec 命名有出入，以实现为准并在报告注明）。

### Task 4: merge origin/main（behind 54）并全量验证

**Files:**
- Modify: merge 波及处（热点预判：`harness/semantix/bridge.go`、`kernel/inject/inject.go` 与 main 上 #454 memory-matrix / #455 repeat-tool-attribution 的交叠）

- [x] **Step 1: merge（仓库惯例 merge commit，不 rebase）**

```bash
git merge origin/main 2>&1 | tee /tmp/merge.log
```

冲突时逐文件处理：语义上「四层准入 + main 新行为」两者都保留；解不开的语义冲突（同一函数两边重写）→ 停下来向用户报告冲突面，不强行择边。

- [x] **Step 2: 全量 build + 测试**

Run: `go build ./... && go test ./... 2>&1 | tee /tmp/full-test.log`
Expected: 全绿。失败 → 逐个修（merge 引入的编译/语义错位），修不动的停下报告。已知惯例：site 前端检查（npm run check）不在本任务范围，Go 全量即可。

- [x] **Step 3: commit merge 结果**（merge commit 已由 git 生成；若有冲突修复则包含在内）

### Task 5: 实验准备 —— 双二进制 + run_arm.sh 供给开关

**Files:**
- Modify: `scripts/experiments/swe_pilot/run_arm.sh`（extract 调用处，约 :60-71 的 warm-up 循环）
- Build 产物: `/tmp/semantix-agent-new` + `/tmp/semantix-new`（分支版）、`/tmp/semantix-agent-old` + `/tmp/semantix-old`（origin/main 版，经 worktree 编译，不切换当前分支）

**Interfaces:**
- Produces: `EXTRACT_FLAGS` 环境变量（默认空 = 旧行为不变；ON-new 臂传 `--distill --consolidate`）；`BIN`/`KBIN` 已是 run_arm.sh 变量（`BIN=/tmp/semantix-agent`、`KBIN=/tmp/semantix`），实验时以环境变量覆盖为准 —— 若脚本内是硬编码赋值则改为 `BIN="${BIN:-/tmp/semantix-agent}"`、`KBIN="${KBIN:-/tmp/semantix}"`。

- [x] **Step 1: run_arm.sh 两处小改**

warm-up extract 行加 flags 透传（保持默认行为不变）：

```bash
# 原： $KBIN extract --input "$prev" --db "$rd/.semantix/project.db" --session "${pname%.jsonl}" > /dev/null 2>&1
# 改： $KBIN extract --input "$prev" --db "$rd/.semantix/project.db" --session "${pname%.jsonl}" ${EXTRACT_FLAGS:-} > /dev/null 2>&1
```

顶部赋值改为可覆盖（若尚未如此）：`BIN="${BIN:-/tmp/semantix-agent}"; KBIN="${KBIN:-/tmp/semantix}"`。

- [x] **Step 2: 编两套二进制**

```bash
cd /Users/song/semantix/semantix
go build -o /tmp/semantix-agent-new ./cmd/semantix-agent && go build -o /tmp/semantix-new ./cmd/semantix
git worktree add /tmp/wt-main-bin origin/main 2>/dev/null || true
cd /tmp/wt-main-bin && go build -o /tmp/semantix-agent-old ./cmd/semantix-agent && go build -o /tmp/semantix-old ./cmd/semantix
cd /Users/song/semantix/semantix && git worktree remove /tmp/wt-main-bin
```

（`cmd/semantix-agent` 若名称不同，以 `ls cmd/` 为准修正；worktree 编译避免动当前分支。）

- [x] **Step 3: 冒烟 1 实例确认脚本改动无回归**

```bash
mkdir -p /tmp/semantix-run/pilot-one
cp /tmp/semantix-run/pilot/django__django-11477.json /tmp/semantix-run/pilot-one/
MAX_STEPS=80 AGENT_TIMEOUT_SECS=1800 SEMANTIX_HOME=/tmp/semantix-run/home-deepseek \
  PILOT_DIR=/tmp/semantix-run/pilot-one BIN=/tmp/semantix-agent-new KBIN=/tmp/semantix-new \
  EXTRACT_FLAGS="--distill --consolidate" \
  bash scripts/experiments/swe_pilot/run_arm.sh on /tmp/semantix-run/probe-new 2>&1 | tee /tmp/probe-new.log
```

Expected: 正常出 `.result.json`/`.patch`/`metrics.tsv`，且 metrics 的 turns 列为真实步数（>1；W1 时期该列曾坏）。turns 仍为 0/1 → 查新版 result JSON 的键名并修 run_arm.sh 的 metrics 提取段后重试。

- [x] **Step 4: commit 脚本改动**

```bash
git add scripts/experiments/swe_pilot/run_arm.sh
git commit -m "exp(swe-pilot): EXTRACT_FLAGS + overridable BIN/KBIN for three-arm distill run

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Task 6: 三臂 × 10 实例正式运行

**Files:**
- 输入: `/tmp/semantix-run/pilot/`（django×5 + sympy×5，smoke2 报告 §6.4 指定设计；同 repo 实例按 glob 序连续运行，满足库累积约束）
- 产物: `/tmp/semantix-run/arm3-{off,on-old,on-new}/`

**Interfaces:**
- Produces: 三份 `metrics.tsv`（instance/arm/elapsed/rc/turns/tokens/cost），Task 7 的输入。

**臂语义（报告必须写明）：** ON-old 用 origin/main 二进制，**已含 #454 memory-matrix 的 strict 准入**（Context/Memory 白名单 + 五门槛），与产生用户口径 40→60 步膨胀的 W1 老代码不同代。三臂回答的是「main 现状还剩多少膨胀」+「四层蒸馏在其上再改善多少」。另：strict 的 MinSourceSessions=2 意味着 ON 臂前 2 题注入必然为空（冷启动保护），每 repo 实际带注入的是第 3-5 题。

- [x] **Step 1: OFF 臂（新二进制，注入关）**

```bash
MAX_STEPS=80 AGENT_TIMEOUT_SECS=1800 SEMANTIX_HOME=/tmp/semantix-run/home-deepseek \
  PILOT_DIR=/tmp/semantix-run/pilot BIN=/tmp/semantix-agent-new KBIN=/tmp/semantix-new \
  bash scripts/experiments/swe_pilot/run_arm.sh off /tmp/semantix-run/arm3-off 2>&1 | tee /tmp/arm3-off.log
```

- [x] **Step 2: ON-old 臂（main 二进制 = 复现步数膨胀基线）**

```bash
MAX_STEPS=80 AGENT_TIMEOUT_SECS=1800 SEMANTIX_HOME=/tmp/semantix-run/home-deepseek \
  PILOT_DIR=/tmp/semantix-run/pilot BIN=/tmp/semantix-agent-old KBIN=/tmp/semantix-old \
  bash scripts/experiments/swe_pilot/run_arm.sh on /tmp/semantix-run/arm3-on-old 2>&1 | tee /tmp/arm3-on-old.log
```

- [x] **Step 3: ON-new 臂（分支二进制 + 四层供给）**

```bash
MAX_STEPS=80 AGENT_TIMEOUT_SECS=1800 SEMANTIX_HOME=/tmp/semantix-run/home-deepseek \
  PILOT_DIR=/tmp/semantix-run/pilot BIN=/tmp/semantix-agent-new KBIN=/tmp/semantix-new \
  EXTRACT_FLAGS="--distill --consolidate" \
  bash scripts/experiments/swe_pilot/run_arm.sh on /tmp/semantix-run/arm3-on-new 2>&1 | tee /tmp/arm3-on-new.log
```

每臂结束即检查 `metrics.tsv` 行数=10、rc 列无大面积 124（超时）。单臂成本超 $3 或大面积超时 → 停，报告后再定。

### Task 7: 汇总 + 报告

**Files:**
- Create: `docs/reports/swe-pilot-three-arm-distill.md`

- [x] **Step 1: 汇总对比**（同题配对：每实例三臂 turns/tokens/cost 并排 + 每臂均值；ON-old vs OFF 应复现膨胀方向，ON-new vs ON-old 是修复效果，ON-new vs OFF 是净开销）

- [x] **Step 2: （可选，若时间允许）官方评测 resolved**：按 smoke2 报告 §5.4 命令对三臂各跑一次，报告 submitted=10 显式声明。

- [x] **Step 3: 写报告并 commit**（结构对齐 smoke2 报告：结论速览 / 实测数字 / 关键发现 / 复现步骤 / 待办；引用本计划与 spec；写明 MAX_STEPS=80 与 W1 cap=40 的口径差异）

### Task 8: push + PR

- [x] **Step 1: push 分支**：`git push -u origin feat/semantic-four-layer-distill 2>&1 | tee /tmp/push.log`
- [x] **Step 2: 创建 PR**（正文含 spec 链接、三臂数字速览、验收记录；尾注 🤖 Generated with [Claude Code](https://claude.com/claude-code)）。**不合并** —— 合并留给用户（分类器拦 `gh pr merge`）。

## 自审记录

- Spec 覆盖：§2.1-2.5 已由既有改动实现（Task 1 保全），§4 缺口唯 task_type_test.go（Task 2），§5 验收（Task 3）。§2.6 出范围项未引入。
- 类型一致性：`ClassifyTask`/常量名取自 task_type.go 实读；`EXTRACT_FLAGS`/`BIN`/`KBIN` 与 run_arm.sh 实读一致。
- 无占位符；实验命令均可直接执行；两处「以实际为准」（CLI flag 名、cmd/ 目录名）已给核对方法。
