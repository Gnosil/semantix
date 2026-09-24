"use client";

import { useEffect, useState } from "react";
import { Download, Info, Terminal } from "lucide-react";
import Reveal from "@/components/Reveal";
import { cn } from "@/lib/utils";

/**
 * DesktopDownload — the "下载桌面版" section.
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

export default function DesktopDownload() {
  // Server render (and first paint) shows every platform equally; once hydrated
  // we highlight the visitor's OS as the primary button.
  const [os, setOs] = useState<OS>("other");
  useEffect(() => setOs(detectOS()), []);

  return (
    <section
      id="desktop"
      className="border-x-[10px] border-[#168b6d] bg-[#101313] md:border-x-[18px]"
    >
      <div className="mx-auto max-w-[1600px] px-5 py-16 md:px-10 md:py-20 lg:px-12 lg:py-24">
        <Reveal>
          <p className="font-mono text-[10px] font-semibold uppercase tracking-[0.24em] text-[#68d0a0]">
            Desktop 桌面版
          </p>
          <h2 className="font-brand-display mt-4 text-5xl font-black tracking-[-0.055em] text-[#f8f8f4] md:text-6xl lg:text-[4.5rem]">
            下载即用的桌面版。
          </h2>
          <p className="mt-3 max-w-2xl text-base leading-7 text-[#a7b0b4] md:text-lg">
            原生窗口包裹同一套 Go 内核，无需装 Go/Node。选择你的系统直接下载。
          </p>
        </Reveal>

        <Reveal delay={80}>
          <div className="mt-12 grid gap-4 sm:grid-cols-2 lg:mt-14">
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
          <div className="mt-6 flex items-start gap-3 border border-[#68d0a0]/25 bg-[#68d0a0]/[0.06] px-4 py-3 text-sm leading-6 text-[#a7b0b4]">
            <Info
              aria-hidden="true"
              className="mt-0.5 h-4 w-4 shrink-0 text-[#68d0a0]"
            />
            <p>
              <span className="font-semibold text-[#f8f8f4]">首次打开 macOS：</span>{" "}
              当前为未签名 beta，被 Gatekeeper 拦时右键 App →「打开」，或终端执行{" "}
              <code className="rounded bg-black/40 px-1.5 py-0.5 font-mono text-[#68d0a0]">
                xattr -cr /Applications/Semantix.app
              </code>
              。Windows 遇 SmartScreen 点「更多信息 → 仍要运行」。
            </p>
          </div>
        </Reveal>

        <Reveal delay={220}>
          <nav
            aria-label="桌面版其他下载方式"
            className="mt-8 flex flex-wrap items-center gap-x-6 gap-y-3 text-sm"
          >
            <a
              href={SOURCE}
              target="_blank"
              rel="noopener"
              className="inline-flex items-center gap-2 font-semibold text-[#68d0a0] transition-colors hover:text-[#f8f8f4]"
            >
              <Terminal aria-hidden="true" className="h-4 w-4" />
              Linux / 从源码构建
            </a>
            <a
              href={ALL_RELEASES}
              target="_blank"
              rel="noopener"
              className="inline-flex items-center gap-2 font-semibold text-[#a7b0b4] transition-colors hover:text-[#f8f8f4]"
            >
              历史版本
              <span aria-hidden="true">↗</span>
            </a>
          </nav>
        </Reveal>
      </div>
    </section>
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
        "flex flex-col justify-between gap-6 border p-6 transition-colors md:p-7",
        primary
          ? "border-[#68d0a0] bg-[#68d0a0]/[0.08]"
          : "border-[#f8f8f4]/14 bg-[#f8f8f4]/[0.03] hover:border-[#f8f8f4]/28",
      )}
    >
      <div>
        <div className="flex items-center justify-between">
          <h3 className="font-brand-display text-2xl font-black tracking-[-0.035em] text-[#f8f8f4]">
            {platform}
          </h3>
          {primary ? (
            <span className="font-mono text-[10px] font-semibold uppercase tracking-[0.16em] text-[#68d0a0]">
              你的系统
            </span>
          ) : null}
        </div>
        <p className="mt-2 text-sm text-[#a7b0b4]">{note}</p>
      </div>
      <div>
        <a
          href={href}
          className={cn(
            "inline-flex w-full items-center justify-center gap-2 px-5 py-3 text-sm font-bold transition-colors",
            primary
              ? "bg-[#68d0a0] text-[#101313] hover:bg-[#7fdcb1]"
              : "border border-[#f8f8f4]/28 text-[#f8f8f4] hover:border-[#68d0a0] hover:text-[#68d0a0]",
          )}
        >
          <Download aria-hidden="true" className="h-4 w-4" />
          下载 {platform}
        </a>
        {secondaryHref ? (
          <a
            href={secondaryHref}
            className="mt-3 inline-flex text-xs font-semibold text-[#a7b0b4] underline-offset-4 transition-colors hover:text-[#68d0a0] hover:underline"
          >
            {secondaryLabel}
          </a>
        ) : null}
      </div>
    </div>
  );
}
