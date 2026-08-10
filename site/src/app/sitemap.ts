import type { MetadataRoute } from "next";

const BASE = "https://semantix.ensureok.ai";

// output: "export" 下 sitemap route 必须显式静态化
export const dynamic = "force-static";

/**
 * sitemap.xml：主站 + /docs 索引入口。
 * 具体文档 URL 由 docs 渲染层（见 PR #23 的 /docs 固定页方案）负责，
 * 本文件只声明站点级入口，避免与任何 docs 路由实现耦合。
 */
export default function sitemap(): MetadataRoute.Sitemap {
  return [
    { url: `${BASE}/`, lastModified: new Date() },
    { url: `${BASE}/docs/`, lastModified: new Date() },
  ];
}
