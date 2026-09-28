"use client";

import type { Locale } from "@/lib/i18n";
import { homePath } from "@/lib/i18n";

export default function LanguageSwitcher({ locale }: { locale: Locale }) {
  const target = locale === "en" ? "zh-CN" : "en";
  return (
    <a
      href={homePath(target)}
      hrefLang={target}
      lang={target}
      aria-label={locale === "en" ? "Switch to Chinese" : "Switch to English"}
      onClick={(event) => {
        // Keep section position while allowing normal new-tab and modified clicks.
        if (!event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey) {
          event.currentTarget.href = homePath(target) + window.location.hash;
        }
      }}
      className="inline-flex min-h-11 min-w-11 items-center justify-center rounded-md border border-border bg-white/90 px-3 text-sm font-medium text-foreground transition-colors hover:border-accent hover:text-accent focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-accent"
    >
      {locale === "en" ? "中文" : "EN"}
    </a>
  );
}
