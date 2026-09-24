# 接入 Coding Agent

Semantix 的内核不要求 agent 更换执行循环。最小接入面只有三件事：任务开始前检索、需要时注入、会话结束后提取。

## 先选择集成层级

| 场景 | 推荐路径 | 改动范围 |
|---|---|---|
| Semantix 完整产品 | 使用内置 Semantix Agent 集成 | 无需重复注册工具 |
| Claude Code | 安装 agent skill | 用户级 skill 目录 |
| 支持 function calling 的 agent | 注册 `semantix_lookup`，按需注册 inject/extract | 工具层 |
| 自定义 harness | 会话导出、事件旁路或直接调用 | 适配层 |

## Claude Code

```bash
semantix install --target claude-code
```

安装目标默认是 `~/.claude/skills/semantix/`。安装完成后重启 Claude Code，并用 `semantix lookup --help` 验证二进制仍可从 `PATH` 找到。

## Semantix Agent

仓库中的 Semantix Agent harness 已内置 Semantix 接口。配置中启用对应段落后，harness 会暴露 lookup 能力并把会话事件旁路给内核。不要在同一轮再手工注入第二份相同内容。

## 自定义 harness 的生命周期

### 任务开始

```bash
semantix lookup --query "<当前任务>" --scope user --json
```

只有命中内容确实与当前任务相符时才使用。`grey` 表示需要进一步验证，不是自动执行许可。

### 构造上下文

```bash
semantix inject --query "<当前任务>" --scope user
```

注入块是低权限历史材料。agent 仍需遵守当前用户请求、仓库规则和权限边界。

### 会话结束

```bash
semantix extract --input <session.jsonl> --scope user --project <业务域>
```

如果 harness 的会话格式不同，先在适配层转成仓库约定的 JSONL，不要让内核直接猜测私有格式。

## 验收

仓库提供自测脚本，覆盖提取、lookup 命中和注入块：

```bash
bash agent-skill/scripts/selftest.sh
```

通过标准为脚本输出 `SELFTEST PASS`。这只证明最小接线成立，不代表你的真实会话命中率已经达标。

## 对应仓库来源

- `agent-skill/SKILL.md`
- `agent-skill/tools/semantix-lookup.md`
- `agent-skill/hooks/session-bypass.md`
- `harness/semantix/`
