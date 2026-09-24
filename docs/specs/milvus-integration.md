# Spec v1 — Milvus 作为可选部署 profile 与 MCP 数据源（待建 issue）

> 判级：**Spec-Exempt**（部署编排 + 配置样例 + 文档；三个新 service 全部置于
> 非默认 profile 之下，默认 `up -d` 的 service 集合与 volume 集合逐字不变；
> 无 Go 代码改动，无新的 semantix 配置键，`[retrieval] retriever` 的取值域
> 未变）。
>
> 按 `CONTRIBUTING.md` 的「Spec 先行」条款,spec 的强制对象是**跨包行为变更**
> (kernel↔harness 契约、事件、配置面),「纯 bug 修复与文档可省略」。本 PR 属
> 可省略之列,**本文是自愿补充**:三服务的编排里有两处反直觉的决定(§2.2 的
> 回环发布、§2.3 的 etcd healthcheck),以及一串未实跑的残余(§5.1),写下来
> 比在 PR 讨论里解释便宜。
>
> **Spec-Required 的那两半不在本文范围内**：把 Milvus 接成 `slice.Index` 的
> 第四个检索后端（新包结构 + 新配置键 + 影响排序契约），以及把 semantix 暴露
> 为 MCP server（新对外契约 + 安全边界）。两者都另设门槛与独立 spec，见 §3。
>
> 基线：`00b6bd4`（main，"Merge pull request #433 … codex/issue-414-desktop"）。
> 本文所有现状判断按此 sha 实测，行号可回查。
>
> 姊妹文档：`docs/integrations/milvus.md`（面向使用者）。

## 1. 问题

### 1.1 这个 PR 不解决什么（先说清，否则整篇会被误读）

Semantix 的切片检索**不经过 Milvus**，本 PR 之后依然不经过。`validRetriever`
的取值域仍是 `bm25|vector|hybrid`（`gateway/config.go:666-672`），填 `milvus`
会被配置校验拒绝。

后端替换属另一件事，且当前规模下是净负债：`store.max_slices` 默认 5000
（`kernel/config/config.go:216`），5000 片 × 1536 维的暴力点积在
`kernel/embed/vecindex.go:57-67` 是数毫秒量级，换成 Milvus 只是多一次网络
往返加三个常驻容器。

本 PR 提供的是**基座与生态接口**：一个符合项目安全约束的本地 Milvus，以及
agent 通过 MCP 使用它的路径。

### 1.2 真源审计（2026-09-22，分支 `feat/milvus-compose-base`）

| 事实 | 证据 |
|---|---|
| `deploy/` 当前是两服务，无 profile 机制 | `deploy/docker-compose.yml:13-48`；`grep -rn profiles deploy/` 无命中 |
| compose 用 `${VAR:?}` 硬失败语法引用两个凭据，但仓库无 `.env` 样板 | `docker-compose.yml:33-34`；`ls deploy/` 只有 `Dockerfile` / `docker-compose.yml` / `semantix-gateway.toml.example` |
| `.gitignore` 已有 `*.env` + `!*.env.example` 成对规则 | `.gitignore:27-28`（`git check-ignore -v` 实测：`deploy/.env` 命中 `:27`，`deploy/.env.example` 命中 `:28`） |
| stdio MCP 插件是**宿主机**子进程，不在 compose 网络内 | `harness/plugin/plugin.go:291` 注释明写经 `exec.CommandContext` 派生子进程 |
| `PluginEntry` 支持 `env`，且字符串字段做 `${VAR}` 展开 | `harness/config/plugin_entry.go:10-46`；展开实现 `harness/config/expand.go:65-78` 的 `ExpandedPlugin()`，`env` 经 `expandMap` |
| 官方 MCP registry 一键安装对需要环境变量的条目不可用 | `harness/mcpregistry/registry.go:288-291`：`len(pkg.EnvironmentVariables) > 0` 即跳过并记 `reasons`，最终 `UnavailableReason = "package requires environment variables or arguments"` |
| Milvus 已在安全文档中被点名三处 | `docs/Security-安全设计.md:172`（回环绑定 + 本地认证 + 运行时身份校验）、`:221`、`:228` |

### 1.3 缺口

1. 想同时用 Milvus 与 Semantix 的使用者，没有可参照的部署与接法；
2. `deploy/.env.example` 缺失，新用户执行 compose 头部写的用法会直接撞上
   变量未设置的失败，且没有可 `cp` 的文件——**这一条与 Milvus 无关，是既有
   缺口**，已拆为独立 PR（§4.1）。

## 2. 设计

### 2.1 三个 service 全部挂在非默认 profile 下

