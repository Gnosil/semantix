"use client";

import { useEffect, useRef, useState } from "react";
import ParticleCanvas from "@/components/ParticleCanvas";
import CopyCode from "@/components/CopyCode";
import { siteIdentity } from "@/lib/site-identity";

const clamp = (value: number) => Math.min(1, Math.max(0, value));

const range = (value: number, start: number, end: number) =>
  clamp((value - start) / (end - start));

export default function BrandIntroOverlay() {
  const storyRef = useRef<HTMLElement>(null);
  const frameRef = useRef<number | null>(null);
  const [progress, setProgress] = useState(0);

  useEffect(() => {
    const update = () => {
      frameRef.current = null;
      const story = storyRef.current;
      if (!story) return;

      const rect = story.getBoundingClientRect();
      const travel = Math.max(1, story.offsetHeight - window.innerHeight);
      setProgress(clamp(-rect.top / travel));
    };

    const requestUpdate = () => {
      if (frameRef.current === null) {
        frameRef.current = window.requestAnimationFrame(update);
      }
    };

    update();
    window.addEventListener("scroll", requestUpdate, { passive: true });
    window.addEventListener("resize", requestUpdate);
    return () => {
      window.removeEventListener("scroll", requestUpdate);
      window.removeEventListener("resize", requestUpdate);
      if (frameRef.current !== null) window.cancelAnimationFrame(frameRef.current);
    };
  }, []);

  const whiteProgress = range(progress, 0.14, 0.58);
  const titleProgress = range(progress, 0.46, 0.74);
  const metaOpacity = 1 - range(progress, 0.1, 0.42);
  const wordScale = 1 - progress * 0.16;
  const wordLift = progress * 18;

  return (
    <section
      ref={storyRef}
      id="brand-intro"
      aria-label="Semantix introduction"
      className="relative h-[165svh] bg-white"
    >
      <div className="sticky top-0 h-[100svh] min-h-[560px] overflow-hidden bg-[#080d0c]">
        <div
          aria-hidden="true"
          className="absolute inset-0 bg-white"
          style={{ opacity: whiteProgress }}
        />

        <div
          aria-hidden="true"
          className="absolute inset-0 text-[#a7a9a8]"
          style={{ opacity: metaOpacity }}
        >
          <div className="absolute left-6 top-7 font-mono text-[10px] leading-6 tracking-[0.3em] sm:left-12 sm:top-12 sm:text-xs">
            SEMANTIX&nbsp; / &nbsp;04
            <br />
            AGENT KERNEL
          </div>

          <div className="font-brand-display absolute right-6 top-7 max-w-[15rem] text-right text-xs font-bold leading-relaxed sm:right-12 sm:top-12 md:max-w-xs">
            让每个 Agent 拥有持续进化的
            <br className="hidden md:block" />
            记忆与推理内核
          </div>

          <div className="absolute bottom-8 left-6 font-mono text-[9px] tracking-[0.34em] sm:bottom-12 sm:left-12 sm:text-[10px]">
            SEMANTIC MEMORY SYSTEM
          </div>

          <span className="absolute bottom-8 right-7 h-2.5 w-2.5 rounded-full bg-[#168b6d] shadow-[0_0_0_9px_rgba(22,139,109,0.12)] sm:bottom-12 sm:right-12" />
        </div>

        <p
          aria-hidden="true"
          className="font-brand-serif brand-intro-wordmark absolute inset-x-0 top-1/2 z-10 m-0 whitespace-nowrap text-center text-[#168b6d]"
          style={{
            transform: `translateY(calc(-50% - ${wordLift}vh)) scale(${wordScale})`,
          }}
        >
          SEMANTIX
        </p>

        <div
          className="absolute inset-x-0 top-[48%] z-10 px-6 text-center text-[#101313]"
          style={{
            opacity: titleProgress,
            transform: `translateY(${(1 - titleProgress) * 56}px)`,
            pointerEvents: titleProgress > 0.95 ? "auto" : "none",
          }}
        >
          <h1 className="font-brand-serif text-balance text-5xl font-normal leading-[0.88] tracking-[0.015em] md:text-6xl lg:whitespace-nowrap lg:text-[4.9rem]">
            A <span className="text-[#00a878]">self-evolving</span> agent kernel.
          </h1>
          <p className="font-brand-display mx-auto mt-7 max-w-4xl text-[2rem] font-black leading-[0.98] tracking-[-0.055em] md:text-[2.75rem]">
            <span className="block md:inline">让每次推理</span>
            <span className="block md:inline">变成下一次绘画的</span>
            <span className="block text-[#168b6d]">记忆</span>
          </p>

          <p className="mx-auto mt-8 max-w-2xl text-sm leading-6 text-muted-foreground md:text-base">
            Semantix 连接 Agent Harness 与资源层，负责并发调度、语义缓存和智能预取。
            <br className="hidden md:block" />
            每次交互都会积累经验，让下一次响应更快、成本更低。
          </p>
          <time
            dateTime={siteIdentity.lastUpdated}
            className="mt-3 block font-mono text-[10px] tracking-[0.12em] text-muted-foreground"
          >
            Last updated · {siteIdentity.lastUpdated}
          </time>

          <div className="mx-auto mt-8 hidden max-w-3xl grid-cols-2 gap-4 text-left md:grid">
            <div className="relative overflow-hidden rounded-lg border border-border bg-white p-4 transition-colors hover:border-accent">
              <ParticleCanvas className="pointer-events-none absolute inset-0 opacity-45" />
              <div className="relative">
                <h2 className="font-semibold">GitHub 仓库</h2>
                <p className="mt-1 text-sm text-muted-foreground">
                  开源 · MIT · 设计文档与路线图都在这里
                </p>
                <a
                  href="https://github.com/Gnosil/semantix"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="mt-3 inline-block rounded-md border border-border px-3 py-1.5 text-sm transition-colors hover:border-accent"
                >
                  GitHub ↗
                </a>
              </div>
            </div>

            <div className="relative overflow-hidden rounded-lg border border-border bg-white p-4 transition-colors hover:border-accent">
              <ParticleCanvas className="pointer-events-none absolute inset-0 opacity-45" />
              <div className="relative">
                <h2 className="font-semibold">CLI</h2>
                <p className="mt-1 text-sm text-muted-foreground">
                  extract / search 切片提取与 BM25 检索
                </p>
                <CopyCode
                  className="mt-3"
                  code="go install semantix/cmd/semantix"
                  prompt
                  singleLine
                />
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
