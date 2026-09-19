# semantix-ml — 本地检索质量模型训练管线

对应 spec：`docs/specs/local-retrieval-model.md`。训练 Python（仅开发机），
推理产物 ONNX；gateway 侧消费见 `[retrieval]` 的 `events_log` / `rerank_*`
配置与 `semantix gc --score-params`。

## 布局

- `semantix_ml/` 核心库（纯标准库，pytest 全覆盖）：dataset / metrics /
  gate / score_params / registry
- 入口脚本：`synth_queries` `build_dataset` `train_reranker` `export_onnx`
  `eval_retrieval` `fit_score_params` `rerank_server` `watch_and_train`
- `lab/` 实验环境定义：`setup-lab.sh` 初始化 `~/.semantix-lab/retrieval-lab`
  双臂（on=重排+采集，off=现状对照）

## 快速开始

```bash
uv sync --group dev && uv run pytest          # 核心库测试
scripts/ml/lab/setup-lab.sh                   # 立起实验环境
~/.semantix-lab/retrieval-lab/run-arm.sh on   # 启动 ON 臂网关
~/.semantix-lab/retrieval-lab/trainctl.sh bootstrap  # 冷启动+首轮训练
~/.semantix-lab/retrieval-lab/trainctl.sh serve      # 重排 sidecar
~/.semantix-lab/retrieval-lab/trainctl.sh watch      # 自进化守护
```

## 自进化循环（spec §6）

watch_and_train 观察切片库与事件日志 → 攒批去抖 → 一轮离线训练 →
held-out 门禁（NDCG@5/MRR@5 均不退步才发布，spec §5）→ registry 发布
新 checkpoint → SIGHUP 热载 sidecar。每轮完整输出落
`train/reports/round-*.log`；`trainctl.sh rollback` 一条命令回滚。

## 红线（spec §7）

- 重排请求只含 (query, id, text)——Stats/Weight/Meta 永不出网（I-5）
- 训练只读（export + 事件日志），不写任何切片（I-1）
- ScoreParams 单轮步长 ≤±20%（I-3，钳制在 `score_params.clamp_step`）
- 发布只认 held-out 外部指标（I-2/I-4）
