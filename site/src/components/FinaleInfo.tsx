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

const labelClass =
  "font-mono text-[11px] font-semibold uppercase tracking-[0.18em] text-white/70";
const navLinkClass =
  "text-base font-medium leading-7 transition-opacity hover:opacity-70 md:text-lg";

export default function FinaleInfo() {
  return (
    <footer
      id="project-info"
      aria-label="项目信息"
      className="bg-[#168b6d] text-[#f8f8f4]"
    >
      <div className="mx-auto max-w-[1600px] px-5 pb-10 pt-20 md:px-10 md:pb-12 md:pt-24 lg:px-12">
        <div className="grid gap-14 border-t border-white/35 pt-9 lg:grid-cols-2 lg:gap-20">
          <section aria-labelledby="license-heading">
            <h2 id="license-heading" className={labelClass}>
              License / 开源许可
            </h2>
            <a
              href={siteIdentity.repositoryUrl + "/blob/main/LICENSE"}
              target="_blank"
              rel="noopener noreferrer"
              className="font-brand-serif mt-6 inline-block text-[clamp(3rem,5vw,5.75rem)] leading-[0.95] tracking-[-0.055em] underline decoration-white/40 decoration-1 underline-offset-[0.16em] transition-colors hover:decoration-white"
            >
              MIT License <span className="inline-block align-top font-sans text-[0.45em]">↗</span>
            </a>
            <p className="mt-7 text-sm leading-6 text-white/75">
              © 2026 Gnosil · 完整许可条款见仓库 LICENSE 文件
            </p>
          </section>

          <section aria-labelledby="operator-heading">
            <h2 id="operator-heading" className={labelClass}>
              Operator / 运营主体
            </h2>
            <p className="font-feature-serif mt-6 text-[clamp(1.75rem,2.1vw,2.5rem)] leading-[1.35] tracking-[-0.035em]">
              {siteIdentity.operator.legalName}
            </p>
            <Link
              href="/about"
              className="mt-6 inline-block text-sm font-medium underline decoration-white/45 underline-offset-4 transition-colors hover:decoration-white"
            >
              了解项目与运营主体 ↗
            </Link>
          </section>
        </div>

        <div className="mt-20 grid gap-12 border-t border-white/35 pt-8 lg:grid-cols-2 lg:gap-20">
          <nav aria-label="项目链接" className="flex flex-col gap-6 sm:flex-row sm:gap-10">
            <h2 className={labelClass + " shrink-0 sm:w-40"}>
              Explore / 项目
            </h2>
            <ul className="flex flex-wrap gap-x-7 gap-y-2">
              {projectLinks.map((link) => (
                <li key={link.href}>
                  <Link href={link.href} className={navLinkClass}>
                    {link.label}
                  </Link>
                </li>
              ))}
              <li>
                <a
                  href={siteIdentity.repositoryUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className={navLinkClass}
                >
                  GitHub ↗
                </a>
              </li>
            </ul>
          </nav>

          <nav aria-label="网站与法律信息" className="flex flex-col gap-6 sm:flex-row sm:gap-10">
            <h2 className={labelClass + " shrink-0 sm:w-40"}>
              Information / 信息
            </h2>
            <ul className="flex flex-wrap gap-x-7 gap-y-2">
              {legalLinks.map((link) => (
                <li key={link.href}>
                  <Link href={link.href} className={navLinkClass}>
                    {link.label}
                  </Link>
                </li>
              ))}
            </ul>
          </nav>
        </div>

        <div className="mt-20 flex flex-col gap-6 border-t border-white/35 pt-6 md:flex-row md:items-end md:justify-between">
          <div>
            <p className="font-brand-serif text-3xl leading-none tracking-[-0.05em]">
              Semantix
            </p>
            <p className="mt-3 font-mono text-[10px] uppercase tracking-[0.15em] text-white/65">
              Open source / MIT License / 2026
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
            <span className="text-white/65">维护者</span>
            {contentAuthors.map((author) => (
              <Link
                key={author.name}
                href={author.profileUrl}
                className="font-medium underline decoration-transparent underline-offset-4 transition-colors hover:decoration-white"
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
