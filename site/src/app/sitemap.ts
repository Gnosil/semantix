import type { MetadataRoute } from "next";
import { documents } from "@/lib/docs";
import { listBlogPosts } from "@/lib/blog";
import { siteIdentity } from "@/lib/site-identity";

const BASE = siteIdentity.productUrl;

export const dynamic = "force-static";

export default function sitemap(): MetadataRoute.Sitemap {
  const lastModified = new Date(siteIdentity.lastUpdated);
  const blogPosts: MetadataRoute.Sitemap = listBlogPosts().map((post) => ({
    url: `${BASE}/blog/${post.slug}`,
    lastModified: new Date(post.updated),
    changeFrequency: "monthly",
    priority: 0.7,
  }));

  return [
    { url: `${BASE}/`, lastModified, changeFrequency: "weekly", priority: 1 },
    { url: `${BASE}/about`, lastModified, changeFrequency: "monthly", priority: 0.8 },
    { url: `${BASE}/contact`, lastModified, changeFrequency: "monthly", priority: 0.7 },
    { url: `${BASE}/docs`, lastModified, changeFrequency: "weekly", priority: 0.9 },
    ...documents.map((document) => ({
      url: `${BASE}/docs/${document.slug}`,
      lastModified: new Date(document.lastUpdated),
      changeFrequency: "weekly" as const,
      priority: 0.8,
    })),
    { url: `${BASE}/docs/faq`, lastModified, changeFrequency: "monthly", priority: 0.7 },
    { url: `${BASE}/benchmarks`, lastModified, changeFrequency: "monthly", priority: 0.8 },
    { url: `${BASE}/evidence/methodology`, lastModified, changeFrequency: "monthly", priority: 0.7 },
    { url: `${BASE}/evidence/semantix-2026-08-12-windows.json`, lastModified, changeFrequency: "monthly", priority: 0.4 },
    { url: `${BASE}/blog`, lastModified, changeFrequency: "weekly", priority: 0.9 },
    ...blogPosts,
    ...["gnosil", "radianceded", "jh10724-dotcom", "allenli1233"].map((name) => ({
      url: `${BASE}/authors/${name}`,
      lastModified,
      changeFrequency: "monthly" as const,
      priority: 0.5,
    })),
    { url: `${BASE}/terms`, lastModified, changeFrequency: "yearly", priority: 0.3 },
    { url: `${BASE}/privacy`, lastModified, changeFrequency: "yearly", priority: 0.3 },
  ];
}
