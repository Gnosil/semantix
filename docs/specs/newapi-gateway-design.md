# New API 网关集成设计 —— Semantix Gateway（v1）

> 日期：2026-08-13（起草）· 状态：**M0 已实现、M1 主干已实现（RuleGate / promote 未接线）、
> §7 验收门未记录；M2 未开始**（2026-08-15 回写，见 §0）· 对应：Semantix 对外形态扩展
> 一句话目标：以 **New API 做 OpenAI 兼容中转站**，在它后面挂一个 **Semantix Gateway**
> （新组件，复用 kernel 三层缓存），让任意 OpenAI 兼容客户端（Claude Code / chatbox / IDE 插件等）
> 的请求在到达上游 LLM 之前先过语义缓存——**L3 命中零上游调用、L2 注入跳过重复探索**，
> 达成省 token、省成本的效果。
>
> 与既有集成方式的关系：LangChain 中间件是「消息级」读/写记忆，Semantix fork 是「事件级」，
> 本设计是**「请求级」的 HTTP 网关形态**——同一个「读记忆（inject/lookup）+ 写记忆（extract）」模型，
> 只是挂在 API 网关上，零侵入任何 agent harness，天然适配所有 OpenAI 兼容客户端。

---

## 0. 交付状态（2026-08-15 回写）

本文档 §1–§10 是 2026-08-13 的 **v1 规划原文**，除本节外未作改动。实现已于 Issue #133 落地
（`gateway/` 五个实现文件 + `cmd/semantix-gateway/main.go`，约 1400 行实现 + 1000 行测试），
验收记录见 [`docs/reports/issue-133-acceptance.md`](../reports/issue-133-acceptance.md)。
本节按 §范围逐条对账，供读规划的人先看清「哪些已经是代码、哪些换了方案、哪些还没有」。

**一句话**：§3 规格的主干（OpenAI 兼容层 / 七步流水线 / L2 注入 / L3 复用 / 流式双向 / 会话旁路写记忆 /
配置与安全）已经是可运行的代码，`go test ./... -race` 全绿（2026-08-15 复核：gateway 包 29 个测试函数）。
注意 §7 给 M1 划的范围里点名了 `RuleGate` 与 `promote`，这两项**都没有接线**（§0.2 / §0.3），
所以 M1 只能说「主干已实现」，不是整条完成。
验收口径同样要分清：Issue #133 的验收报告核对的是**该 issue 自己的 checklist**（结论：验收通过，
2026-08-14），**不是 §7 的 M0 / M1 Gate**——报告中所有端到端证据的上游都是 `httptest` 假上游，
「真实客户端 → New API → DeepSeek 全链路」与「第二次命中 + 成本节省 ≥30% 实测」至今没有记录。
**M2（Claude 多模型 + 计费对账）未开始**，配置层显式拒绝 `vendor="anthropic"`。

> **两套 M 编号别混**：Issue #133 的标题是「**M2-GW1**: Semantix Gateway v1」，那是**路线图**的阶段编号；
> 本文档 §7 的 M0 / M1 / M2 是**网关自己的**实施阶段。「M2-GW1 验收通过」说的是路线图任务交付，
> 不等于本文档 §7 的 M2 完成——§7 的 M2 未开始。

### 0.1 已实现（§范围内）

| §  | 条目 | 落点 |
|---|---|---|
| §3.1 | 独立 Go 进程、复用 kernel、不侵入 New API | `cmd/semantix-gateway/main.go`、`gateway/`（kernel 无反向依赖） |
| §3.1 | 切片库用 `slice.NewFileStore`（JSONL），0600/0700 权限 | `gateway.go` `appendJSONLLines` / kernel store |
| §3.2 | `GET /v1/models`（列出全部 model_alias） | `server.go` `handleModels` |
| §3.2 | `POST /v1/chat/completions`（含 `stream=true`） | `server.go` → `pipeline.go` `handleChat` |
| §3.2 | `GET /healthz`（New API 渠道探活） | `server.go` `handleHealth`（上游探活已实现，见 §0.3） |
| §3.2 | 错误一律走 OpenAI 信封 `{"error":{message,type,code}}` | `server.go` `writeAPIError` |
| §3.3 | 七步流水线：鉴权 → 归一化 → L3 → L2 → 转发 → 透传 usage → 异步写记忆 | `pipeline.go` `handleChat` |
| §3.3 | 「缓存永不阻塞主链路」：注入失败只记日志继续转发 | `pipeline.go`（`inject` 错误不中断） |
| §3.3 | L3 命中可观测：`x-semantix-cache: hit` / `miss` 响应头 | `pipeline.go` `replyFromCache` / `passthrough` |
| §3.3 | ablation 开关 `SEMANTIX_GATEWAY_DISABLE` 一键退化纯透传 | `gateway.go` `disableEnv`（只认 1/true/yes/on，`0`/`false` 保持缓存开启） |
| §3.4 | 未命中流式逐块透传，不重排不重写 | `pipeline.go` `streamThrough` |
| §3.4 | 命中流式：按 SSE 协议重建回放（role 首块 → content 分块 → finish_reason → `[DONE]`） | `pipeline.go` `replayStream`（分块 256B） |
| §3.5 | Result 类型 + zone Hit + deps mtime 快速失败 + sha256 指纹权威，fail-closed | `kernel/cache` `L3Decider.DecideL3/verified`（U16/#59 既有能力） |
| §3.5 | 上下文 / 模型隔离：同 query 不同历史或不同模型绝不互相复用 | `normalize.go` `contextHash` + `cache.Query{ContextHash,Model}`，未打标的旧切片一律 fail closed |
| §3.5 | 无 deps 的网关结果默认不进 L3（`l3_safe_default = false`） | `gateway.go` `l3SafeExtractor` + `cache` 侧 `Meta.L3Safe` 判定 |
| §3.6 | `inject.Injector{K,Budget,Zones}` 检索并拼注入块，位置在 **system 提示末尾**（字节稳定） | `pipeline.go` `rewriteOutgoing` / `attachBlock`（无 system 消息则前置一条） |
| §3.7 | 请求/响应对旁路落盘 `sessions_dir/<id>.jsonl`（0600，`ingest.JSONLSource` 兼容） | `gateway.go` `recordSession`（session id 正则白名单，防路径穿越）。**流式路径只写请求侧**，见 §0.3 |
| §3.7 | 异步 `ingest.Pipeline.Run` 提取入库，失败只记日志不影响主链路 | `gateway.go` `ingestSession`；关停时 `Close()` drain 在途提取 |
| §3.8 | 模型映射三层：New API 模型名 = `model_alias` → `upstream_model` | `config.go` `UpstreamFor` + `pipeline.go` `rewriteOutgoing` |
| §3.8 | 上游超时 + 单次重试兜底（仅网络错误重试，HTTP 状态错误不重试） | `pipeline.go` `forward`（client timeout 120s） |
| §3.9 | `semantix-gateway.toml` 全部小节；`${VAR}` 替换与 `~` 展开，未解析即启动失败 | `config.go` `Load/expand/expandField/validate` |
| §3.10 | 网关 Key 常量时间比较；上游 key 只来自环境变量 | `server.go` `authenticate`；`config.go`（配置文件只写 `${VAR}` 引用） |
| §4.3 | L3 命中返回合成 usage：`completion_tokens=0` + `prompt_tokens_details.cached_tokens` | `pipeline.go` `replyFromCache` |
| §4.3 | 网关侧 `kernel/usage` 事件记录（L3 复用 / 注入 token / 切片命中数） | `gateway.go` `recordUsage` + `[ingest] usage_log` |
| §9 D4 | 缓存库与切片库**合库**（同一 JSONL Store，L3 条目即 Result 切片） | `config.go` `[store] db` 单一路径 |
| §3.5 | grey 区 LLM judge：`[cache] judge_api_key` 接线 `judge.RuleGate.Chain`（Issue #186 / GW6） | `kernel/cache` `L3Decider.Judge`（灰区候选 `judgeGrey`）+ `gateway.go` `New()` 构造 `judge.LLMJudge`；未配 key 时灰区保守 Reject |
| §3.9 | `[retrieval] retriever = bm25 \| vector \| hybrid` 接线（Issue #186 / GW6）：vector/hybrid 走 `kernel/embed` HashEmbedder/VectorIndex | `gateway/retriever.go` `newRetriever`（返回 `slice.Index`，L2/L3 无感知）；hybrid 双路归一化融合 |

