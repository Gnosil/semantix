"use client";

import { TextAnimate } from "@/components/ui/text-animate";
import type { Locale } from "@/lib/i18n";

export default function AnimatedText({ children, locale, className, delay = 0 }: {
  children: string;
  locale: Locale;
  className?: string;
  delay?: number;
}) {
  return (
    <TextAnimate as="span" by={locale === "en" ? "word" : "character"}
      animation="blurInUp" duration={0.5} delay={delay} once className={className}>
      {children}
    </TextAnimate>
  );
}
