import type { Metadata } from "next";
import Link from "next/link";
import { defaultEvidenceArtifact, evidenceLevelLabel } from "@/lib/evidence";

export const metadata: Metadata = {
  title: "Evidence methodology | Semantix",
  description: "How Semantix labels repository tests, synthetic runs, and independent evidence.",
  alternates: { canonical: "/evidence/methodology" },
};

const levels = [
  ["E0", "Design or editorial context", "A design note, roadmap item, or explanation without a reproduced output."],
  ["E1", "Repository tests", "A first-party unit or integration test with a reproducible command and visible result."],
  ["E2", "Synthetic end-to-end run", "A fixed fixture or labeled query set exercised from input through output."],
  ["E3", "Independent or real-session reproduction", "A reproduction by an external developer or on a real session corpus with method and raw results."],
] as const;

export default function EvidenceMethodologyPage() {
  return (
    <main className="wrap py-12 md:py-16">
      <header className="max-w-3xl border-b border-border pb-10">
        <p className="font-mono text-xs font-semibold text-accent">EVIDENCE / METHOD</p>
        <h1 className="mt-5 text-4xl font-semibold tracking-tight md:text-6xl">How to read a Semantix result.</h1>
        <p className="mt-6 text-lg leading-8 text-muted-foreground">
          Evidence labels describe how a result was produced. They do not turn a passing test into a production guarantee.
        </p>
      </header>
      <section className="mt-12 max-w-4xl" aria-labelledby="levels">
        <h2 id="levels" className="text-2xl font-semibold tracking-tight">Evidence levels</h2>
        <div className="mt-5 divide-y divide-border border-y border-border">
          {levels.map(([level, title, description]) => (
            <div key={level} className="grid gap-3 py-5 md:grid-cols-[80px_220px_1fr]">
              <span className="font-mono text-sm text-accent">{level}</span>
              <h3 className="font-semibold">{title}</h3>
              <p className="text-sm leading-6 text-muted-foreground">{description}</p>
            </div>
          ))}
        </div>
      </section>
      <section className="mt-14 max-w-3xl border-t border-border pt-8">
        <h2 className="text-2xl font-semibold tracking-tight">Current boundary</h2>
        <p className="mt-5 text-sm leading-7 text-muted-foreground">
          The current public run is {evidenceLevelLabel("E1")}. It supports claims about tested repository behavior. Real-session relevance, universal framework compatibility, and lower production cost remain unverified until a fixed corpus and independent reproduction are published.
        </p>
      </section>
      <section className="mt-14 max-w-3xl border-t border-border pt-8" aria-labelledby="publishing-rules">
        <h2 id="publishing-rules" className="text-2xl font-semibold tracking-tight">Publishing rules</h2>
        <div className="mt-5 space-y-4 text-sm leading-7 text-muted-foreground">
          <p>每个结果都应同时公开输入、命令、环境、原始输出、解释和限制。没有这些字段时，页面只显示 E0，而不会给出精确分数。</p>
          <p>当前站点公开的 E2 资料是一个小型仓库 fixture：{defaultEvidenceArtifact.output}。它用于说明评估流程，不代表真实会话相关率或生产成本下降。</p>
        </div>
        <Link href="/benchmarks" className="mt-5 inline-block text-sm font-medium text-accent underline underline-offset-4">查看完整运行记录与下载数据 ↗</Link>
      </section>
    </main>
  );
}
