# Gateway Anthropic 路径：tool_result 修正与 prompt-cache 断点（P0-a）

## 状态

2026-09-27 用户批准三项决定（加开关、纳入 system 拆分、做真实上游验证），已按本规格实现（`gateway/anthropic.go`、`gateway/pipeline.go`、`gateway/config.go`；测试见 `gateway/anthropic_breakpoints_test.go`、`gateway/anthropic_live_test.go`）。配置键最终命名为每上游的 `cache_breakpoints`（见 §设计 3）。真实上游验证需要凭证，见 §验证。

## 背景

Gateway 对 `vendor="anthropic"` 的上游做 OpenAI → Anthropic Messages 转换（`gateway/anthropic.go`，设计见 `newapi-gateway-design.md` §0.5 / §3.11）。代码审读和本地 overlay 探测（测试放在本地未入库的 `/lab/`，仓库未改动）确认了四个问题。前两个与缓存无关，但会影响正确性：

1. **`tool_result` 内容序列化在 `input` 字段。**
   - 位置：`anthropicBlock` 只有 `Input any \`json:"input"\``，注释写着 “tool_use.input / tool_result.content both ride on Input”（`anthropic.go:73-74`）；`splitSystem` 构造 tool_result 时把内容放进 `Input`（`anthropic.go:239`）。
   - 线上字节是 `{"type":"tool_result","input":"<输出>","tool_use_id":"..."}`，而 Messages API 的 `tool_result` 形状是 `tool_use_id / content / is_error / cache_control`。
   - 后果：严格校验的上游会拒收；宽松的上游会丢掉工具输出。
   - 现有单测 `TestToAnthropicRequest…`（`anthropic_test.go:104`）只断言 Go 结构体字段，没有断言序列化后的 JSON，所以一直没发现。
   - 未经真实 Anthropic API 验证。
2. **Anthropic 分支跳过了 `sanitizeOutgoing`。**
   - `sanitizeOutgoing` 负责剥离计费/归因头、tools 按名排序、按上游剥离 `cache_control`，但只在 OpenAI 透传的 `rewriteOutgoing` 里调用（`pipeline.go:199-205`）；Anthropic 分支直接调用 `toAnthropicRequest`（`pipeline.go:120-131`）。
   - 后果：Anthropic 路径上 tools 顺序随客户端而变，tools 又位于缓存前缀最前端，顺序一变整条前缀失配。
3. **只有注入了切片的请求才打 `cache_control`。**
   - `hasBlock` 为假时（未注入、或 gateway 关闭 L2）请求里一个断点都没有（`anthropic.go:150-203`）。Anthropic 缓存需要显式开启，这类请求完全不缓存。
4. **会话尾部断点只打在 text 块上。**
   - `markLastTextBlock` 只标 `text` 块，注释称 “Anthropic only allows it on text/tool_use blocks”（`anthropic.go:417-426`），这与文档不符：`cache_control` 可以放在任意 content block（text、image、tool_use、tool_result、document）上。
   - 后果：tool loop 中最后一条消息通常只有 `tool_result`，于是没有会话断点，每一轮都只能复用到 system。

另外，现有的 system 断点打在 “system + L2 块” 拼接后的末尾。L2 块一变，连静态 system 和 tools 也一起失配。

## 目标

- tool_result 按 Messages API 的正确形状发送。
- Anthropic 路径与 OpenAI 路径享有同样的前缀卫生处理（sanitize）。
- 每个请求都有合理的缓存断点：静态前缀（tools + 静态 system）单独一个断点，会话尾部一个断点。有没有 L2、最后一块是什么类型，都不影响这一点。
- L2 块变化时，至少静态前缀仍能命中缓存。

## 非目标（留给后续阶段）

- **不改变 L2 块本身**：内容、位置（仍在 system 末尾）、检索时机、`score` 字段都不动。块逐请求变化导致的会话级失配，由 P1（注入账本、块移入历史）解决。
- **不透传客户端 `cache_control`**：OpenAI 侧没有标准写法，system 还会被 `splitSystem` 拼成一个字符串，需要单独设计约定。
- 不新增 Anthropic 原生 `/v1/messages` 入口，不处理 signed thinking。
- 不修改 usage 记账：`cache_creation_input_tokens` 目前被并入 Prompt，单独计量另行处理。

