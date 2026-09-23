import Link from "next/link";
import { contentAuthors } from "@/lib/content-authors";
import { siteIdentity } from "@/lib/site-identity";

const projectLinks = [
  { label: "Documentation", href: "/docs" },
  { label: "Blog", href: "/blog" },
  { label: "Benchmarks", href: "/benchmarks" },
] as const;

const legalLinks = [
  { label: "About", href: "/about" },
  { label: "Terms", href: "/terms" },
  { label: "Privacy", href: "/privacy" },
  { label: "Contact", href: "/contact" },
] as const;

export default function FinaleInfo() {
  return (
    <footer
      id="project-info"
      aria-label="项目信息"
      className="bg-[#168b6d] text-[#f8f8f4]"
    >
      <div className="mx-auto max-w-[1600px] px-5 pb-8 pt-20 md:px-10 md:pb-10 md:pt-24 lg:px-12">
        <div className="grid gap-x-14 gap-y-14 border-t border-white/35 pt-8 sm:grid-cols-2 lg:grid-cols-[1.2fr_1.4fr_0.8fr_0.8fr] lg:gap-x-12">
          <div>
            <h2 className="font-mono text-[10px] font-semibold uppercase tracking-[0.18em] text-white/70">
              License / 开源许可
            </h2>
            <a
              href={`${siteIdentity.repositoryUrl}/blob/main/LICENSE`}
              target="_blank"
              rel="noopener noreferrer"
              className="mt-5 inline-block text-2xl font-semibold tracking-tight underline decoration-white/45 underline-offset-8 transition-colors hover:decoration-white"
            >
              MIT License ↗
            </a>
            <p className="mt-3 text-sm leading-6 text-white/75">
              Copyright © 2026 Gnosil. 完整条款见仓库 LICENSE 文件。
            </p>
          </div>
          <div>
            <h2 className="font-mono text-[10px] font-semibold uppercase tracking-[0.18em] text-white/70">
              Operator / 运营主体
            </h2>
            <p className="mt-5 text-xl font-semibold leading-snug tracking-tight">
              {siteIdentity.operator.legalName}
            </p>
            <Link
              href="/about"
              className="mt-3 inline-block text-sm font-semibold underline decoration-white/45 underline-offset-4 transition-colors hover:decoration-white"
            >
              了解项目与运营主体 ↗
            </Link>
          </div>
          <nav aria-label="项目链接">
            <h2 className="font-mono text-[10px] font-semibold uppercase tracking-[0.18em] text-white/70">
              Explore / 项目
            </h2>
            <ul className="mt-5 space-y-3">
              {projectLinks.map((link) => (
                <li key={link.href}>
                  <Link href={link.href} className="text-base font-medium transition-opacity hover:opacity-70">
                    {link.label}
                  </Link>
                </li>
              ))}
              <li>
                <a href={siteIdentity.repositoryUrl} target="_blank" rel="noopener noreferrer" className="text-base font-medium transition-opacity hover:opacity-70">
                  GitHub ↗
                </a>
              </li>
            </ul>
          </nav>
          <nav aria-label="网站与法律信息">
            <h2 className="font-mono text-[10px] font-semibold uppercase tracking-[0.18em] text-white/70">
              Information / 信息
            </h2>
            <ul className="mt-5 space-y-3">
              {legalLinks.map((link) => (
                <li key={link.href}>
                  <Link href={link.href} className="text-base font-medium transition-opacity hover:opacity-70">
                    {link.label}
                  </Link>
                </li>
              ))}
            </ul>
          </nav>
        </div>

        <div className="mt-20 flex flex-col gap-4 border-t border-white/35 pt-5 text-xs leading-6 text-white/75 lg:flex-row lg:items-center lg:justify-between">
          <p className="font-mono uppercase tracking-[0.12em]">Semantix © 2026 · MIT License</p>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <span>维护者</span>
            {contentAuthors.map((author) => (
              <Link
                key={author.name}
                href={author.profileUrl}
                className="font-medium text-white underline decoration-transparent underline-offset-4 transition-colors hover:decoration-white"
              >
                {author.name}
              </Link>
            ))}
          </div>
        </div>
      </div>
    </footer>
  );
}
