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
      aria-labelledby="project-info-title"
      className="bg-[#f8f8f4] text-[#101313]"
    >
      <div className="mx-auto flex min-h-[75svh] max-w-[1600px] flex-col px-5 pb-8 pt-8 md:px-10 md:pb-10 md:pt-10 lg:px-12">
        <div className="flex items-center justify-between gap-4 border-b border-[#101313]/20 pb-5 font-mono text-[10px] font-semibold uppercase tracking-[0.2em] text-[#168b6d]">
          <span>Semantix / 05</span>
          <span>Project information</span>
        </div>

        <div className="grid flex-1 gap-14 py-16 lg:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)] lg:gap-20 lg:py-24">
          <div>
            <h2
              id="project-info-title"
              className="font-brand-serif text-[clamp(4.5rem,10vw,10rem)] leading-[0.85] tracking-[-0.075em]"
            >
              The details.
            </h2>
            <p className="mt-8 max-w-lg text-base leading-7 text-[#596269]">
              关于开源许可、项目运营和维护者的信息，都在这里。
            </p>

            <div className="mt-14 grid gap-10 border-t border-[#101313]/20 pt-6 sm:grid-cols-2">
              <div>
                <h3 className="font-mono text-[10px] font-semibold uppercase tracking-[0.18em] text-[#168b6d]">
                  License / 开源许可
                </h3>
                <a
                  href={`${siteIdentity.repositoryUrl}/blob/main/LICENSE`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="mt-4 inline-block text-2xl font-semibold tracking-tight underline decoration-[#168b6d]/40 underline-offset-8 transition-colors hover:text-[#168b6d]"
                >
                  MIT License ↗
                </a>
                <p className="mt-3 text-sm leading-6 text-[#596269]">
                  Copyright © 2026 Gnosil. 完整条款见仓库 LICENSE 文件。
                </p>
              </div>
              <div>
                <h3 className="font-mono text-[10px] font-semibold uppercase tracking-[0.18em] text-[#168b6d]">
                  Operator / 运营主体
                </h3>
                <p className="mt-4 text-xl font-semibold leading-snug tracking-tight">
                  {siteIdentity.operator.legalName}
                </p>
                <Link
                  href="/about"
                  className="mt-3 inline-block text-sm font-semibold text-[#168b6d] underline decoration-[#168b6d]/40 underline-offset-4 transition-colors hover:decoration-[#168b6d]"
                >
                  了解项目与运营主体 ↗
                </Link>
              </div>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-x-8 gap-y-12 self-end border-t border-[#101313]/20 pt-6 sm:gap-x-14 lg:mt-5 lg:border-t-0 lg:pt-0">
            <nav aria-label="项目链接">
              <h3 className="font-mono text-[10px] font-semibold uppercase tracking-[0.18em] text-[#168b6d]">
                Explore / 项目
              </h3>
              <ul className="mt-5 space-y-3">
                {projectLinks.map((link) => (
                  <li key={link.href}>
                    <Link href={link.href} className="text-base font-medium transition-colors hover:text-[#168b6d]">
                      {link.label}
                    </Link>
                  </li>
                ))}
                <li>
                  <a href={siteIdentity.repositoryUrl} target="_blank" rel="noopener noreferrer" className="text-base font-medium transition-colors hover:text-[#168b6d]">
                    GitHub ↗
                  </a>
                </li>
              </ul>
            </nav>
            <nav aria-label="网站与法律信息">
              <h3 className="font-mono text-[10px] font-semibold uppercase tracking-[0.18em] text-[#168b6d]">
                Information / 信息
              </h3>
              <ul className="mt-5 space-y-3">
                {legalLinks.map((link) => (
                  <li key={link.href}>
                    <Link href={link.href} className="text-base font-medium transition-colors hover:text-[#168b6d]">
                      {link.label}
                    </Link>
                  </li>
                ))}
              </ul>
            </nav>
          </div>
        </div>

        <div className="flex flex-col gap-4 border-t border-[#101313]/20 pt-5 text-xs leading-6 text-[#596269] lg:flex-row lg:items-center lg:justify-between">
          <p className="font-mono uppercase tracking-[0.12em]">Semantix © 2026 · MIT License</p>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <span>维护者</span>
            {contentAuthors.map((author) => (
              <Link
                key={author.name}
                href={author.profileUrl}
                className="font-medium text-[#168b6d] underline decoration-transparent underline-offset-4 transition-colors hover:decoration-[#168b6d]"
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
