# Spec：gateway 打通可选真实语义 embedder（Issue 275）

> 对应 Issue：#275 gateway 打通可选真实语义 embedder（现硬编码 hash）
> 判级预估：**Spec-Required**（gateway 配置键 + 外部可选依赖 + 向量兼容性隔离）。
> 复用基线：CLI 已有 `buildEmbedder`/`ModelEmbedder` 装配（`cmd/semantix/embedder.go`）、
> `ModelEmbedder` fail-soft 退回 hash（`kernel/embed/model.go`）、gateway `CacheConfig`
> judge 外部 API 配置段先例。
>
> **状态（2026-08-21）**：本文档为 Issue 275 实现规格，先审后写；依团队决策本轮**实现 gateway
> 配置 + 装配开关，默认仍 hash（零外部依赖、零行为变化），不实际调用外部 embedding API**。

## 1. 目标与范围

**核心目标**：让 gateway 的向量检索不再硬编码 hash embedding——通过 `[retrieval]` 配置段可选
切到真实语义 `ModelEmbedder`（OpenAI 兼容），默认仍 `hash`（零配置零行为变化、无外部依赖）。
语义缓存的命中质量以 embedding 质量为上界，真实 embedder 是命中质量的地基。

**范围内**：

- `gateway/config.go`：`[retrieval]` 增加可选 `Embedder` 配置段（kind/base_url/model，仿 `CacheConfig`
  judge 段）；默认空 → hash。
- `gateway/retriever.go`：`vectorIndex.emb` 从具体 `embed.HashEmbedder` 改为 `embed.Embedder` 接口；
  `newRetriever` 接受可选的真实 embedder，kind=`model` 时装配 `ModelEmbedder`。
- 复用 `kernel/embed` 既有能力（`ModelEmbedder` fail-soft 回 hash 已在 `model.go`），**不新增外部调用**。
- 回归测试：默认 hash 行为不变、model 配置装配正确、按 (model,dim) 隔离。
- spec 文档：本文档。

**不在范围内**（后续单元，本 PR 不实现）：

- **实际调用真实 embedding API**：本轮只做装配开关与配置解析，不连接/验证任一外部服务（部署方
  填配置后由 ModelEmbedder 走既有 fail-soft）。
- **ANN 索引**（库规模上千后评估，`kernel/embed/vecindex.go:51-73` 暴力扫描）。
- **重嵌入迁移路径**（跨 embedder 的既有切片向量重嵌入）——本轮仅定义隔离口径（见 §3.3），
  迁移工具另议。

## 2. 现状核实（2026-08-21）

- `gateway/retriever.go:61` 硬编码 `embed.HashEmbedder{Dim: dim}`，`vectorIndex.emb` 是具体类型，
  无法切换。
- `HashEmbedder`（`kernel/embed/hash.go`）FNV-1a feature hashing，包注释自陈"无真实语义"；256 维。
- `ModelEmbedder`（`kernel/embed/model.go:30-70`）已实现（OpenAI 兼容 `/embeddings`、key
  `SEMANTIX_EMBED_API_KEY`、远程失败 fail-soft 回 hash），但装配点只在 CLI。
- `Meta.EmbedModel`/`EmbedDim`（`kernel/slice/slice.go:113-114`）记录每个切片向量的 provenance，
  是隔离判定依据。

## 3. 设计

### 3.1 配置（gateway/config.go）

`RetrievalConfig` 新增可选 `Embedder EmbedderConfig`（仿 `CacheConfig` judge 段，`toml:"embedder"`）：

```go
type EmbedderConfig struct {
    Kind     string `toml:"kind"`       // "hash"（默认）| "model"
    BaseURL  string `toml:"base_url"`   // model: OpenAI-compatible base (e.g. https://api.openai.com/v1)
    Model    string `toml:"model"`      // model: embedding model id
    // APIKey 沿用 env SEMANTIX_EMBED_API_KEY（与 CLI 一致，不写进 toml）
    Dim      int    `toml:"dim"`        // hash: 维度（<=0 -> 256）；model: 记录声明维度（可选，用于隔离校验）
}
```