## 设计

### 1. tool_result 形状

- `anthropicBlock` 新增字段 `Content any \`json:"content,omitempty"\``，只供 `tool_result` 使用。`Input` 只保留给 `tool_use`，并更新注释。
- `splitSystem` 的 `case "tool"` 改为 `Content: textParts(m.Content)`。
- 内容为空时，`omitempty` 会省略 `content` 字段；API 允许不带 `content` 的 tool_result，保持最小改动。
- 不引入 `is_error`：OpenAI 的 tool 消息没有对应字段。

### 2. Anthropic 分支执行 sanitize

在 `pipeline.go` 的 Anthropic 分支中，调用 `toAnthropicRequest` 之前，先对 OpenAI 请求体执行与 `rewriteOutgoing` 相同的 `sanitizeOutgoing`：

- 解码为 `map[string]any` → 执行 sanitize → 重新编码 → 交给 `toAnthropicRequest`。
- 抽出一个小函数 `sanitizeBody(body, up) []byte`，两条路径共用；解析失败时原样返回（与现有 fail-open 行为一致）。
- 剥离归因头只作用于第一条 system 消息的开头，与 OpenAI 路径的语义完全相同。
- tools 按名排序后，`mapTools` 保持该顺序。

### 3. 断点策略

在 `toAnthropicRequest` 中重写断点逻辑，规则如下：

| 断点 | 位置 | 条件 |
|---|---|---|
| **BP1 静态前缀** | 静态 system 的最后一个 text block。L2 块作为**单独的第二个 system text block**，放在 BP1 之后 | system 非空时总是打 |
| BP1′ 回退 | 没有静态 system 时，打在最后一个 tool 定义上（`anthropicTool` 新增 `CacheControl` 字段） | 无 system 且有 tools |
| **BP2 会话尾部** | 最后一条消息中最后一个“可缓存”块：text、image、tool_use、tool_result 均可；`thinking` 块不可缓存，从末尾往前找到第一个可缓存块 | 有消息时总是打 |

- 请求中总断点数最多 2 个，远低于 API 上限 4。
- TTL 统一使用默认 5 分钟（`{"type":"ephemeral"}`），不引入 1 小时 TTL。
- **每上游开关** `cache_breakpoints = "always" | "l2_only" | "off"`（`UpstreamConfig.CacheBreakpoints`，仅 `vendor="anthropic"` 有效，配置加载时校验取值）：`always`（默认，空值即默认）按上表打；`l2_only` 只在注入了切片的请求上打（P0-a 之前的触发条件，但用新位置）；`off` 不打。`strip_cache_control = true` 等价于 `off`。
- 有 L2 块但无静态 system 时，system 只有 L2 块一个 block，不在它上面打 BP1（因为它每次都可能变），改走 BP1′（若有 tools）。
- 前缀长度低于模型最小可缓存长度（512–4096 token，视模型而定）时 API 会静默不缓存，不报错，无需特殊处理。

system 字段的形状变化：

- 之前：无 L2 块时为字符串；有 L2 块时为 1 个 block（system 与 L2 拼接）。
- 之后：只要打了 BP1（或有 L2 块），就是 block 数组：`[静态 system (cache_control), L2 块]` 或 `[静态 system (cache_control)]`。`StripCacheControl` 且无 L2 时仍为字符串。

效果：

- L2 块变化时，tools + 静态 system 仍命中 BP1；L2 块不变或没有 L2 时，会话尾部断点通过 20 个位置的回看命中上一请求写入的条目。
- 没有 L2 的请求也会开始缓存。

### 4. 注释与规格同步

- 修正 `markLastTextBlock`（改名为 `markLastCacheableBlock`）的错误注释，以及 `rewriteOutgoing` 注释中 “first system message” 的笔误（代码实际追加到最后一条字符串 system 消息）。
- 更新 `newapi-gateway-design.md`：
  - §3.11.1 的断点规则改为本节表格；
  - §3.6 注明 “system 末尾放注入块保证后续历史稳定” 的推理在块逐请求变化时不成立，由 P1 处理；
  - 在 §0.2 记录 “改变了方案”。

