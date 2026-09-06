import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { isValidElement, type ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import CopyCode from "@/components/CopyCode";
import { documents, getDocument, readDocument } from "@/lib/docs";
import { siteIdentity } from "@/lib/site-identity";
import { getContentAuthor, personJsonLd } from "@/lib/content-authors";

type DocsPageProps = {
  params: Promise<{ slug: string }>;
};

function extractCodeText(node: ReactNode): string {
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(extractCodeText).join("");
  if (isValidElement<{ children?: ReactNode }>(node)) return extractCodeText(node.props.children);
  return "";
}

export const dynamicParams = false;

export function generateStaticParams() {
  return documents.map(({ slug }) => ({ slug }));
}

export async function generateMetadata({ params }: DocsPageProps): Promise<Metadata> {
  const { slug } = await params;
  const document = getDocument(slug);

  if (!document) return {};

  return {
    title: `${document.title} | Semantix`,
    description: document.description,
  };
}

export default async function DocsDocumentPage({ params }: DocsPageProps) {
  const { slug } = await params;
  const document = getDocument(slug);

  if (!document) notFound();

  const content = await readDocument(document);
  const sourceUrl = `${siteIdentity.repositoryUrl}/blob/main/site/content/docs/${document.fileName}`;
  const isEnglish = document.language === "English";
  const author = getContentAuthor(`docs/${document.slug}`);

  const articleJsonLd = {
    "@context": "https://schema.org",
    "@type": "TechArticle",
    "@id": `${siteIdentity.productUrl}/docs/${document.slug}#article`,
    headline: document.title,
    description: document.description,
    datePublished: document.published,
    dateModified: document.lastUpdated,
    inLanguage: isEnglish ? "en" : "zh-CN",
    mainEntityOfPage: `${siteIdentity.productUrl}/docs/${document.slug}`,
    author: personJsonLd(author),
    publisher: { "@id": `${siteIdentity.operator.url}#organization` },
    citation: sourceUrl,
  };

  return (
    <div className="px-6 py-10 md:px-10 md:py-14 lg:px-14">
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{
          __html: JSON.stringify(articleJsonLd).replace(/</g, "\\u003c"),
        }}
      />
      <div className="mx-auto max-w-4xl">
        <div className="mb-8 flex flex-wrap items-center justify-between gap-4 border-b border-border pb-5">
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Link href="/docs" className="font-medium hover:text-accent">文档</Link>
            <span aria-hidden="true">/</span>
            <span>{document.section}</span>
          </div>
          <div className="flex flex-wrap items-center gap-3 font-mono text-xs text-muted-foreground">
            <a href={author.url} target="_blank" rel="author noopener noreferrer" className="hover:text-accent">
              {isEnglish ? `By ${author.name}` : `作者 · ${author.name}`}
            </a>
            <span>{document.language}</span>
            <time dateTime={document.published}>Published · {document.published}</time>
            <time dateTime={document.lastUpdated}>Last updated · {document.lastUpdated}</time>
          </div>
        </div>

        <aside className="mb-8 max-w-3xl border-l-2 border-accent bg-muted/40 px-5 py-4 text-sm leading-6 text-muted-foreground">
          <p>{isEnglish
            ? "Evidence and limitations: check implementation claims against the current main branch. Repository tests and design documents are not independent production benchmarks."
            : document.evidence === "E0"
              ? "证据与限制：本文解释设计与机制边界。实现状态应回到 main 分支代码和测试核对，设计说明不等于生产效果。"
              : "证据与限制：本文依据当前仓库实现、命令和测试整理。它证明的是可检查的产品路径，不代表独立生产环境收益。"}</p>
          <a
            href={sourceUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="mt-2 inline-block font-medium text-foreground underline decoration-border underline-offset-4 hover:text-accent"
          >
            {isEnglish ? "View source and revision history ↗" : "查看原文与修订记录 ↗"}
          </a>
          <p className="mt-3 text-xs">{isEnglish ? `Evidence label: ${document.evidence}. ` : `证据等级：${document.evidence}。`}{isEnglish ? "Read the evidence boundary before treating an implementation statement as a measured result." : "如需查看已复现的仓库测试与实验夹具，请先阅读证据页。"} <Link href="/benchmarks" className="underline underline-offset-4 hover:text-accent">{isEnglish ? "Evidence page ↗" : "证据页 ↗"}</Link></p>
        </aside>

        <article className="geo-prose max-w-3xl">
          <ReactMarkdown
            remarkPlugins={[remarkGfm]}
            components={{
              h1: () => <h1>{document.title}</h1>,
              pre: ({ children }) => (
                <CopyCode
                  code={extractCodeText(children).replace(/\n$/, "")}
                  className="my-6"
                  tone="dark"
                />
              ),
            }}
          >
            {content}
          </ReactMarkdown>
        </article>
      </div>
    </div>
  );
}