实现另外做了两件 spec 没要求、但属于同方向的收尾：上游异常断流时网关补发 `data: [DONE]`
（客户端不会挂死）、以及转发时过滤 hop-by-hop 头（RFC 9110 §7.6.1）。

### 0.2 改变了方案（spec 写法与实现不一致，以实现为准）

| §  | spec 原文 | 实际实现 |
|---|---|---|
| §3.5 | `缓存键 = hash(scope \| 归一化 query \| 模型名 \| deps 指纹 \| messages 上下文指纹)` | **没有缓存键，也没有哈希查表**。实际是 kernel 既有的「检索 + 分层门禁」：BM25 检索 top-k → zone Hit 分类 → Result 类型 → `ContextHash`/`Model` 精确相等 → deps mtime + sha256 验证。scope / 模型名 / 上下文指纹是**过滤条件**，归一化 query 是**检索查询**，都不是键的组成部分。隔离效果与 spec 意图一致，机制不同 |
| §3.5 | 验证走 `judge.RuleGate.Chain`，grey 区可配 `SEMANTIX_JUDGE_API_KEY` 走 LLM judge | **GW6 已接线**（Issue #186）：`cache.L3Decider` 新增 `Judge` 字段，灰区候选（`zone.Grey`）经 `judgeGrey` → `judge.RuleGate.Chain`（fingerprint 门 → LLM judge）确认后才复用；`[cache] judge_api_key` + `judge_base_url` + `judge_model`（+ 可选 `judge_protocol`，默认 openai）构造 `judge.LLMJudge`。未配 key → 灰区保守 Reject（fail-closed）。`verified` 的 deps 指纹校验仍在 judge 之后兜底 |
| §3.5 | TTL 按 vendor 差异化（DeepSeek 24h / DashScope 5m / Anthropic 5m） | 单一 `[cache] ttl_seconds` 时间窗，作用在 `Slice.CreatedAt` 上；`0` 表示不设时间窗。无 vendor 分支（M2 未开始，多 vendor 尚无实际差异） |
| §3.2 | `/v1/completions` 可选，MVP 默认返回 501 | 该路由未注册，落到默认分支返回 **404 `not_found`**（不是 501） |
| §4.4 | 方案 B：渠道注入固定 `x-project` header，网关按 header **选 scope 库** | 实现的是 `x-semantix-scope` header，传的是 scope **枚举**（`session`/`project`/`user`）而非项目名；底层始终是**同一个 store 文件**，只切 kernel 的 scope 字段。是方案 B 的接入点，不是多库隔离 |
| §5 | 「零第三方依赖——`go.mod` 无外部包」 | `go.mod` 有 `github.com/BurntSushi/toml v1.6.0`（早于本设计，由 `kernel/config` 引入，网关沿用它解析 TOML）。§3.1 的「零第三方 **HTTP** 依赖」仍然成立：传输层是标准库 `net/http` + `http.Flusher` |
| §3.9 | 配置草案的键 | 实现多两个键：`[store] deps_root`（§3.5 所说「deps root 由配置提供」的落点）、`[ingest] usage_log`（§4.3 usage 记录的落点） |
| §3.11.1 | 「仅注入时打 ≤2 个断点；断点 ① 在 system + 注入块末尾；断点 ② 只打 text block」 | **P0-a 修订**：默认每请求都打；BP1 在静态 system（注入块拆成其后的独立 block，不带标记）；BP2 可落 tool_result / image / tool_use；每上游 `cache_breakpoints` 开关；Anthropic 路径补跑 `sanitizeOutgoing`；`tool_result.input` → `content`。见 [`gateway-anthropic-cache-p0a.md`](gateway-anthropic-cache-p0a.md) |

### 0.3 未实现

下列多数条目在 Issue #133 验收时就是**已知债务**（验收报告 §4 分级 nit、§6「未闭合风险与后续边界」），
不是被遗忘的；列在这里是为了让只读本设计文档的人不会误以为它们能用。

