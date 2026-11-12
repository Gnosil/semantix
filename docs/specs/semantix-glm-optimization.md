# Spec：Semantix-GLM 优化方案（GLM-5.x 适配的缓存命中与加速）

> 对应 Issue：#233（Spike 周，已完成）；实施时按批次开 issue
> 真源约束：见 §2
> 门禁分级：**本文档 Spec-Exempt（E2 调研/规划）**；所述实施内容跨 kernel 多包 + provider 适配，
> **实施时为 Spec-Required（预判命中 R4/R7）**，本文即其 spec
> **状态（2026-08-20 修订版）**：Spike 周完成（PR #238，报告 `docs/reports/glm-spike-week.md`），
> 按报告 §6 回写清单修订并转正 — **排期：Agile 2 完成后实施**（依赖见 §7）
> **端点战略（2026-08-20 决策）**：**只走云厂商托管端点（腾讯云 MaaS、AtomClub 等），
> 不接智谱官方 API / Coding Plan**。官方价目与条款降为对照参考；一切缓存行为参数
> （TTL/粒度/usage 形态/折扣率）按 per-云厂商标定——报告 §3B 证明这不是防御性设计而是实测硬需求。
> 事实来源：4 路并行检索（2026-08-20）+ Spike 周双网关实测；
> 全部事实带来源与置信度标注，未核实处列入 §8 残留清单

---

## 1. 定位：三个判断在 GLM 环境下的推演

文献调研（[literature-survey-2026-08.md](../reports/literature-survey-2026-08.md) §8）给出三个判断：
①竞争焦点在「复用质量门」；②安全是 SSL 的架构级前提；③进化必须保守。
GLM 环境（API 形态 + Coding Plan 形态）带来的新事实改变了它们的落地权重：

**推演一：GLM 系缓存折扣浅、TTL 短且行为因栈而异 → 价值重心从 L1 向 L2/L3 移动，且质量门+遥测升为主路径。**
官方口径下缓存命中价 ≈ 标价 18.6%–25%（Z.AI $0.26/$1.4；bigmodel ¥2/¥8——均为对照参考），
而 DeepSeek 命中价 ≈ 未命中价 1/30；云厂商托管栈实测（报告 §1/§3A）TTL 仅 (8,12] 分钟（腾讯云）
甚至 ~5 分钟即衰减（AtomClub），且不承诺命中率。在 DeepSeek 下「把前缀保住」就赢了大半；
在 GLM 系下前缀保住只省约 3/4，且保不住 10 分钟以上的间隔——
**完全避免调用（L3）与缩短上下文（L2 注入经济学）的相对价值上升**——
这恰好把判断①的「质量门升级」从技术债提到了主路径。

**推演二：云厂商栈行为互异把「per-provider 标定」从设计原则升级为生存问题。**
报告 §3B 双网关实测：TTL、命中粒度（原子 vs 块级）、usage 字段形态三维度全部因栈而异，
跨栈不变量仅「严格前缀、首 token 敏感、cache_control 容忍」三条。
且前缀污染的后果在云厂商端点**更重**：官方服务端会剥离的 attribution 计费头，
第三方端点不剥离（社区实测 133 倍命中差距，见 §4.1），前缀卫生要靠我们自己。
**接下来对我们最重要的一件事：把「前缀卫生 + 命中遥测 + 复用质量门」做成
provider 无关、按 provider 标定的机制**——这是本方案的主轴。

**推演三：判断②③不因 GLM 改变；judge 经济学改走云厂商最低价档。**
弃用智谱官方免费档后，judge/评估类离线作业改走云厂商最低价 GLM 档
（型号可用性与价目按 §8 残留 1 核对后配置化）；外发前过 sanitize 管线不变，
且**云厂商数据使用条款核对是实施前置**（§8 残留 2）。
Coding Plan 的配额经济学（双限额、非高峰 50% 积分、命中率×4 烧穿）保留为
**外部 harness 用户形态的参考**：gateway/agent-skill 服务的 Claude Code 用户可能自带 Coding Plan 端点，
净化中间件与遥测对他们同样成立，但我们自身的排程/预算决策不再依赖 Coding Plan 窗口。

