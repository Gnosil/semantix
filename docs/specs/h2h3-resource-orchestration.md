# Spec：Harness 合体 + 资源编排契约（Agile 2 · U37）

> 对应 Issue：U37（Agile 2 批次，见 `docs/Agile路线图.md` §2）
> 真源约束：`kernel/sched/sched.go`（`RoundInput` / `ToolCallInfo` / `RoundPlan` / `Decider`，接口冻结于 U1）、
> `kernel/event/event.go`（12 种 Kind + `KindCount` 哨兵）、`patches/semantix-sched-prefetch.patch`（U13c 成果，本期作迁移素材）；
> 架构基线：`docs/reports/harness-refactor-blueprint.md` §3/§4/§6、`docs/Agent-Infra-架构设计.md` §5。
>
> **状态（2026-08-17）**：v1 草案，**先审后写**——本文档获批准前，U38-U43 不开工实现。
> 判级：Spec-Required（新顶层目录/包结构 + 事件契约扩展 + 新配置面，多条命中）。

## 0. 战略决策（2026-08-17，Song）

**停止在 DeepSeek-Reasonix fork 仓库上改动。** Reasonix 的 agent 系统（主循环/工具/TUI 骨架）
直接**复用进 semantix 仓**（MIT 允许，保留 attribution），在 semantix 开长期集成分支，
换上我们自己的视觉（Semantix Design，蓝图 §4），与 kernel **进程内**结合。

这是蓝图 §6 风险项的正式落地（「fork 维护负担 → 评估后可能停止跟随上游，本项目自研方向已定」）。
后果：

- `patches/` 模式废弃（保留作历史与迁移素材，U13c 的接线成果按 §5 迁移）
- 「跨仓库 wire 契约」降级为「单仓进程内接口」——控制通道不再需要 CLI 子进程与 3s 超时软降级
- H4 视觉从「可并行的 UI 换肤」升级为集成分支的组成部分（#157 成果迁移、#158 重定位到本仓）
- **集成分支**：`harness-integration`（从 main 切出；U38-U43 的实现 PR 全部以它为 base，
  阶段验收后整体回合 main）

## 1. 背景与缺口

Agile 2 验收三条（路线图 §2）：① 工具可挂起/恢复 + 预算配额生效（H2）；② 调度演示：kernel 决策改变
harness 行为 + 可量化收益（H3）；③ 命中率/成本随使用提升曲线（H5）。

现状与差距（U13c 已在 fork 验证过的能力按 §5 迁入本仓）：

| 能力 | 现状 | 缺口 |
|---|---|---|
| harness 代码 | 在 fork 仓（另一个 module） | 未进本仓：无 `harness/` 目录 |
| 观测上行 | ✅ fork 侧 sink 镜像 12 种事件（U13c） | 迁移后改为进程内直发总线；资源目录缺失 |
| 调度决策 | ⚠️ fork 侧 `internal/semantix/sched.go` 是 RuleDecider 的本地移植 | 迁移后直接 import `kernel/sched`，删除移植副本 |
| 工具挂起/恢复 | ❌ | `RoundPlan` 无挂起语义；无执行点 |
| 预算配额 | ❌ | 配置键存在，无 Controller 强制执行 |
| prefetch/evolve 闭环 | ⚠️ kernel 包已实现（U18/U18b） | `PrefetchHit`/`PrefetchWaste`/`EvolutionTick` 无人发射，参数不生效 |
| 视觉 | fork TUI 原样 + U33 复用面板（#168 已并入 fork） | Semantix Design（深色 + 语义绿 #2F967F）未落地 |

## 2. 范围

**范围内**：

- C0 Reasonix agent 系统 vendor 进本仓（目录/模块/构建/attribution）
- C1 资源目录（进程内构造 + 新增 `ResourceCatalog` 事件 Kind 用于落盘观测）
- C2 调度接线（harness 直接调用 `sched.Decider` + `RoundPlan` 字段扩展）
- C3 预算配额模型（BudgetController 阶梯降级）
- C4 反馈闭环最小集（PrefetchHit/Waste + EvolutionTick 发射点）
- C5 视觉基线迁移（U33 复用面板 + Semantix Design 主题 token）

