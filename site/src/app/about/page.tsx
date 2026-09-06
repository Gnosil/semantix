import type { Metadata } from "next";
import Link from "next/link";
import Footer from "@/components/Footer";
import Nav from "@/components/Nav";
import { siteIdentity } from "@/lib/site-identity";
import { maintainersJsonLd } from "@/lib/content-authors";

export const metadata: Metadata = {
  title: "关于 Semantix",
  description: "了解 Semantix 的项目定位、运营主体、维护关系与联系渠道。",
  alternates: { canonical: "/about" },
};

const operatorDetails = [
  ["运营主体", siteIdentity.operator.legalName],
  ["品牌名称", siteIdentity.operator.brandName],
  ["英文名称", siteIdentity.operator.englishName],
] as const;

export default function AboutPage() {
  const aboutJsonLd = {
    "@context": "https://schema.org",
    "@type": "AboutPage",
    "@id": `${siteIdentity.productUrl}/about#about`,
    url: `${siteIdentity.productUrl}/about`,
    name: "关于 Semantix",
    dateModified: siteIdentity.lastUpdated,
    publisher: { "@id": `${siteIdentity.operator.url}#organization` },
    mainEntity: maintainersJsonLd["@graph"],
  };

  return (
    <>
      <Nav />
      <main className="min-h-[100dvh] bg-background pt-16">
        <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(aboutJsonLd).replace(/</g, "\\u003c") }} />
        <section className="border-b border-border bg-muted/40 py-20 md:py-28">
          <div className="wrap">
            <p className="font-mono text-sm font-semibold text-accent">
              About Semantix
            </p>
            <h1 className="mt-5 max-w-3xl text-4xl font-semibold tracking-tight text-foreground md:text-6xl">
              关于 Semantix
            </h1>
            <p className="mt-6 max-w-2xl text-lg leading-8 text-muted-foreground">
              Semantix 是一个面向智能体运行时的开源优化内核，探索跨会话语义复用、调度与安全预取。
              项目目前处于架构与增量实现阶段。
            </p>
            <p className="mt-5 font-mono text-xs text-muted-foreground">
              最后更新：{siteIdentity.lastUpdated}
            </p>
          </div>
        </section>

        <section className="wrap py-16 md:py-24">
          <div className="grid gap-12 lg:grid-cols-[minmax(0,1.15fr)_minmax(18rem,0.85fr)] lg:gap-20">
            <div>
              <h2 className="text-3xl font-semibold tracking-tight text-foreground">
                运营主体
              </h2>
              <p className="mt-5 max-w-2xl leading-8 text-muted-foreground">
                Semantix 官网及项目运营由确石智能负责。工商注册主体如下，品牌名称不能替代法律主体名称。
              </p>

              <dl className="mt-10 border-t border-border">
                {operatorDetails.map(([label, value]) => (
                  <div
                    key={label}
                    className="grid gap-2 border-b border-border py-5 sm:grid-cols-[8rem_1fr] sm:gap-6"
                  >
                    <dt className="font-mono text-sm text-muted-foreground">{label}</dt>
                    <dd className="font-medium text-foreground">{value}</dd>
                  </div>
                ))}
              </dl>
            </div>

            <aside className="rounded-2xl border border-border bg-card p-6 md:p-8">
              <h2 className="text-xl font-semibold text-foreground">官方渠道</h2>
              <div className="mt-6 space-y-6 text-sm">
                <div>
                  <p className="font-mono text-xs text-muted-foreground">公司官网</p>
                  <a
                    href={siteIdentity.operator.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="mt-2 inline-block break-all font-medium text-foreground underline decoration-border underline-offset-4 transition-colors hover:text-accent hover:decoration-accent"
                  >
                    {siteIdentity.operator.url}
                  </a>
                </div>
                <div>
                  <p className="font-mono text-xs text-muted-foreground">联系方式</p>
                  <Link
                    href="/contact"
                    className="mt-2 inline-block break-all font-medium text-foreground underline decoration-border underline-offset-4 transition-colors hover:text-accent hover:decoration-accent"
                  >
                    站内联系页面
                  </Link>
                </div>
                <div>
                  <p className="font-mono text-xs text-muted-foreground">代码仓库</p>
                  <a
                    href={siteIdentity.repositoryUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="mt-2 inline-block break-all font-medium text-foreground underline decoration-border underline-offset-4 transition-colors hover:text-accent hover:decoration-accent"
                  >
                    {siteIdentity.repositoryUrl}
                  </a>
                </div>
              </div>
            </aside>
          </div>
        </section>

        <section className="border-y border-border bg-muted/40">
          <div className="wrap py-16 md:py-20">
            <h2 className="text-3xl font-semibold tracking-tight text-foreground">
              项目与主体的关系
            </h2>
            <div className="mt-10 grid gap-10 md:grid-cols-2 md:gap-16">
              <div>
                <h3 className="font-semibold text-foreground">运营与维护</h3>
                <p className="mt-3 leading-7 text-muted-foreground">
                  {siteIdentity.operator.legalName} 负责 Semantix 官网内容、项目运营和官方联系渠道。
                </p>
              </div>
              <div>
                <h3 className="font-semibold text-foreground">开源协作</h3>
                <p className="mt-3 leading-7 text-muted-foreground">
                  当前代码托管于 Gnosil/semantix。GitHub 命名空间用于代码协作，不代表网站运营主体。
                </p>
              </div>
              <div>
                <h3 className="font-semibold text-foreground">许可方式</h3>
                <p className="mt-3 leading-7 text-muted-foreground">
                  项目当前采用 {siteIdentity.licenseName} 许可。各版本会依照许可证条款在规定日期转换为 MIT License。
                </p>
              </div>
              <div>
                <h3 className="font-semibold text-foreground">继续了解</h3>
                <p className="mt-3 leading-7 text-muted-foreground">
                  技术定位、架构边界和路线图记录在站内文档中。
                </p>
                <Link
                  href="/docs"
                  className="mt-4 inline-flex rounded-md border border-border px-4 py-2 text-sm font-semibold text-foreground transition-colors hover:border-accent hover:text-accent active:translate-y-px"
                >
                  阅读文档
                </Link>
              </div>
            </div>
          </div>
        </section>
      </main>
      <Footer />
    </>
  );
}
