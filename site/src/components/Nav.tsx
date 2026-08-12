"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { cn } from "@/lib/utils";
import type { NavLink } from "@/types/content";

const links: NavLink[] = [
  { label: "特性", labelEn: "Features", href: "/#features" },
  { label: "组件", labelEn: "Components", href: "/#components" },
  { label: "路线图", labelEn: "Roadmap", href: "/#roadmap" },
  { label: "文档", labelEn: "Docs", href: "/docs" },
  { label: "博客", labelEn: "Blog", href: "/blog" },
  { label: "社区", labelEn: "Community", href: "/#community" },
  { label: "安装", labelEn: "Install", href: "/#start" },
];

export default function Nav() {
  const [scrolled, setScrolled] = useState(false);
  const [visible, setVisible] = useState(false);
  const [open, setOpen] = useState(false);

  // 滚动状态：>8px 后白底 + 边框（对齐原站 header 行为）
  useEffect(() => {
    const onScroll = () => {
      setScrolled(window.scrollY > 8);
      const intro = document.getElementById("brand-intro");
      const revealAt = intro ? intro.offsetTop + intro.offsetHeight - 64 : 0;
      setVisible(Boolean(intro && window.scrollY >= revealAt));
    };
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("resize", onScroll);
    return () => {
      window.removeEventListener("scroll", onScroll);
      window.removeEventListener("resize", onScroll);
    };
  }, []);

  // 菜单打开时锁定 body 滚动
  useEffect(() => {
    document.body.style.overflow = open ? "hidden" : "";
    return () => {
      document.body.style.overflow = "";
    };
  }, [open]);

  return (
    <header
      className={cn(
        "fixed top-0 z-50 w-full border-b transition-all duration-300",
        !visible && "hidden",
        scrolled || open
          ? "border-border bg-white/85 backdrop-blur"
          : "border-transparent bg-transparent",
      )}
    >
      <div className="mx-auto flex h-16 w-full max-w-[1360px] items-center justify-between px-5 sm:px-6">
        {/* Logo */}
        <Link href="/" aria-label="Semantix 首页" className="flex items-center gap-2.5">
          <img
            src="/seo/favicon.svg"
            alt=""
            className="size-8 text-foreground"
            aria-hidden="true"
          />
          <span className="font-mono text-lg font-semibold text-foreground">
            semantix
          </span>
        </Link>

        {/* 桌面端中间导航链接（移动端隐藏） */}
        <nav className="hidden items-center gap-4 xl:flex 2xl:gap-6">
          {links.map((link) => (
            <Link
              key={link.href}
              href={link.href}
              className="shrink-0 whitespace-nowrap text-xs text-muted-foreground transition-colors hover:text-accent 2xl:text-sm"
            >
              <span className="font-semibold">{link.labelEn}</span> {link.label}
            </Link>
          ))}
        </nav>

        {/* 右侧操作区 */}
        <div className="flex items-center gap-3">
          <a
            href="https://github.com/Gnosil/semantix"
            target="_blank"
            rel="noopener noreferrer"
            className="hidden rounded-md border border-border px-3 py-1.5 text-sm text-muted-foreground transition-colors hover:border-accent hover:text-foreground sm:inline-block"
          >
            GitHub <span aria-hidden="true">↗</span>
          </a>
          <Link
            href="/#start"
            className="rounded-md bg-accent px-3 py-1.5 text-sm text-white transition-opacity hover:opacity-90"
          >
            <span className="font-semibold">Install</span> 安装
          </Link>

          {/* 汉堡按钮（移动端） */}
          <button
            type="button"
            aria-label={open ? "关闭菜单" : "打开菜单"}
            aria-expanded={open}
            onClick={() => setOpen((v) => !v)}
            className="flex size-9 items-center justify-center rounded-md border border-border text-foreground xl:hidden"
          >
            <span className="flex flex-col gap-[5px]">
              <span
                className={cn(
                  "block h-[2px] w-5 bg-current transition-transform duration-200",
                  open && "translate-y-[7px] rotate-45",
                )}
              />
              <span
                className={cn(
                  "block h-[2px] w-5 bg-current transition-opacity duration-200",
                  open && "opacity-0",
                )}
              />
              <span
                className={cn(
                  "block h-[2px] w-5 bg-current transition-transform duration-200",
                  open && "-translate-y-[7px] -rotate-45",
                )}
              />
            </span>
          </button>
        </div>
      </div>

      {/* 移动端全屏菜单面板 */}
      <div
        className={cn(
          "absolute inset-x-0 top-16 z-40 h-[calc(100vh-4rem)] overflow-y-auto bg-white/95 backdrop-blur transition-opacity duration-200 xl:hidden",
          open ? "opacity-100" : "pointer-events-none opacity-0",
        )}
      >
        <nav className="wrap flex flex-col gap-2 py-6">
          {links.map((link) => (
            <Link
              key={link.href}
              href={link.href}
              onClick={() => setOpen(false)}
              className="rounded-lg px-3 py-4 text-lg font-medium text-foreground transition-colors hover:bg-muted"
            >
              <span className="font-semibold">{link.labelEn}</span> {link.label}
            </Link>
          ))}
          <a
            href="https://github.com/Gnosil/semantix"
            target="_blank"
            rel="noopener noreferrer"
            className="rounded-lg px-3 py-4 text-lg text-muted-foreground transition-colors hover:bg-muted"
          >
            GitHub <span aria-hidden="true">↗</span>
          </a>
        </nav>
      </div>
    </header>
  );
}