## 风险与取舍

| 风险 | 说明 | 处理 |
|---|---|---|
| **一次性请求多付写入溢价** | 以前无 L2 的请求不写缓存；现在每次都打断点，前缀若之后不再复用，要按 1.25× 付写入费（只作用于可缓存部分） | 多轮会话和 tool loop 里第二个请求起就回本（5 分钟 TTL 两次请求即打平）。已加每上游开关 `cache_breakpoints`（默认 `always`） |
| 第三方 Anthropic 兼容端点不接受 tool_result / tools 上的 `cache_control` | GLM 规格记录两套 GLM 栈“容忍 cache_control 字段”，但没测过 tool_result 上的 | 已有 `strip_cache_control` 按上游关闭；上线前在目标上游做一次真实请求验证 |
| system 由字符串变为 block 数组 | 语义等价；缓存前缀字节会变一次（一次性失配） | 可接受，部署后第一个请求重建缓存 |
| sanitize 改变 Anthropic 路径上的 tools 顺序 | 模型看到的 tools 顺序改变 | 与 OpenAI 路径保持一致；排序是稳定排序 |
| tool_result 字段修正改变线上行为 | 若某个兼容端点曾“依赖” `input` 字段 | 可能性很低；以 Messages API 规范为准，发布说明中注明 |

## 验证

单元测试（`gateway/anthropic_test.go`、`gateway/sanitize_test.go`），全部断言**序列化后的 JSON**，而不是 Go 结构体：

1. tool loop 转换后：`tool_result` 带 `content`、不带 `input`；`tool_use` 仍带 `input`。
2. 断点表覆盖以下情形：
   - 无 L2 + 有 system；
   - 有 L2 + 有 system（system 为两个 block，BP1 在第一个）；
   - 有 L2 + 无 system + 有 tools（BP1′ 在最后一个 tool）；
   - 无 system + 无 tools；
   - 最后一块为 tool_result / image；
   - 最后一条 assistant 消息以 thinking 结尾（往前回退）；
   - `StripCacheControl`（零断点）。

   以上每种都断言断点数 ≤ 4。
3. Anthropic 分支执行 sanitize：tools 乱序输入 → 线上已排序；system 头部归因行被剥离。
4. 修改现有的 `TestToAnthropicRequestNoBlockNoBreakpoints`：行为有意改变，改名并断言新规则。
5. 前缀稳定性回归：同一会话连续两个请求，L2 块不同 → 两个请求在 tools + 静态 system 部分逐字节相同；L2 块相同 → 前一请求是后一请求的字节前缀（除断点标记外）。
6. `go test ./gateway/... ./kernel/...`、`go vet` 全绿。

真实 API 验证：`gateway/anthropic_live_test.go` `TestLiveAnthropicPromptCache`，默认跳过；设置环境变量后运行：

```bash
SEMANTIX_LIVE_ANTHROPIC_API_KEY=... \
SEMANTIX_LIVE_ANTHROPIC_BASE_URL=https://api.anthropic.com/v1 \
SEMANTIX_LIVE_ANTHROPIC_MODEL=claude-sonnet-4-6 \
go test ./gateway/ -run TestLiveAnthropicPromptCache -v -count=1
```

（后两个变量可选，默认官方端点与 `claude-sonnet-4-6`。）场景：约 6k token 的静态 system（高于所有当前模型的最小可缓存长度）+ 3 个请求的 tool loop。断言：第 2 个请求的 `cached_tokens > 0`（BP1 写入被读回）；第 3 个请求的 `cached_tokens` 大于第 2 个（BP2 生效）；模型能复述只出现在 tool_result 正文里的 nonce（`content` 字段映射正确）。花费约 3 × 6k 输入 token（大部分按缓存价）。凭证只从环境变量读取，不进配置文件和日志。

## 决定记录（2026-09-27）

1. 加开关：是。实现为每上游 `cache_breakpoints`，默认 `always`。
2. system 拆分纳入 P0-a：是。
3. 真实上游验证：做。以环境变量注入凭证运行 `TestLiveAnthropicPromptCache`；上游与 key 由用户提供。