`profiles: [milvus]` 使默认 `docker compose up -d` 完全不感知它们。这是
Exempt 判级成立的前提：现存部署的 service 集合、volume 集合、启动顺序逐字
不变。若不加 profile，本 PR 会让每一个现有用户凭空多起三个容器、多背数 GB
镜像与 etcd/MinIO 常驻开销——那是行为变更，判级须升为 Required。

验证方式见 §5 的 A1。

### 2.2 Milvus 必须把 19530 发布到回环，仅 `expose` 不可行

这是本 PR 最容易写错的一处，且与安全条款的表面读法相反。

`expose` 只在 compose 内部网络生效。而 MCP server（`uvx mcp-server-milvus`）
是 **semantix 在宿主机上派生的 stdio 子进程**（`harness/plugin/plugin.go:291`），
不在 compose 网络里——只写 `expose: ["19530"]` 的话 §2.6 的整条链路连不通。

因此显式写 `ports: ["127.0.0.1:19530:19530"]`。这**正是**
`docs/Security-安全设计.md:172`「仅绑定回环地址」要求的形态，不是对它的
妥协：绑定 `127.0.0.1` 与绑定 `0.0.0.0` 的差别就是这一条款本身。

> 反面写法 `"19530:19530"` 会绑到 `0.0.0.0`，把向量库暴露到局域网。compose
> 里已就地写了警告注释，因为这是个一字之差、后果严重且不会有任何报错的改动。

etcd（2379）与 MinIO（9000）无宿主机访问需求，保持 `expose`，不发布。

### 2.3 etcd 必须有 healthcheck

`depends_on` 不带 `condition` 不会等待健康,只等容器创建。Milvus 会在 etcd
就绪前启动并反复重启。故 etcd 补 `etcdctl endpoint health`,并让 milvus 用
`condition: service_healthy` 前置 etcd 与 minio 两者。

Milvus standalone 冷启动含索引节点初始化,`start_period` 给 90s;etcd/minio
给 10s。

### 2.4 凭据无默认值,硬失败优于弱口令

MinIO 凭据走 `${MINIO_ROOT_USER:?}` / `${MINIO_ROOT_PASSWORD:?}` 从
`deploy/.env` 注入。`:?` 对**未设置与空值同样报错**,所以 `.env` 里留空不会
静默启动一个弱口令服务。

变量名用新版的 `MINIO_ROOT_USER` / `MINIO_ROOT_PASSWORD`;`MINIO_ACCESS_KEY` /
`MINIO_SECRET_KEY` 已废弃。

**不采用**上游示例常见的 `minioadmin/minioadmin` 默认值——一份可以直接
`up -d` 拉起的部署里不应该存在众所周知的口令。对齐 `CONTRIBUTING.md:31`
「key 只走环境变量」。

### 2.5 鉴权用 `user.yaml` 增量覆盖,不整份替换 `milvus.yaml`

Milvus 读 `milvus.yaml` 后再读 `user.yaml` 做覆盖。挂一份只含
`common.security.authorizationEnabled: true` 的 `user.yaml`,其余默认值全部
保留。若直接挂 `milvus.yaml`,会把整份默认配置替换掉,升级镜像时默认值的
变化也就随之丢失。

回环绑定挡不住本机上的其他进程直连 19530,所以这一项不能省。

### 2.6 MCP 配置以注释态进 example 文件

`semantix-agent.example.toml` 的既有惯例是把可选能力写成注释块
（`:206-217` 的 `[[plugins]]` 样例即如此）。Milvus 条目照此形态,默认不生效。

`env` 值写成 `${MILVUS_URI:-http://127.0.0.1:19530}` 与 `${MILVUS_TOKEN}`,
利用 `ExpandedPlugin()` 的展开（`harness/config/expand.go:65-78`）让 token
留在环境里而非配置文件里。

同时在注释中写明**不能走 registry 一键安装**及其原因（`registry.go:288-291`）
——否则使用者会先去试 `/mcp` 安装,失败后无从判断是自己配错还是功能缺失。

### 2.7 使用者文档把能力边界放在第一节

`docs/integrations/milvus.md` 首节即为「能力边界」,用一张表说明 Milvus 与
Semantix 职责正交,并明确写出 `retriever` 不接受 `milvus`。

理由是铁律「诚实标注限制」（`CODEBASE-TOUR.md:539`）。一篇标题含 Milvus 的
集成文档,读者的默认推断就是「检索后端换成 Milvus 了」;不在最前面挡住这个
推断,后面写什么都会被误读。