| §  | 条目 | 现状 |
|---|---|---|
| §3.8 / §7 M2 | Claude / Anthropic 适配（messages 格式转换 + `cache_control` 断点） | **已实现**（Issue #185，§3.11）；断点策略按 P0-a 修订（§3.11.1，[`gateway-anthropic-cache-p0a.md`](gateway-anthropic-cache-p0a.md)）。本行原记载的「配置层显式拒绝」已过期 |
| §3.5 | `promote.CascadeInvalidate` 级联失效 | gateway 零引用 `kernel/promote`。上游内容版本变化时不会级联失效下游条目（deps 指纹仍能兜住文件类变更） |
| §3.9 | `[retrieval] retriever = bm25 \| vector \| hybrid` | **GW6 已接线**（Issue #186）：`gateway/retriever.go` `newRetriever` 按配置构造索引——`bm25` 保持 `kernel/bm25`；`vector` 用 `kernel/embed` HashEmbedder + VectorIndex（cosine）；`hybrid` 双路检索、`kernel/fuse` 融合（Issue #274）：weighted 等权/可配权重（默认，历史行为）或 rrf（倒数排名融合，分数线性标度化到 [0,1]，zone 绝对阈值语义保持）。`vector_dim` 键（默认 256）控制 HashEmbedder 维度；非法 retriever/fusion 值 `validate()` 报错 |
| §3.4 | 未命中流式：上游不返回 usage 时，网关在 `[DONE]` 前补含注入统计的末块 | 已实现（Issue #187）：完整流且上游无 usage 时，在 `[DONE]` 前补发 OpenAI 格式 usage 事件（含 `prompt_tokens_details.cached_tokens` 注入统计 + `"estimator":"bytes/4"`）；异常断流只补 `[DONE]` 不补 usage（半截流不计量） |
| §3.7 | 流式路径的**响应侧**写记忆 | **已实现**（Issue #182）：`streamThrough` 聚合 SSE 内容进旁路记忆。GW4 实机验证（Issue #184，`gateway-m1-acceptance.md` §5.2）：流式任务 t09 在切片库中有 3 条自己的 Result 切片，**流式响应确实成为可复用 Result 切片**。此前本行记载的「L3 的写入实际只来自非流式请求」已过期，据实测更正 |
| §3.2 | `/healthz` 检查「切片库可打开 + **上游可达性**」 | 切片库在 `New()` 阶段已打开（打不开进程起不来）；上游探活已实现（Issue #183）：`GET {base_url}/models` 逐个上游探测，2xx=健康，任一失败/超时返回 503 + OpenAI envelope；`[server] health_timeout_seconds` 可配（0=禁用探活） |
| §5 | 部署产物：`docker-compose.yml`、网关镜像 Dockerfile、`semantix-gateway.toml` 示例 | 已存在 `deploy/`（Issue #183）：`semantix-gateway.toml.example`（全部小节 + `${VAR}` 示范）、多阶段 Dockerfile（alpine + CA 证书 + 非 root，取舍见 deploy/Dockerfile 注释）、`docker-compose.yml`（New API + 网关两服务 + healthcheck，named volume 持久化）；部署指引见 `docs/QUICKSTART.md`「网关部署」节 |
| §4.3 | 与 New API 计费对账的 token 口径 | 合成 usage 附 `"estimator":"bytes/4"` 字段显式标注字节估算（非真 tokenizer 计数）。**tokenizer 评估结论（Issue #187）：不引入**——tiktoken 兼容实现需新增 BPE 词表依赖面，与「零第三方运行依赖」铁律冲突；`bytes/4` 对英文偏差约 ±10%、对中文偏大，口径差由 estimator 字段承载，对账时按字段识别 |
| §7 M0 | 门：真实客户端 → New API → 网关 → DeepSeek 全链路跑通 **且** 会话入库后 `semantix search` 可检索到切片 | **已记录，通过**（Issue #184，2026-08-20）：`docs/reports/gateway-m1-acceptance.md` §4——真实 DeepSeek 上游（响应带 `X-Ds-Trace-Id`）经 New API v1.0.0-rc.25 → 网关全链路 200；会话旁路落盘 11 个文件，`semantix search --retriever bm25` 检出 Prompt/Result 切片 |
| §7 M1 | 门：重复任务第二次命中 `x-semantix-cache: hit` 且零上游调用；成本节省 ≥30% 实测 | **已记录，成本达标／命中部分达标**（Issue #184，2026-08-20）：`gateway-m1-acceptance.md` §5–§6——10 组重复任务 **7 组**命中 `x-semantix-cache: hit`，网关 usage（`l3_reuse:true`/`tokens_out:0`）与 New API 计费（`completion=0`）双证；按 New API 口径 **节省 52.7%（≥30%）**，单次命中省 75.3%。**前提偏离**：`l3_safe_default=true` + 启用 grey judge（不开 judge 只有 1/10）。未达 10/10 的结构性原因见报告 §8.1（`kernel/cache/l3.go` 的 `top1` 取自未按类型过滤的命中列表，Prompt 切片压低 Result 的相对置信度） |
| §7 M2 | 四家模型全通 + 命中率周报 + New API 对账一致 | 未开始 |
| §9 D1 | L3 命中是否对客户端降价（计费倍率） | **仍未决**。网关侧的合成 usage 已就位，New API 侧的倍率是部署决策，未在仓库中体现 |
| §9 D2 | `/v1/embeddings` 透传 | 未做（与 spec 建议一致：MVP 不支持） |

### 0.4 读这份规划时的注意事项

- §2 的收益模型、§6 的时序图、§8 的风险表描述的是**设计意图**，不是实测结论。79.8% 来自
  `docs/reports/m0-cost-comparison.md` 的合成演示，**不是网关链路的实测数字**；网关场景的 30%–80%
  预期区间至今没有实测数据支撑（§7 M1 门未记录）。
- §9 的 D1–D4 中，只有 D4（合库）在实现里有确定答案，D3 按建议用了方案 A，D1 仍是待决策项。
- §10 结论里「天然服务 DeepSeek / Claude / Kimi / GPT 四家模型」是 v1 规划的终局描述。当前实现
  接受的 vendor 只有 `deepseek` / `openai` / `moonshot`（同为 OpenAI 兼容协议），Claude 需要
  M2 的格式转换才能接入（§0.3）。

---

## 1. 背景与缺口

### 1.1 Semantix 现状（v0.3.1）

- kernel 三层语义缓存已实现：**L1** 前缀字节稳定（依赖上游前缀缓存价）、**L2** 语义切片注入（`kernel/inject`）、**L3** 已验证结果复用（`kernel/fingerprint` + `kernel/judge` + `kernel/promote`，issue-08 已验收）；
- 成本演示（`docs/reports/m0-cost-comparison.md`）：跨会话复用**节省 79.8%**（目标 ≥20%），敏感性分析在最保守参数下仍 ≥30%；
- 对外形态目前是**纯 CLI + kernel 库**：`extract / search / lookup / inject / verify / eval / usage`，**没有 HTTP 服务、没有 OpenAI 兼容 API 层**。

### 1.2 New API 现状

New API（QuantumNous/new-api，one-api 加强分支）是 **OpenAI 兼容的 API 中转/分发面板**：
统一入口 `/v1/*`、多用户 Token 管理、渠道管理（多上游 + 模型映射 + 负载均衡 + 重试）、按模型定价计费、额度与速率限制。它本身**不带 agent loop、不做语义缓存**——这正是 Semantix 要补的位置。

### 1.3 缺口与方案

> **缺口**：New API 的「渠道」必须是一个 HTTP 上游，而 Semantix 没有 HTTP 层，两者无法直接对接。

**方案**：新增 **Semantix Gateway**（独立 Go 进程，复用 kernel 全部包），对外暴露 OpenAI 兼容 API；
New API 侧把它配成一个**自定义渠道**。数据流：

```
客户端 (Claude Code / chatbox / 任意 OpenAI 兼容客户端)
   │  base_url = https://new-api.example.com
   ▼
New API（key 管理 / 计费 / 渠道分发）
   │  渠道类型 = 自定义（base_url = http://semantix-gateway:8080）
   ▼
Semantix Gateway（★ 本设计的核心新组件）
   │  L3 语义缓存命中 ──► 直接返回缓存结果（零上游调用）
   │  L2 注入 + 转发 ────► 上游 LLM（DeepSeek / Claude / Kimi / GPT）
   │  会话旁路提取 ──────► 切片库（写记忆，供下次复用）
```

---

## 2. 省 token 机制与收益模型

### 2.1 三层机制在网关场景的映射

| 层 | 机制 | 网关中的落点 | 省钱方式 |
|---|---|---|---|
| **L1** | 前缀字节稳定 | 网关把注入块放在 **system 提示末尾、消息尾部之前**（Semantix KV Cache 机制研究结论：静态在前、动态在后），且注入块 ID 规范序 → 字节稳定 | 上游按**缓存价**计费（DeepSeek miss $0.27/M vs hit $0.07/M，价差约 4 倍） |
| **L2** | 语义切片注入 | 未命中时 `inject.Injector.Build(query)` 检索 top-k 切片，拼入请求后转发上游 | 模型**跳过重复探索**，少生成工具调用/中间步骤 → 省**输出 token**（演示中 80% 的重复步骤被替代，是主要节省来源） |
| **L3** | 已验证结果复用 | 请求归一化 → 指纹校验（deps/mtime）→ `judge.RuleGate` 验证 → 命中直接返回缓存响应 | **零上游调用**，节省约 100% 的该请求成本 |

### 2.2 收益公式（沿用 m0-cost-comparison 模型）

```
单请求成本 = P_miss × (inject_bytes + 新内容) + P_hit × 稳定前缀 + P_out × output_tokens

baseline（无网关） = P_miss × 全量输入 + P_out × 全量输出
gateway 未命中     ≈ P_hit × 注入前缀 + P_miss × 增量 + P_out × (1 - reuse_ratio) × 全量输出
gateway L3 命中    ≈ 0（仅网关本地检索，<100ms）※ 计费上需 D1 的「合成 usage + 近零倍率」落地（§4.3）
```

