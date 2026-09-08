import type { Metadata } from "next";
import Link from "next/link";
import { documentSections, documents } from "@/lib/docs";

export const metadata: Metadata = {
  title: "文档 | Semantix",
  description: "安装、配置并理解 Semantix 的切片、检索、缓存、调度与 Gateway。",
};

const learningPaths = [
  {
    index: "01",
    title: "第一次使用",
    description: "从安装开始，跑通提取、检索和注入的最小闭环。",
    href: "/docs/quickstart",
    link: "开始安装",
  },
  {
    index: "02",
    title: "接入现有 Agent",
    description: "选择 Semantix Agent、Claude Code 或自定义 harness 的最小接线。",
    href: "/docs/agent-integration",
    link: "选择接入方式",
  },
  {
    index: "03",
    title: "理解复用边界",
    description: "区分语义检索、L2 注入与需要额外验证的 L3 结果复用。",
    href: "/docs/slices-and-cache",
    link: "理解三级缓存",
  },
  {
    index: "04",
    title: "验证与排障",
    description: "用 verify、usage、dashboard 和 doctor 检查真实运行状态。",
    href: "/docs/verify-observe",
    link: "查看验收路径",
  },
] as const;

export default function DocsIndexPage() {
  return (
    <div className="px-6 py-10 md:px-10 md:py-14 lg:px-14">
      <div className="mx-auto max-w-5xl">
        <p className="font-mono text-xs font-semibold text-accent">SEMANTIX 文档</p>
        <h1 className="mt-5 max-w-4xl text-4xl font-semibold tracking-tight text-foreground md:text-6xl">
          从第一次运行，到理解内核边界。
        </h1>
        <p className="mt-6 max-w-3xl text-lg leading-8 text-muted-foreground">
          文档按使用顺序组织，并直接对应当前仓库中的命令、模块、测试和验收报告。先完成最小闭环，再按任务进入左侧目录。
        </p>

        <section className="mt-12 grid gap-px overflow-hidden rounded-xl border border-border bg-border sm:grid-cols-2 xl:grid-cols-4" aria-labelledby="learning-paths">
          <h2 id="learning-paths" className="sr-only">推荐阅读路径</h2>
          {learningPaths.map((path) => (
            <article key={path.index} className="flex min-h-64 flex-col bg-background p-6">
              <span className="font-mono text-xs font-semibold text-accent">{path.index}</span>
              <h3 className="mt-8 text-lg font-semibold text-foreground">{path.title}</h3>
              <p className="mt-3 text-sm leading-6 text-muted-foreground">{path.description}</p>
              <Link href={path.href} className="mt-auto pt-7 text-sm font-medium text-accent hover:underline hover:underline-offset-4">
                {path.link} <span aria-hidden="true">→</span>
              </Link>
            </article>
          ))}
        </section>

        <section className="mt-16 border-t border-border pt-10" aria-labelledby="document-map">
          <div className="max-w-2xl">
            <p className="font-mono text-xs font-semibold text-accent">DOCUMENT MAP</p>
            <h2 id="document-map" className="mt-3 text-2xl font-semibold tracking-tight md:text-3xl">按问题查文档</h2>
            <p className="mt-4 text-sm leading-7 text-muted-foreground">
              目录结构参考成熟 coding agent 的任务式文档：开始使用、核心工作流、理解机制、集成运维，最后再读项目背景。
            </p>
          </div>

          <div className="mt-9 grid gap-x-12 gap-y-10 md:grid-cols-2">
            {documentSections.map((section) => {
              const sectionDocuments = documents.filter((document) => document.section === section);
              return (
                <div key={section}>
                  <h3 className="border-b border-border pb-3 text-sm font-semibold text-foreground">{section}</h3>
                  <ul className="mt-3 divide-y divide-border/70">
                    {sectionDocuments.map((document) => (
                      <li key={document.slug}>
                        <Link href={`/docs/${document.slug}`} className="group flex items-start justify-between gap-5 py-4">
                          <span>
                            <span className="block text-sm font-medium text-foreground transition-colors group-hover:text-accent">{document.title}</span>
                            <span className="mt-1 block text-xs leading-5 text-muted-foreground">{document.navDescription}</span>
                          </span>
                          <span aria-hidden="true" className="mt-0.5 text-accent">→</span>
                        </Link>
                      </li>
                    ))}
                  </ul>
                </div>
              );
            })}
          </div>
        </section>

        <section className="mt-16 border-t border-border pt-10">
          <h2 className="text-2xl font-semibold tracking-tight">验证与来源</h2>
          <p className="mt-4 max-w-3xl text-sm leading-7 text-muted-foreground">
            每篇文档都标注证据等级和仓库来源。设计说明、仓库测试、合成回放与生产数据不会被混写成同一种结论。
          </p>
          <Link href="/benchmarks" className="mt-5 inline-block text-sm font-medium text-accent underline underline-offset-4">
            查看证据与复现记录 ↗
          </Link>
        </section>
      </div>
    </div>
  );
}