## 2. 真源约束

| 约束 | 位置 |
|---|---|
| 路线图与 Agile 2 范围 | [docs/Agile路线图.md](../Agile路线图.md)（本方案排在 Agile 2 之后） |
| H2/H3 资源编排契约 | `docs/specs/h2h3-resource-orchestration.md`（PR #181，遥测与指令通道是本方案依赖） |
| 文献调研三判断与七建议 | [docs/reports/literature-survey-2026-08.md](../reports/literature-survey-2026-08.md) §8/§9 |
| Spike 周实测与回写依据 | [docs/reports/glm-spike-week.md](../reports/glm-spike-week.md)（§1–§3B 云厂商栈行为、§4–§5 价目与条款、§6 回写清单） |
| wire 契约 | [docs/events.md](../events.md)——新增遥测字段只增不改 |
| GLM 模型能力事实 | docs.z.ai / docs.bigmodel.cn / HF 模型卡 / vLLM/SGLang 官方 recipes（**价目与端点行为以云厂商实测为准，官方页降为对照**） |

## 3. GLM 事实清单（已核验，置信度标注）

### 3.1 缓存机制与计费 ★ 本方案的经济学地基

| 事实 | 置信度 |
|---|---|
| 缓存为**隐式**：无 cache_control 依赖、无 TTL 配置、无手动清除；官方称「相同或高度相似」，**实测无相似命中——严格前缀**（两栈一致，报告 §3/§3A） | official + **measured** |
| 「细微格式差异可能影响缓存效果」在云厂商栈是**必然清零**（腾讯云全有或全无）或**截断改动点后**（AtomClub 块级）；最佳实践=稳定 system prompt 模板 | official + **measured** |
| TTL：腾讯云 **(8,12] 分钟**原子过期；AtomClub **~5 分钟已剩 28%**（似块级 LRU）；「第三方均值 3–5 分钟」传闻低估——**TTL 必须 per-栈标定，不可作跨栈工程常数** | **measured**（报告 §1/§3A/§3B） |
| usage 字段形态 per-栈 per-路径：OpenAI 风格恒有 `prompt_tokens_details.cached_tokens`（prompt 全量）；Anthropic 风格腾讯云**命中才出** `cache_read_input_tokens`（input 增量、对账 cache_read+input=全量），AtomClub 恒含 cache 字段族 | **measured**（报告 §2/§3A.2） |
| 命中报告天花板 ~97.6%（尾部不足块不计入 cached）；种下 ~30s 内可能不可见；存在前缀级本底丢失（1/8，样本小） | **measured**（报告 §1/§3） |
| **主经济学参数 = per-云厂商价目配置**；腾讯云 MaaS GLM 价目**未核**（§8 残留 1）。对照参考：Z.AI 官方 GLM-5.3/5.2/5.1 = $1.4/$0.26/$4.4（命中 18.6%）；bigmodel 国内 GLM-5.3/5.2 = ¥8/¥2/¥28 **非阶梯**（命中 25%），5.1/5-Turbo/5/4.7 按输入长度阶梯，4.7-Flash 全免费 | official（两站均已亲验，2026-08-20） |
| bigmodel 价目表含「缓存存储（¥/M/小时）」计费维度，现全系**限时免费**——限免结束构成新成本轴（§10）；旧传闻「命中限免、存储 ¥1/M/h」**两列颠倒不实** | official（已亲验） |
| 官方缓存文档「通常为标准价 50%」与定价页实际 18–25% **矛盾**，以定价页为准 | official（已亲验矛盾存在） |
| 对照 DeepSeek：严格从 token 0 前缀匹配、64-token 存储单元、命中价 ≈ 未命中 1/30、峰谷计价 | official |

### 3.2 模型档位（sched tier 映射的原料）