- 合成演示实测：1800 → 360 completion tokens，成本 $0.001980 → $0.000399，**节省 79.8%**；
- 网关场景（API 网关、真实重复任务）预期区间：**30%–80%**，取决于重复率（垂类工作流重复度高，收益靠近上沿）；
- L3 命中是「纯赚」：一次缓存命中省掉该请求的全部上游费用，且延迟更低（<100ms vs 秒级）。

### 2.3 目标模型单价（2026-08 参考，网关设计按此建模）

| 模型 | 输入(miss) | 输入(缓存命中) | 输出 | 缓存机制 |
|---|---|---|---|---|
| DeepSeek-chat | $0.27/M | $0.07/M | $1.10/M | 自动前缀缓存（24h），无需显式标记 |
| Kimi (Moonshot) | ~$0.60/M | 前缀缓存价 | ~$2.20/M | OpenAI 兼容端点，自动前缀缓存（自动性待上游确认，§3.5） |
| GPT-4o 级 | ~$2.50/M | ~$1.25/M | ~$10/M | 自动前缀缓存（provider 侧） |
| Claude Sonnet 级 | ~$3.00/M | ~$0.30/M | ~$15/M | 需 `cache_control:{type:"ephemeral"}` 断点（≤2 个） |

> 注入块**字节稳定**是 L1 生效的前提；对 Claude 还需在注入块边界打 cache_control 断点（见 §3.8）。
> 单价随时可能变动，网关不硬编码价格——价格只在 New API 侧做计费用，网关只透传 usage。

---

## 3. Semantix Gateway 组件规格

### 3.1 形态与进程

- **独立 Go 进程**：新 `cmd/semantix-gateway/`，复用 `kernel/` 全部包（结构铁律不变：kernel 不得反向依赖网关）；
- 不侵入 New API（New API 无插件机制，渠道化对接是唯一合理方式）；
- 单二进制、零第三方 HTTP 依赖（标准库 `net/http` 即可，流式用 `http.Flusher`）；
- 切片库复用 `slice.NewFileStore`（JSONL 单文件 + 原子重写，0600/0700 权限约定延续；MVP 明确不用 bbolt，量大再评估切换）。

### 3.2 OpenAI 兼容 API 端点

| 端点 | 方法 | 说明 |
|---|---|---|
| `GET /v1/models` | 透传 | 返回可路由模型列表（网关上游已配置的模型名） |
| `POST /v1/chat/completions` | 核心 | 完整流水线（§3.3），支持 `stream=true`（SSE） |
| `POST /v1/completions` | 可选 | 文本补全透传（MVP 可先不做，默认 501） |
| `GET /healthz` | 健康 | 检查切片库可打开 + 上游可达性（New API 渠道健康检查用） |

请求/响应结构严格遵循 OpenAI 协议；错误响应也走 OpenAI 格式（`{"error": {"message", "type", "code"}}`），保证 New API 与客户端能正确识别。

### 3.3 请求处理流水线（核心）

```
POST /v1/chat/completions {model, messages, stream, ...}
   │
   ├─ 1. 鉴权：校验网关 Key（New API 转发时带的上游 key，见 §4.1）
   ├─ 2. 归一化：提取 (project/scope, 最后一条用户消息 → query, 完整 messages 指纹)
   │        项目 scope 来源：可配置 header（如 x-project）或 New API 渠道固定值
   ├─ 3. L3 查询：查缓存库
   │        · 命中（指纹 + deps/mtime 校验 + RuleGate 通过）
   │        │   ├─ 非流式：直接返回缓存响应（含原始 usage）
   │        │   └─ 流式：按缓存响应重建 SSE 分块回放（§3.4）
   │        · 未命中 → 4
   ├─ 4. L2 注入：inject.Injector{Scope,K,Budget,Zones}.Build(query)
   │        有命中 → 注入块拼入请求（system 提示末尾，字节稳定）
   ├─ 5. 上游转发：模型映射（§3.8 适配层）→ 调用上游（超时/重试由 New API 与网关双层兜底）
   ├─ 6. 响应：透传 content + usage；L3 候选判定（§3.5）
   └─ 7. 异步写记忆：请求/响应旁路 → 会话 JSONL → ingest → extract（不阻塞主链路）
```

**关键原则**：
- **缓存永不阻塞主链路**：检索、注入、写库都是本地操作（<10ms 级）；上游失败/超时按 OpenAI 错误格式返回，客户端可重试；
- **L3 命中必须可观测**：响应头或 usage 中标记命中（如 `x-semantix-cache: hit` / `prompt_tokens_details.cached_tokens`），便于 New API 侧计费与用户看到省钱；
- **ablation 开关**：`SEMANTIX_GATEWAY_DISABLE=1` 一键退化为纯透传（风险预案）。

### 3.4 流式（SSE）

- **未命中流式**：网关向上游转发 `stream=true`，把上游 SSE 事件**逐块透传**；上游若未在末块返回 usage，网关在 `[DONE]` 前补一个含注入块 usage 统计的末块；保持 `[DONE]` 终止；
- **命中流式**：缓存存的是完整响应；网关按 OpenAI SSE 协议把缓存内容切成 `choices[0].delta.content` 分块回放（每块 ≤ 4KB 或按原缓存分块）。注意这是**重建流**：`id`/`created`、工具调用首块（`index`/`id` 必须在第一个 delta）、`finish_reason` 位置、token 边界都与上游原始流不同；M1 落地时用真实客户端回归验证（见 §8）；
- **字节稳定注意**：透传不重排、不重写上游事件，避免破坏客户端兼容性；注入块只在请求侧生效，不触碰响应流。

#### 3.4.1 流式场景不要依赖 `x-semantix-cache` 响应头（Issue #242）

**结论先行：流式请求判断是否命中，以网关 usage 日志为准，不要读响应头。**

网关在流式路径**确实设置了** `x-semantix-cache`（`gateway/pipeline.go` `streamThrough` /
`streamThroughAnthropic` / `replyFromCache`）。直连网关可以看到：

```
$ curl -D - http://semantix-gateway:8080/v1/chat/completions -d '{…,"stream":true}'
HTTP/1.1 200 OK
Content-Type: text/event-stream
X-Semantix-Cache: miss
```

但同一个请求经 New API 中继后，这个头**不见了**（GW4 验收 §8.2 实测，New API v1.0.0-rc.25）。
**这不是网关缺陷，是 New API 中继 SSE 的行为**，两条路径的差异在其源码里可以直接读出
（仓库 `QuantumNous/new-api`，v1.0.0-rc.25 与 main 一致）：

| 路径 | 代码 | 对上游自定义响应头的处理 |
|---|---|---|
| 非流式 | `service.IOCopyBytesGracefully`（`service/http.go`） | 遍历上游全部响应头，只排除 `Content-Length` 与 request-id → **自定义头透传**，所以非流式能看到 `x-semantix-cache` |
| 流式 | `relay/helper.StreamScannerHandler` → `copyCodexSSEHeaders`（`relay/helper/stream_scanner.go`） | 只复制**硬编码白名单** `X-Reasoning-Included` / `X-Codex-Turn-State`，随后 `SetEventStreamHeaders` 写自己的 SSE 头 → **其余上游头全部丢弃** |

即：**New API 没有「透传自定义响应头」的配置项**，白名单写死在代码里；要放行 `x-semantix-cache`
只能给上游提 PR 把白名单做成可配置。在那之前，客户端侧的正确做法是：

- **权威口径**：网关 usage 日志（`kernel/usage`，`l3_reuse:true` 即 L3 命中）。这条日志无论流式
  与否、无论中间过了几层中继都存在；
