"use client";

import { useEffect, useRef, useState } from "react";
import Reveal from "@/components/Reveal";

type RoadmapStep = {
  phase: string;
  titleEn: string;
  title: string;
  body: string;
  href: string;
};

const steps: RoadmapStep[] = [
  {
    phase: "P0",
    titleEn: "Observability",
    title: "可观测层",
    body: "harness 适配器 + 事件流 + 基线指标",
    href: "#features",
  },
  {
    phase: "P1",
    titleEn: "Slice Library",
    title: "语义切片库",
    body: "提取器 + 嵌入 + ANN 索引，项目/用户双库",
    href: "#component-slice",
  },
  {
    phase: "P2",
    titleEn: "Semantic Cache",
    title: "语义缓存",
    body: "L2 稳定注入 + L3 验证复用 + 污染检测",
    href: "#component-retrieval",
  },
  {
    phase: "P3",
    titleEn: "Scheduler",
    title: "调度器",
    body: "intent 分类 + 并发行为学习 + tier 映射",
    href: "#component-schedule",
  },
  {
    phase: "P4",
    titleEn: "Prefetcher",
    title: "预取器",
    body: "T-Slice 转移矩阵 + 路径模式 + 预算控制",
    href: "#component-prefetch",
  },
  {
    phase: "P5",
    titleEn: "Evolution Loop",
    title: "进化闭环",
    body: "在线 EWMA + 离线重训 + ablation",
    href: "#community",
  },
];

