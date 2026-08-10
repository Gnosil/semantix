import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  /* Static export: this is a pure landing page with no API routes or
     dynamic data; Cloudflare Pages / any static host can serve it. */
  output: "export",
  /* 目录式 URL（/docs/xxx/ → out/docs/xxx/index.html），任意静态托管均可直接服务，
     规避 Cloudflare Pages 对无扩展名 .html 文件的解析问题（spec §7）。 */
  trailingSlash: true,
  images: { unoptimized: true },
};

export default nextConfig;
