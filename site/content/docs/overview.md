# Semantix 项目速览

Semantix 是一个用 Go 实现的 Agent Kernel 与 coding-agent 产品仓库。它把历史会话转成可检索的语义切片，并围绕这些切片提供检索、上下文注入、受约束结果复用、调度、预取和反馈演化。

## 它解决什么问题

Coding agent 会重复读取同一批仓库文件、重新定位相似错误、重新组织已经验证过的步骤。普通 transcript 保存了历史，却没有提供稳定的检索、作用域、失效和验收边界。

Semantix 把问题拆成四步：

1. 从会话事件中提取可复用切片。
2. 用词法、向量或混合检索寻找候选。
3. 根据风险选择只注入背景，或经过附加门后复用结果。
4. 记录命中、浪费和任务反馈，用于受约束调参。

## 仓库里有什么

| 模块 | 责任 | 主要目录 |
|---|---|---|
| CLI | 提取、检索、注入、验证、诊断和维护 | `cmd/semantix` |
| Coding agent | Semantix Agent harness 与可执行入口 | `cmd/semantix-agent`、`harness` |
| 语义切片 | 类型、作用域、存储、压缩与淘汰 | `kernel/slice` |
| 检索 | BM25、embedding、融合与 zone | `kernel/bm25`、`kernel/embed`、`kernel/fuse`、`kernel/zone` |
| 复用 | lookup、注入、L3 判定、指纹与提升 | `kernel/lookup`、`kernel/inject`、`kernel/cache`、`kernel/fingerprint`、`kernel/judge`、`kernel/promote` |
| 资源编排 | round plan、预取和有界演化 | `kernel/sched`、`kernel/prefetch`、`kernel/evolve` |
| Gateway | OpenAI 兼容代理、SSE、上游路由与用量 | `gateway`、`cmd/semantix-gateway` |
| 外部接入 | Agent skill、工具 schema 和会话旁路 | `agent-skill` |

## 核心术语

**Harness** 是承载模型调用、工具、权限和会话循环的宿主。**Kernel** 是相对独立的记忆与优化层。两者通过事件和工具接口连接。

**Semantic slice** 是从会话中提取、能够独立检索的经验单元。它带有类型、来源和作用域，不是完整聊天记录的别名。

**L1 / L2 / L3** 分别表示供应商前缀缓存、语义切片注入和已验证结果复用。风险逐级上升，验证要求也逐级上升。

**Zone** 把检索候选分为 hit、grey 和 miss，用来保留不确定性。hit 仍不是越过当前权限或项目状态检查的许可。

## 当前实现边界

仓库已经包含上述模块的实现和测试入口，也包含合成回放、验收报告与 Gateway 示例。它们能够证明特定代码路径和仓库夹具可运行。

仍需单独验证的是：不同团队、不同模型和真实长期会话中的命中率、错误复用率、延迟与实际账单变化。因此官网文档把“实现存在”“仓库测试”“合成回放”和“生产证据”分开描述。

## 建议阅读顺序

1. 《安装与首次运行》：先跑通最小闭环。
2. 《接入 Coding Agent》：选择宿主接线方式。
3. 《语义切片与三级缓存》：理解每层能做什么。
4. 《验证、用量与健康检查》：建立自己的验收数据。

## 事实来源

- `README.md` 与 `README.zh-CN.md`
- `docs/QUICKSTART.md`
- 各 `kernel/*`、`gateway/*`、`harness/*` 模块及其测试
- `docs/reports/` 与 `docs/specs/`
