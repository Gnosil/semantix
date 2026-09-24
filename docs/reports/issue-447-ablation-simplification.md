# Issue #447 消融与精简记录

日期：2026-09-03

对象：Semantix 记忆导致步数增加

基线：`ed8b0528af84c21964a4347d793bcecc4e128d80`

## 目的

这轮不再追加门禁，而是逐项回答两个问题：

1. 该结构是否对应独立、可观察的失败模式？
2. 删除后是否减少无效配置或实验成本，同时保留现有安全边界？

## 消融结果

| 候选 | 去掉后的反事实 | 观测 | 决定 |
|---|---|---|---|
| `Injector.RequireVerifiedResults` | 严格 allowlist 调用方忘记置 `true` 时，probation Result 会进入提示词 | 失败测试实际录得 probation 与 verified 两条均被接纳 | 删除开关；Result 验证改为严格 `AllowedTypes` admission 的内建不变量 |
| 强制 D-legacy 实验臂 | 每次正式矩阵都要准备旧 binary，并额外执行 3 个 run | A/B/C 已分别回答无记忆基线、仅检索观测、当前严格注入；D 只复现已废弃策略 | 默认只跑 A/B/C；仅显式提供 legacy binary 时追加 D |
| B-shadow | 删除后只能比较 A/C，无法区分“检索命中变化”和“提示词注入变化” | B 与 A 保持请求字节一致，同时保留 admission trace | 保留 |
| structured retrieval query | 回退为整段 turn 检索会再次混入状态、命令输出和既有注入文本 | 已有单测覆盖从真实用户目标构造查询 | 保留 |
| loop/progress fuse | 删除后负迁移切片可在无进展循环中反复进入后续 turn | 已有 loop/progress guard 集成测试覆盖清除历史与禁止 prefetch 回灌 | 保留 |
| Result probation/verification | 删除后未经宿主验证的“成功结论”可成为下一任务证据 | 已有 import 降级、mutation 后重验、显式审核测试 | 保留；只删除其调用方布尔开关 |
| 冻结 seed 与 repo 隔离 | 删除后实验臂之间的历史和仓库内容互相污染 | runner 测试覆盖同 repo 顺序、跨 repo 隔离和 seed 标记 | 保留 |

## 可复现证据

### 1. Result 开关反事实

测试先移除调用方的 `RequireVerifiedResults: true`，旧实现失败：

```text
--- FAIL: TestInjectorAlwaysRequiresVerifiedResults (0.00s)
    inject_test.go:197: admitted results = [0x… 0x…], want verified only
FAIL
```

删除该字段并把判定固化到严格 `AllowedTypes` 路径的 `BuildHits` 与 top-margin
eligibility 后，同一测试通过；未启用 allowlist 的通用 lookup/security probe 维持原语义：

```text
ok  semantix/kernel/inject
```

这不是新增门禁，而是删掉一个会制造“不小心关闭验证”状态的抽象。

### 2. 矩阵结构消融

冻结 50 ID、3 repetitions 的 dry-run manifest：

```text
RUN_COUNT=9
ARM_ORDER=A,B,C
```

旧默认是 12 个 run（A/B/C/D × 3）；新默认是 9 个 run，调度量减少 25%，并去掉
准备 legacy binary 的前置依赖。传入 `--legacy-semantix-bin` 的兼容测试仍生成
A/B/C/D，历史复现实验没有丢失。

## 边界

本轮消融验证的是控制流、接纳行为和实验结构，不把 dry-run 当成步数收益。真实的
50×3 executor calls、steps、tokens、重复工具调用和 resolve rate 仍由 A/B/C 正式运行
给出；报告必须按 `(repetition, instance_id)` 配对，C 相对 A 的 P50/P75/P90 与
resolved 不退化共同决定 Issue #447 是否关闭。

## 结论

- 删除 1 个生产配置字段和 1 个调用方配置分支。
- 默认实验从 4 臂缩到 3 臂，减少 25% 调度量。
- 不删除 B-shadow、结构化查询、熔断、Result 生命周期、冻结 seed：它们分别对应
  不同的可观察失败模式，不是同义包装。