| 事实 | 置信度 |
|---|---|
| GLM-5.3（2026-08-14）：1M ctx / 128K 输出；与 5.2 同基座（约 743B MoE / ~40B 激活），提升全在后训练 | official + reported |
| GLM-5.3 **思考常开不可关**，`reasoning_effort` 三档 low/high/max（默认 max）；GLM-5-Turbo/GLM-5 可 `thinking.type=disabled` | official |
| 5.x 无 air/flash 变体；快档=GLM-5-Turbo（200K ctx）或 4.7 系（FlashX/Flash） | official |
| Z.AI 官方 Claude Code 映射：opus/sonnet→glm-5.2（现自动升 5.3）、haiku→glm-4.7——官方自己的 pro/flash 分层 | official |
| Coding Plan 仅含 5.3 / 5-Turbo / 4.7；**对 5.2/5.1 的请求自动路由到 5.3**（无法钉版本） | official |
| Coding Plan 双限额（5 小时 + 周）；非高峰（周一至五 14:00–18:00 UTC+8 之外）积分 **50%** | official |
| GLM-5.2 权重 MIT 开源（BF16/FP8/MXFP4/NVFP4）；5.3 权重承诺 ≈2026-08-28 开源 | official + reported |

### 3.3 harness 接入的坑（社区实测，多源交叉）

| 坑 | 影响 | 置信度 |
|---|---|---|
| **Claude Code ≥2.1.36 在 system prompt 最前端注入动态计费头**（x-anthropic-billing-header 的 cch 字段），官方服务端剥离、第三方端点不剥离→**从第一个 token 起前缀失配** | 实测关闭后命中 512→68,096 tokens（133×）、单请求 $0.204→$0.021、延迟 17.5s→2.1s；2.1.42 改为会话内固定，跨会话仍破坏 | reported（≥3 源交叉） |
| 前缀污染源清单：attribution 指纹、ISO/Unix 时间戳、UUID、**工具数组顺序不稳定**、MCP server/tool 枚举无序、临时路径、trace_id | 每一项都可从首 token 破坏隐式缓存 | reported |
| **preserved thinking 与缓存的交互**：Coding Plan 端点默认开启 preserved thinking，要求 reasoning_content「完整、未修改、不重排」回传，否则「性能下降且缓存命中率受影响」 | L2 注入层若触碰 reasoning 序列会破坏缓存 | official |
| 多轮 agent 实测（阿里云托管 GLM-5.2，2.5h 会话）：前缀稳定时命中率 **96.2%**，1980 万 token 总费 ¥50、缓存部分仅 ¥1.11 | 证明 GLM 隐式缓存在纪律良好时可达 Claude 原生水平 | reported（单源） |
| **腾讯云 MaaS→GLM-5.3 实测**：严格前缀、全有或全无（中段 25%/82%/单空格差异均清零）；TTL (8,12] min 原子过期；天花板 97.6%；Anthropic 路径命中才出 cache_read 字段（增量语义）；**thinking 参数两路径不互通**（Anthropic 路径强制 `thinking:{type:enabled}`、不收 `reasoning_effort`） | 主力云厂商端点的行为基线 | **measured**（报告 §1–§3） |
| **AtomClub→GLM-5.2 实测**：严格前缀但块级部分命中（中段差异保留改动点前 ~48%）；TTL ~5 min 已剩 28%；usage 恒含 cache 字段族 | 第二云厂商对照：三维度与腾讯云互异 | **measured**（报告 §3A/§3B） |
| OpenRouter 上 GLM-5.2 有 26 家 provider，缓存价与命中行为各异 | 经聚合网关接 GLM 时不能按 Z.AI 价目估算——已被 §3B 双网关实测证实 | reported + measured |

### 3.4 自托管线（gateway/私有化形态）

