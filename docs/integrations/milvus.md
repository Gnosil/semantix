# Semantix × Milvus 集成

> 本文面向想把 Semantix 与 Milvus 一起用起来的使用者。
> 相关：`deploy/docker-compose.yml`（部署）、`semantix-agent.example.toml`（MCP 配置）。

## 能力边界（请先读这一节）

Semantix 与 Milvus 在这套集成里**职责正交**，把它们放在一起不会让任何一方变快：

| | 负责什么 | 不负责什么 |
|---|---|---|
| **Milvus** | 向量检索：编排器的 RAG 语料、agent 通过 MCP 做的向量查询 | 不参与 Semantix 的缓存判定 |
| **Semantix** | LLM 侧跨会话记忆：L2 语义注入、L3 结果复用、前缀卫生 | 不把 Milvus 当检索后端 |

**当前版本中，semantix 二进制不使用 Milvus 做自己的切片检索。** `[retrieval] retriever` 的合法值仍是 `bm25` / `vector` / `hybrid`；填 `milvus` 会被配置校验拒绝。用 Milvus 替换 Semantix 的 dense 检索路属于另一件事（内部代号 M2，注意区别于缓存层 L2），尚未实现，且设有量化触发门槛——在切片库规模达到 ~5×10⁴ 之前，内存暴力检索（5000 片 × 1536 维 ≈ 毫秒级）比引入一次网络往返更快。

所以这套集成**实际带来的**是三件事：

1. 一个开箱即用、已鉴权、已钉版本的本地 Milvus 实例（`docker-compose.milvus.yml`）
2. agent 通过 MCP 获得向量检索与 collection 管理能力（真实的新增能力）
3. 编排器（Dify / Langflow / n8n / FastGPT）可同时接 Milvus 做检索、接 semantix-gateway 做 LLM 缓存

---

## 一、启动 Milvus

三个服务（milvus / etcd / minio）都在 `milvus` profile 下，**默认不启动**——已有部署的行为不受任何影响。

```bash
cp deploy/.env.example deploy/.env
# 编辑 deploy/.env，至少填 MINIO_ROOT_USER / MINIO_ROOT_PASSWORD
#   生成建议：openssl rand -hex 16

docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.milvus.yml \
  --profile milvus up -d
```

不加后一个 `-f` 就是原来的两服务部署，一字未变。

### 验证

```bash
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.milvus.yml --profile milvus ps
```

预期：`milvus` / `etcd` / `minio` 三行 STATUS 均为 `healthy`（Milvus 冷启动约 30–90s）。

PORTS 列应当只出现 `127.0.0.1:19530->19530/tcp`，**不应出现** `0.0.0.0:` 或 `:::`——后者意味着向量库被暴露到局域网。

### 首次启动后必须改密

Milvus 已启用鉴权（`deploy/milvus-user.yaml`），但镜像会创建默认账号 `root` / `Milvus`。**这个默认密码众所周知，必须立刻改掉**：

镜像里没有交互式客户端（`milvus_cli` 不在 `milvusdb/milvus` 镜像内），用宿主机
pymilvus 一行改密：

```bash
uvx --with pymilvus python -c \
  'from pymilvus import MilvusClient; MilvusClient(uri="http://127.0.0.1:19530", token="root:Milvus").update_password("root", "Milvus", "<新密码>")'
```

（或用任一 Milvus 客户端连上后执行等价改密。）改后的密码由 Milvus 自己持久化在 etcd 中，不经环境变量——这也是 `deploy/.env` 里没有 Milvus 密码项的原因。

---

## 二、让 agent 用上 Milvus（MCP）

这一步给 agent 带来**真实的新增能力**：向量检索与 collection 管理工具。

前置：`uv`（提供 `uvx`）、上一节的 Milvus 已 healthy。

官方 server 是 zilliztech/mcp-server-milvus，其 `pyproject.toml` 已声明
`[project.scripts] mcp-server-milvus`，因此 `uvx` 直接以官方形态零安装运行
（uvx 按 `mcp-server-milvus` 从源码仓库解析，不经过 PyPI 上的社区 fork）：

编辑 `~/.semantix/semantix-agent.toml`（样板见 `semantix-agent.example.toml`）：

```toml
[[plugins]]
name    = "milvus"
type    = "stdio"
command = "uvx"
args    = ["--from", "git+https://github.com/zilliztech/mcp-server-milvus", "mcp-server-milvus"]
env     = { MILVUS_URI = "${MILVUS_URI:-http://127.0.0.1:19530}", MILVUS_TOKEN = "${MILVUS_TOKEN}" }
```

> 供应链提示：`uvx --from git+…` 在首次运行时按锁定分支拉取并构建官方仓库。
> 对安全敏感的部署可以改为固定 commit（`git+https://github.com/zilliztech/mcp-server-milvus@<sha>`）。