**不在范围内**：桌面端 Wails 重画（#158，等 C0 落地后在本仓续作）；serve/JSON-RPC 对外协议
（Agile 3）；会话级调度（蓝图 §6：先工具级）；`Decider` 接口签名变更（冻结，只加 `RoundPlan` 字段）；
上游 Reasonix 的后续同步（正式脱钩）。

## 3. C0 Vendor 方案（新顶层目录）

```
semantix/
├── kernel/          # 既有，不动
├── gateway/         # 既有，不动
├── harness/         # 新增：Reasonix agent 系统落点（单 Go module 合并进本仓 module）
│   ├── agent/       # 主循环（run_loop / execute_batch / turnruntime / sampling_request）
│   ├── tool/        # 内置工具注册表
│   ├── control/     # 单 Controller 多前端骨架（蓝图 §2.2「最大资产」）
│   ├── tui/         # chatREPL/runAgent（bubbletea）→ Semantix Design 重画面
│   ├── provider/    # DeepSeek 前缀缓存 provider
│   └── ATTRIBUTION.md  # 上游 MIT 声明 + 脱钩 commit 记录
└── cmd/semantix-agent/  # 新增：合体后的可执行入口（TUI 主力形态）
```

- **模块路径**：全部改写为 `semantix/harness/...`（进本仓 module，不留 `reasonix` module）；
- **裁剪原则**：只搬 agent 系统运行必需（memory/skills/权限门控/事件流按需保留），
  desktop/、ACP、serve 等前端本期不搬（#158 时再取）；搬运清单在 U38 实现 PR 中逐目录列出；
- **fork 里的 semantix 桥**（`internal/semantix/`）**不搬**——它是跨进程时代的产物，
  其职责由 §4 的进程内接线替代；U13c patch 中 agent 文件的改动点作为迁移对照表。

## 4. C1/C2 编排契约（进程内）

### 4.1 C1 资源目录

harness 启动时与目录变化时（工具注册/挂起变化）构造全量目录，直发事件总线（幂等，以最新为准）。
新增 event Kind（追加在 `EvolutionTick` 之后、`KindCount` 之前——`ingest.JSONLSource` 按 int
反序列化，追加式演进不破坏旧会话文件）：

```go
// ResourceCatalog reports the harness resource inventory (tools/models/budget).
ResourceCatalog

type ResourceCatalogPayload struct {
    Tools  []ResourceTool  `json:"tools"`  // name, readOnly, suspended
    Models []ResourceModel `json:"models"` // id, tier ("flash"|"pro"), inputPrice, outputPrice
    Budget ResourceBudget  `json:"budget"` // limitUSD, spentUSD, window ("session"|"day")
}
```

进程内已可直接读结构体，仍保留事件的原因：**落盘可观测**（会话 JSONL 回放）+ evolve 的信号源
+ 未来多 harness（Agile 3）时该 Kind 即外部上报契约。

### 4.2 C2 调度接线

- harness 每个工具轮直接调用 `sched.Decider.DecideRound`（默认注入 `RuleDecider`）；
  错误时降级为静态 `ReadOnly()` 分组（fail-open 三铁律），不再有子进程与超时层；
- 删除 fork 时代的本地移植副本（迁移后 `kernel/sched` 是唯一实现）；
- `RoundPlan` 字段扩展（契约演进，需批准；Go 加字段向后兼容，JSON 旧消费者忽略未知键）：

```go
type RoundPlan struct {
    ParallelGroups [][]string
    Tier           string
    InjectIDs      []string
    PrefetchIDs    []string
    // 新增：
    SuspendTools   []string `json:"suspendTools,omitempty"`   // 本轮应挂起的工具名（声明式全量）
    MaxParallel    int      `json:"maxParallel,omitempty"`    // 0 = 不限（沿用 harness 配置）
    BudgetAction   string   `json:"budgetAction,omitempty"`   // "" | "degrade_tier" | "halt_prefetch" | "hard_stop"
}
```

挂起语义：`SuspendTools` 是**声明式全量**（每轮下发当前应挂起集合），不是增量指令——
harness 不维护指令历史，恢复 = 下一轮不在集合中。

### 4.3 执行点（harness/ 内，对照 U13c patch 迁移）

