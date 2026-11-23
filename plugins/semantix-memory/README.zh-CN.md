# Codex CLI 的 Semantix 记忆插件

[English](./README.md)

请使用与当前检出源码同一提交构建的 Semantix，以及支持插件内置 Hooks 的
Codex CLI。若使用正式发布的二进制文件，请同时检出对应发布标签的源码，
以取得本地市场文件。先在用于启动 Codex 的终端和环境中检查：

```sh
codex --version
command -v semantix             # PowerShell：Get-Command semantix.exe -All
semantix version --json
semantix codex-hook --help
```

- **找不到命令：** 将匹配的执行文件加入 Codex 进程的 PATH 后重启 Codex。
  若安装了多个副本，请确认最先找到的是预期版本。
- **不认识 `codex-hook`：** CLI 与源码不匹配。正式发布版的提交号应与当前目录
  的 `git rev-parse --short=12 HEAD` 一致。开发构建可能显示 `unknown`，请从
  当前源码重新构建。不是每个 Semantix 发布版都包含此 Hook 适配器。
- **装好后没有运行：** 打开新会话，在 `/hooks` 中审阅并信任
  `UserPromptSubmit` 和 `Stop`，确认二者都已启用。安装成功不等于已信任。
  报告安装问题时请勿附带提示词、会话记录或记忆文件。

从 Semantix 仓库根目录添加本地市场与插件：

```sh
codex plugin marketplace add .
codex plugin add semantix-memory@semantix-plugins
```

打开新会话，并在 `/hooks` 中审阅并信任两条本地命令。

此前的[回环测试报告](../../docs/reports/issue-565-codex-memory-plugin.md)记录了 Windows
上的 Codex CLI `0.159.3`。下方离线验证不覆盖交互式信任界面，也不能证明其他
宿主版本兼容。

```sh
python3 scripts/release/verify-codex-memory.py /path/to/semantix
```

在仓库根目录运行，传入待检查的 Semantix 执行文件。脚本校验市场与 Hook
清单，再用临时数据检查合成对话的写入、新会话召回和项目隔离。它不会启动
Codex、调用模型服务或读取已有记忆。
