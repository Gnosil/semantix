import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { getBlogPost, listBlogPosts, readBlogPost } from "@/lib/blog";
import { siteIdentity } from "@/lib/site-identity";
import { getContentAuthor, personJsonLd } from "@/lib/content-authors";
import { defaultEvidenceArtifact, evidenceLevelLabel, getBlogEvidence } from "@/lib/evidence";

type BlogPageProps = { params: Promise<{ slug: string }> };

export const dynamicParams = false;

export function generateStaticParams() {
  return listBlogPosts().map(({ slug }) => ({ slug }));
}

export async function generateMetadata({ params }: BlogPageProps): Promise<Metadata> {
  const post = getBlogPost((await params).slug);
  return post
    ? {
        title: `${post.title} | Semantix`,
        description: post.description,
        alternates: { canonical: `/blog/${post.slug}` },
      }
    : {};
}

export default async function BlogArticlePage({ params }: BlogPageProps) {
  const postMeta = getBlogPost((await params).slug);
  if (!postMeta) notFound();
  const post = readBlogPost(postMeta);
  const author = getContentAuthor(`blog/${post.slug}`);
  const evidence = getBlogEvidence(post.slug);
  const sourceUrl = `${siteIdentity.repositoryUrl}/blob/main/blog/${post.fileName}`;
  const articleJsonLd = {
    "@context": "https://schema.org",
    "@type": "BlogPosting",
    "@id": `${siteIdentity.productUrl}/blog/${post.slug}#article`,
    headline: post.title,
    description: post.description,
    dateModified: post.updated,
    datePublished: post.updated,
    inLanguage: "en",
    mainEntityOfPage: `${siteIdentity.productUrl}/blog/${post.slug}`,
    author: personJsonLd(author),
    publisher: { "@id": `${siteIdentity.operator.url}#organization` },
    citation: sourceUrl,
  };

  return (
    <div className="px-6 py-10 md:py-14">
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(articleJsonLd).replace(/</g, "\\u003c") }}
      />
      <div className="mx-auto max-w-4xl">
        <div className="mb-8 flex flex-wrap items-center justify-between gap-4 border-b border-border pb-5">
          <Link href="/blog" className="text-sm font-medium text-muted-foreground hover:text-accent">
            ← 返回 Blog
          </Link>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2 font-mono text-xs text-muted-foreground">
            <Link href={author.profileUrl} rel="author" className="hover:text-accent">Maintainer attribution · {author.name}</Link>
            <span aria-hidden="true">·</span>
            <time dateTime={post.updated}>Updated {post.updated}</time>
            <span aria-hidden="true">·</span>
            <span>{evidenceLevelLabel(evidence.level)}</span>
            <span aria-hidden="true">·</span>
            <a href={sourceUrl} target="_blank" rel="noopener noreferrer" className="hover:text-accent">
              Source ↗
            </a>
          </div>
        </div>

        <aside className="mb-8 max-w-3xl border-l-2 border-accent bg-muted/40 px-5 py-4 text-sm leading-6 text-muted-foreground">
          <p>
            Evidence and limitations: commands, observable outputs, and known failure boundaries are kept in the article. Repository tests are first-party engineering evidence, not an independent production benchmark.
          </p>
          <div className="mt-2 flex flex-wrap gap-x-5 gap-y-2">
            <a href={sourceUrl} target="_blank" rel="noopener noreferrer" className="font-medium text-foreground underline decoration-border underline-offset-4 hover:text-accent">View source and revision history ↗</a>
            <a href={author.contributionsUrl} target="_blank" rel="noopener noreferrer" className="font-medium text-foreground underline decoration-border underline-offset-4 hover:text-accent">Author contribution history ↗</a>
            {evidence.run ? <Link href="/benchmarks" className="font-medium text-foreground underline decoration-border underline-offset-4 hover:text-accent">View evidence run ↗</Link> : null}
          </div>
          <p className="mt-3 text-xs">{author.description} This attribution identifies a stable maintainer link; it does not claim that the person wrote every sentence.</p>
        </aside>

        {evidence.run ? (
          <p className="mb-8 max-w-3xl text-xs leading-6 text-muted-foreground">
            This article belongs to the repository-tested set. The related public retrieval artifact is <Link href="/benchmarks#replay-artifact" className="underline underline-offset-4 hover:text-accent">{defaultEvidenceArtifact.title}</Link>; it remains a fixture-level result, not a production benchmark.
          </p>
        ) : null}

        <article className="geo-prose max-w-3xl">
          <ReactMarkdown
            remarkPlugins={[remarkGfm]}
            components={{ h1: () => <h1>{post.title}</h1> }}
          >
            {post.content}
          </ReactMarkdown>
        </article>
      </div>
    </div>
  );
}