`${VAR}` 会从环境展开，所以凭据不落在配置文件里。改密后把 token 放进环境：

```bash
export MILVUS_TOKEN="root:<你的新密码>"
```

重启 agent 后用 `/mcp` 查看连接状态，工具应以 `mcp__milvus__*` 形式出现在工具列表里。

> **为什么要手写这段配置**：公开 MCP registry 的一键安装对 `mcp-server-milvus` 不可用。Semantix 的 registry 解析器会把任何需要环境变量或参数的条目标记为不可安装（`harness/mcpregistry/registry.go`），`mcp-server-milvus` 必须有 `MILVUS_URI`，因此命中该规则，`UnavailableReason` 为 `package requires environment variables or arguments`。这是刻意的保守策略，不是缺陷——手写配置是预期路径。

---

## 三、编排器：Milvus 做检索，Semantix 做缓存

Dify / Langflow / n8n / FastGPT / AnythingLLM 等都支持自定义 OpenAI base URL，而 `semantix-gateway` 是 OpenAI 兼容网关。于是：

```
编排器
   ├─ 向量库  → Milvus            （不变，继续承担 RAG 检索）
   └─ LLM     → semantix-gateway  （改一行 base URL）
                    └→ 上游 LLM
```

编排器侧每一通 LLM 调用自动获得 L2 语义注入、L3 结果复用与前缀卫生，而 RAG 检索链路完全不受影响。两者互不侵入。

`semantix dashboard` 可以看到 L2/L3 命中数随重复查询增长。

---

## 四、安全

对照 `docs/Security-安全设计.md` §7 的三项要求：

| 要求 | 状态 | 说明 |
|---|---|---|
| 仅绑定回环地址（`:172` / `:228`） | ✅ | Milvus 显式绑 `127.0.0.1:19530`；etcd / minio 只在 compose 内网，不发布 |
| 启用本地认证（`:172` / `:221`） | ✅ | `deploy/milvus-user.yaml` 开 `authorizationEnabled`；MinIO 凭据无默认值，未设即启动失败 |
| 运行时服务身份校验（`:172`） | ❌ **未满足** | compose 层无法提供。该项要求调用方校验所连服务的进程/二进制指纹，属客户端实现，随 M2（MilvusIndex）落地 |

第三项如实标注为未满足。在本机存在不受信进程的威胁模型下，回环绑定 + 鉴权挡得住网络侧，挡不住本机进程冒充服务端——这是当前形态的已知边界。

其他注意事项：

- `deploy/.env` 已被 `.gitignore` 忽略（`*.env`），`deploy/.env.example` 通过 `!*.env.example` 显式保留。**不要**把凭据写进 `docker-compose.yml` 或任何 `.toml`。
- 镜像版本全部钉定。升级前请查对应版本的 CVE 公告；Milvus 需保持 `>= 2.5`。
- 向量库里会存放你的语料内容。它只监听回环，但仍是本机上的一份明文副本。

---

## 五、常见问题

**`up` 之后 milvus 反复重启** — 多为 etcd 或 minio 未就绪。compose 已用 `condition: service_healthy` 等待两者，若仍失败先看 `docker compose logs etcd minio`。

**`MINIO_ROOT_USER` 未设置导致启动失败** — 这是刻意行为（`${VAR:?}`），避免弱口令部署。填 `deploy/.env` 即可。

**MCP 工具连不上 Milvus** — 确认 PORTS 列有 `127.0.0.1:19530->19530/tcp`。MCP server 以宿主机子进程运行，不在 compose 网络内，仅 `expose` 是连不上的。

**能不能设 `retriever = "milvus"`** — 不能，见开头能力边界。会得到一条只列出 `bm25, vector, hybrid` 的配置错误。

---

## 六、状态与后续

| 能力 | 状态 |
|---|---|
| Milvus 本地基座（compose + 鉴权 + 钉版本） | ✅ 本文 |
| agent 通过 MCP 使用 Milvus | ✅ 本文 |
| 编排器接法（Milvus 检索 + Semantix 缓存） | ✅ 本文 |
| Semantix 把 Milvus 作为检索后端（M2） | ⏳ 未实现，设量化门槛 |
| 把 Semantix 暴露为 MCP server 供生态消费 | ⏳ 规划中 |
| 运行时服务身份校验 | ⏳ 随 M2 |

M2 的触发门槛（三者占其一才启动）：切片库稳定超过 ~5×10⁴ 片；出现真实的多进程并发写同一库；检索 P99 超过单次工具调用的感知阈值。在此之前引入 Milvus 作为检索后端是净负债——多一次网络往返、三个常驻容器，换不来可测的检索提升。
