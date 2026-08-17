"use client";

import { useEffect, useState } from "react";
import Image from "next/image";
import Link from "next/link";
import { cn } from "@/lib/utils";
import {
  INTRO_COMPLETION_EVENT,
  type IntroCompletionDetail,
} from "@/lib/intro-events";
import { siteIdentity } from "@/lib/site-identity";
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

function NavLabel({ label, labelEn }: Pick<NavLink, "label" | "labelEn">) {
  return (
    <span className="inline-flex items-baseline gap-1.5">
      <span>{labelEn}</span>
      <span>{label}</span>
    </span>
  );
}

export default function Nav() {
  const [visible, setVisible] = useState(false);
  const [open, setOpen] = useState(false);

  // 首页导航由品牌开场动画发出完成信号；其他页面直接显示。
  useEffect(() => {
    const syncVisibility = () => {
      const intro = document.getElementById("brand-intro");
      setVisible(!intro || document.documentElement.dataset.introComplete === "true");
    };

    const onIntroCompletion = (event: Event) => {
      const { complete } = (event as CustomEvent<IntroCompletionDetail>).detail;
      setVisible(complete);
    };

    syncVisibility();
    window.addEventListener(INTRO_COMPLETION_EVENT, onIntroCompletion);
    return () => {
      window.removeEventListener(INTRO_COMPLETION_EVENT, onIntroCompletion);
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
        "fixed top-0 z-50 w-full border-b border-border bg-white/90 backdrop-blur transition-[transform,opacity] duration-500 ease-[cubic-bezier(0.16,1,0.3,1)]",
        visible
          ? "translate-y-0 opacity-100 delay-100"
          : "pointer-events-none -translate-y-full opacity-0 delay-0",
      )}
    >
      <div className="mx-auto flex h-16 w-full max-w-[1360px] items-center justify-between px-5 sm:px-6">
        {/* Logo */}
        <a
          href={siteIdentity.operator.url}
          target="_blank"
          rel="noopener noreferrer"
          aria-label="访问 EnsureOK 官网"
          className="block shrink-0 transition-opacity hover:opacity-65"
        >
          <span className="flex flex-col items-center gap-0.5 text-[#050505]">
            <Image
              src="/seo/favicon.svg"
              alt=""
              width={22}
              height={22}
              className="size-[1.15rem] sm:size-5"
              aria-hidden="true"
            />
            <span className="font-brand-display text-[10px] font-black leading-none tracking-[0.045em] sm:text-[11px]">
              ENSUREOK
            </span>
          </span>
        </a>

        {/* 桌面端中间导航链接（移动端隐藏） */}
        <nav className="hidden items-center gap-5 xl:flex 2xl:gap-7">
          {links.map((link) => (
            <Link
              key={link.href}
              href={link.href}
              className="font-brand-display shrink-0 whitespace-nowrap text-xs font-bold tracking-[0.025em] text-muted-foreground transition-colors hover:text-accent 2xl:text-sm"
            >
              <NavLabel label={link.label} labelEn={link.labelEn} />
            </Link>
          ))}
        </nav>

        {/* 右侧操作区 */}
        <div className="flex items-center gap-3">
          <a
            href="https://github.com/Gnosil/semantix"
            target="_blank"
            rel="noopener noreferrer"
            className="font-brand-display hidden rounded-md border border-border px-3 py-1.5 text-sm font-bold text-muted-foreground transition-colors hover:border-accent hover:text-foreground sm:inline-block"
          >
            GitHub <span aria-hidden="true">↗</span>
          </a>
          <Link
            href="/#start"
            className="font-brand-display rounded-md bg-accent px-3 py-1.5 text-sm font-bold text-white transition-opacity hover:opacity-90"
          >
            Install 安装
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
              className="font-brand-display rounded-lg px-3 py-4 text-lg font-bold tracking-[0.025em] text-foreground transition-colors hover:bg-muted"
            >
              <NavLabel label={link.label} labelEn={link.labelEn} />
            </Link>
          ))}
          <a
            href="https://github.com/Gnosil/semantix"
            target="_blank"
            rel="noopener noreferrer"
            className="font-brand-display rounded-lg px-3 py-4 text-lg font-bold text-muted-foreground transition-colors hover:bg-muted"
          >
            GitHub <span aria-hidden="true">↗</span>
          </a>
        </nav>
      </div>
    </header>
  );
}