为空 / kind=hash 时沿用现状（`embed.HashEmbedder`，零变化）。kind=model 且 BaseURL/Model 缺一
时由 validate 拒绝（fail-fast，避免静默退化到无意义配置）。

### 3.2 装配（gateway/retriever.go）

- `vectorIndex.emb` 改为 `embed.Embedder` 接口（`HashEmbedder` / `ModelEmbedder` 均满足）。
- `newRetriever(kind string, dim int, emb embed.Embedder)`：`emb == nil` 时用
  `embed.HashEmbedder{Dim: dim}`（默认/兼容）。vector/hybrid 用传入的 emb 建 index。
- gateway.New 解析 `cfg.Retrieval.Embedder`：kind=model 时 build `ModelEmbedder`（复用
  `kernel/embed.NewModelEmbedder(cfg)` + `SEMANTIX_EMBED_API_KEY`），传入 newRetriever。

```go
emb, err := buildGatewayEmbedder(cfg.Retrieval.Embedder) // hash 或 model；失败即 error
idx := newRetriever(cfg.Retrieval.Retriever, cfg.Retrieval.VectorDim, emb)
```

- **零行为变化**：未配置 embedder 段 / kind=hash 时 `emb=nil` → newRetriever 内部 fallback 到
  `HashEmbedder`，与现状逐字节一致。

### 3.3 向量兼容性隔离（既有的 (model,dim) 判据）

跨 embedder 的切片向量**不可混用检索**。判定依据 `Meta.EmbedModel` / `Meta.EmbedDim`：

- 检索 index 在 Insert 时嵌入得到的向量属于「当前 embedder 空间」；存储切片若 Meta.EmbedModel
  异于当前模型（或 EmbedDim 异），判为**不兼容**——**不纳入该同构检索**（跳过，不进候选）。
- 本轮在 vectorIndex.Insert/Search 边界实现该隔离守卫（按当前 embedder 的 model/dim 过滤），
  防止 model↔hash 双空间混检。
- 默认 hash：`EmbedModel` 为空/`hash`，维度=配置；现有行为不变。

> 注：gateway 是即时嵌入（Insert 时用当前 emb 嵌入，`retriever.go:67-77`），存储切片多为 hash 或
> CLI 产出的 model 向量；混检守卫针对「库中存在异模型切片」的情形，本轮以测试覆盖隔离判定。

## 4. 测试

- `gateway/retriever_test.go`（或新增）：默认（nil emb）行为不变 —— hash 检索与原一致。
- `gateway/config_test.go`：`Embedder` 段解析；kind=model 缺 base_url/model 时 validate 拒绝。
- `gateway/slicestats_test`（或 retriever_test）新增：kind=model 时装配出 `*embed.ModelEmbedder`
  （fake 远端，验证装配而非真实调用）；model↔hash 异模型切片不进入同一检索（隔离守卫单测）。
- 既有 vector/hybrid/bm25 检索测试保持通过。

## 5. 验收

- [ ] 未配置 embedder 段（默认）时 gateway 行为与现状完全一致（hash、无外部依赖）。
- [ ] 配置 `[retrieval.embedder] kind="model"` 后 gateway 装配 `ModelEmbedder`（不实际调用远端，
  部署方填 base_url/model + env key 后由其 fail-soft）。
- [ ] 异 (model,dim) 切片不进入同一向量检索（隔离守卫测试通过）。
- [ ] `go build ./...` + `go test`（gateway/kernel/embed 相关）通过。
- [ ] spec（本文档）先审后写。

## 6. 后续候选（不在本轮）

- 实际连通真实 embedder 的回归基线（对照 hash 误命中率 / clear-hit），需可选外部服务。
- 重嵌入迁移工具（跨 embedder 时对存量切片重嵌入并更新 Meta 模型/dim）。
- ANN 索引替换暴力扫描（库规模上千）。
