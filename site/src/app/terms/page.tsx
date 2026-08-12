import type { Metadata } from "next";
import Footer from "@/components/Footer";
import Nav from "@/components/Nav";
import { siteIdentity } from "@/lib/site-identity";

export const metadata: Metadata = {
  title: "服务条款 · Semantix",
  description: "Semantix 官网服务条款：适用主体、服务性质、许可边界、免责声明与联系方式。",
  alternates: { canonical: "/terms" },
};

const sections = [
  {
    title: "1. 适用主体与服务性质",
    body: [
      `本网站（${siteIdentity.productUrl}）由 ${siteIdentity.operator.legalName}（品牌名 ${siteIdentity.operator.brandName}）运营和维护。本服务条款适用于访问和使用本网站的访问者。`,
      "Semantix 是一个开源软件项目。本网站提供项目介绍、技术文档、路线图以及指向开源代码仓库的链接，不向访问者提供任何商业软件即服务（SaaS）形态的服务。",
    ],
  },
  {
    title: "2. 开源许可边界",
    body: [
      `本项目代码采用 ${siteIdentity.licenseName} 许可发布。各版本将依照许可证条款，在规定的日期后转换为 MIT License。`,
      "使用、修改或分发项目代码前，请阅读仓库内随附的 LICENSE 文件及 FSL-1.1 许可的完整条款。代码的使用行为受对应许可证约束，与本服务条款相互独立。",
    ],
  },
  {
    title: "3. 网站内容的使用",
    body: [
      "站内文档、设计说明和路线图内容仅用于信息参考。在注明来源的前提下，可对站内内容进行引用或转载。",
      "站内描述的技术能力、性能指标和路线图属于项目当前或预期的开发状态，可能随开发进展发生变化，不构成对任何功能的交付承诺。",
    ],
  },
  {
    title: "4. 免责声明",
    body: [
      "本网站及站内所有内容按“现状”（as is）提供，运营主体不对内容的完整性、准确性或适用性作任何明示或暗示的保证。",
      "本网站内容不构成法律、商业或投资建议。如因依赖站内信息作出决策而产生损失，运营主体不承担责任，但法律法规另有规定的除外。",
    ],
  },
  {
    title: "5. 外部链接",
    body: [
      "本网站可能包含指向第三方网站（包括但不限于代码托管平台、公司官网）的链接。第三方网站的内容、隐私实践和服务由其各自运营方负责，与本服务条款无关。",
    ],
  },
  {
    title: "6. 条款更新与联系方式",
    body: [
      "运营主体可能适时更新本服务条款，更新后的条款将在本页面发布并更新生效日期。重大变更将以网站公告方式提示。",
      `如对本服务条款或网站内容有疑问，可通过站内联系页面联系运营主体，或在项目仓库 ${siteIdentity.repositoryUrl} 提交 Issue。`,
    ],
  },
] as const;

export default function TermsPage() {
  return (
    <>
      <Nav />
      <main className="min-h-[100dvh] bg-background pt-16">
        <section className="border-b border-border bg-muted/40 py-20 md:py-28">
          <div className="wrap">
            <p className="font-mono text-sm font-semibold text-accent">
              Terms of Service
            </p>
            <h1 className="mt-5 max-w-3xl text-4xl font-semibold tracking-tight text-foreground md:text-6xl">
              服务条款
            </h1>
            <p className="mt-6 max-w-2xl text-lg leading-8 text-muted-foreground">
              本条款适用于访问和使用 Semantix 官网（{siteIdentity.productUrl}）的所有访问者。
            </p>
            <div className="mt-5 flex flex-wrap gap-x-8 gap-y-2 font-mono text-xs text-muted-foreground">
              <p>生效日期：{siteIdentity.lastUpdated}</p>
              <p>运营主体：{siteIdentity.operator.legalName}</p>
            </div>
          </div>
        </section>

        <section className="wrap py-16 md:py-24">
          <div className="mx-auto max-w-3xl">
            <div className="space-y-12">
              {sections.map((section) => (
                <section key={section.title}>
                  <h2 className="text-xl font-semibold tracking-tight text-foreground">
                    {section.title}
                  </h2>
                  <div className="mt-4 space-y-4">
                    {section.body.map((paragraph) => (
                      <p key={paragraph} className="leading-7 text-muted-foreground">
                        {paragraph}
                      </p>
                    ))}
                  </div>
                </section>
              ))}
            </div>
          </div>
        </section>
      </main>
      <Footer />
    </>
  );
}