| 事实 | 置信度 |
|---|---|
| vLLM 官方 recipe 对 GLM-5 **默认启用 automatic prefix caching**；SGLang RadixAttention 默认开启（严格前缀、radix tree） | official |
| MTP 投机解码为官方主推：5.2 草稿 token 3→5，接受长度提升最高 20%；生产案例 prefill=1/decode=3 不对称配置 | official |
| **陷阱**：HiCache/Mooncake 层级 KV 缓存只持久化目标模型 KV，不含 MTP draft KV → 缓存命中后 MTP 接受率 0.98→0.10（TTFT 赚 7.5×、解码吞吐塌） | reported（SGLang issue） |
| 陷阱：`--kv-cache-dtype fp8` 官方警告推理密集任务可能明显掉精度；DSA 稀疏 indexer 在 ≥~325K ctx 有 off-by-one 崩溃 bug；社区 INT4 构建常缺 MTP draft 权重 | official + reported |
| GLM-5.x 自托管 flash 档缺位：最近的开源小模型是 GLM-4.5-Air（106B/12B 激活） | reported |

## 4. 方案设计

### 4.1 P0 · L1 前缀卫生工程（最便宜的巨大收益）

GLM 隐式缓存 + 格式敏感 = L1 字节稳定纪律**直接适用且更加必要**，但纪律边界要从
「注入块锁定」扩大到「整个请求前缀」：

1. **前缀污染审计**：按 §3.3 清单审计自有 harness 的请求组装路径
   （时间戳、会话 id、工具枚举顺序、MCP 列表顺序），逐项固定序或移出前缀。
   自有 harness 无 attribution header，但 **gateway 与 agent-skill 形态服务的是
   Claude Code 等外部 harness**——gateway 加**前缀净化中间件**：剥离/固定
   attribution 计费头（等效 `CLAUDE_CODE_ATTRIBUTION_HEADER=0`）、
   工具数组稳定排序；agent-skill 的 SKILL.md 增加该环境变量的接入指引。
2. **thinking 回传纪律**：preserved thinking 场景下注入器（inject）与 harness 请求组装
   **不得改动/重排 reasoning_content 序列**；L2 注入块位置保持「系统提示后、用户消息前」，
   永不插入 reasoning 块之间。GLM-5.3 思考常开：sched 低延迟路径用「降低思考档」而非关思考，
   且**参数按路径翻译**（实测：OpenAI 风格路径收 `reasoning_effort`；腾讯云 Anthropic 路径
   只收 `thinking:{type:enabled,budget_tokens}`、给 `reasoning_effort` 直接 400——报告 §2）。
3. **模型版本漂移防护**：Coding Plan 下 5.2/5.1 自动升 5.3——kernel 的切片
   `model` 维度按「模型家族+端点」记录而非裸 model id，避免版本漂移污染
   evolve 统计与 L3 命中判定。

#### Manual thinking token limits (Issue #549)

The Anthropic adapter maps `low`, `medium`, `high`, and `max` to budgets of 1024, 4096, 16384, and 32768 tokens.
`max_tokens` takes precedence over `max_completion_tokens` when both contain positive values.

- With no positive output limit, enabled thinking sets the limit to its budget plus 4096. This preserves a 4096-token output allowance at every budget.
- With an explicit positive output limit, the adapter reduces a generated budget to at most `max_tokens - 1`. A limit of 1024 or less returns HTTP 400 locally.
- An explicit `thinking` object overrides `reasoning_effort`. For `type: "enabled"`, its budget must be at least 1024 and below an explicit output limit. The adapter returns HTTP 400 for invalid budgets or malformed thinking values and does not rewrite valid objects. With no output limit, the same default expansion applies.
- `disabled` and `adaptive` objects pass through unchanged. Unknown effort values still use the endpoint default without a thinking parameter.

These rules apply to the Anthropic request conversion. The OpenAI forwarding path is unchanged.

### 4.2 P0 · 命中遥测（一切标定的前提）

1. **usage 解析适配**：**per-provider × per-路径**缓存命中字段适配器（报告 §3B：同栈两路径形态即不同）——
   OpenAI 风格（恒有 `prompt_tokens_details.cached_tokens`、prompt 全量）/
   Anthropic 风格·腾讯云型（命中才出 `cache_read_input_tokens`、input 增量，对账 cache_read+input=全量）/
   Anthropic 风格·AtomClub 型（恒含 cache 字段族）/ DeepSeek（`prompt_cache_hit_tokens`）/
   Anthropic 官方（`cache_read_input_tokens`）归一为统一事件字段（wire 契约**只增**：
   在既有 usage 事件上追加可选字段，同步 docs/events.md）。
