# L3 复用验证 Rubric（代码场景）

> 对应 Issue #8（两级验证：指纹快速否决 + 异步 LLM judge 最终确认）。
> 论文基础：Krites（arXiv:2602.13165）二元判定函数 J(q, s, a)。
> 实现：`kernel/judge` 包（RuleGate + Judge 接口）。

## 两级验证链

```
灰色地带候选（zone.Grey，来自 Issue #7 三段分类）
  ├─ ① RuleGate（零成本、确定性、先过）
  │     · zone=hit        → Confirm（直接复用，不花 judge 钱）
  │     · zone=miss       → Reject
  │     · zone=grey       → NeedJudge（有 Judge）或 Reject（无 Judge，保守）
  ├─ ② Judge（异步 LLM，J(q,s,a)）
  │     · 模型批准 → Confirm（提升入缓存，下次直接命中）
  │     · 模型否决 → Reject（waste++，降信号源）
  └─ 执行位置：P4 预取器等待期（模型流式输出时 off-path 验证）
```

## 代码场景检查维度（judge prompt 指南）

| 维度 | 检查内容 | 判定 |
|---|---|---|
| **API 签名** | 缓存的答案引用的函数/方法签名是否仍匹配 | 签名变更 → 否决 |
| **文件路径** | 答案涉及的路径/模块是否存在于当前工作树 | 缺失 → 否决 |
| **依赖版本** | go.mod/package.json 中相关依赖版本是否一致 | 版本漂移 → 否决 |
| **语言/框架一致** | 答案语言/框架与问题上下文一致（`pkg/foo.go` 的修复不复用给 `pkg/bar.go`） | 不一致 → 否决 |
| **新鲜度** | 代码快照、构建产物、清单文件变更（规则层已覆盖，judge 可省） | 变更 → 否决 |
| **个性化** | 项目配置、用户偏好差异（由 scope 分层：project/user 双库） | 库分层处理 |

## 当前实现边界（诚实声明）

- `kernel/judge` 已落地：RuleGate 三段路由 + Judge 接口 + NoopJudge（无模型时 grey 保守 Reject）。
- **依赖指纹已接入**（`kernel/fingerprint`）：`extract --fingerprint <paths>` 采集 sha256 存入 `SliceMeta.Deps`；`RuleGate.Chain` 对带 Deps 的候选先跑指纹闸（变化 → 硬 Reject，零 LLM 成本）。
- **waste++ 观测已接入**：`judge.Stats`（Confirmed/RulesReject/Fingerprint/JudgeReject/JudgeApproved/JudgeError），`verify --judge-*` 输出统计行；judge 调用出错单独计 `JudgeError`（与「judge 拒绝」区分，Issue #245）。
- **LLM judge 已实现**（`kernel/judge/llm.go`，双协议）：用户自选 `openai`（chat completions）或 `anthropic`（Messages API），`--judge-base-url` + `--judge-model` 指向自己的端点/模型，API key 从环境变量 `SEMANTIX_JUDGE_API_KEY` 读取（绝不入库/入参）。

## 模型 judge 配置（用户操作）

```bash
# OpenAI 协议（OpenAI 兼容端点均可：OpenAI/DeepSeek/Kimi/...）
export SEMANTIX_JUDGE_API_KEY="sk-..."
semantix verify --session <dir> --judge-protocol openai \
  --judge-base-url https://api.openai.com/v1 --judge-model gpt-4o-mini

# Anthropic 协议
export SEMANTIX_JUDGE_API_KEY="sk-ant-..."
semantix verify --session <dir> --judge-protocol anthropic \
  --judge-base-url https://api.anthropic.com/v1 --judge-model claude-sonnet-4-5
```

## 验收（Issue #8 checklist）

- [x] 两级接口（RuleGate + Judge）落地，失败保守 Reject
- [x] 无 judge 时灰色地带保守处理，可观测（reason 输出）
- [x] 指纹闸接入切片 Meta（`SliceMeta.Deps`，`extract --fingerprint`）
- [x] LLM judge 实现（OpenAI/Anthropic 双协议 + env key）+ rubric prompt 入库
- [x] 验证收益统计（`judge.Stats` waste++ 观测）
- [ ] 端到端：真实会话 verify + judge 全链路跑通（等真实数据）
