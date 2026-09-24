# Spec：本地检索质量模型（重排器 + 价值打分器，离线自进化闭环）

> 对应 Issue：待建（本 spec 合入时创建，label `retrieval-model`）。
> 判级：**Spec-Required（架构级）**——新增子系统（本地模型训练管线 + gateway
> 检索链扩展点），须过门禁评审后动手。
> 源码现状基线：`kernel/inject/inject.go`（`Injection.Decisions` 已保存
> 每候选裁决轨迹）、`kernel/slice/score.go`（`ScoreParams`/`ComputeWeight`/
> `Rescore` 纯函数通道）、`gateway/retriever.go`（`slice.Index` 三路检索 +
> `ModelEmbedder` fail-soft 先例）、`scripts/experiments/embed_server.py`
> （loopback 本地模型服务先例）。
> 状态：先审后写。一期产物**全部默认关闭**，在 `~/.semantix-lab/retrieval-lab/`
> 双臂实验环境验证，拿到收益证据后再走转正 RFC。

## 0. 一句话

用切片库自然产生的使用信号，离线训练两个分形态的本地小组件——
**重排器**（小 cross-encoder，重排召回 top-K）与**价值打分器**（拟合
`ScoreParams` 超参，驱动淘汰）——并以「攒批去抖 + 评估门禁 + 可回滚」的
守护循环随切片更新持续自训练，全程不违反 evolution-invariants 五条红线。

## 1. 背景与动机

- 检索链现状：`fuse.Fuse` → `zone.Classify` → 词法闸门 → judge，**召回后无
  重排**；相关性完全依赖 BM25/hash-cosine，语义匹配是已知短板（bge-m3 级
  本地 embedding 在 TECHNICAL-OVERVIEW 里挂了 "next step" 多期未做）。
- 价值函数现状：`ComputeWeight` 超参是拍脑袋默认值（HalfLifeDays=30 等），
  从未用真实使用数据校准过。
- 使用信号现状：`SliceStats{Hits,Injected,Rejected,UserFeedback,LastUsed}`
  四处回写挂点已接线、`Injection.Decisions` 已保存每候选裁决轨迹——
  **训练标签的原料已经在自然产生，只差落盘与消费**。
- 「自进化」卖点现状：Features 卡片 05 文案含 "offline retrain"，代码里
  不存在（agile2 报告实验 2 诚实记了 FAIL）。本 spec 是把这三个字做出来的
  第一个可验证闭环。

## 2. 目标与非目标

**目标（一期，全部在实验环境闭环）**

- G1 检索事件日志：gateway 把「query + 候选裁决轨迹」落盘（flag 默认关）。
- G2 重排器：小 cross-encoder（主选 `BAAI/bge-reranker-v2-m3`，备选
  `Qwen3-Reranker-0.6B`，以 ONNX 导出顺利为准），合成 query 冷启动 +
  事件日志增量训练，loopback 服务化，gateway 装饰器接入（fail-soft）。
- G3 价值打分器：回放历史使用数据拟合 `ScoreParams` 五超参，
  产出 `score_params.json`，`semantix gc --score-params` 注入。
- G4 自进化守护：watch 切片库与事件日志，攒批去抖触发「训练 → 评估门禁 →
  发布 checkpoint → 服务热载」，checkpoint 版本化可一键回滚。
- G5 双臂对照证据：`~/.semantix-lab/retrieval-lab/` on/off 双臂，离线
  NDCG@5/MRR@5 + 在线注入采纳率/成本对照报告。

**非目标**

- 不做本地知识 LLM、不做切片管理元模型、不做单骨干双头模型。
- 不动 `kernel/slice` 内部、不动 BM25/vector/fuse 打分实现、不改 Weight 语义。
- 不做每次 `Put` 触发训练（gateway 逐请求 ingest，频率不可行且破坏
  freeze-window 纪律）。
- 本地 embedding 替换（bge-m3 换 HashEmbedder）不在本期——重排器先证明
  语义模型有收益，embedding 替换是它的直接后继。

## 3. 架构

