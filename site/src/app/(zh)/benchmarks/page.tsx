import type { Metadata } from "next";
import Link from "next/link";
import { defaultEvidenceArtifact, defaultEvidenceRun, evidenceLevelLabel } from "@/lib/evidence";
import { siteIdentity } from "@/lib/site-identity";
import { contentAuthors } from "@/lib/content-authors";

export const metadata: Metadata = {
  title: "Benchmarks and evidence | Semantix",
  description: "Reproducible Semantix repository test runs, environments, outputs, and known limits.",
  alternates: { canonical: "/benchmarks" },
};

const capabilities = [
  ["JSONL event parsing", "E1", "Repository tests"],
  ["Semantic slice extraction", "E1", "Repository fixtures"],
  ["BM25 retrieval", "E1", "Repository tests"],
  ["Stable cross-session injection", "E1", "Repository tests"],
  ["Real-session relevance", "E0", "Not yet measured"],
  ["Production cost reduction", "E0", "Not yet independently benchmarked"],
] as const;

export default function BenchmarksPage() {
  const run = defaultEvidenceRun;
  const editor = contentAuthors.find((author) => author.name === "radianceded")!;
  const commitUrl = `${siteIdentity.repositoryUrl}/commit/${run.commit}`;

  return (
    <main className="wrap py-12 md:py-16">
      <header className="max-w-3xl border-b border-border pb-10">
        <p className="font-mono text-xs font-semibold text-accent">EVIDENCE / BENCHMARKS</p>
        <h1 className="mt-5 text-4xl font-semibold tracking-tight md:text-6xl">What has Semantix actually measured?</h1>
        <p className="mt-6 text-lg leading-8 text-muted-foreground">
          This page separates repository behavior from claims that still need real-session or production evidence.
        </p>
        <p className="mt-5 border-l-2 border-accent pl-4 text-sm leading-6 text-muted-foreground">
          Current evidence level: <span className="font-medium text-foreground">{evidenceLevelLabel(run.level)}</span>. This is first-party engineering evidence, not an independent production benchmark.
        </p>
        <p className="mt-5 text-sm text-muted-foreground">
          Run record and page review by <Link href={editor.profileUrl} className="font-medium text-foreground underline underline-offset-4">{editor.name}</Link>. Last reviewed <time dateTime="2026-08-13">2026-08-13</time>.
        </p>
      </header>

      <section className="mt-12 max-w-3xl" aria-labelledby="why-this-run">
        <h2 id="why-this-run" className="text-2xl font-semibold tracking-tight">Why this run is public</h2>
        <div className="mt-5 space-y-4 text-sm leading-7 text-muted-foreground">
          <p>We published the run after the website audit found that architecture claims were easier to find than raw outcomes. The aim is narrower than a performance benchmark: a reader should be able to see the command, environment, passing packages, and adverse result without trusting a product summary.</p>
          <p>The Windows permission failures are kept in the record because removing them would overstate portability. They do not invalidate BM25 or extraction tests, but they do show that Unix mode-bit assertions need a platform-specific interpretation.</p>
        </div>
      </section>

      <section className="mt-12" aria-labelledby="capability-status">
        <h2 id="capability-status" className="text-2xl font-semibold tracking-tight">Capability status</h2>
        <div className="mt-5 divide-y divide-border border-y border-border">
          {capabilities.map(([name, level, status]) => (
            <div key={name} className="grid gap-2 py-4 md:grid-cols-[1fr_220px_230px] md:items-center">
              <span className="font-medium">{name}</span>
              <span className="font-mono text-xs text-accent">{level}</span>
              <span className="text-sm text-muted-foreground">{status}</span>
            </div>
          ))}
        </div>
      </section>

      <section className="mt-14 max-w-4xl" aria-labelledby="latest-run">
        <div className="flex flex-wrap items-baseline justify-between gap-3">
          <h2 id="latest-run" className="text-2xl font-semibold tracking-tight">Latest reproducible run</h2>
          <a href={`/evidence/${run.id}.json`} download className="font-mono text-xs text-accent underline underline-offset-4">Download JSON ↗</a>
        </div>
        <div className="mt-5 rounded-xl border border-border bg-muted/25 p-5 md:p-7">
          <div className="grid gap-4 text-sm md:grid-cols-2">
            <p><span className="text-muted-foreground">Run:</span> {run.label}</p>
            <p><span className="text-muted-foreground">Date:</span> {run.date}</p>
            <p><span className="text-muted-foreground">Environment:</span> {run.environment}</p>
            <p><span className="text-muted-foreground">Command:</span> <code>{run.command}</code></p>
          </div>
          <p className="mt-6 leading-7">{run.summary}</p>
          <div className="mt-6 grid gap-6 md:grid-cols-2">
            <div>
              <h3 className="font-semibold text-emerald-700">Passed packages</h3>
              <ul className="mt-2 space-y-1 text-sm text-muted-foreground">
                {run.passed.map((item) => <li key={item}><code>{item}</code></li>)}
              </ul>
            </div>
            <div>
              <h3 className="font-semibold text-red-700">Known failures</h3>
              <ul className="mt-2 space-y-1 text-sm text-muted-foreground">
                {run.failed.map((item) => <li key={item}><code>{item}</code></li>)}
              </ul>
            </div>
          </div>
          <p className="mt-6 text-sm leading-6 text-muted-foreground">
            Commit: <a className="underline underline-offset-4 hover:text-accent" href={commitUrl}>{run.commit.slice(0, 12)} ↗</a>
          </p>
        </div>
      </section>

      <section className="mt-14 max-w-4xl" aria-labelledby="replay-artifact">
        <div className="flex flex-wrap items-baseline justify-between gap-3">
          <h2 id="replay-artifact" className="text-2xl font-semibold tracking-tight">Reproducible retrieval artifact</h2>
          <span className="font-mono text-xs text-accent">{evidenceLevelLabel(defaultEvidenceArtifact.level)}</span>
        </div>
        <div className="mt-5 rounded-xl border border-border bg-muted/25 p-5 md:p-7">
          <h3 className="text-lg font-semibold">{defaultEvidenceArtifact.title}</h3>
          <p className="mt-3 text-sm leading-7 text-muted-foreground">{defaultEvidenceArtifact.purpose}</p>
          <dl className="mt-6 divide-y divide-border border-y border-border text-sm">
            <div className="grid gap-2 py-4 md:grid-cols-[7rem_1fr]"><dt className="text-muted-foreground">Input</dt><dd className="break-words font-mono text-xs leading-6">{defaultEvidenceArtifact.input}</dd></div>
            <div className="grid gap-2 py-4 md:grid-cols-[7rem_1fr]"><dt className="text-muted-foreground">Command</dt><dd><code className="break-words text-xs leading-6">{defaultEvidenceArtifact.command}</code></dd></div>
            <div className="grid gap-2 py-4 md:grid-cols-[7rem_1fr]"><dt className="text-muted-foreground">Observed</dt><dd className="leading-7">{defaultEvidenceArtifact.output}</dd></div>
          </dl>
          <p className="mt-6 text-sm leading-7">{defaultEvidenceArtifact.interpretation}</p>
          <ul className="mt-4 space-y-2 text-sm leading-6 text-muted-foreground">
            {defaultEvidenceArtifact.limitations.map((limitation) => <li key={limitation}>· {limitation}</li>)}
          </ul>
          <a href={defaultEvidenceArtifact.sourceUrl} target="_blank" rel="noopener noreferrer" className="mt-5 inline-block text-sm font-medium text-accent underline underline-offset-4">View fixture source ↗</a>
        </div>
      </section>

      <section className="mt-14 max-w-3xl border-t border-border pt-8" aria-labelledby="limitations">
        <h2 id="limitations" className="text-2xl font-semibold tracking-tight">What this does not prove</h2>
        <ul className="mt-5 space-y-3 text-sm leading-7 text-muted-foreground">
          {run.limitations.map((limitation) => <li key={limitation}>· {limitation}</li>)}
        </ul>
        <h3 className="mt-8 text-lg font-semibold text-foreground">Next evaluation</h3>
        <p className="mt-3 text-sm leading-7 text-muted-foreground">
          The next useful run is a frozen set of real, consented sessions with relevance labels written before threshold tuning. It should publish per-query decisions, rejected slices, downstream task outcomes, and a no-memory baseline. Until that exists, this page supports implementation claims only.
        </p>
        <Link href="/docs/faq" className="mt-6 inline-block text-sm font-medium text-accent underline underline-offset-4">Read the FAQ on current project status →</Link>
      </section>
    </main>
  );
}