2. **命中率面板**：`semantix usage` 增加 per-provider L1 命中率、命中节省金额
   （按 per-云厂商价目配置标定）；`semantix verify` 的回放报告加入命中率回归项。
3. **告警阈值**：命中率 < 85% 时 doctor 提示前缀污染排查；统计口径**排除 turn 首轮**
   （种下 ~30s 内可能不可见，报告 §1 结论 3），分母天花板按 ~97.6% 校准。
4. **端点画像在线估计**（§3B 结论 3）：TTL/粒度画像从遥测数据按端点自动拟合
   （命中率×间隔曲线），不写死在配置——云厂商行为无 SLA 且互异，静态参数必然漂移。

### 4.3 P1 · sched tier 映射与作业排程

tier 映射按**所接云厂商实际提供的 GLM 型号目录**配置化（腾讯云 MaaS 型号目录与价目核对
= §8 残留 1，实施 P1 前完成）。骨架（以官方档位关系为参照，具体型号/价格填云厂商值）：

| tier | 档位骨架 | 依据 |
|---|---|---|
| pro | glm-5.3（思考档 max/high） | 官方 opus/sonnet 位 |
| mid | glm-5-turbo 或同级（可关思考、低延迟） | 低延迟路径 |
| flash | glm-4.7 / 4.7-flashx 或同级 | 官方 haiku 位 |
| **离线档** | 云厂商**最低价可用 GLM 档** | judge/评估作业成本最小化 |

- kernel 离线作业（extract 的 LLM judge、eval_judge、切片共识审计）走云厂商**最低价档**：
  智谱官方免费 flash 档方案**已废弃**（端点战略），judge 成本从「零」改为「最低价档、
  成本进 hit-waste 账本且 usage 可观测」；外发前 sanitize 管线不变，
  **云厂商数据条款核对为实施前置**（§8 残留 2）；
- 非高峰排程降级为**条件性特性**：仅当所接端点存在时段性价差/配额（如用户自带 Coding Plan）
  时启用，sched 预算控制器的时段感知作为可选插件；
- 思考档（reasoning_effort / thinking.budget_tokens，per-路径翻译见 §4.1-2）映射进
  RoundPlan 的 tier 决策维度（现只有模型选择）。

### 4.4 P1 · 复用质量门的 GLM 标定（文献建议 1/4 的落地首发）

- **L3 门控**（文献建议 1）：逐切片自适应阈值 + 复用前门控（查询相似 × 指纹有效 ×
  词面支持）以 GLM 为首发 provider 标定——错误命中的代价参数用**所接云厂商价目**：
  一次误复用省一次输入价，但错误答案的返工成本按 pro 档输出价起算（输出价通常为输入价 3–4 倍）；
- **L2 注入成本入账**（文献建议 4）：注入 token 的真实成本 = 输入价 ×（1−命中率×折扣率），
  其中**输入价与折扣率均为 per-endpoint 配置**（对照参考：Z.AI 折扣 0.814、bigmodel 0.75；
  云厂商值待 §8 残留 1 核对后填入）；遥测就绪后按实测命中率动态计价，预算紧时倾向不注入。

### 4.5 P2 · 短 TTL 对策与自托管线

- **TTL 对策按实测分档**（Spike-1 已给首批数据）：腾讯云 (8,12] 分钟——coding agent 常规
  turn 间隔落在安全区，**不预设 keep-alive ping** 的决策成立；但两类场景要在遥测中跟踪：
  ①>10 分钟的长思考/长工具等待（大规模测试、长编译）会系统性丢前缀缓存，
  ②AtomClub 型短 TTL 栈（~5 分钟）上安全区显著收窄。若遥测显示此类占比可观，
  keep-alive 的成本收益按「一次 ping ≈ 全量前缀重算的 1/N」核算，并计入
  bigmodel「缓存存储限免」结束后的存储费风险（§10）；per-端点 TTL 画像由 §4.2-4 在线估计供决策；
