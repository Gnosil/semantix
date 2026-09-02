# Issue #447：SWE-bench 记忆 A-D 配对矩阵

> 状态：执行器与报告器已实现（2026-09-02）
>
> 跟踪 Issue：[#447](https://github.com/Gnosil/semantix/issues/447)

## 1. 目的

P0 的 shadow、repo 隔离、严格准入、历史降权和硬预算需要通过真实任务配对回答三个问题：

1. 检索本身是否改变 provider 请求或基础运行开销；
2. 严格策略能否在 pass@1 非劣时降低 executor calls、工具轮数、token 和成本；
3. 改善来自 P0 门禁还是随机波动/实例构成。

旧 full/`--ablate all` 不是记忆因果对照，本矩阵只使用显式 memory/retrieval 臂。

## 2. 实验臂

| 臂 | memory | retrieval | binary | 目的 |
|---|---|---|---|---|
| A | off | off | current | 无记忆基线 |
| B | on | shadow | current | 运行检索与诊断，但 provider bytes 等价 A |
| C | on | strict | current | P0 保守策略 |
| D | on | strict | legacy @ `cb5e9cc` | 旧全类型宽松注入 |

D 固定在 repo 隔离已存在、P0.4 尚未进入的 commit，避免把跨 repo 污染差异错误算到准入策略上。current binary 必须包含待验收的全部 P0 PR。

## 3. 冻结与隔离

- 同一 dataset、`--ids`、model、preset、effort、价格表和超时；
- 建议至少 20 instances × 3 repetitions；正式验收使用 frozen-50 × 3；
- repetition 内严格 A→B→C→D 串行，避免跨臂 CPU/API 竞争；
- 每个 repetition/arm 有独立 state 和 work 目录；
- memory-on 内继续使用 repo 独立 store 和 repo 内冻结顺序；
- 每个 run 的完整命令和路径写入 matrix manifest；
- 中断后复用相同 run-id，按 `preds.jsonl` 继续，不重跑已完成实例。

## 4. 指标

每个 `(repetition, instance_id)` 与同 repetition 的 A 配对。报告至少包含：

- resolved；
- executor/total calls；
- input tokens、cost、wall；
- tool calls；
- read/search/test 工具族总量；
- provider retries；
- Semantix inject turns/bytes。

每个指标同时输出 absolute 与 `arm - A` 的 mean、median、P75、P90。缺失实例集合直接失败，不能用不完整均值继续。

当前 `tool_calls_by_name` 不包含参数签名，因此 read/search/test 只是工具族总量，不是重复率。重复相同路径/查询/测试、策略反转和 rollback 必须由 P1 trajectory/fuse 事件提供；报告器不做无证据推断。

## 5. 判定顺序

1. B 与 A 的 provider-byte 回归测试必须先通过；
2. 比较 B-A 判断检索/日志的非注入开销；
3. 比较 C-A 判断 strict 的端到端收益；
4. 比较 C-D 判断门禁相对旧策略的增量；
5. pass@1 非劣后才评价成本优化；
6. 重点检查 median 与 P75/P90，均值只作补充；
7. 任一污染事件或大幅负迁移实例进入逐 trajectory 审核。

## 6. 执行

```bash
cd scripts/swebench
python3 memory_matrix.py \
  --dataset data/swebench_verified.jsonl \
  --ids subsets/verified-50-s20260824.txt \
  --model deepseek-v4-flash --repetitions 3 --workers 4 \
  --semantix-bin ../../bin/semantix-agent \
  --legacy-semantix-bin ../../bin/semantix-agent-issue447-legacy \
  --semantix-kernel-bin ../../bin/semantix
```

对 manifest 中每个 run 完成 `evaluate.py`，然后：

```bash
python3 memory_matrix_report.py \
  --manifest results/issue447-memory.matrix.json \
  --format json --out results/issue447-memory.report.json
python3 memory_matrix_report.py \
  --manifest results/issue447-memory.matrix.json \
  --format md --out results/issue447-memory.report.md
```

## 7. 验证

```powershell
python -m py_compile scripts/swebench/memory_matrix.py scripts/swebench/memory_matrix_report.py
python -m unittest discover -s scripts/swebench -p 'test_*.py' -v
python scripts/swebench/memory_matrix.py <required arguments> --dry-run
```

单测固定：A-D 顺序、两轮目录隔离、C/D binary 选择、按实例配对、分位数和实例集合不一致 fail closed。
