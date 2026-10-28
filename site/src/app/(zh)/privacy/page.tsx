import type { Metadata } from "next";
import Footer from "@/components/Footer";
import Nav from "@/components/Nav";
import { siteIdentity } from "@/lib/site-identity";

export const metadata: Metadata = {
  title: "隐私政策 · Semantix",
  description: "Semantix 官网隐私政策：数据收集、使用、保存方式、第三方服务与联系渠道。",
  alternates: { canonical: "/privacy" },
};

const sections = [
  {
    title: "1. 适用范围",
    body: [
      `本隐私政策适用于访问和使用本网站（${siteIdentity.productUrl}）的行为，由 ${siteIdentity.operator.legalName}（品牌名 ${siteIdentity.operator.brandName}）运营和维护。`,
    ],
  },
  {
    title: "2. 我们收集哪些数据",
    body: [
      "本网站是一个纯静态展示站点：不提供账号注册、不提供在线表单、不设置 Cookie、不使用任何第三方分析或追踪脚本（包括但不限于统计、广告、行为追踪类服务）。",
      "因此，本站不会主动收集、存储或处理访问者的个人数据，例如姓名、邮箱、联系方式、浏览行为画像等。",
    ],
  },
  {
    title: "3. 第三方服务与访问日志",
    body: [
      "本网站由第三方静态托管平台提供服务（如 Cloudflare Pages）。托管平台可能依据其自身服务条款，在服务器层面产生标准访问日志（如 IP 地址、请求时间、User-Agent 等）。此类日志由托管平台按其隐私政策处理，超出本站运营主体的控制范围。",
      "本站页面通过字体服务（Google Fonts / next/font 自托管）加载字体资源；本站使用的字体以自托管方式部署，不会因加载字体而向第三方发送请求。",
    ],
  },
  {
    title: "4. 数据保存与共享",
    body: [
      "本站运营主体不保存访问者的个人数据，因此不存在个人数据的存储期限、跨境传输或与第三方共享的问题。",
      "如未来本站引入任何数据收集功能，本隐私政策将先行更新并明确说明收集范围、用途与保存方式。",
    ],
  },
  {
    title: "5. 外部链接",
    body: [
      "本网站可能包含指向第三方网站（包括代码托管平台、公司官网）的链接。第三方网站的隐私实践由其各自运营方负责，与本隐私政策无关。",
    ],
  },
  {
    title: "6. 政策更新与联系方式",
    body: [
      "运营主体可能适时更新本隐私政策，更新后的政策将在本页面发布并更新生效日期。",
      `如对本隐私政策或网站的数据处理有任何疑问，可通过站内联系页面联系运营主体，或在项目仓库 ${siteIdentity.repositoryUrl} 提交 Issue。`,
    ],
  },
] as const;

export default function PrivacyPage() {
  return (
    <>
      <Nav />
      <main className="min-h-[100dvh] bg-background pt-16">
        <section className="border-b border-border bg-muted/40 py-20 md:py-28">
          <div className="wrap">
            <p className="font-mono text-sm font-semibold text-accent">
              Privacy Policy
            </p>
            <h1 className="mt-5 max-w-3xl text-4xl font-semibold tracking-tight text-foreground md:text-6xl">
              隐私政策
            </h1>
            <p className="mt-6 max-w-2xl text-lg leading-8 text-muted-foreground">
              本政策说明 Semantix 官网（{siteIdentity.productUrl}）的数据处理方式。
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
