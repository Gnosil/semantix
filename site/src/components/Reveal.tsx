"use client";

import type { ReactNode } from "react";
import { BlurFade } from "@/components/ui/blur-fade";

export default function Reveal({ children, className, delay = 0 }: {
  children: ReactNode;
  className?: string;
  delay?: number;
}) {
  return (
    <BlurFade className={className} delay={delay / 1000} duration={0.6}
      offset={12} direction="up" blur="4px" inView inViewMargin="0px 0px -32px 0px">
      {children}
    </BlurFade>
  );
}
