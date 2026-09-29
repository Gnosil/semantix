import type { Metadata } from "next";
import { faqItems } from "@/lib/faq-items";
import { siteIdentity } from "@/lib/site-identity";
import Link from "next/link";

export const metadata: Metadata = {
  title: "常见问题 | Semantix 文档",
  description: "关于 Semantix 定位、语义缓存、实验性参数反馈和项目参与方式的常见问题。",
  alternates: { canonical: "/docs/faq" },
};

export default function FaqPage() {
  const faqJsonLd = {
    "@context": "https://schema.org",
    "@type": "FAQPage",
    "@id": `${siteIdentity.productUrl}/docs/faq#faq`,
    mainEntity: faqItems.map((item) => ({
      "@type": "Question",
      name: item.question,
      acceptedAnswer: {
        "@type": "Answer",
        text: item.answer,
      },
    })),
  };

  return (
    <div className="px-6 py-10 md:px-10 md:py-14 lg:px-14">
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{
          __html: JSON.stringify(faqJsonLd).replace(/</g, "\\u003c"),
        }}
      />
      <div className="mx-auto max-w-4xl">
        <p className="font-mono text-xs font-semibold text-accent">FAQ 常见问题</p>
        <h1 className="mt-5 text-4xl font-semibold tracking-tight md:text-5xl">
          关于 Semantix 的五个常见问题。
        </h1>
        <p className="mt-5 max-w-2xl text-lg leading-8 text-muted-foreground">
          从项目定位到三级语义缓存，再到参与开发所需的验证要求。
        </p>
        <p className="mt-5 max-w-3xl border-l-2 border-accent bg-muted/40 px-5 py-4 text-sm leading-6 text-muted-foreground">
          FAQ 的回答以当前仓库代码、测试和公开限制为准；它不是生产性能承诺。需要查看输入、命令与原始输出时，请阅读 <Link href="/benchmarks" className="font-medium text-foreground underline underline-offset-4 hover:text-accent">证据与复现记录 ↗</Link>。
        </p>

        <div className="mt-12 max-w-3xl border-t border-border">
          {faqItems.map((item) => (
            <section key={item.question} className="border-b border-border py-7">
              <h2 className="text-lg font-semibold tracking-tight">{item.question}</h2>
              <p className="mt-3 text-sm leading-7 text-muted-foreground">{item.answer}</p>
            </section>
          ))}
        </div>
      </div>
    </div>
  );
}
