import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { contentAuthors } from "@/lib/content-authors";
import { listBlogPosts } from "@/lib/blog";
import { getBlogEvidence, evidenceLevelLabel } from "@/lib/evidence";

type AuthorPageProps = { params: Promise<{ name: string }> };

export const dynamicParams = false;

export function generateStaticParams() {
  return contentAuthors.map((author) => ({ name: author.profileUrl.split("/").pop()! }));
}

export async function generateMetadata({ params }: AuthorPageProps): Promise<Metadata> {
  const name = (await params).name;
  const author = contentAuthors.find((item) => item.profileUrl.endsWith(`/${name}`));
  return author ? { title: `${author.name} | Semantix contributors`, description: author.description } : {};
}

export default async function AuthorPage({ params }: AuthorPageProps) {
  const name = (await params).name;
  const author = contentAuthors.find((item) => item.profileUrl.endsWith(`/${name}`));
  if (!author) notFound();

  const authoredPosts = listBlogPosts().filter((post) => {
    const hash = [...`blog/${post.slug}`].reduce((value, character) => (value * 31 + character.codePointAt(0)!) >>> 0, 0);
    return contentAuthors[hash % contentAuthors.length].name === author.name;
  });

  const personJsonLd = {
    "@context": "https://schema.org",
    "@type": "ProfilePage",
    "@id": `https://semantix.ensureok.ai${author.profileUrl}`,
    mainEntity: {
      "@type": "Person",
      name: author.name,
      url: author.url,
      sameAs: [author.url, author.contributionsUrl],
      description: author.description,
    },
  };

  return (
    <main className="wrap py-12 md:py-16">
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(personJsonLd).replace(/</g, "\\u003c") }} />
      <header className="max-w-3xl border-b border-border pb-10">
        <p className="font-mono text-xs font-semibold text-accent">CONTRIBUTOR PROFILE</p>
        <h1 className="mt-5 text-4xl font-semibold tracking-tight md:text-6xl">{author.name}</h1>
        <p className="mt-5 text-lg leading-8 text-muted-foreground">{author.description}</p>
        <p className="mt-4 max-w-2xl text-sm leading-7 text-muted-foreground">
          This profile is generated from public repository evidence. It records reviewable work, not a job title, endorsement, or claim of sole authorship over the pages listed below.
        </p>
        <div className="mt-6 flex flex-wrap gap-x-5 gap-y-2 text-sm">
          <a className="text-accent underline underline-offset-4" href={author.url}>GitHub profile ↗</a>
          <a className="text-accent underline underline-offset-4" href={author.contributionsUrl}>Semantix contribution history ↗</a>
        </div>
      </header>

      <section className="mt-12 max-w-4xl" aria-labelledby="verified-work">
        <h2 id="verified-work" className="text-2xl font-semibold tracking-tight">Verified repository work</h2>
        <p className="mt-3 max-w-3xl text-sm leading-7 text-muted-foreground">
          Current focus: {author.focus} Each item links to the commit that supports the description, so readers can inspect the patch, review context, and date directly.
        </p>
        <div className="mt-6 grid gap-x-8 gap-y-8 md:grid-cols-3">
          {author.verifiedContributions.map((contribution) => (
            <article key={contribution.url} className="border-t border-border pt-4">
              <time dateTime={contribution.date} className="font-mono text-xs text-muted-foreground">{contribution.date}</time>
              <h3 className="mt-3 font-semibold leading-6">{contribution.title}</h3>
              <p className="mt-3 text-sm leading-6 text-muted-foreground">{contribution.summary}</p>
              <a href={contribution.url} className="mt-4 inline-block text-sm font-medium text-accent underline underline-offset-4">Inspect commit</a>
            </article>
          ))}
        </div>
      </section>

      <section className="mt-12 max-w-4xl" aria-labelledby="maintained-pages">
        <h2 id="maintained-pages" className="text-2xl font-semibold tracking-tight">Attributed pages</h2>
        <p className="mt-3 text-sm leading-6 text-muted-foreground">The site uses stable maintainer attribution so a page keeps the same linked identity across builds. It is not a claim that the person wrote every sentence.</p>
        <div className="mt-5 divide-y divide-border border-y border-border">
          {authoredPosts.map((post) => {
            const evidence = getBlogEvidence(post.slug);
            return (
              <Link key={post.slug} href={`/blog/${post.slug}`} className="block py-5 transition-colors hover:bg-muted/35 md:px-3">
                <div className="flex flex-wrap items-center gap-x-4 gap-y-2 font-mono text-xs text-muted-foreground">
                  <time dateTime={post.updated}>{post.updated}</time>
                  <span>{evidenceLevelLabel(evidence.level)}</span>
                </div>
                <h3 className="mt-2 font-semibold transition-colors group-hover:text-accent">{post.title}</h3>
              </Link>
            );
          })}
        </div>
      </section>

      <section className="mt-12 max-w-3xl border-t border-border pt-8" aria-labelledby="verification-note">
        <h2 id="verification-note" className="text-xl font-semibold">How to verify this profile</h2>
        <p className="mt-4 text-sm leading-7 text-muted-foreground">
          Follow the commit links above for exact diffs. The complete contribution-history link may include merges or follow-up fixes that are not summarized here. Editorial attribution is deterministic so pages do not change names between builds, while repository commits remain the authority for who changed code.
        </p>
      </section>
    </main>
  );
}
