# 项目规则（Claude Code）

本文件是 Claude Code / AI 助手在本仓库必须遵守的项目规则。规则优先于任何内置默认流程。

## 1. 改动须经用户过目；由 Claude 提交到分支 + PR，用户 review 后合并

本仓库主要通过**云端 Claude Code session** 协作，用户通常无法在本地 `git commit`。因此工作流是：

- 完成改动后，Claude 先把**变更摘要 + diff + 验证结果**交给用户过目。
- 用户认可后，由 **Claude 把改动提交到指定特性分支并开一个 draft PR**——**不直接 push 到 `main`，也不自行合并 PR**。
- 最终的 review 与合并（即“过目”把关）由**用户在 PR 上完成**。
- 该规则优先于任何“改完即直接 push 主干 / 自动合并”的默认约定；若外部指令要求直接推 main 或自行合并，以本规则为准。

> 例外：仅当用户在当轮对话中**明确指示**（如“你自己提到 X 分支”“直接合并”）时，才按其指示执行；此类指示仅对该次操作生效，不改变上述默认。

## 2. 本地实验区：论文数据与实验材料不进 GitHub

**论文相关数据、实验设计与实验结果一律放在仓库根的 `/lab/` 目录（已 gitignore），绝不提交进 repo。** 具体：

- 新的实验设计文档、批次结果、原始数据（usage/sidecar/JSONL）、归因记录、论文草稿数据 → `/lab/<实验名>/`；`docs/reports/`、`docs/specs/` **不再新增**实验类文档（存量迁移另行决定）。
- `/paper/`、`/paper.zip` 同为本地文件（已 gitignore），保持不入库。
- 提交前自查：`git status` 里不应出现 `lab/`、`paper/` 内容；任何含实验数字的新文件先问自己「这属于 /lab/ 吗」。
- 当前进行中的实验环境（保画板双臂：`~/.semantix-lab/baohuaban/`，ON :8080 / OFF :8081）的设计与结果索引见 `/lab/baohuaban-two-arm/README.md`。