安全一节逐条对照 `Security-安全设计.md` 的三项要求,其中运行时服务身份校验
**如实标为未满足**（§5.1）。

## 3. 非目标

- **不碰 `gateway/`**。`validRetriever`（`config.go:666-672`）、`newRetriever`
  （`retriever.go:34-46`）、`RetrievalConfig` 一律不动。本 PR 的 diff 不应
  出现该目录下任何文件。
- **不碰 `kernel/`**。尤其是 `kernel/zone/`、`kernel/fuse/`、`kernel/bm25/`
  ——它们是判定模型与融合的单一真源,与本 PR 正交。
- **不碰 `cmd/semantix/search.go`**。CLI 的 `--retriever` 取值不变。附带说明:
  CLI 与网关是两条独立装配路径（CLI 的 `index := deps.newIndex()` 恒为 BM25,
  `search.go:118`;vector/hybrid 在 `:138-189` 内联另建),日后接 Milvus 时
  这是个独立问题,不应顺手并进来。
- **不把 semantix 暴露为 MCP server**。那是对外契约 + 安全边界,判级
  Spec-Required,另开 spec。
- **不修改 `docs/Security-安全设计.md`**。Milvus 的约束该文档已有（`:172`/
  `:221`/`:228`）,本 PR 是去满足它们,不是去改它们。
- **不引入任何 Go 依赖**。`go.mod` 不变。

## 4. 任务与验证

无 Go 代码改动,故无单元测试可写。验证手段相应地是**结构断言 + 真解析器
喂入 + 实跑**三类,分列于 §5。

### 4.1 拆分为两个 PR

| PR | 范围 | 阻塞条件 |
|---|---|---|
| PR-1 | `deploy/.env.example`（仅两个既有变量）+ compose 用法注释 | 无,已验证完毕 |
| PR-2 | milvus/etcd/minio profile + `milvus-user.yaml` + `.env.example` 追加两个 MinIO 变量 + `docs/integrations/milvus.md` + README 集成表一行 + MCP 样例 | 需在有 docker 的环境跑通 §5 的 B 组 |

拆分理由:PR-1 修的是与 Milvus 无关的既有缺口(§1.3 第 2 条),且完全静态可
验证;把它压在需要 docker 实跑的 PR-2 后面没有道理。

PR-2 内部不再细拆:三服务、鉴权配置、使用者文档三者互为前提,分开推会出现
「能起但没鉴权」或「文档指向不存在的服务」的中间状态。README 那行
`✅ shipped` 必须与可用的 compose 同批进。

## 5. 验收标准

### A 组 — 静态,已执行

- **A1** `docker-compose.yml` 经 YAML 解析后,`milvus`/`etcd`/`minio` 三者的
  `profiles` 均为 `['milvus']`,且均有 `healthcheck`。**已通过。**
- **A2** 三者中任何 `ports` 条目都以 `127.0.0.1:` 开头(不得有 `0.0.0.0` 或
  裸端口映射);etcd 与 minio 无 `ports`。**已通过。**
- **A3** PR-1 的 compose 改动仅涉及注释:解析后 service 集合仍为
  `['new-api','semantix-gateway']`、volume 集合仍为 `['semantix-data']`。
  **已通过。**
- **A4** compose 引用的 `${VAR}` 集合与 `.env.example` 定义的变量集合精确
  相等(不多不少)。**已通过。**
- **A5** `deploy/.env` 命中 `.gitignore:27`,`deploy/.env.example` 命中
  `:28` 的否定规则。**已通过（`git check-ignore -v` 实测）。**
- **A6** §2.6 的插件片段以**去注释后的原样**喂给 `config.PluginEntry` 真实
  解析器,得到 `type=stdio` / `command=uvx` / `args=[mcp-server-milvus]` /
  `env` 两键,且无 undecoded key。**已通过。**
- **A7** `go build ./...`、`go vet ./...`、`git diff --check` 干净;`go.mod` /
  `go.sum` 无变化;`git diff main --name-only` 不含任何 `.go` 文件。
  **已通过。**
- **A8** `go test ./... -race` 全量:除
  `harness/remote` 的 `TestClientPromptsPerEncryptedIdentity` 外全绿。该失败
  为**负载敏感的既有 flake,与本改动无因果关系**,三步对照:
  (i) 基线 `00b6bd4` 隔离跑该测试通过;
  (ii) 本分支单独跑 `./harness/remote/` 整包通过(17.0s,而失败时的 deadline
  是 18.98s——该测例本就贴着超时边缘);
  (iii) 本分支相对 main 的 diff 零 `.go` 文件(见 A7),不存在影响该测试的
  途径。**已通过(含如实标注)。**