| 指令 | 执行点 |
|---|---|
| `ParallelGroups`/`MaxParallel` | `harness/agent/execute_batch.go`（U13c 已验证的替换点） |
| `SuspendTools` | `executeBatch` 前过滤：被挂起工具立即返回 tool error「suspended by scheduler」 |
| `Tier` | `harness/agent/run_loop.go` 工具轮末（U13c 记录 tier → 本期真正切模型） |
| `BudgetAction` | BudgetController（§5） |
| `InjectIDs`/预热 | `sampling_request.go` 注入块 + `startInjectWarm`（U13c 已验证，进程内化） |

## 5. C3 预算配额（BudgetController）

`harness/agent/budget.go`：从配置读 `limitUSD`，以 `Usage` 事件累计 `spentUSD`。
**阶梯降级**（正确性 > 缓存 > 并发 > 预取，架构 §5.1 优先级反向裁剪）：

| 阈值 | 行为 |
|---|---|
| ≥ 70% | `halt_prefetch`：停止预取（赌博先停） |
| ≥ 90% | `degrade_tier`：强制 flash |
| ≥ 100% | `hard_stop`：拒绝新工具轮，向用户显式报错（不静默） |

预算状态随 C1 目录进总线；`RuleDecider` 读到后可经 `BudgetAction` 提前下发
（kernel 决策优先，harness 本地阈值兜底——两层同规则，谁先触发谁生效）。

## 6. C4 反馈闭环最小集

- `PrefetchHit`/`PrefetchWaste`：发射点在注入预热结果消费处（预热块被本轮请求使用 = Hit，
  被丢弃/过期 = Waste）；payload 既有定义不变；
- `EvolutionTick`：`kernel/evolve` 参数更新时发射；本期参数生效面**只限** RuleDecider 行为门阈值
  与 prefetch 信号源权重（架构 §6.2 在线层），离线重训不在本期。

## 7. C5 视觉基线（Semantix Design 最小集）

- **H4 换皮已另行实现并验证**（2026-08-17，spec `docs/specs/h4-branding.md` 已批准 + as-built，
  待随治理文件落仓）：品牌色真源 = site `--primary` `oklch(0.608 0.14 165)` ≈ **`#009c6d`**
  （蓝图旧值 #2F967F 作废）；产物为 binary patch（基于 fork@bf0d859 干净基线），
  vendor 后 `git apply --directory=harness h4-branding-reskin.patch` 套用——U39 的主题部分
  由套 patch + 验证构成，不重做；
- U33 复用面板（每 turn：📦 命中切片数 / 💰 节省成本 / 🗂 来源会话）从 fork 迁入 `harness/tui/`，
  数据源从 CLI 子进程改为进程内直读；
- 资源仪表（侧栏实时资源占用）**本期只留挂点不实现**（等 C1-C3 数据稳定后单独 issue）。

## 8. 验收（对应 Agile 2 DoD）

- [ ] `go build ./...` + `go test ./... -race` 单仓全绿（harness 并入后）
- [ ] `cmd/semantix-agent` 真实会话跑通：注入/复用面板显示（C0+C5 冒烟）
- [ ] 挂起演示：kernel 下发挂起某工具 → 调用被拒；恢复后可用（H2 门①）
- [ ] 预算演示：压到 70%/90%/100% 三阈值，行为逐级降级且用户可见（H2 门①）
- [ ] 调度演示报告：同一任务 kernel 决策 on/off 对比（延迟/成本/并发度量化），落 `docs/reports/`（H3 门②）
- [ ] PrefetchHit/Waste 与 EvolutionTick 出现在会话 JSONL 且 `semantix search` 可检索（H5 前置）

## 9. 风险

- **vendor 一次性成本**：模块路径改写 + 裁剪面判断失误会拖长 U38 → 对策：先最小可跑
  （agent+tool+control+tui+provider），冒烟绿了再谈裁剪回补；
- **wire 契约演进纪律不因进程内而放松**：事件 Kind 只追加不重排；`RoundPlan` 只加 omitempty 字段
  ——会话 JSONL 是落盘契约，旧文件必须永远可回放；
- **与上游脱钩**：Reasonix 后续修复不再自动获得 → `ATTRIBUTION.md` 记录脱钩 commit，
  重大上游修复按需手工 cherry-pick；
- **集成分支长期漂移**：`harness-integration` 与 main 的偏差随时间增大 → 每完成一个 U 即回合一次
  main（小步合入，不攒大 PR）。