- **客户端侧启发式**（非协议保证，仅供无法读日志的场景参考）：L3 命中的回放流末块 usage 满足
  `completion_tokens=0` 且 `prompt_tokens_details.cached_tokens>0`，且 `id` 为 `chatcmpl-<sliceID>`；
- **已否决的方案**：在首个 SSE chunk 里塞命中标记——那会改动 wire 格式，与本节「透传不重排、
  不重写上游事件」的约束直接冲突，为一个观测性便利去动协议不划算。

### 3.5 L3 语义缓存设计

```
缓存键 = hash(scope | 归一化 query | 模型名 | deps 指纹 | messages 上下文指纹)
```

- **messages 上下文指纹**：完整 messages（含 system/历史）的归一化哈希——防止不同会话历史/系统提示下**同 query 互相复用**（跨上下文复用与信息泄漏）；MVP 用全量指纹，宽松模式（仅前缀参与）待 M1 后评估；
- **归一化 query**：最后一条用户消息去空白/规范化（复用 `sanitize` 纪律）；
- **deps 指纹**：复用 `fingerprint.Capture/Verify`（path → sha256）+ mtime 快速失败（`SliceMeta.Mtimes`）——**文件一变缓存即失效**（issue-08 已验收的机制）；网关条目的 deps root 由配置提供（如项目根目录），缺失文件一律视为已变更 → 失效；**网关生成且 deps 为空的结果默认不进入 L3**（`l3_safe=false`，需显式配置才启用）——否则指纹/RuleGate 验证形同虚设（空 deps 时 Chain 会跳过指纹阶段）；
- **验证**：`judge.RuleGate.Chain`（grey zone 规则，Krites §3.1）；grey 区候选可配置 `SEMANTIX_JUDGE_API_KEY` 走 LLM judge 确认；**judge 一律异步/离主链路执行**（不阻塞响应，保持 <100ms）；
- **提升与级联失效**：`promote.Store` 存提升条目 + 包级函数 `promote.CascadeInvalidate(store, sourceSliceID, currentContent)`——上游响应内容变化（content 版本号变更）时**级联失效**同一源切片衍生的下游缓存条目；
- **TTL**：缓存条目按模型 vendor 差异化（DeepSeek 24h / DashScope 5m / Anthropic 5m，沿用 `semantix-kvcache-mechanisms.md` 的 vendor-aware 结论）；Kimi/Moonshot 与 GPT 的缓存 TTL **以上游文档确认后配置**（Moonshot 历史上需显式建缓存，勿假设自动生效）；
- **模型名进缓存键**：防止 Claude 的响应被 GPT 复用（跨模型语义相同但行为/风格不同，绝不混用）；
- **只缓存可安全复用结果**：带工具调用副作用的结果默认不入 L3（R-Slice 需 `--l3-safe` 或 deps 指纹非空，见 `SliceMeta.L3Safe`）。

### 3.6 L2 注入设计

- 检索：`inject.Injector`（K=5、Budget=4096、Zones 灰度分类默认开）；
- 注入位置：**system 提示末尾**。原注记「= 前缀尾部，保证注入块之后的历史消息字节稳定 → L1 生效」的推理方向有误：前缀缓存从第一个变化字节起全部失效，注入块逐请求变化时其后整段历史都失配（本地 overlay 实测，见 `gateway-anthropic-cache-p0a.md` 背景）。P0-a 已把 Anthropic 路径的静态 system 与注入块拆成两个 block 并把断点打在静态部分；注入块本身的会话内冻结留待后续阶段；
- 注入块形态沿用内核：`[semantix-reuse] ... [/semantix-reuse]`，**低权威定位**（内容仅供模型参考，不当作指令）；块内 ID 规范序（内核行为）保证字节稳定；
- 注入块不改变客户端可见的 model/messages 语义，仅内部改写后转发。

### 3.7 会话提取（写记忆）

- **旁路落盘**：每个请求/响应对（含工具调用如存在）追加到 `~/.semantix/sessions/<gateway-session-id>.jsonl`（0600），每行 `{role, content, tool_calls}`，与 `ingest.JSONLSource` 格式兼容；
- **提取**：异步执行 `ingest.Pipeline.Run` → 切片入库（P/C/R/T/M）；可复用现有 extract 逻辑（`cmd/semantix/extract.go`）；
- **scope 策略**：默认 `project`（New API 渠道级隔离），可选 `user`（按客户端 key 前缀映射，见 §4.4）；
- **写库失败不影响主链路**：提取是 best-effort，失败仅记日志 + usage 统计。

### 3.8 上游适配层（DeepSeek / Claude / Kimi / GPT）

网关统一对上游发 **OpenAI 兼容协议**，适配差异收敛到配置：

| 上游 | base_url（示例） | 鉴权头 | 需特殊处理 |
|---|---|---|---|
| DeepSeek | `https://api.deepseek.com/v1` | `Authorization: Bearer` | 无需 cache_control（自动前缀缓存），刻意不发 |
| Kimi | `https://api.moonshot.cn/v1` | 同上 | 同 OpenAI 兼容，自动前缀缓存 |
| GPT | `https://api.openai.com/v1` | 同上 | 同 OpenAI 兼容 |
| Claude | `https://api.anthropic.com/v1` | `x-api-key` + `anthropic-version` | ① 需要把 messages 转 Anthropic 格式（或走官方 OpenAI 兼容端点）；② 注入块边界打 `cache_control:{type:"ephemeral"}`（≤2 断点：system 尾 + 最后消息尾） |

> 转换规则与 cache_control 断点的具体设计见 §3.11（M2 上半，Issue #185）；§3.11 落地前
> `vendor="anthropic"` 在配置层被显式拒绝，避免把 Anthropic 流量误发到 OpenAI 式端点。

**模型映射**：`semantix-gateway.toml` 中定义 `upstreams[].model_alias`（New API 侧模型名 ↔ 上游模型名）；New API 渠道的模型名即网关的 model_alias，网关负责换成上游真实模型名。

**超时与重试**：网关给上游设保守超时（connect 10s / 首字节 60s / 总时长继承 New API 配置）；重试逻辑**主要放 New API 渠道层**（New API 有重试/负载均衡），网关只做一次重试兜底（幂等 GET 类；chat 请求重试仅对网络错误）。

### 3.9 配置（semantix-gateway.toml 草案）

