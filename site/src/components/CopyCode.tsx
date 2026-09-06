"use client";

import { Check, Copy } from "lucide-react";
import { useEffect, useRef, useState } from "react";

type CopyCodeProps = {
  code: string;
  className?: string;
  prompt?: boolean;
  singleLine?: boolean;
  tone?: "light" | "dark" | "gray";
};

type CopyState = "idle" | "copied" | "failed";

function fallbackCopy(code: string) {
  const textarea = document.createElement("textarea");
  textarea.value = code;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.select();

  const copied = document.execCommand("copy");
  textarea.remove();

  if (!copied) {
    throw new Error("Copy command was rejected");
  }
}

export default function CopyCode({
  code,
  className = "",
  prompt = false,
  singleLine = false,
  tone = "light",
}: CopyCodeProps) {
  const [state, setState] = useState<CopyState>("idle");
  const resetTimer = useRef<number | undefined>(undefined);
  const isDark = tone === "dark";
  const isGray = tone === "gray";

  useEffect(
    () => () => {
      if (resetTimer.current !== undefined) {
        window.clearTimeout(resetTimer.current);
      }
    },
    [],
  );

  async function handleCopy() {
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(code);
      } else {
        fallbackCopy(code);
      }
      setState("copied");
    } catch {
      try {
        fallbackCopy(code);
        setState("copied");
      } catch {
        setState("failed");
      }
    }

    if (resetTimer.current !== undefined) {
      window.clearTimeout(resetTimer.current);
    }
    resetTimer.current = window.setTimeout(() => setState("idle"), 1800);
  }

  const label = state === "copied" ? "已复制" : state === "failed" ? "重试" : "复制";

  return (
    <div
      className={`flex min-w-0 overflow-hidden rounded-md border ${
        isDark
          ? "border-white/10 bg-[oklch(0.21_0.006_260)]"
          : isGray
            ? "border-slate-500 bg-slate-600"
            : "border-border/60 bg-[oklch(0.976_0.005_165)]"
      } ${className}`}
    >
      <pre
        className={`min-w-0 flex-1 p-3 font-mono leading-5 ${
          singleLine
            ? "overflow-hidden text-ellipsis whitespace-nowrap text-[11px]"
            : "whitespace-pre-wrap break-words text-xs"
        } ${
          isDark
            ? "text-slate-300"
            : isGray
              ? "text-slate-200"
              : "text-[oklch(0.45_0.02_260)]"
        }`}
      >
        <code>{prompt ? `$ ${code}` : code}</code>
      </pre>
      <button
        type="button"
        onClick={handleCopy}
        aria-label={`${label}代码`}
        className={`flex w-11 shrink-0 items-center justify-center border-l transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent active:translate-y-px ${
          isDark
            ? "border-white/10 bg-[oklch(0.25_0.008_260)] text-slate-300 hover:bg-[oklch(0.29_0.01_260)] hover:text-emerald-300"
            : isGray
              ? "border-slate-500 bg-slate-500 text-slate-100 hover:bg-slate-400 hover:text-emerald-300"
              : "border-border/60 bg-white text-accent hover:bg-accent/5"
        }`}
      >
        {state === "copied" ? (
          <Check aria-hidden="true" className="size-4" strokeWidth={1.8} />
        ) : (
          <Copy aria-hidden="true" className="size-4" strokeWidth={1.8} />
        )}
        <span className="sr-only" aria-live="polite">
          {label}
        </span>
      </button>
    </div>
  );
}
