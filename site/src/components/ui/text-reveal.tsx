"use client";

import { useRef } from "react";
import { motion, useReducedMotion, useScroll, useTransform, type MotionValue } from "motion/react";
import { cn } from "@/lib/utils";

export function TextReveal({ children, className, by = "word" }: {
  children: string;
  className?: string;
  by?: "word" | "character";
}) {
  const target = useRef<HTMLSpanElement>(null);
  const reducedMotion = useReducedMotion();
  const { scrollYProgress } = useScroll({
    target,
    offset: ["start 0.92", "end 0.48"],
  });
  const segments = by === "character" ? children.match(/.[，。！？、：；…,.!?;:）】」》]*/gu) ?? [] : children.split(/(\s+)/);

  return (
    <span ref={target} className={cn("relative inline-block", className)} aria-label={children}>
      <span className="sr-only">{children}</span>
      <span aria-hidden="true">
        {segments.map((segment, index) => (
          <RevealWord key={index} progress={scrollYProgress}
            range={[index / segments.length, (index + 1) / segments.length]}
            reducedMotion={!!reducedMotion}>
            {segment}
          </RevealWord>
        ))}
      </span>
    </span>
  );
}

function RevealWord({ children, progress, range, reducedMotion }: {
  children: string;
  progress: MotionValue<number>;
  range: [number, number];
  reducedMotion: boolean;
}) {
  const opacity = useTransform(progress, range, [0.2, 1]);
  return (
    <motion.span className="inline-block whitespace-pre" style={{ opacity: reducedMotion ? 1 : opacity }}>
      {children}
    </motion.span>
  );
}
