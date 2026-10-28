# MiniMax Code：SQLite 会话旁路

对应 [#538](https://github.com/Gnosil/semantix/issues/538)。这是基于 MCode 3.1.3
`local-runtime-v2` 数据契约的本地转换示例，使用 Python 3 标准库，不依赖 MCode SDK。
测试使用该契约的合成 SQLite 数据；尚未在真实 MCode 安装上验收。

## 导出并提取

在 semantix 仓库根目录运行。先结束目标会话；`--session` 使用 MCode 的真实会话 ID，
可重复传入。未指定时扫描所有会话，不根据未约定的 `status` 枚举猜测哪些已结束。

```bash
python3 agent-skill/scripts/export_mcode.py \
  --session '<已结束的会话ID>' \
  --extract --store "$HOME/.semantix/user.db"
```

默认输入：

- 会话数据库：`~/.minimax/v2/sqlite/runtime-state.sqlite`（可用 `--db` 覆盖）。
- 偏好文件：`~/.minimax/memory/user.md`，存在时一并导出。`--memory <路径>` 指定其他文件，
  显式指定的文件不存在会报错；`--no-memory` 跳过。
- 导出目录和水位：`~/.semantix/sessions/mcode/`（可用 `--output-dir` 覆盖）。
- `--semantix /path/to/semantix` 指定 CLI；不加 `--extract` 时只导出 JSONL，不写切片库。

输出为 JSON 摘要，包含新文件路径、会话消息数和记忆块数，不打印消息正文。
JSONL 文件名使用会话 ID 的 SHA-256 和 rowid 范围，不直接拼接不可信 ID 为路径。

只保留高信号偏好、避免导出原始聊天时：

```bash
python3 agent-skill/scripts/export_mcode.py --memory-only \
  --memory "$HOME/.minimax/memory/user.md" \
  --extract --store "$HOME/.semantix/user.db"
```

两种模式可共用导出目录；`--memory-only` 不打开会话数据库。

## 在另一个 agent 中检索

Claude Code / OpenClaw 等 agent 使用现有 `semantix_lookup` 工具或调用同一 CLI：

```bash
semantix lookup --query '字幕对齐时如何处理时间轴' \
  --scope user --db "$HOME/.semantix/user.db" --json
```

`--store` / `--db` 必须指向同一个文件。显式指定路径可避免项目配置中的 `store.db`
把写入或检索重定向到另一个库。此示例不会自动导入 Claude Code 或 OpenClaw 的原始会话；
各 agent 的 JSONL 也可通过[通用会话旁路](session-bypass.md)写入同一库。

## 转换规则与重复运行

- **角色取 `data_json.role`，不用 SQL 的 `role` 列。** `msg_type=3` 的运行时事件跳过。
- 字符串正文直接保留；内容块列表提取 `text` 和 `tool_use`。显式 `tool_calls` 与块内调用
  合并去重；没有正文但有工具调用的 assistant 消息仍保留。图片等非文本块不导出。
- 记忆文件按 HTML 注释分块，每块生成**一行 JSONL、一个 user 消息**，内容为
  `【首个加粗标题】\n完整块`，不拼虚构 assistant 回复，不裁掉末尾应用说明。
- 每个会话独立记录 rowid 水位，只读取新增行；偏好文件独立按内容哈希检测更新，
  即使会话没有新增行，也会重新导出变更后的偏好。
- 未改变的输入产生零个新 JSONL 文件。会话按 rowid 排序，读取使用同一 SQLite 快照；
  WAL 中已提交的消息也可见。
- 加 `--extract` 时，全部新文件提取成功后才保存水位。若某个文件提取失败，之前已成功
  提取的切片可能已在库内；重新运行会重放该批，已有内容由内核的切片 ID 去重。
- 只导出模式保存的是**导出水位**。之后手工提取失败，应重试摘要中列出的现有文件；
  需要从只导出模式改用 `--extract` 时，使用新的导出目录重放历史。
- 导出状态绑定源数据库、目标共享库和是否自动提取。切换库或提取模式会报错，需使用新
  导出目录，避免旧水位让另一个库漏掉历史内容。

## 边界与隐私

- SQLite 使用 `mode=ro` 和 `query_only`，不修改 MCode 数据。缺库、缺必要表/列、坏消息
  或缺显式偏好文件都会报错；源数据校验完成前不发布批次或调用提取。
- 同一导出目录只允许一个任务写入，定时运行应串行。水位假设消息追加且 rowid 不回退；
  更换数据库或重写历史后使用新的导出目录，不复用旧水位。
- POSIX 上导出目录以 `0700`、文件以 `0600` 创建；Windows 应通过目录 ACL 控制访问。
  **导出的原始 JSONL 尚未经过内核净化，可能包含聊天中的敏感内容。** 不应提交进仓库。
  `extract` 会执行现有净化流程，但不能承诺识别所有秘密。
- 自动提取统一标为 `session-auto`，不伪造验证回执，也不自动晋升为人工信任或 L3 可复用。
- 这是一条增量摄取路径，**不自动撤销旧偏好**。文件内修改/删除规则后，已经入库的旧切片
  仍需另外审查；它不是偏好文件与共享库之间的权威镜像同步。
- 导出保留完整规则，内核仍有 Prompt/Result 长度预算。超长规则应先人工拆为完整的小规则，
  不应把导出成功等同于所有长文本都会原样入库。`zone=hit` 也不是相关性或正确性证明。

## 验证

```bash
python3 -m unittest discover -s agent-skill/scripts -p 'test_*.py'
go build -o /tmp/semantix ./cmd/semantix
SEMANTIX_BIN=/tmp/semantix python3 -m unittest discover -s agent-skill/scripts -p 'test_*.py'
```

第二次运行包含真实 CLI 的导出→提取→跨工作目录检索检查，使用隔离临时库，不读个人会话。