### B 组 — 需 docker 实跑,**尚未执行**

- **B1** `docker compose -f deploy/docker-compose.yml up -d --build`(不加
  profile)启动的容器集合与本 PR 之前完全一致。
- **B2** `--profile milvus up -d` 后,`ps` 中三者 STATUS 均为 `healthy`。
- **B3** `ps` 的 PORTS 列只出现 `127.0.0.1:19530->19530/tcp`,不含 `0.0.0.0:`
  或 `:::`。
- **B4** `deploy/.env` 中 MinIO 两个变量留空时,`up` 失败并指出变量名。
- **B5** 鉴权生效:不带凭据连 19530 被拒。
- **B6** 按 §2.6 配置后 agent 侧 `/mcp` 可见 `milvus` 服务器,工具以
  `mcp__milvus__*` 出现。

### 5.1 已知残余

以下各项在提交时**未经实跑验证**,合并前须在有 docker 的环境核实。本机无
docker(`which docker` 无命中),且 milvus.io 被网络策略拦截,无法查证官方
文档。按 `CODEBASE-TOUR.md:539`「诚实标注限制」如实记录,不得以「照抄上游
示例」替代验证:

1. **镜像标签可用性**:`milvusdb/milvus:v2.5.4`、
   `quay.io/coreos/etcd:v3.5.16`、
   `minio/minio:RELEASE.2024-12-18T13-15-44Z` 未拉取验证。Milvus 钉 ≥ 2.5 是
   刻意的——新版 Go SDK(`milvus/client/v2`)以此为下限,日后做 M2（Milvus 检索后端，代号避开缓存层 L2）时不必
   再换基座。
2. **healthcheck 所用二进制是否存在于镜像内**:milvus 与 minio 的 healthcheck
   用 `curl`,etcd 用 `etcdctl`,均未在对应镜像内验证。若缺失,healthcheck 会
   一直 unhealthy 而服务本身正常——B2 会直接暴露此问题。
3. **`MINIO_ACCESS_KEY_ID` / `MINIO_SECRET_ACCESS_KEY` 的覆盖语义**（文档形式；Milvus 侧仍规范化为 minio.accesskeyid）:这两个变量
   名基于「Milvus 以 yaml 路径去点全大写的形式接受配置覆盖」的惯例,未对
   v2.5.4 核实。若不生效,Milvus 会以默认凭据连 MinIO 而失败。
4. **`user.yaml` 的增量覆盖语义**未对 v2.5.4 核实。若该版本是整份替换而非
   合并,§2.5 的做法会丢掉其余默认配置。
5. ~~`docs/integrations/milvus.md` 中的改密命令**(`milvus_cli`)**未验证其是否
   存在于 milvus 镜像内~~ **已修(评审 #497 指出)**:该镜像不包含
   `milvus_cli`;改密命令已改为宿主机 pymilvus 一行
   (`MilvusClient(...).update_password(...)`)。
6. **MCP server 来源**:**已修(评审 #497 指出)**:zilliztech 官方仓库的
   pyproject 已声明 `[project.scripts] mcp-server-milvus`,文档与 example 已
   改为官方形态 `uvx --from git+https://github.com/zilliztech/mcp-server-milvus
   mcp-server-milvus`(不经 PyPI 社区 fork),并提示安全敏感部署可钉 commit。
   `MILVUS_URI` / `MILVUS_TOKEN` 两个变量名已确认正确。(初稿误写成
   zilliztech 官方发布,后一度改为社区 fork 标注;
   commit `46403ec` 的 message 仍含该错误表述,属历史记录不改写)。零安装与官方来源现已同时满足。
7. **`docs/Security-安全设计.md:172` 的运行时服务身份校验未满足。** 该条要求
   调用方校验所连服务的进程/二进制指纹,校验失败 fail-closed。compose 层无法
   提供,属客户端实现,随 M2 的 MilvusIndex 连接路径落地。已在使用者文档的
   安全表中标为 ❌。当前安全姿态是三项中的两项。

## 6. 参考

- `docs/integrations/milvus.md` — 本 spec 的使用者侧对应文档
- `docs/Security-安全设计.md` §7 — `:172` / `:221` / `:228` 三处 Milvus 约束
- `CODEBASE-TOUR.md` §10 — 设计铁律(零第三方依赖、本地优先、诚实标注限制)
- `CONTRIBUTING.md:29-31` — 契约纪律与凭据处理
- `harness/config/plugin_entry.go` · `harness/config/expand.go` — MCP 配置契约
- `harness/mcpregistry/registry.go:288-291` — registry 可安装性判定
