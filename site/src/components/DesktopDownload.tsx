"use client";

import { useSyncExternalStore } from "react";
import { Download, Info, Terminal } from "lucide-react";
import Reveal from "@/components/Reveal";
import { cn } from "@/lib/utils";

/**
 * Desktop downloads within the shared installation section.
 *
 * Links point at the rolling `desktop-latest` GitHub release, whose assets are
 * replaced on every desktop release (see .github/workflows/desktop-release.yml).
 * That keeps the URLs stable for this statically exported site — no GitHub API
 * lookup at build time. Asset names match the in-app updater contract
 * (desktop/updater.go). macOS builds are unsigned, hence the first-open note.
 */

const REPO = "https://github.com/Gnosil/semantix";
const LATEST = `${REPO}/releases/download/desktop-latest`;

const MAC_DMG = `${LATEST}/Semantix-darwin-universal.dmg`;
const WIN_EXE = `${LATEST}/Semantix-windows-amd64-installer.exe`;
const WIN_ZIP = `${LATEST}/Semantix-windows-amd64.zip`;
const SOURCE = `${REPO}/blob/main/desktop/README.md`;
const ALL_RELEASES = `${REPO}/releases`;

type OS = "mac" | "win" | "other";

function detectOS(): OS {
  if (typeof navigator === "undefined") return "other";
  const ua = navigator.userAgent;
  if (/Mac|iPhone|iPad|iPod/i.test(ua)) return "mac";
  if (/Win/i.test(ua)) return "win";
  return "other";
}

// The visitor's OS is a read-only, client-only value (navigator is absent during
// the static export / SSR). useSyncExternalStore reads it without a setState in
// an effect: the server snapshot is "other" (no highlight in the baked HTML),
// and the client snapshot is the detected OS after hydration. subscribe never
// fires because the value cannot change within a session.
const noopSubscribe = () => () => {};

function useDetectedOS(): OS {
  return useSyncExternalStore<OS>(noopSubscribe, detectOS, () => "other");
}

export default function DesktopDownload() {
  // First paint (and the static HTML) shows every platform equally; once hydrated
  // we highlight the visitor's OS as the primary button.
  const os = useDetectedOS();

  return (
    <div
      id="desktop"
      className="mt-10 scroll-mt-24 text-[#111411]"
    >
        <Reveal>
          <h3 className="font-brand-display text-xl font-bold tracking-[-0.025em]">
            桌面版
          </h3>
          <p className="mt-2 text-sm leading-6 text-muted-foreground">
            下载应用，无需安装 Go 或 Node。
          </p>
        </Reveal>

        <Reveal delay={80}>
          <div className="mt-5 grid gap-4 sm:grid-cols-2">
            <DownloadCard
              primary={os === "mac"}
              platform="macOS"
              note="Intel + Apple Silicon 通用 · .dmg"
              href={MAC_DMG}
            />
            <DownloadCard
              primary={os === "win"}
              platform="Windows"
              note="Windows 10/11 · 安装器 .exe"
              href={WIN_EXE}
              secondaryHref={WIN_ZIP}
              secondaryLabel="或下载免安装 .zip"
            />
          </div>
        </Reveal>

        <Reveal delay={160}>
          <div className="mt-7 flex items-start gap-3 text-xs leading-6 text-muted-foreground">
            <Info
              aria-hidden="true"
              className="mt-1 h-4 w-4 shrink-0 text-accent"
            />
            <p>
              <span className="font-semibold text-[#111411]">首次打开 macOS：</span>{" "}
              当前为未签名 beta，被 Gatekeeper 拦时右键 App →「打开」，或终端执行{" "}
              <code className="break-words rounded bg-accent/5 px-1.5 py-0.5 font-mono text-accent">
                xattr -cr /Applications/Semantix.app
              </code>
              。Windows 遇 SmartScreen 点「更多信息 → 仍要运行」。
            </p>
          </div>
        </Reveal>

        <Reveal delay={220}>
          <nav
            aria-label="桌面版其他下载方式"
            className="mt-6 flex flex-wrap items-center gap-x-6 gap-y-2 text-xs"
          >
            <a
              href={SOURCE}
              target="_blank"
              rel="noopener"
              className="inline-flex min-h-10 items-center gap-2 font-semibold text-accent underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-accent"
            >
              <Terminal aria-hidden="true" className="h-4 w-4" />
              Linux / 从源码构建
            </a>
            <a
              href={ALL_RELEASES}
              target="_blank"
              rel="noopener"
              className="inline-flex min-h-10 items-center gap-2 font-semibold text-muted-foreground transition-colors hover:text-accent focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-accent"
            >
              历史版本
              <span aria-hidden="true">↗</span>
            </a>
          </nav>
        </Reveal>
    </div>
  );
}

function DownloadCard({
  platform,
  note,
  href,
  primary,
  secondaryHref,
  secondaryLabel,
}: {
  platform: string;
  note: string;
  href: string;
  primary: boolean;
  secondaryHref?: string;
  secondaryLabel?: string;
}) {
  return (
    <div
      className={cn(
        // No justify-between: the buttons top-align right after the equal-height
        // title rows, so both cards' primary actions sit on one baseline; the
        // Windows zip link extends below without pushing its button up.
        "flex flex-col gap-6 rounded-lg border p-6 transition-colors md:p-7",
        primary
          ? "border-accent/35 bg-white"
          : "border-border bg-white hover:border-accent/30",
      )}
    >
      <div>
        <div className="flex items-center justify-between">
          <h4 className="font-brand-display text-2xl font-black tracking-[-0.035em] text-[#111411]">
            {platform}
          </h4>
          {primary ? (
            <span className="font-mono text-[10px] font-semibold tracking-[0.12em] text-accent">
              你的系统
            </span>
          ) : null}
        </div>
        <p className="mt-2 text-sm text-muted-foreground">{note}</p>
      </div>
      <div>
        <a
          href={href}
          className={cn(
            "inline-flex min-h-10 items-center justify-center gap-2 whitespace-nowrap rounded-md border px-4 py-2 text-sm font-semibold transition-colors focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-accent",
            primary
              ? "border-accent bg-accent text-white hover:bg-accent/90"
              : "border-border text-[#111411] hover:border-accent hover:text-accent",
          )}
        >
          <Download aria-hidden="true" className="h-4 w-4" />
          下载 {platform}
        </a>
        {secondaryHref ? (
          <a
            href={secondaryHref}
            className="ml-4 inline-flex min-h-9 items-center text-xs text-muted-foreground underline-offset-4 transition-colors hover:text-accent hover:underline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-accent"
          >
            {secondaryLabel}
          </a>
        ) : null}
      </div>
    </div>
  );
}