```toml
# semantix-gateway.toml
# 注：配置加载器需实现 ${VAR} 环境变量替换与 ~ 路径展开；
#     任何未解析的 ${...} 启动即报错（防把字面量当凭据）。
[server]
addr = ":8080"
gateway_key = "${SEMANTIX_GATEWAY_KEY}"   # New API 渠道转发时携带的 key（env 注入，不入库）

[store]
db = "~/.semantix/gateway.jsonl"          # 切片库 + L3 缓存库（JSONL 单文件，§3.1）
scope = "project"                         # 默认切片作用域

[retrieval]
retriever = "hybrid"                      # bm25 | vector | hybrid（GW6 接线：vector/hybrid 走 kernel/embed HashEmbedder，无需外部 embedding API）
fusion = "weighted"                      # hybrid 融合策略（Issue #274）：weighted（等权/可配权重，默认）| rrf（倒数排名融合）
rrf_k = 60                               # RRF 常数（rrf 模式，默认 60）
bm25_weight = 0.5                        # weighted 模式 BM25 路权重（[0,1]，默认 0.5；显式 0 = 纯向量路）
top_k = 5
budget = 4096                             # L2 注入块字节预算
vector_dim = 256                          # HashEmbedder 维度（<=0 取默认 256；模型级 Embedder 接入后忽略）

[cache]
ttl_seconds = 86400                       # 默认 TTL（vendor 差异化优先）
vendor_ttl = { anthropic = 300 }          # 可选：按 vendor 覆盖 TTL（秒；内置默认 DeepSeek 24h / Anthropic 5m，§3.11.4）
judge_api_key = "${SEMANTIX_JUDGE_API_KEY}"   # 可选：grey 区 LLM judge（GW6 接线，见下三键）
judge_base_url = "https://api.openai.com/v1"  # judge 端点（judge_api_key 非空时必填）
judge_model = "gpt-4o-mini"               # judge 模型（judge_api_key 非空时必填）
judge_protocol = "openai"                 # openai | anthropic（默认 openai）

[ingest]
sessions_dir = "~/.semantix/sessions"     # 会话旁路落盘目录
l3_safe_default = false                   # 无 deps 的结果切片默认不进入 L3

[[upstreams]]                             # 每个 New API 渠道模型对应一段
name = "deepseek-chat"
base_url = "https://api.deepseek.com/v1"
api_key = "${DEEPSEEK_API_KEY}"
model_alias = ["deepseek-chat", "ds-chat"]   # 网关侧别名（New API 渠道模型名，§4.2 第一层）
upstream_model = "deepseek-chat"          # 上游真实模型名（§4.2 映射目标，第三层）
vendor = "deepseek"                       # deepseek | anthropic | openai | moonshot（决定 cache_control/格式处理）
```

### 3.10 安全

- 网关 Key（`SEMANTIX_GATEWAY_KEY`）由 New API 渠道配置注入（渠道密钥字段），**客户端不接触**；网关不校验客户端 key——校验在 New API 层完成（New API 已做用户认证/配额）；
- 网关 key 支持**轮换与按渠道隔离**（每渠道独立 key 更佳）；明确边界：**New API 容器被攻破 = 网关凭据全部暴露**（内网 + 凭据不落盘缓解，不构成绝对隔离）；
- 上游 key 只存环境变量，**不落配置文件与日志**；
- 切片库 0600/0700 权限、原子写、防 symlink（沿用现有安全约定）；
- 敏感内容：默认 `scope=project` 隔离；脱敏接入点预留（`sanitize` 已有，正式版按策略启用）；
- 网关无公网暴露面：只允许 New API 所在网段访问（compose 内网即可，见 §5）。

---

## 3.11 M2 设计节：Claude / Anthropic 适配（Issue #185）

> 本节是 §7 M2「多模型」的上半（Anthropic 一家），2026-08-17 起草，随 Issue #185 落地。
> 覆盖：chat/completions ↔ Anthropic messages 双向转换、cache_control 断点注入、流式 SSE 事件映射、
> TTL 按 vendor 差异化。Kimi / GPT（OpenAI 兼容系）与计费对账属 M2 下半，不在本节。

**适配形态**：客户端（New API 侧）永远只看到 OpenAI 兼容协议；转换只发生在网关 → 上游这一跳。
`vendor="anthropic"` 的上游走 `POST {base_url}/v1/messages`，鉴权头 `x-api-key` + `anthropic-version: 2023-06-01`
（**不发** `Authorization`）。转换层独立文件 `gateway/anthropic.go`，不触碰 OpenAI 直通路径
（非 anthropic vendor 的转发、透传、流式逻辑一律不变）。

### 3.11.1 请求转换（chat/completions → /v1/messages）

| OpenAI chat/completions | Anthropic /v1/messages | 规则 |
|---|---|---|
| `model`（客户端别名） | `model`（upstream_model） | 与 OpenAI 路径相同的三层模型映射（§4.2） |
| `messages[].role=system` | 顶层 `system` | **system 提升**：全部 system 文本以 `\n\n` 合并到顶层；有注入块时升级为 block 数组 `[{type:"text",text,cache_control}]` |
| `messages[].role=user` | `user`，content 为 block 数组 | 字符串 → `{type:"text"}`；part 数组：text → text block、image_url → image block（`data:` URI 转 base64 source，其余 url source） |
| `messages[].role=assistant`（含 tool_calls） | `assistant`，content 含 text + `{type:"tool_use"}` | `tool_calls[].function.arguments`（JSON 字符串）→ `input`（object，解析失败置 `{}`） |
| `messages[].role=tool` | 并入相邻 user 消息的 `{type:"tool_result",tool_use_id,content}` | Anthropic 无 tool role；连续 tool_result 合并进同一条 user 消息 |
| 相邻同 role 消息 | 合并 | Anthropic 强制 user/assistant 交替，OpenAI 允许连续同角色 |
| `max_tokens` / `max_completion_tokens` | `max_tokens`（**必填**） | OpenAI 未传时默认 4096 |
| `temperature` / `top_p` / `stream` | 同名透传 | — |
| `stop` | `stop_sequences` | string 或数组统一归一为数组 |
| `tools[].function` | `tools[]{name,description,input_schema}` | `function.parameters` → `input_schema` |
| `tool_choice` | `tool_choice` | auto→auto、none→none、required→any、`{function:{name}}`→`{type:"tool",name}` |

**cache_control 断点（P0-a 修订，规格见 [`gateway-anthropic-cache-p0a.md`](gateway-anthropic-cache-p0a.md)）**：

- 每个上游可配 `cache_breakpoints = "always" | "l2_only" | "off"`（默认 `always`；`strip_cache_control = true` 等价于 `off`）；
- **BP1 静态前缀**：合并后的静态 system 作为第一个 text block 并带 `cache_control:{type:"ephemeral"}`；**注入块是独立的第二个 system block，放在 BP1 之后且不带标记**，因此注入块逐请求变化时只使自身失配，tools + 静态 system 仍命中。无 system 时 BP1 退到最后一个 tool 定义（tools 位于缓存前缀最前端）；注入块本身永不承载 BP1；
- **BP2 会话尾**：最后一条消息中最后一个可打标记的 block（text / image / tool_use / **tool_result**；跳过 thinking）。tool loop 末尾通常是 tool_result，旧规则「只打 text」使其没有会话断点；
- 与 `l2_only` 相比，`always` 让未注入切片的请求也进入缓存（旧实现此类请求不带任何断点、完全不缓存）；
- 每请求最多 2 个标记（API 上限 4），统一默认 5 分钟 TTL；
- Anthropic 分支在转换前同样执行 `sanitizeOutgoing`（归因头剥离、tools 按名排序），此前只有 OpenAI 透传路径执行；
- `tool_result` 的内容字段为 `content`（此前误写为 `input`，Messages API 无该键）。

> 历史（2026-08-17 至 P0-a 前）：只在注入了切片时打断点；断点 ① 打在「system + 注入块」拼接后的末尾，断点 ② 只打 text block。

### 3.11.2 响应转换（/v1/messages → chat.completion）

| Anthropic | OpenAI | 规则 |
|---|---|---|
| `content[].type=text` | `choices[0].message.content` | 全部 text 块按序拼接 |
| `content[].type=tool_use` | `choices[0].message.tool_calls[]` | `{id, type:"function", function:{name, arguments: JSON.stringify(input)}}` |
| `stop_reason` | `finish_reason` | end_turn / stop_sequence → stop、max_tokens → length、tool_use → tool_calls |
| `usage.input_tokens` | `usage.prompt_tokens` | — |
| `usage.output_tokens` | `usage.completion_tokens` | — |
| `usage.cache_read_input_tokens` | `usage.prompt_tokens_details.cached_tokens` | L3/计费口径：注入前缀按缓存价（§4.3 合成 usage 同源） |
| `id` / `model` | `id` / `model`（客户端别名） | `created` 由网关生成 |