```
主仓（三处改动，均默认关闭）
├─ A. gateway 检索事件日志  [retrieval] events_log = "path.jsonl"（空=关）
│     pipeline.go 在 injector.Build 返回后写一行：
│     {ts, session, arm, query, top_margin, decisions:[{id,score,coverage,zone,admitted,reason}]}
├─ B. gateway rerank 装饰器  [retrieval] rerank_base_url / rerank_top_n / rerank_timeout_ms
│     rerankIndex{inner slice.Index} 包裹 newRetriever 产物：
│     inner.Search 过取 rerank_top_n → POST /rerank → 重排取 k 返回
│     fail-soft：超时/非 200/维度不符 → 原样返回 inner 结果（照抄 ModelEmbedder 模式）
└─ C. cmd/semantix gc --score-params <json>  显式加载完整 ScoreParams 覆盖配置键

主仓 scripts/ml/（uv + pyproject.toml，Python 仅存在于开发机）
├─ synth_queries.py    切片内容 → 伪 query（云端 LLM 批量，一次性冷启动）
├─ build_dataset.py    slice.Export + 事件日志 → 训练/评估对（按时间切分 held-out）
├─ train_reranker.py   sentence-transformers CrossEncoder 微调（MPS），从上一 checkpoint 继续
├─ export_onnx.py      checkpoint → ONNX（int8 量化可选）
├─ fit_score_params.py 回放拟合 ScoreParams（单轮步长钳制 ≤20%）
├─ eval_retrieval.py   NDCG@5 / MRR@5 + 门禁判定，报告落盘
├─ rerank_server.py    loopback /rerank 服务（onnxruntime CPU/CoreML）
└─ watch_and_train.py  自进化守护（见 §6）

实验环境 ~/.semantix-lab/retrieval-lab/（沿用 baohuaban 惯例）
├─ on/ off/            双臂 gateway.toml + gateway.jsonl + usage.jsonl + sessions/ + events.jsonl(on)
├─ train/{datasets,checkpoints,current,reports}/
├─ run-arm.sh          on|off 启动器
└─ trainctl.sh         bootstrap | train | eval | watch | rollback | status
```

### 3.1 rerank 分数尺度契约

zone 分类器支持两种共存尺度（`gateway/retriever.go` 头注释）：BM25
（>>1，只看相对置信）与 bounded [0,1]（绝对下限也生效）。重排器输出
sigmoid 归一到 [0,1]，**替换** Hit.Score 并重排序——落在既有 bounded
尺度契约内；`Lexical`/`LexicalValid` 保留原值（词法闸门语义不变）。
on 臂的 zone 阈值按 bounded 尺度独立标定（实验环境各臂本就独立配置）。

### 3.2 重排器特征边界（I-5 合规的结构保证）

rerank 请求体只含 `{query, documents:[{id, text}]}`——text 是切片
Content（注入前原文）。**Stats、Weight、Meta 一律不出现在请求里**，
自我强化环在接口层被切断，代码评审可逐字段核对。

## 4. 训练信号与数据集

| 信号 | 来源 | 用途 |
|---|---|---|
| (query, admitted 切片) | 事件日志 `decisions[].admitted` | 重排正例 |
| (query, zone_miss/below_min_score 候选) | 事件日志 reason | 重排负例（同 query 内对比） |
| (query, 合成伪 query→源切片) | synth_queries.py | 冷启动正例（GPL 式） |
| 随机负采样 | 库内非候选切片 | 冷启动负例 |
| 切片未来 30 天是否再被使用 | Stats 快照回放（LastUsed/Hits/Injected 时序） | ScoreParams 拟合目标 |

- held-out：按时间切分（后 20% 时段的事件），合成对分层抽样 10%。
  评估集一经生成冻结版本号，训练集永不掺入。
- 冷启动现实：实验环境当前仅个位数事件。bootstrap 阶段以合成对为主
  （目标 ≥2000 对），真实事件随双臂狗粮积累逐轮稀释合成占比。

## 5. 评估协议与发布门禁