- **自托管 GLM（gateway 私有化形态）**：vLLM/SGLang 前缀缓存默认开、严格前缀 →
  L1 纪律收益比 API 形态更确定；部署基线记入 gateway 文档，
  **陷阱清单**（§3.4：HiCache×MTP、fp8 KV 精度、DSA 长上下文 bug、INT4 无 draft）
  作为红线写入；GLM-5.3 权重开源（≈08-28）后复核适用性。

### 4.6 文献七条建议 × GLM 落地映射

| 文献建议 | GLM 化落地 | 归入 |
|---|---|---|
| 1 L3 自适应阈值+门控 | 云厂商价目标定错配代价；cached_tokens 遥测喂在线阈值 | §4.4 · P1 |
| 2 SSL 写入信任边界 | 不因 GLM 改变；共识审计作业跑云厂商最低价档（成本入账） | Agile 2 后并行 |
| 3 evolve 保守进化 | 模型家族维度防版本漂移污染统计（§4.1-3） | §4.1 · P0 |
| 4 L2 注入成本入账 | 按实测命中率×per-endpoint 折扣动态计价注入成本 | §4.4 · P1 |
| 5 prefetch 负载/隐私门 | 端点配额/限流=「负载」信号（Coding Plan 双限额仅当用户自带时适用）；外呼型工具不预取不变 | P1 |
| 6 投毒测例进 CI | 不因 GLM 改变 | 并行 |
| 7 跟踪非前缀 KV 复用 | **Spike-3 已答：两栈均无非严格前缀命中**（报告 §3/§3B），L1 字节稳定仍是唯一路径；自托管线跟 LMCache/CacheBlend 普及 | 观察项 |

## 5. 契约与数据影响

- usage 事件追加 per-provider 缓存命中可选字段：**wire 契约只增不改**，同步 docs/events.md；
- 切片 schema 增加「模型家族」维度（落盘格式变更 → 实施时 R5，需迁移策略）；
- CLI `--json` 信封不变；`semantix usage`/`doctor` 输出增列（增量）。

## 6. 安全与隐私

- gateway 前缀净化中间件**剥离计费头属于修改客户端请求**：默认开启但可配置关闭，
  文档明示行为（不剥离 = 用户缓存费多付 4–5 倍）；`cache_control` 字段**默认透传**
  （两栈实测不报错，报告 §2/§3A.2），剥离逻辑保留为 per-provider 配置项；
- judge 作业外发（云厂商最低价档）：切片内容外发前过既有脱敏（sanitize）管线；
  云厂商条款已核（`glm-p0-tokenhub-catalog.md` §4）：《腾讯云服务协议》6.7 无训练授权条款，
  三条款域中最保守——**「按 bigmodel 保守级别对待」解除**，TokenHub 非「原厂直供」型号上
  judge 外发（过 sanitize 后）条款风险可接受；「原厂直供」型号数据流向原厂，judge/主链路避开；
- 其余安全边界遵循文献建议 2 的独立工作流，本方案不改变信任模型。

## 7. 实施排期与依赖（Agile 2 完成后）

| 阶段 | 内容 | 依赖 | 预估 |
|---|---|---|---|
| ~~Spike 周~~ | ✅ 完成（PR #238，双网关实测超预期交付） | — | — |
| P0 | 前缀卫生审计+净化中间件 + 命中遥测 | H2 遥测通道（资源目录上报）+ §8 残留 1/2 | 1–2 周 |
| P1 | tier 映射+作业排程 + 质量门标定 | H3 指令通道（sched 下发）+ P0 遥测 | 2–3 周 |
| P2 | TTL 对策评估 + 自托管基线 | P0 遥测数据积累 ≥2 周 | 1–2 周 |

## 8. 残留清单（原 Spike 清单已全部落地，见报告；以下为实施前置与观察项）

