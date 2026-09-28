import type { Metadata } from "next";
import Footer from "@/components/Footer";
import Nav from "@/components/Nav";
import ProtectedEmail from "@/components/ProtectedEmail";
import { siteIdentity } from "@/lib/site-identity";

export const metadata: Metadata = {
  title: "联系 Semantix",
  description: "联系 Semantix 官网运营与维护团队。",
  alternates: { canonical: "/contact" },
};

export default function ContactPage() {
  return (
    <>
      <Nav />
      <main className="min-h-[100dvh] bg-background pt-16">
        <section className="border-b border-border bg-muted/40 py-20 md:py-28">
          <div className="wrap">
            <p className="font-mono text-sm font-semibold text-accent">Contact</p>
            <h1 className="mt-5 max-w-3xl text-4xl font-semibold tracking-tight text-foreground md:text-6xl">
              联系 Semantix
            </h1>
            <p className="mt-6 max-w-2xl text-lg leading-8 text-muted-foreground">
              项目合作、网站内容反馈与运营咨询，由 {siteIdentity.operator.brandName} 统一处理。
            </p>
          </div>
        </section>

        <section className="wrap py-16 md:py-24">
          <div className="grid max-w-4xl gap-8 md:grid-cols-2">
            <section className="rounded-2xl border border-border bg-card p-6 md:p-8">
              <p className="font-mono text-xs font-semibold text-accent">官方邮箱</p>
              <h2 className="mt-3 text-xl font-semibold text-foreground">邮件联系</h2>
              <p className="mt-3 leading-7 text-muted-foreground">
                请复制下面的邮箱地址发送邮件。邮件中请说明事项、相关页面或仓库链接。
              </p>
              <ProtectedEmail className="mt-6 block break-all font-mono text-sm font-semibold text-foreground" />
            </section>

            <section className="rounded-2xl border border-border bg-card p-6 md:p-8">
              <p className="font-mono text-xs font-semibold text-accent">开源协作</p>
              <h2 className="mt-3 text-xl font-semibold text-foreground">GitHub Issue</h2>
              <p className="mt-3 leading-7 text-muted-foreground">
                可复现的缺陷、功能建议和文档问题，请优先通过公开 Issue 提交，便于社区跟踪。
              </p>
              <a
                href={`${siteIdentity.repositoryUrl}/issues/new`}
                target="_blank"
                rel="noopener noreferrer"
                className="mt-6 inline-flex rounded-md border border-border px-4 py-2 text-sm font-semibold text-foreground transition-colors hover:border-accent hover:text-accent active:translate-y-px"
              >
                创建 Issue ↗
              </a>
            </section>
          </div>
        </section>
      </main>
      <Footer />
    </>
  );
}
