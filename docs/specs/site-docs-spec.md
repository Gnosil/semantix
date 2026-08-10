# Spec：site/ 下新增 /docs 文档站（Issue 3 实现规格）

> 对应 Issue：`docs/issues/issue-03-site-docs-迁移.md`（P0/P1/P2 分级）。
> 真源约束：文档真源为仓库 `docs/` 目录；`site/` 只做读取与渲染，不复制文档副本。
>
> **状态（2026-08-10）**：本文档最初按"site/ 下完整实现 /docs 文档站"编写（P0 渲染 + P1 入口 + P2 GEO 收尾）。
> 后发现上游 PR #23（feat(site): add official website and first-party documentation）已实现
> `site/src/app/docs/` 固定页方案（`site/content/geo/` 4 篇副本 + react-markdown 渲染 + Footer/Nav 入口）。
> **本 PR（#26）收敛为增量配套**，只交付 #23 未覆盖的 GEO 收尾部分（见 §1 范围内）；P0 渲染层方案（真源直读）
> 作为后续可选演进记录在本文件 §10，不重复实现。

## 1. 目标与范围

**本 PR（#26）范围**：在现有 `site/` 内补齐 AI 引擎发现层，与 PR #23 的文档站路由配套：

- `site/src/app/sitemap.ts`：sitemap.xml（站点级入口 `/` 与 `/docs/`，不绑定具体文档 slug）；
- `site/public/llms.txt`：站点一句话说明 + 站点级链接；
- `site/public/robots.txt`：允许全部爬虫 + `Sitemap:` 声明；
- `site/next.config.ts`：`trailingSlash: true`（目录式 URL，Cloudflare Pages 可直接服务 `/docs/`）；
- `site/wrangler.toml`：Cloudflare Pages 静态导出配置；
- `docs/issues/issue-03-site-docs-迁移.md` + `docs/specs/site-docs-spec.md`：需求与规格记录。

**不在本 PR 范围**（由 PR #23 或后续负责）：/docs 路由与 Markdown 渲染、Footer/Nav 链接收敛（#23 已实现）、
文档内容改动（真源在 docs/）。

## 2. 技术选型（演进方案参考 —— 本 PR 未实现）

> 以下为 P0 渲染层的原设计方案（真源直读），因 PR #23 已实现文档站路由而**未在本 PR 落地**，保留作为后续演进的参考基线。

| 项 | 选择 | 理由 |
|---|---|---|
| 渲染 | `remark` + `remark-gfm` + `rehype-highlight` + `remark-math`/`rehype-katex`（按需） | 文档为纯 Markdown，无 JSX 需求；`next-mdx-remote` 引入动态编译，与静态导出诉求不符 |
| 路由 | App Router 动态段 `docs/[...slug]/page.tsx` + `generateStaticParams` | 静态导出（output: export）要求所有动态路由显式枚举 |
| 元数据 | 构建时解析 frontmatter；**无 frontmatter 时回退**：首行 `# ` 为标题、`git log -1 --format=%cs -- <file>` 为更新日期 | docs 现有 11 篇均无 frontmatter，禁止为渲染改动文档真源 |
| 代码高亮 | `rehype-highlight`（highlight.js），浅色主题与 landing 一致 | 静态渲染、零运行时 JS |
| 样式 | Tailwind v4 + 现有 globals.css design tokens（白 + `#2F967F`） | 复用现有视觉体系 |

## 3. 数据流与文档模型

```
docs/*.md ──> src/lib/docs.ts（构建期扫描，同步执行）
                ├─ 解析 frontmatter（若有）：title / updated / order / group
                ├─ 无 frontmatter：首行 "# " → title；git 提交日期 → updated
                └─ 生成 DocMeta[]：{ slug, title, updated, group, order }
         ──> generateStaticParams() → 每篇文档静态页面
         ──> docs 首页索引（按 group/order 排序）
         ──> sitemap.ts / llms.txt / robots.txt（P2）
```

**DocMeta 模型**（`src/lib/docs.ts`）：

```ts
export interface DocMeta {
  slug: string;        // URL slug，中文文件名保留原文（URL 编码由框架处理）
  title: string;       // frontmatter.title 或首行 H1
  updated: string;     // YYYY-MM-DD，frontmatter.updated 或 git 提交日期；无则 "unknown"
  group: string;       // frontmatter.group 或 "general"
  order: number;       // frontmatter.order 或 999
  path: string;        // docs 下相对路径，用于读正文
}
```