export default function Roadmap() {
  const flowRef = useRef<HTMLDivElement | null>(null);
  const [activePhase, setActivePhase] = useState(-1);
  const [focusedPhase, setFocusedPhase] = useState<number | null>(null);
  const loopComplete = activePhase === steps.length - 1;

  useEffect(() => {
    const flow = flowRef.current;
    if (!flow) return;

    const timers: Array<ReturnType<typeof setTimeout>> = [];
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (!entry?.isIntersecting) return;

        if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
          setActivePhase(steps.length - 1);
        } else {
          steps.forEach((_, index) => {
            timers.push(
              setTimeout(() => setActivePhase(index), index * 170),
            );
          });
        }
        observer.disconnect();
      },
      { threshold: 0.28 },
    );

    observer.observe(flow);
    return () => {
      observer.disconnect();
      timers.forEach(clearTimeout);
    };
  }, []);

  const relationshipLabel = focusedPhase === null
    ? "SCROLL TO TRACE THE SYSTEM PATH"
    : [focusedPhase - 1, focusedPhase, focusedPhase + 1]
        .filter((index) => index >= 0 && index < steps.length)
        .map((index) => steps[index].phase)
        .join("  →  ");

  return (
    <section
      id="roadmap"
      className="border-y border-[#168b6d]/25 bg-white text-[#168b6d]"
    >
      <div className="mx-auto max-w-[1760px] px-5 py-16 md:px-10 md:py-20 lg:px-12 lg:py-24">
        <Reveal>
          <header className="mx-auto max-w-5xl border-b border-[#168b6d]/30 pb-10 text-center md:pb-12">
            <p className="font-mono text-[10px] font-semibold tracking-[0.24em] text-[#168b6d]">
            Roadmap 路线图
            </p>
            <h2 className="font-brand-display mx-auto mt-4 max-w-4xl text-[clamp(2.5rem,4.6vw,5.25rem)] font-black leading-[0.92] tracking-[-0.06em] text-[#101313]">
              从可观测到
              <span className="block text-[#168b6d]">可验证的反馈闭环。</span>
            </h2>
            <p className="mt-6 font-mono text-[10px] font-semibold uppercase tracking-[0.2em] text-[#168b6d]">
              Six phases to a closed loop.
            </p>
            <p className="mx-auto mt-4 max-w-3xl text-sm leading-6 text-[#101313]/65 md:text-base md:leading-7">
              每一步都建立在前一步之上：先能看见，再能复用，然后验证调度、预取和参数反馈。路线图表示计划，不代表所有阶段已经投入生产。
            </p>
          </header>
        </Reveal>

        <div
          ref={flowRef}
          role="list"
          className="mt-12 grid border-b border-r border-[#168b6d]/30 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6"
        >
          {steps.map((s, i) => (
            <Reveal key={s.phase} delay={i * 60} className="h-full">
              <a
                href={s.href}
                role="listitem"
                aria-label={`${s.phase} ${s.title}，前往对应内容`}
                onMouseEnter={() => setFocusedPhase(i)}
                onMouseLeave={() => setFocusedPhase(null)}
                onFocus={() => setFocusedPhase(i)}
                onBlur={() => setFocusedPhase(null)}
                className={`group flex min-h-[15rem] h-full flex-col border-l border-t p-5 transition-[opacity,transform,background-color,border-color] duration-300 ease-out focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[#168b6d] md:p-6 ${
                  i === 0 || i === steps.length - 1 ? "bg-[#168b6d]/[0.045]" : "bg-white"
                } ${i <= activePhase ? "border-[#168b6d]/35" : "border-[#168b6d]/15 opacity-40"} ${
                  focusedPhase === null
                    ? ""
                    : focusedPhase === i
                      ? "-translate-y-1 border-[#168b6d] bg-[#168b6d]/[0.075] opacity-100"
                      : Math.abs(focusedPhase - i) === 1
                        ? "opacity-65"
                        : "opacity-25"
                }`}
              >
                <div className="flex items-center justify-between gap-4">
                  <span className="font-mono text-xs font-bold tracking-[0.18em] text-[#168b6d]">
                    {s.phase}
                  </span>
                  <span className="font-mono text-[9px] tracking-[0.14em] text-[#168b6d]/55">
                    {String(i + 1).padStart(2, "0")} / {String(steps.length).padStart(2, "0")}
                  </span>
                </div>
                <div className="mt-auto pt-12">
                  <h3 className="font-brand-display text-2xl font-black leading-[1.02] tracking-[-0.04em] text-[#101313] transition-colors duration-300 group-hover:text-[#168b6d] group-focus-visible:text-[#168b6d]">
                    {s.title}
                  </h3>
                  <p className="mt-2 font-mono text-[9px] uppercase tracking-[0.16em] text-[#168b6d]">
                    {s.titleEn}
                  </p>
                  <p className="mt-5 text-sm leading-6 text-[#101313]/62">{s.body}</p>
                </div>
              </a>
            </Reveal>
          ))}
        </div>

        <Reveal delay={180}>
          <div className="mt-5 hidden grid-cols-[auto_1fr_auto] items-center gap-5 xl:grid">
            <p className={`font-mono text-[10px] font-bold tracking-[0.18em] text-[#168b6d] ${loopComplete ? "roadmap-return-pulse" : ""}`}>
              P0 OBSERVE
            </p>
            <div className="relative flex items-center gap-3" aria-hidden="true">
              <span className={`font-mono text-base text-[#168b6d] transition-opacity duration-500 ${loopComplete ? "opacity-100" : "opacity-20"}`}>←</span>
              <span className="relative h-px flex-1 overflow-hidden bg-[#168b6d]/15">
                <span
                  className={`absolute inset-0 origin-right bg-[#168b6d] transition-transform duration-1000 ease-out ${loopComplete ? "scale-x-100" : "scale-x-0"}`}
                />
              </span>
            </div>
            <p className="font-mono text-[10px] font-bold tracking-[0.18em] text-[#168b6d]">
              P5 FEEDBACK RETURN
            </p>
          </div>
        </Reveal>

        <p className="mt-4 text-center font-mono text-[9px] tracking-[0.16em] text-[#168b6d]/60 xl:text-left">
          {relationshipLabel}
        </p>
      </div>
    </section>
  );
}