- 离线指标：NDCG@5、MRR@5（held-out），每轮训练产出
  `train/reports/<version>.md`（含 vs 上一版对比、门禁结论、样本量）。
- 门禁：新 checkpoint 两项指标**均不低于**当前发布版才允许发布；
  否则保留 checkpoint 但不切 current，报告标记 REJECTED。
- 在线锚定（I-4）：双臂对照注入采纳率 Injected/(Injected+Rejected)、
  slice_hits、tokens 成本——离线过门禁 + 在线不劣化，才有转正资格。
- 验证输出必须 tee 落盘（训练/评估日志进 reports/，禁止只 grep 管道）。

## 6. 自进化守护（watch_and_train）

- watch 对象：切片库文件 mtime/大小 + 事件日志行数（纯文件观察，
  不加 kernel 钩子——`score.go` 明文禁止回调接口）。
- 触发条件：新事件 ≥ N（默认 200）**或** 距上轮 ≥ T（默认 24h）且有新数据；
  二者皆无则静默。去抖：触发后等待静默期（默认 10min 无新写入）再开跑。
- 循环体：build_dataset → train_reranker（从 current 继续）→ export_onnx →
  eval_retrieval 门禁 → 通过则原子切换 `current/` 软链 + 通知
  rerank_server 热载（SIGHUP）→ fit_score_params → 门禁 → 更新
  score_params.json。
- 模型版本只在服务重载边界生效，会话进行中不切换（freeze-window 纪律）。
- 回滚：`trainctl.sh rollback [version]` 切回任意历史 checkpoint 并热载。

## 7. evolution-invariants 逐条对照（§4 验收要求）

- **I-1 原始层不可变：满足。** 训练管线只读（`slice.Export` + 事件日志），
  不写任何切片；合成 query 是训练数据不是切片，存于 train/datasets/，
  永不入库。
- **I-2 固化高门槛：满足。** (a) 发布以 held-out 外部指标达标为前提；
  (b) 模型产物存于实验环境 train/ 独立命名空间，不混入切片空间；
  (c) checkpoint 版本化，rollback 一条命令，原始层不受任何影响。
- **I-3 增量进化：满足。** 重排器每轮从上一 checkpoint 继续训练；
  ScoreParams 单轮步长钳制 ≤20% 且走既有 params 单向流（evolve → params
  → gc 同构）；模型切换钉在服务重载边界（注入集冻结期语义）。
- **I-4 外部信号锚定：满足。** 全部标签来自外部信号（admitted/zone/
  Stats 时序），无模型自评；合成 query 仅作冷启动正例来源，其质量由
  held-out 真实事件检验。
- **I-5 Weight 永不参与检索：满足。** 价值打分器输出只经 `gc --score-params`
  进淘汰通道；重排器特征在接口层排除 Stats/Weight/Meta（§3.2），检索仍
  只依据内容相似性。

## 8. 安全与隐私

- 事件日志含 query 明文与切片 ID：flag 默认关，一期只在实验环境开启；
  日志路径不进遥测，不随任何上报离开本机。
- rerank 服务只绑 loopback；gateway 侧 base_url 无鉴权即明文 HTTP，
  故 config 校验强制 `rerank_base_url` 为 127.0.0.1/localhost。
- 训练依赖锁定于 pyproject.toml + uv.lock；模型权重缓存目录进 .gitignore。

## 9. 里程碑与 DoD

| 里程碑 | DoD |
|---|---|
| M1 本 spec | 门禁评审通过 |
| M2 数据地基 | 事件日志落盘可回放出注入集；synth 数据集 ≥2000 对；uv sync 一键就绪 |
| M3 重排器闭环 | 训练→ONNX→服务→gateway 接入全通；杀服务检索链无感降级（测试覆盖） |
| M4 价值打分器 | 拟合产出 score_params.json；gc --score-params 生效且步长钳制有测试 |
| M5 自进化守护 | watch 触发完整循环一次成功；rollback 演练通过 |
| M6 收益证据 | 双臂报告：离线 NDCG/MRR + 在线采纳率/成本，给出转正/继续实验/放弃结论 |
