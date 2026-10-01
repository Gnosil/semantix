"use client";

import { createContext, useContext, useRef, type ReactNode } from "react";
import { motion, useReducedMotion, useScroll, useTransform, type MotionValue } from "motion/react";

const SceneProgress = createContext<MotionValue<number> | null>(null);

/** Content follows the scroll position in either direction; no extra pinned distance. */
export function ScrollScene({ children, className }: { children: ReactNode; className?: string }) {
  const target = useRef<HTMLDivElement>(null);
  const { scrollYProgress } = useScroll({ target, offset: ["start end", "start 0.2"] });
  return <div ref={target} className={className}><SceneProgress.Provider value={scrollYProgress}>{children}</SceneProgress.Provider></div>;
}

export function ScrollLayer({ children, className, from = 0.08, to = 0.7, rise = 32, scaleFrom = 1, settled = false }: {
  children: ReactNode;
  className?: string;
  from?: number;
  to?: number;
  rise?: number;
  scaleFrom?: number;
  settled?: boolean;
}) {
  const progress = useContext(SceneProgress);
  if (!progress) throw new Error("ScrollLayer requires a ScrollScene");
  return <ProgressLayer progress={progress} className={className} from={from} to={to} rise={rise} scaleFrom={scaleFrom} settled={settled}>{children}</ProgressLayer>;
}

function ProgressLayer({ children, className, progress, from, to, rise, scaleFrom, settled }: {
  children: ReactNode;
  className?: string;
  progress: MotionValue<number>;
  from: number;
  to: number;
  rise: number;
  scaleFrom: number;
  settled: boolean;
}) {
  const reducedMotion = useReducedMotion();
  const opacity = useTransform(progress, [from, to], [0.12, 1]);
  const y = useTransform(progress, [from, to], [rise, 0]);
  const scale = useTransform(progress, [from, to], [scaleFrom, 1]);
  return <motion.div className={className} style={{ opacity: reducedMotion || settled ? 1 : opacity, y: reducedMotion || settled ? 0 : y, scale: reducedMotion || settled ? 1 : scale }}>{children}</motion.div>;
}