### 3.11.3 流式 SSE 事件映射（Anthropic → OpenAI）

| Anthropic 事件 | OpenAI 事件 | 规则 |
|---|---|---|
| `message_start` | `delta:{role:"assistant"}` 首块 | 客户端识别角色 |
| `content_block_start`（tool_use） | `delta:{tool_calls:[{index,id,type:"function",function:{name,arguments:""}}]}` | tool index 独立编号 |
| `content_block_delta`（text_delta） | `delta:{content}` | 文本增量原样透传 |
| `content_block_delta`（input_json_delta） | `delta:{tool_calls:[{index,function:{arguments}}]}` | arguments 增量透传 |
| `message_delta`（stop_reason） | `delta:{}, finish_reason` | 映射同非流式 |
| `message_stop` | `data: [DONE]` | 终止 |
| `event: ping` / 非 data 行 / thinking 块 | 丢弃 | 客户端只见 OpenAI 形状 |
| 异常断流（无 message_stop） | 网关补 `finish_reason` + `[DONE]` | 客户端不挂死（同 §3.4 保证） |

流式响应侧仍只写请求 turns 进旁路（assistant 内容解析属既有债务，与 OpenAI 流式路径一致）。

### 3.11.4 TTL 按 vendor 差异化

- 新增 `[cache] vendor_ttl`（`map[string]int64`，可选）；`Config.TTLFor(vendor)` 解析顺序：显式 `vendor_ttl` > 内置默认 > 通用 `ttl_seconds`；
- 内置默认（§3.5）：DeepSeek 24h、**Anthropic 5m**；Kimi / GPT 用通用 `ttl_seconds`（上游文档确认后再配）；
- L3 命中判定 `cacheFresh(slice, vendor)` 按上游 vendor 取 TTL（`l3Eligible` 传入 `up.Vendor`）。

### 3.11.5 验收（e2e，假 Anthropic 上游）

- 非流式全链路：客户端收到 OpenAI 格式响应（content / usage 映射正确），假上游收到 `x-api-key` + `anthropic-version`、`/v1/messages` 路径、system 提升后的 body，且**无** `Authorization` 头；
- 流式全链路：客户端收到 OpenAI SSE（role 首块 → content 分块 → finish_reason → [DONE]），无 Anthropic 原始事件泄漏；
- L2 注入断言：注入块出现在 system 尾且带 `cache_control:{type:"ephemeral"}`；
- `go test ./gateway/... -race` 全绿。

---

## 4. New API 接入配置

### 4.1 渠道配置

New API 管理后台 → 渠道 → 新建渠道：

| 字段 | 值 |
|---|---|
| 类型 | **自定义**（Custom / OpenAI-API 兼容，按 New API 版本选择） |
| 名称 | semantix-gateway |
| 代理地址 (Base URL) | `http://semantix-gateway:8080`（compose 内网名）或公网地址（若网关单独部署） |
| 密钥 (Key) | `SEMANTIX_GATEWAY_KEY` 的值（New API 转发请求时作为 `Authorization: Bearer` 带给网关） |
| 模型 | 该渠道要路由的模型（如 `deepseek-chat`、`claude-sonnet-4`、`kimi-k2`、`gpt-4o`） |
| 模型映射 | 可选：New API 展示名 → 上游真实名（不映射则同名透传） |
| 启用 | 勾选 |

> 一个渠道 = 一个上游服务 + 一组模型。可**每个上游模型建一个渠道**（deepseek / claude / kimi / gpt 各一个渠道，均指向同一网关地址），便于 New API 侧按渠道做分组、权重与禁用。

### 4.2 模型映射示例

```
New API 侧模型名（客户端可见）  →  网关 model_alias  →  上游真实模型
deepseek-chat                   →  deepseek-chat      →  deepseek-chat
claude-sonnet                   →  claude-sonnet      →  claude-sonnet-4-20250514（示例）
kimi-k2                         →  kimi-k2            →  moonshot-v1-128k（示例）
gpt-4o                          →  gpt-4o             →  gpt-4o
```

### 4.3 计费与配额

- New API 按渠道模型**定价倍率**计费（按上游单价配置模型价格）；
- 网关透传上游 usage，命中缓存时：
  - **L3 命中返回合成 usage**：`completion_tokens=0`、`prompt_tokens=缓存前缀量`并标记 `prompt_tokens_details.cached_tokens` 全部为缓存——否则缓存里的 completion tokens 仍会按全价计费，「≈0 成本」不成立；
  - New API 侧给网关渠道配**近零价格倍率**（如 0.001x）落 L3 命中的费用——**这是商业决策，见 §9 待决策项 D1**；
- 网关 `usage` 记录（`kernel/usage.Recorder/Summarize`）用于 Semantix 侧成本统计与验证报告，与 New API 侧计费对账用。

**灰区 judge 的成本口径（Issue #242）**：grey 区 judge 是网关**自己发起的另一次上游模型调用**，
不走 New API 渠道，因此 New API 侧永远看不到它，只能由网关自己记账。落地方式是「日志 + usage 字段」两份：

- **结构化日志**（`gateway: l3 judge slice=… rel=… verdict=… called=… fail_closed=… latency_ms=… reason=… err=…`）
  给排障用：一条 grep 就能回答「这次为什么没命中」，且不依赖是否配了 usage 日志；
- **usage 事件字段** `judge[]`（`kernel/usage.JudgeDecision`）给算账用：`usage.Summarize` 是本项目
  唯一把日志变成节省率的地方，**一条日志行是没法被求和的**，所以要进成本口径就必须是这里的字段。

`verdict` 四态严格区分「判了」和「没判成」：`approved` / `declined` 是真裁决；`fail_closed` 是
调用失败或超时后的保守拒绝（**GW4 §8.3 无法区分的就是这一态**）；`skipped` 是指纹门在 judge
之前就拒了、根本没发生调用。只有 `called=true` 的决策才计入 `Summary.JudgeCalls` 与
`JudgePromptTokens`（`bytes/4` 估算，同 §0.3 的 estimator 口径）。

`Summary.SavingsUSD` / `SavingsRate` **仍是不含 judge 开销的毛口径**——judge 用的是独立配置的模型
（`[cache] judge_model`），单价不在这份日志里，硬折进去只会得到一个用错价格的数。净口径由调用方
自己算：`SavingsUSD - Summary.JudgeCostUSD(judge 模型输入单价)`。GW4 报告 §6 的 52.7% 是毛口径，
这个字段补上后该缺口才**可测**（此前是完全测不出来）。

### 4.4 多用户 / 多项目隔离（可选）

- 方案 A（简单）：整个网关一个 scope=project，所有用户共享切片库（适合个人/小团队）；
- 方案 B（多项目）：New API 渠道按项目拆分（每项目一个渠道 + 固定 `x-project` header 由渠道配置注入），网关按 header 选 scope 库；
- 方案 C（用户级）：客户端 key 前缀映射到 scope=user 库。**注意：网关看不到客户端 key**（New API 只转发渠道 key，见 §4.1），需 New API 侧透传用户身份（如按用户拆渠道 + 固定 header，或 New API 支持的用户标识转发）才可行。MVP 用 A，需要时上 B。

---

## 5. 部署方案（推荐：单机 Docker Compose）