原五个 Spike 全部完成：TTL/边界/Anthropic 端点行为（双网关实测，报告 §1–§3B）、
国内价目（§4）、数据条款（§5）。端点战略确定「只走云厂商」后，残留如下：

1. ~~云厂商价目与型号目录核对~~：✅ **完成（2026-08-21）**，结论与 tier/成本参数见
   [`glm-p0-tokenhub-catalog.md`](../reports/glm-p0-tokenhub-catalog.md) §1–§3
   （GLM-5.3 = ¥8/28/2 零加价；GLM 无 flash 档，离线 judge 首选跨家族 deepseek-v4-flash；
   限流/配额与「节省计划」套餐折扣未核，P1 预算控制器实施时补）；
2. ~~云厂商数据使用条款核对~~：✅ **完成（2026-08-21）**，见同报告 §4——
   《腾讯云服务协议》6.7 无训练授权，保守级别解除，避开「原厂直供」型号即留在腾讯云条款域；
3. **多云厂商扩展时逐家标定**：新增端点按报告 §1–§3 的实验设计跑一轮（脚本可复用），
   TTL/粒度/usage 形态/价目四件套入配置；
4. ~~Z.AI 官方端点复测~~：**已随端点战略废弃**（不接官方 API）；
5. 观察项：0.5min 前缀「持续不可见」机制（扩样本定量本底丢失率）、
   bigmodel「缓存存储限免」截止（对照参考价目的季度复核）、
   TokenHub `pre-offline` 型号下线节奏（doctor 目录刷新告警，报告 §1）。

## 9. 验收标准

- [ ] `go vet ./... && go test ./... -race` 全绿（每阶段）
- [ ] P0 前置：§8 残留 1/2 核对结论归档（云厂商价目/型号目录 + 数据条款）
- [ ] P0：云厂商 GLM 端点多轮 agent 会话 L1 命中率 ≥ 90%（≈实测命中报告天花板 97.6% 的 92%，
      报告 §6 回写 9；统计口径排除 turn 首轮种缓存请求；`semantix verify` 回放证明）
- [ ] P0：`semantix usage` 显示 per-provider 命中率与节省金额，云厂商 GLM / DeepSeek 双端点核对无误
- [ ] P1：tier 映射生效——离线 judge 作业实际走云厂商最低价档且成本入账（usage 可证）；
      时段感知排程仅当所接端点存在时段价差/配额时启用并可证生效
- [ ] P1：L3 误复用率在云厂商价目标定下 < 1%（vCache 式错误率上界思路，回放测量）
- [x] 五个 Spike 报告归档 docs/reports/（`glm-spike-week.md`，双网关实测，PR #238）

## 10. 风险与回滚

| 风险 | 缓解 |
|---|---|
| 云厂商 GLM 栈行为无 SLA 且互异、随 serving 栈升级漂移（TTL/粒度/usage 形态——§3B 已实证三维度全不同） | 遥测先行 + §4.2-4 端点画像在线估计；新端点按 §8 残留 3 复用实验脚本标定后接入 |
| 单一云厂商依赖（停服、变价、限流收紧） | 经济学参数全配置化（per-provider 价目表），切换成本 = 一轮标定；标定流程与脚本已就绪 |
| 官方对照价目「限时免费」项到期（bigmodel 缓存存储按 token-小时计费启动） | 事实清单标注日期、季度复核；keep-alive 决策把存储费计入（§4.5） |
| 剥离计费头可能与 harness 客户端未来版本冲突（Claude Code 已在 2.1.42 部分修复） | 中间件按 harness 版本探测降级；跟踪上游 issue |
| Coding Plan 条款变化（仅影响自带 Coding Plan 端点的外部 harness 用户） | 已降级为条件性特性并隔离（§4.3）；事实清单标注版本与日期 |
| 切片 schema 加模型家族维度的迁移 | 实施时按 R5 走独立 spec + 迁移工具 |

**回滚**：净化中间件可配置关闭；遥测字段为可选追加；tier 映射回退到现有 RuleDecider 默认值。