**slug 策略**：`docs/Agent-Infra-架构设计.md` → `/docs/Agent-Infra-架构设计`（保留中文名，URL 由 Next 编码）。Windows 路径分隔符统一为正斜杠；**段内 `.` 转 `-`**（`GEO-guide.en` → `GEO-guide-en`），避免 Next Link 将带点段判为文件扩展名而丢失尾斜杠。slug 唯一性以文件名去扩展名为准；冲突（罕见）以 group 前缀消歧。**扫描范围排除 `docs/issues/` 与 `docs/specs/`**（内部治理文档不发布到公开站点）。

## 4. 页面与组件

| 文件 | 职责 |
|---|---|
| `src/app/docs/layout.tsx` | docs 布局：复用 Nav/Footer；正文容器 wrap + max-w-[860px]（首版不做侧栏，靠 /docs 索引页分组导航；侧栏留待后续 issue） |
| `src/app/docs/page.tsx` | `/docs/` 首页：全部文档清单（分组、标题、更新日期、一句话摘要取自正文前 120 字） |
| `src/app/docs/[...slug]/page.tsx` | 文档正文页：Markdown → HTML；`generateStaticParams` 枚举全部 slug；`generateMetadata` 输出 title/description；页面顶部显示标题 + `Last updated: YYYY-MM-DD`（消除 GEO"无日期"缺陷） |
| `src/app/docs/not-found.tsx` | 未知 slug 404 页（静态导出下未枚举路径自然 404，此页兜底） |
| `src/lib/docs.ts` | 扫描 + 元数据解析 + slug 表（纯函数，可单测） |
| `src/lib/markdown.ts` | remark/rehype 管线封装：GFM、代码高亮、锚点 id、mermaid 源块 `pre` 原样保留 |

**Markdown 渲染管线**（`markdown.ts`）：

```ts
unified()
  .use(remarkParse)
  .use(remarkGfm)          // 表格、任务列表、删除线
  .use(remarkRehype)
  .use(rehypeHighlight)    // 代码高亮（pre > code[data-highlighted]）
  .use(rehypeSlug)         // 标题锚点 id
  .use(rehypeStringify)
```

mermaid 与绘图源块：渲染为普通 `pre` 代码块（保留原文），不引入浏览器端 mermaid 运行时——维持零 JS 静态站点。

## 5. 入口收敛（P1 —— 已由 PR #23 实现）

`Footer.tsx`/`Nav.tsx` 的 Docs 链接收敛到自有域名由 PR #23 完成（Nav 已加 Docs → `/docs`）。
README 与 docs 内交叉引用：仓库内相对路径链接（`docs/xxx.md`）保留不变（GitHub 场景仍可用）。

## 6. GEO 收尾（P2 —— 本 PR 落地）

- `src/app/sitemap.ts`（Next 内置 sitemap 生成，静态导出支持）：站点级入口 `/` 与 `/docs/`（不绑定具体文档 slug，避免与 #23 路由耦合）；
- `site/public/llms.txt`：站点一句话说明 + 站点级链接（AI 引擎入口）；
- `site/public/robots.txt`：`Sitemap: https://semantix.ensureok.ai/sitemap.xml`；
- `site/wrangler.toml`：Cloudflare Pages 静态导出配置。

## 7. 静态导出与构建约束

- `next.config.ts` 已有 `output: "export"`，本 PR 增加 `trailingSlash: true`——`/docs/` 目录式 URL 在 Cloudflare Pages 可直接服务，规避无扩展名 .html 解析问题；
- `npm run check`（lint + typecheck + build）为唯一构建门禁。

## 8. 验收（本 PR #26）

- [ ] `npm run check` 通过；
- [ ] `site/out/sitemap.xml` 生成，含 `/` 与 `/docs/` 两个 URL；
- [ ] `site/out/llms.txt`、`site/out/robots.txt` 生成且 robots 含 `Sitemap:` 声明；
- [ ] `next.config.ts` 含 `trailingSlash: true`，构建产物为目录式 URL；
- [ ] 部署后 `curl -s https://semantix.ensureok.ai/docs/` 返回 200（部署环节验收，不在本 spec 代码范围内）。

## 9. 非目标与后续

- 文档站全文搜索、评论区、暗色模式：后续独立 issue；
- **P0 渲染层演进**：若后续将文档站从固定页升级为全量真源直读（`docs/` 11 篇），以 §2/§3 方案为基线，同时需将 sitemap.ts 升级为枚举全部文档 URL；
- `docs.` 子域方案：仅在 `/docs` 路径因部署限制不可行时启用（决策点记录于 Issue 3）。