```
docker-compose.yml
├─ new-api          镜像: quantumnew/new-api  端口 3000 → 公网（或经 nginx）
│     volumes: ./data/new-api:/data            （SQLite 持久化）
├─ semantix-gateway 镜像: <semantix 构建产物镜像（见下）>  端口 8080（仅内网）
│     env: SEMANTIX_GATEWAY_KEY / DEEPSEEK_API_KEY / ANTHROPIC_API_KEY / MOONSHOT_API_KEY / OPENAI_API_KEY / SEMANTIX_JUDGE_API_KEY
│     volumes: ./data/semantix:/root/.semantix  （切片库 + 会话旁路持久化）
└─ (可选) nginx：TLS 终结 + /v1/* 反代到 new-api:3000
```

要点：
- **new-api 用 SQLite**（单机够用，零外部依赖）；规模上来再迁 MySQL；
- 网关镜像：`go build ./cmd/semantix-gateway` → scratch/distroless 单二进制镜像（**零第三方依赖**——`go.mod` 无外部包，文件存储为 JSONL；镜像 <10MB）；
- 网络：`semantix-gateway:8080` **只暴露给 new-api 容器**（compose 内部 network），不发布到宿主机；
- 健康检查：New API 渠道可用性检测可配置为 `GET /healthz`（或渠道自检开关）；
- 日志：stdout JSON 日志 + usage 汇总（`semantix usage` 可对账）；建议接 Loki/Promtail 或仅文件轮转。

### 5.1 非 Docker 备选

- 二进制直跑：`semantix-gateway` 与 `new-api` 同机/异机，`base_url` 填实际地址；用 systemd/launchd 托管 + 环境变量注入 key。

---

## 6. 数据流时序

### 6.1 L3 命中（零上游调用）

```
客户端 ──POST /v1/chat/completions──► New API（认证/计费）──► 网关
   │                                    ▲                        │
   │   200 {content, usage}  ◄──────────┘  指纹+RuleGate 校验通过      │
   │   headers: x-semantix-cache: hit     直接返回缓存响应        │
   ◄──────────── 响应 <100ms，无上游费用 ────────────────────────┘
```

### 6.2 未命中（L2 注入 + 上游）

```
客户端 ──► New API ──► 网关
                        ├─ inject.Build(query) → 注入块（字节稳定）
                        ├─ 转发上游（OpenAI 兼容协议 / Claude 转格式+cache_control）
                        ├─ 上游 200 / SSE 流
                        ├─ 透传响应 + usage（cached_tokens 标记注入前缀）
                        └─ 异步：旁路写会话 JSONL → ingest 提取 → L3 候选入库
客户端 ◄── 响应 ◄──────────────────────────────────────────────
```

---

## 7. 实施路线与验收标准

| 阶段 | 内容 | 工期 | Gate（验收） |
|---|---|---|---|
| **M0 网关 MVP** | `cmd/semantix-gateway`：OpenAI 兼容层（chat/completions + SSE 透传 + /healthz）+ 鉴权 + 上游 DeepSeek 适配 + 会话旁路入库 | 1–2 周 | 任意 OpenAI 兼容客户端 → New API → 网关 → DeepSeek 全链路跑通；会话入库后 `semantix search` 可检索到切片 |
| **M1 缓存闭环** | L2 注入 + L3 缓存（指纹/RuleGate/promote/TTL）+ 流式命中回放 + usage 标记 | +1–2 周 | 重复任务第二次命中 `x-semantix-cache: hit` 且零上游调用；合成演示成本节省 ≥30%（复用 m0-cost-comparison 脚本） |
| **M2 多模型 + 上线** | Claude（格式转换 + cache_control，**上半已落地，见 §3.11**）/ Kimi / GPT 适配 + 模型映射表 + 计费对账 + 健康监控 | +1 周 | 四家模型渠道全通；命中率/节省率有周报数字；New API 侧对账一致 |

**M0 为决策门**：若网关形态下「请求级注入」收益显著低于预期（重复率低的场景），及时转向纯 L3 缓存策略或按会话聚合注入，控制投入在 2 周内。

---

## 8. 风险与边界

| 风险 | 缓解 |
|---|---|
| L3 误命中（文件变了还复用旧结果） | deps 指纹 + mtime 快速失败 + RuleGate grey zone；副作用结果默认不入 L3（`l3_safe=false`）；**网关生成且 deps 为空的条目默认不入 L3**（§3.5） |
| 注入块改变模型行为（低质量参考干扰） | 注入块低权威定位 + ablation 开关一键关闭 + 灰度分类（Zones）只注入明确可复用的切片 |
| 流式命中回放兼容性 | MVP 先做非流式 L3 命中 + 流式透传；流式命中回放为 M1 项，用真实客户端回归 |
| 敏感数据进切片库 | scope 隔离（project/user）+ 0600 权限 + 脱敏接入点预留；`scope=user` 方案 C 为可选增强 |
| 缓存键跨模型混用 | 模型名进缓存键（§3.5），绝不跨模型复用 |
| L3 跨上下文复用/泄漏（同 query 不同历史） | messages 上下文指纹进缓存键（§3.5）；共享 scope 仅限可信用户（§4.4） |
| L3 缓存无界增长 / JSONL 全量重写放大 | 条目上限 + TTL 惰性清理 + 异步批量写（D4，§9） |
| 共享 scope 切片注入投毒（对抗指令） | 方案 A 仅限可信用户；多租户用方案 B/C 隔离；judge 内容消毒 |
| L3 命中计费不落地（completion 仍全价） | §4.3 合成 usage + New API 近零倍率（D1，§9） |
| 网关成为单点/性能瓶颈 | 纯本地操作 <10ms；`/healthz` 探活；New API 侧可配多网关渠道做负载均衡（同一切片库需共享 volume） |
| 上游 key 泄露 | 仅环境变量 + 内网暴露 + 日志脱敏 |
| 计费口径不一致（网关 vs New API） | usage 统一以网关透传/回填为准，`kernel/usage` 周对账 |

---

## 9. 待决策项（D1–D4）

| 编号 | 决策 | 选项 | 建议 |
|---|---|---|---|
| **D1** | L3 缓存命中是否对客户端降价 | ① New API 模型价格倍率下调（如命中 0.25x）② 不降价，省钱体现为服务方利润 ③ 仅统计展示不调价 | 个人使用建议 ①（钱省在自己账户）；商业化看②③——**上线前定** |
| **D2** | 网关是否支持 `/v1/embeddings` 透传 | 支持 / 不支持 | MVP 不支持；有向量检索/embedding 需求再加（透传很简单） |
| **D3** | 多用户 scope 方案 | A 单库 / B 多项目渠道 / C 用户级库 | MVP 用 A，B 按需 |
| **D4** | 网关缓存库与切片库分库还是合库 | 分库 / 合库（同一 JSONL Store） | 合库（同一 Store，L3 缓存作为特殊 Slice 类型）+ 条目上限与 TTL 惰性清理，防无界增长 |

---

## 10. 结论

Semantix Gateway 是 Semantix 从「agent kernel / CLI」走向「**API 网关形态**」的关键一步：
把已验证的 79.8% 成本节省机制（L2 注入 + L3 复用 + L1 字节稳定）搬到 New API 后面，**零侵入任何客户端与 harness**，
一条命令部署（`go build ./cmd/semantix-gateway`），天然服务 DeepSeek / Claude / Kimi / GPT 四家模型
（Claude 经 §3.11 的格式转换 + cache_control 断点接入，其余三家同为 OpenAI 兼容协议）。
M0 决策门控制投入在 2 周内，收益不成立可随时退化为纯透传。
