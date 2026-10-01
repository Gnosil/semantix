"use client";

import type { ReactNode } from "react";
import { motion, useMotionTemplate, useMotionValue, useReducedMotion } from "motion/react";
import { cn } from "@/lib/utils";

export function MagicCard({ children, className, gradientSize = 220,
  gradientColor = "#168b6d", gradientOpacity = 0.09,
  gradientFrom = "#168b6d", gradientTo = "#b4d7c9" }: {
  children: ReactNode;
  className?: string;
  gradientSize?: number;
  gradientColor?: string;
  gradientOpacity?: number;
  gradientFrom?: string;
  gradientTo?: string;
}) {
  const reducedMotion = useReducedMotion();
  const mouseX = useMotionValue(-gradientSize);
  const mouseY = useMotionValue(-gradientSize);
  const border = useMotionTemplate`radial-gradient(${gradientSize}px circle at ${mouseX}px ${mouseY}px, ${gradientFrom}, ${gradientTo}, #10131329 100%)`;
  const spot = useMotionTemplate`radial-gradient(${gradientSize}px circle at ${mouseX}px ${mouseY}px, ${gradientColor}, transparent 100%)`;
  return (
    <div className={cn("group relative isolate overflow-hidden p-px", className)}
      onPointerMove={(event) => {
        if (reducedMotion || event.pointerType === "touch") return;
        const rect = event.currentTarget.getBoundingClientRect();
        mouseX.set(event.clientX - rect.left);
        mouseY.set(event.clientY - rect.top);
      }}
      onPointerLeave={() => { mouseX.set(-gradientSize); mouseY.set(-gradientSize); }}>
      <motion.div aria-hidden="true" className="pointer-events-none absolute inset-0" style={{ background: border }} />
      <div className="pointer-events-none absolute inset-px bg-white" />
      <motion.div aria-hidden="true" className="pointer-events-none absolute inset-px"
        style={{ background: spot, opacity: reducedMotion ? 0 : gradientOpacity }} />
      <div className="relative h-full">{children}</div>
    </div>
  );
}
