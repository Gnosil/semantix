import Link from "next/link";
import { siteIdentity } from "@/lib/site-identity";

const links = [
  { label: "About", href: "/about", external: false },
  { label: "Docs", href: "/docs", external: false },
  { label: "Blog", href: "/blog", external: false },
  { label: "Benchmarks", href: "/benchmarks", external: false },
  { label: "Terms", href: "/terms", external: false },
  { label: "Privacy", href: "/privacy", external: false },
  {
    label: "Contact",
    href: "/contact",
    external: false,
  },
  { label: "GitHub", href: "https://github.com/Gnosil/semantix", external: true },
  {
    label: "README",
    href: "https://github.com/Gnosil/semantix/blob/main/README.md",
    external: true,
  },
] as const;

export default function Footer() {
  return (
    <footer className="border-t border-border bg-muted py-8">
      <div className="wrap flex flex-col justify-between gap-6 md:flex-row md:items-end">
        <div className="max-w-xl text-sm leading-6 text-muted-foreground">
          <p className="font-mono">
            Semantix © 2026 · {siteIdentity.licenseName}
          </p>
          <p className="mt-1">
            由
            <Link
              href="/about"
              className="mx-1 font-medium text-foreground underline decoration-border underline-offset-4 transition-colors hover:text-accent hover:decoration-accent"
            >
              {siteIdentity.operator.legalName}
            </Link>
            运营与维护。
          </p>
          <p className="mt-2">
            联系渠道：
            <Link
              href="/contact"
              className="font-medium text-foreground underline decoration-border underline-offset-4 transition-colors hover:text-accent hover:decoration-accent"
            >
              联系页面
            </Link>
          </p>
        </div>
        <nav aria-label="页脚导航" className="flex flex-wrap items-center gap-x-6 gap-y-3">
          {links.map((link) => (
            <Link
              key={link.label}
              href={link.href}
              target={link.external ? "_blank" : undefined}
              rel={link.external ? "noopener noreferrer" : undefined}
              className="text-sm text-muted-foreground transition-colors hover:text-accent"
            >
              {link.label}
            </Link>
          ))}
        </nav>
      </div>
    </footer>
  );
}
