# Docs 内容资产迁回自有域名：site/ 下新增 /docs 文档站（GEO Quick Win #3）

## 背景与动机

GEO 审计（2026-08-10，Citon）Quick Wins 第 3 条：

> Content assets (docs/blog/FAQ) live on rented domains (github.com) — authority accrues to those hosts, not to your domain. Move them onto your own domain (e.g. `/docs` or `docs.`).

当前唯一的技术文档（架构设计、安全设计、流程树、GEO 指南、events、M0 报告等）全部住在 `github.com/Gnosil/semantix/tree/main/docs`，主站 `semantix.ensureok.ai` 页脚 "Docs" 按钮也直接指向该 GitHub URL。后果：

1. **权威归属错位**：AI 引擎与搜索引擎把 docs 内容的权威记到 github.com 名下，`semantix.ensureok.ai` 域名在 AI 眼里"无可引用内容"，AI Citability 仅 24/100；
2. **控制权风险**：租用域名受 GitHub 规则约束（可关停/改版/限流），且无法 301 控制；
3. **连带拖累其他 Quick Win**：补了 schema/法人信息后，AI 顺链接验证时发现内容仍在站外，信任度打折。

上游 `main`（d2839e6）已含 `site/`（Next.js 16 + Tailwind v4 + shadcn/ui，`output: "export"` 静态导出 + Cloudflare Pages 部署文档 + 子域名方案 `semantix.ensureok.ai`）。**方案 A = 在现有 `site/` 下新增 `/docs` 路由**，直接复用既有站点框架与部署链路，不另起 VitePress/mdBook。

## 现状问题

1. `site/` 只有单页 landing（`src/app/page.tsx`），无任何 docs 路由；
2. `site/package.json` 无 Markdown/MDX 渲染依赖（next/remark/rehype 均未引入）；
3. `docs/` 下文档为中文 Markdown（含中文文件名），需要确定静态导出下 App Router 的渲染与路由方案；
4. 主站 Footer/Nav 的 Docs 链接指向 `https://github.com/Gnosil/semantix/tree/main/docs`（`Footer.tsx` 内）；
5. 无 sitemap.xml / llms.txt / robots.txt Sitemap 声明（连带 Week 1 缺失项）。

## 方案：site/ 下新增 /docs 文档站

```
site/src/app/docs/
  layout.tsx            # docs 布局（侧栏索引 + 正文）
  page.tsx              # /docs 首页（文档清单索引）
  [...slug]/page.tsx    # 动态路由渲染各 Markdown（静态导出需 generateStaticParams）
site/src/lib/docs.ts    # 文档元数据解析（frontmatter: title/updated/order）+ slug 表
docs/                   # 文档真源保持不动（site 构建时读取，或构建脚本拷贝）
```

渲染链路（静态导出约束下）：
```
docs/*.md --frontmatter 解析--> metadata（title/updated/order）
         --remark/rehype--> HTML（代码高亮、标题锚点、表格、mermaid 源块原样保留）
         --App Router 动态路由+generateStaticParams--> site/out/docs/...
```

### 任务分级

- **P0 — /docs 核心闭环**（本周）
  - P0.1 引入渲染依赖：`remark`、`remark-frontmatter`、`remark-gfm`、`rehype-highlight` 等（或 `next-mdx-remote`，spec 定夺）；
  - P0.2 `src/lib/docs.ts`：扫描 `docs/*.md` 生成 slug 表（含中文文件名 URL 编码策略）+ frontmatter 解析；
  - P0.3 `src/app/docs/[...slug]/page.tsx` + `generateStaticParams`：全部文档静态导出为 HTML；
  - P0.4 `src/app/docs/page.tsx` + `layout.tsx`：docs 首页索引 + 布局（Nav/Footer 复用现有组件）；
  - P0.5 验收：`npm run build` 静态导出 `site/out/docs/**` 全部生成，本地预览可访问。

- **P1 — 入口迁移与链接收敛**
  - P1.1 `Footer.tsx`：Docs 链接 `github.com/.../docs` → `/docs/`；
  - P1.2 README 与 docs 内交叉引用指向自有域（仓库内相对路径保留，站外权威入口改）；
  - P1.3 docs 首页显示每篇文档 `Last updated`（frontmatter 提供），消除"无日期"GEO 缺陷。

- **P2 — GEO 收尾（连带 Week 1）**
  - P2.1 `sitemap.xml`：包含 `/docs/**` 全部 URL；
  - P2.2 `llms.txt`：站点说明 + docs 入口清单；
  - P2.3 `robots.txt`：`Sitemap:` 声明（静态导出放 `site/public/`）。

## 风险与对策

1. **静态导出 + 动态路由**：App Router 静态导出要求所有动态段显式枚举 → `generateStaticParams` 从 docs 扫描结果生成，缺一页即构建失败（fail-closed）；
2. **中文文件名 URL**：中文 slug 直接进 URL 会被编码，锚点/索引需一致处理（统一用 frontmatter 显式 slug 或 URL 编码规范，spec 定夺）——禁止出现"GitHub 能打开、自己域名 404"的双标准；
3. **文档真源漂移**：docs 被上游频繁更新（本 pull 新增 GEO.md 等 4 篇）→ site 构建从 `docs/` 现读现渲染，不复制副本；文档增删只影响索引与 sitemap 自动生成；
4. **渲染 fidelity**：现有文档含 mermaid 源、表格、代码块 → 至少代码高亮 + GFM 表格 + 标题锚点，mermaid 源块原样展示（不引入重 JS 运行时，保持静态导出）；
5. **部署绑定**：`/docs` 与 landing 同域同站，Cloudflare Pages 已有部署链路（site/README.md），无新增基础设施。

## 验收标准

- [ ] `npm run check`（lint + typecheck + build）通过，`site/out/docs/` 覆盖 docs/ 下全部文档；
- [ ] 本地 `npm run dev` 访问 `/docs/` 与每篇 `/docs/<slug>` 渲染正常（中文标题、代码块、表格、锚点）；
- [ ] 主站 Footer Docs 链接指向 `/docs/`，页面无 `github.com/Gnosil/semantix/tree/main/docs` 残留链接；
- [ ] sitemap.xml / llms.txt / robots.txt 生成且含 `/docs/**`；
- [ ] `curl -s https://semantix.ensureok.ai/docs/` 返回 200（部署后验收）。

## 参考

- GEO 审计报告 `clipboard-20260810-120906.211687-000003.pdf`：Quick Wins #3、Week 1（sitemap/llms.txt/robots）
- `site/README.md`：现有 Next.js 16 静态导出 + Cloudflare Pages 部署链路
- 上游 `dbe7ad6 site: static export mode + Cloudflare Pages deployment docs`
- `docs/issues/issue-01-l3-三段阈值.md`、`issue-02-l3-两级验证.md`：本项目 issue 格式惯例
