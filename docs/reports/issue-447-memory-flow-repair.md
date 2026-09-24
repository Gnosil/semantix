# Issue #447 memory-flow 修复：实验与交付证据

日期：2026-09-18。状态：Draft PR；关联 [实现 SPEC](../specs/issue-447-memory-flow-repair.md)。本报告只提交经整理的指标与结果，不上传原始模型轨迹、凭据、机器路径或二进制。

## 2026-09-24 本地 PR 文案补充（待同步远端）

- 旧库不自动迁移；Prompt/ToolPattern 或缺来源/版本的旧记录不会因为撤除六项默认门槛就取得注入资格。重收割的完整命令统一见 [SPEC §6.1](../specs/issue-447-memory-flow-repair.md#61-旧库重收割配方逐个真实会话执行)，不覆盖原库。
- SWE 与交互式 Harness 历史均建议逐会话使用 `--distill --consolidate --origin session-auto --base-commit "$SOURCE_REVISION" --embedder hash`；输入路径、会话 ID、项目与版本必须来自真实会话。ObservedCommit 是当时版本，不是当前 HEAD；不通过改标签、补造来源或改写历史版本制造合格卡。
- 本地文案更新不代表远端 PR 已更新，也不代表已经执行真实库重收割或新一轮模型收益实验。

## 1. 结果摘要及可比性

| 组 | 版本说明 | 输出 | 有效 | PASS | FAIL | 超时 | 其它 |
|---|---|---:|---:|---:|---:|---:|---|
| pre447 | eceb763f + memory bridge 生命周期兼容修复（不是未改原版） | 36 | 36 | 15 | 21 | 11 | 无中断 |
| strict | f55a3354 | 36 | 35 | 16 | 19 | 12 | 1环境无效 |
| hygiene | f55a3354 上的 memory-hygiene 修复 | 36 | 33 | 18 | 15 | 8 | 2历史中断、1官方未知；中断2题超时状态未知 |
| memory-flow | ea1af530 上本次修复；前7/后29两段构建 | 36 | 35 | 18 | 17 | 10 | 1环境无效、0基础设施中断 |

超时与 PASS 可以重叠；无效、未知、中断不算模型失败。费用缺少可靠计价数据，保持未知。

共同有效样本按 instance_id 配对，整轮合计仅为描述统计：

| 历史对照 | 共同有效 | 新增通过 | 回归 | 说明 |
|---|---:|---:|---:|---|
| pre447 | 35 | 4 | 0 | 旧14 → 新18；D11001、D11422、P6116、P7373新增通过 |
| strict | 35 | 4 | 2 | 新增D10914、D11133、D11422、P5692；回归D10924、A14365 |
| hygiene | 32 | 2 | 2 | 新增D11422、A6938；回归D10924、P7490 |

“总数15到18”是观察到的改善，但分母不同。共同有效35题14到18更准确；这仍不是单一记忆变更的因果证明，也不是统计显著性结论。

## 2. 协议、两段构建与交付版本

同一36题清单及同repo顺序，glm-5.3-flash / balanced，40steps、900秒solver、600秒grader、4096B记忆预算，solver2GiB/grader4GiB；strict；同repo串行、跨repo最多3solver。独立空记忆启动，不混入历史对照组、未来题或金标准记忆。官方评分不变。

- `pre_boot_fix`：构建17:02:28Z，7题，有效7/PASS3/FAIL4/超时2。发现 Build 返回即关闭 Bridge 后，现有任务自然结束；原7题保留且不重跑。
- `post_boot_fix`：构建17:58:20Z，仅恢复未开始29题，有效28/PASS15/FAIL13/超时8/环境无效1。
- 恢复后28共同有效对pre447：新增4/回归0；对strict：新增3/回归1；对hygiene27共同有效：新增1/回归1。
- 最后控制器exit0，36题全部终态，solver/grader/prepare归零；未另起第二轮。
- **最终 PR 的 shell 验证器修复在该轮结束后集成，只经过代码回归测试，没有再次运行 SWE36。** 两段实验与最终提交版本必须区别标注。

## 3. 记忆机制与实际收益分开

36次extract均exit0，累计stored227条记录（不是227份独立有效知识）；25题形成显式文件指纹，合计72条路径；实际consolidation groups=0。

前7题诊断 sink 被过早关闭，日志0不等于实际provider注入0。后29题924条strict检索诊断中，正bytes+sliceIds事件0；未独立捕获这轮全部provider payload，实际请求级注入次数不填写伪造的0。正式SWE没有可验证的“注入→模型采用→成绩收益”证据。

真实拒因包含dependency_changed、stale_commit、result_probation、type_not_allowed及相关性/任务资格等。前五条Prompt拒绝日志不等于只搜索了五张卡。整会话Deps被所有卡复用，使通用知识可能依赖过多文件；但删除Deps或忽略commit会破坏正确性。逐卡必要依赖归属仍未完成。

唯一正式条件OFF/ON invoice微型配对，两臂均独立功能PASS：OFF92.23秒/10HTTP请求，ON62.75秒/7请求；ON有6个实际provider请求带user-role469B历史，后续相同核心unittest命令是采用迹象。前史由真实镜像/提炼链路产生，并非手写卡绕过。无关文件跨commit变化、实际依赖不变的受控正例仍可注入469B。

这证明受控条件下传输机制可运行，不证明任意SWE历史可迁移，也不证明29.47秒差值来自记忆。两臂测试量/返工、固定顺序、单次随机性均有混杂。更早6步auto小测两臂功能均FAIL，其较短时间不作为成功证据。

## 4. 失败、回归与超时反例

- P5413：最后stash未pop，生产修改未进入最终补丁。
- P5495：bytes表示缺b前缀；失败pytest通过tail仍exit0，曾产生错误verified收据。最终PR共享mask_exit修复针对这个实际缺陷，不改历史记录。
- A14182：修改写端header，读端start_line/dtype问题未解决。
- P7220：仍未正确使用初始调用目录。
- D11630：按数据库重复E028而非期望W035，且漏import；本轮仍超时失败。
- P7490：新的超时失败，相对hygiene回归；F2P通过不代表全部P2P通过。
- D11422、P9359：超时但官方PASS；D11001、D11583更快通过却无明确注入，不能称记忆收益。
- P7168：模型补丁F2P有失败，正校准通过，负校准受network错误污染；按既定协议列环境无效，未补跑。

四组超时并集21题，保留原17题。工具数增加不预设为超时原因，非工具残差不称纯API延迟，短FAIL不称提速。

## 5. 测试与回退记录

交付前7个完整相关Go包共2021个顶层测试通过：evidence89、agent1326、boot218、semantix50、slice114、inject38、cmd/semantix186。Linux全仓库build和两个程序build通过；Linux运行器Python31项通过。这不是全平台或全仓库race测试通过声明。

原缺陷回归：基线编译0/运行1；独立修复副本运行0；回退脚本exit0恢复27个原文件并移除16个新增源码/测试文件；仅放回回归测试后编译0/运行1，重现原缺陷。主工作副本保留修复。以上43文件计数对应追加本次PR文档前的交付包，不是最终PR文件数。

保留失败记录：Windows Python symlink权限错误；同31项在Linux普通用户下通过。root运行chmod000读取测试与普通用户语义不同，未为此跳过测试。历史Desktop原/改均有newDesktopTray编译失败；本次PR全量新验证结果另记下节，不用历史局部通过覆盖新失败。审计备份曾误用.go后缀参与构建，改备份后缀后构建通过，没有改产品逻辑掩盖错误。

运行级回退使用既有off/shadow。代码回退用Git revert；不回写已完成实验，不删用户库。source_sessions是可选加法字段，旧写者可能丢新字段，见SPEC兼容性说明。

## 6. 本次提交前的新验证

使用 Go1.26.5 / Linux amd64 普通用户，未重新调用付费模型、未再次运行36题。

| 命令/检查 | 实测结果 |
|---|---|
| `go vet -p 2 ./...`（原生Linux，CGO_ENABLED=1） | exit0 |
| `go test -p 2 ./... -race -count=1 -timeout=5m`（原生Linux） | 首次exit1：142包通过，仅harness/memory的旧时间排序fixture失败；未发现race报告 |
| `go test -p 2 ./harness/memory -race -count=10`（修正fixture后） | exit0，完整包连续10次通过 |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -p 2 ./...` | exit0 |
| `python3 -m unittest discover -s scripts/swebench -p 'test_*.py'`（Linux） | exit0，31测试通过 |
| `git diff --cached --check`；修改Go文件`gofmt -l` | exit0；无格式差异 |

**不把修正后单包10次通过写成一次全量命令exit0；修正后的全量race重跑留给PR CI，Draft保持待验证。**

新验证中修正两个测试fixture，未放松原断言：control recovery的`vitest | tail`改为直接vitest，使测试继续覆盖实际失败后的自动恢复而不是被mask_exit预先拦截；memory排序测试把legacy文件mtime显式设为已保存记录updated_at+1秒，不依赖文件系统时钟精度或sleep。memory生产代码与基准相同。

更早跨Windows编译/WSL执行的全量尝试还遇到runtime.Caller包含Windows路径（extension fixtures）、子测试找不到Linux go（plugin）；安装同版本原生Linux工具链后这两包在上述全量race中通过。首次失败输出保留在本地审计，不改产品代码掩盖跨平台运行器问题。

提交前定向独立review没有P1/P2发现；它不是GitHub维护者批准，也不替代CI。

## 7. 四组全部36题

每格“结果 / 超时”；未知不填0，阶段和构建时间单列。数据来自本次终态报告的已复算逐题表。

| instance_id | runtime_phase | runtime build UTC | pre447 | strict | hygiene | memory-flow |
|---|---|---|---|---|---|---|
| django__django-10914 | pre_boot_fix | 2026-09-18T17:02:28Z | PASS / True | FAIL / False | PASS / False | PASS / True |
| django__django-10924 | pre_boot_fix | 2026-09-18T17:02:28Z | FAIL / False | PASS / False | PASS / True | FAIL / False |
| django__django-11001 | post_boot_fix | 2026-09-18T17:58:20Z | FAIL / True | PASS / True | PASS / True | PASS / False |
| django__django-11019 | post_boot_fix | 2026-09-18T17:58:20Z | FAIL / False | FAIL / False | FAIL / True | FAIL / True |
| django__django-11039 | post_boot_fix | 2026-09-18T17:58:20Z | PASS / False | PASS / False | PASS / False | PASS / False |
| django__django-11049 | post_boot_fix | 2026-09-18T17:58:20Z | PASS / False | PASS / False | PASS / False | PASS / False |
| django__django-11099 | post_boot_fix | 2026-09-18T17:58:20Z | PASS / False | PASS / False | PASS / False | PASS / False |
| django__django-11133 | post_boot_fix | 2026-09-18T17:58:20Z | PASS / False | FAIL / False | PASS / False | PASS / False |
| django__django-11179 | post_boot_fix | 2026-09-18T17:58:20Z | PASS / False | PASS / False | PASS / False | PASS / False |
| django__django-11283 | post_boot_fix | 2026-09-18T17:58:20Z | FAIL / False | FAIL / True | FAIL / True | FAIL / False |
| django__django-11422 | post_boot_fix | 2026-09-18T17:58:20Z | FAIL / True | FAIL / False | FAIL / True | PASS / True |
| django__django-11564 | post_boot_fix | 2026-09-18T17:58:20Z | FAIL / True | FAIL / True | FAIL / False | FAIL / True |
| django__django-11583 | post_boot_fix | 2026-09-18T17:58:20Z | PASS / False | PASS / False | PASS / False | PASS / False |
| django__django-11620 | post_boot_fix | 2026-09-18T17:58:20Z | FAIL / False | FAIL / True | FAIL / False | FAIL / True |
| django__django-11630 | post_boot_fix | 2026-09-18T17:58:20Z | FAIL / True | FAIL / True | FAIL / True | FAIL / True |
| astropy__astropy-6938 | pre_boot_fix | 2026-09-18T17:02:28Z | PASS / False | PASS / False | FAIL / False | PASS / False |
| astropy__astropy-7746 | pre_boot_fix | 2026-09-18T17:02:28Z | FAIL / False | FAIL / False | 基础设施中断 / 未知 | FAIL / False |
| astropy__astropy-12907 | pre_boot_fix | 2026-09-18T17:02:28Z | PASS / False | PASS / False | PASS / False | PASS / False |
| astropy__astropy-14182 | post_boot_fix | 2026-09-18T17:58:20Z | FAIL / True | FAIL / True | FAIL / False | FAIL / True |
| astropy__astropy-14365 | post_boot_fix | 2026-09-18T17:58:20Z | FAIL / False | PASS / False | FAIL / False | FAIL / False |
| astropy__astropy-14995 | post_boot_fix | 2026-09-18T17:58:20Z | PASS / False | PASS / False | PASS / False | PASS / False |
| pytest-dev__pytest-5103 | pre_boot_fix | 2026-09-18T17:02:28Z | FAIL / True | FAIL / True | 基础设施中断 / 未知 | FAIL / True |
| pytest-dev__pytest-5221 | pre_boot_fix | 2026-09-18T17:02:28Z | FAIL / False | FAIL / True | FAIL / False | FAIL / False |
| pytest-dev__pytest-5227 | post_boot_fix | 2026-09-18T17:58:20Z | PASS / False | PASS / True | PASS / False | PASS / False |
| pytest-dev__pytest-5413 | post_boot_fix | 2026-09-18T17:58:20Z | FAIL / True | FAIL / False | FAIL / False | FAIL / False |
| pytest-dev__pytest-5495 | post_boot_fix | 2026-09-18T17:58:20Z | FAIL / True | FAIL / True | FAIL / False | FAIL / False |
| pytest-dev__pytest-5692 | post_boot_fix | 2026-09-18T17:58:20Z | PASS / False | FAIL / False | PASS / False | PASS / False |
| pytest-dev__pytest-6116 | post_boot_fix | 2026-09-18T17:58:20Z | FAIL / False | PASS / False | 未知 / True | PASS / False |
| pytest-dev__pytest-7168 | post_boot_fix | 2026-09-18T17:58:20Z | PASS / True | 评分无效 / False | PASS / False | 评分无效 / False |
| pytest-dev__pytest-7220 | post_boot_fix | 2026-09-18T17:58:20Z | FAIL / True | FAIL / False | FAIL / True | FAIL / False |
| pytest-dev__pytest-7373 | post_boot_fix | 2026-09-18T17:58:20Z | FAIL / False | PASS / False | PASS / False | PASS / False |
| pytest-dev__pytest-7432 | post_boot_fix | 2026-09-18T17:58:20Z | PASS / False | PASS / False | PASS / False | PASS / False |
| pytest-dev__pytest-7490 | post_boot_fix | 2026-09-18T17:58:20Z | FAIL / False | FAIL / False | PASS / False | FAIL / True |
| pytest-dev__pytest-8365 | post_boot_fix | 2026-09-18T17:58:20Z | FAIL / False | FAIL / True | FAIL / False | FAIL / False |
| pytest-dev__pytest-8906 | post_boot_fix | 2026-09-18T17:58:20Z | FAIL / False | FAIL / False | FAIL / False | FAIL / False |
| pytest-dev__pytest-9359 | post_boot_fix | 2026-09-18T17:58:20Z | PASS / False | PASS / True | PASS / False | PASS / True |
