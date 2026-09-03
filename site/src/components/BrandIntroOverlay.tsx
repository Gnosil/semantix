"use client";

import { useEffect, useRef, useState } from "react";
import ParticleCanvas from "@/components/ParticleCanvas";
import CopyCode from "@/components/CopyCode";
import {
  INTRO_COMPLETION_EVENT,
  type IntroCompletionDetail,
} from "@/lib/intro-events";
import { siteIdentity } from "@/lib/site-identity";

const clamp = (value: number) => Math.min(1, Math.max(0, value));

const range = (value: number, start: number, end: number) =>
  clamp((value - start) / (end - start));

const NAV_REVEAL_PROGRESS = 0.78;

export default function BrandIntroOverlay() {
  const storyRef = useRef<HTMLElement>(null);
  const frameRef = useRef<number | null>(null);
  const scrollFrameRef = useRef<number | null>(null);
  const introCompleteRef = useRef(false);
  const [progress, setProgress] = useState(0);

  const continueIntro = () => {
    const story = storyRef.current;
    if (!story) return;

    const storyTop = window.scrollY + story.getBoundingClientRect().top;
    const storyEnd = storyTop + story.offsetHeight - window.innerHeight;
    const reduceMotion = window.matchMedia(
      "(prefers-reduced-motion: reduce)",
    ).matches;

    if (scrollFrameRef.current !== null) {
      window.cancelAnimationFrame(scrollFrameRef.current);
      scrollFrameRef.current = null;
    }

    if (reduceMotion) {
      window.scrollTo({ top: storyEnd, behavior: "auto" });
      return;
    }

    const startY = window.scrollY;
    const distance = storyEnd - startY;
    const duration = 1800;
    const startTime = window.performance.now();

    const animate = (now: number) => {
      const elapsed = clamp((now - startTime) / duration);
      const eased = elapsed;

      window.scrollTo(0, startY + distance * eased);
      if (elapsed < 1) {
        scrollFrameRef.current = window.requestAnimationFrame(animate);
      } else {
        scrollFrameRef.current = null;
      }
    };

    scrollFrameRef.current = window.requestAnimationFrame(animate);
  };

  useEffect(() => {
    const update = () => {
      frameRef.current = null;
      const story = storyRef.current;
      if (!story) return;

      const rect = story.getBoundingClientRect();
      const travel = Math.max(1, story.offsetHeight - window.innerHeight);
      const nextProgress = clamp(-rect.top / travel);
      const complete = nextProgress >= NAV_REVEAL_PROGRESS;
      const publishedComplete = document.documentElement.dataset.introComplete;

      setProgress(nextProgress);
      document.documentElement.dataset.introComplete = String(complete);
      if (
        introCompleteRef.current !== complete ||
        publishedComplete !== String(complete)
      ) {
        introCompleteRef.current = complete;
        window.dispatchEvent(
          new CustomEvent<IntroCompletionDetail>(INTRO_COMPLETION_EVENT, {
            detail: { complete },
          }),
        );
      }
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
      if (scrollFrameRef.current !== null) {
        window.cancelAnimationFrame(scrollFrameRef.current);
      }
      delete document.documentElement.dataset.introComplete;
    };
  }, []);

  const whiteProgress = range(progress, 0.14, 0.58);
  const titleProgress = range(progress, 0.46, 0.74);
  const metaOpacity = 1 - range(progress, 0.1, 0.42);
  const scrollCueOpacity = 1 - range(progress, 0.015, 0.11);
  const wordScale = 1 - progress * 0.16;
  const wordLift = progress * 18;

  return (
    <section
      ref={storyRef}
      id="brand-intro"
      aria-label="Semantix introduction"
      className="relative h-[140svh] bg-white"
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

          <div className="absolute bottom-8 left-6 font-mono text-[9px] tracking-[0.34em] sm:bottom-12 sm:left-12 sm:text-[10px]">
            SEMANTIC MEMORY SYSTEM
          </div>

        </div>

        <button
          type="button"
          aria-label="Continue to the Semantix overview"
          onClick={continueIntro}
          className="group absolute bottom-4 left-1/2 z-20 flex h-12 w-12 -translate-x-1/2 items-center justify-center text-[#168b6d] transition-opacity duration-300 focus-visible:rounded-sm focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#168b6d] sm:bottom-6"
          style={{
            opacity: scrollCueOpacity,
            pointerEvents: scrollCueOpacity > 0.1 ? "auto" : "none",
          }}
        >
          <svg
            aria-hidden="true"
            viewBox="0 0 16 10"
            className="h-2.5 w-4 transition-transform duration-300 group-hover:translate-y-0.5"
          >
            <path
              d="M1 1.25 8 8.25 15 1.25"
              fill="none"
              stroke="currentColor"
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth="1.25"
            />
          </svg>
        </button>

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
          className="brand-intro-title-block absolute inset-x-0 z-10 px-6 text-center text-[#101313]"
          style={{
            opacity: titleProgress,
            transform: `translateY(${(1 - titleProgress) * 56}px)`,
            pointerEvents: titleProgress > 0.95 ? "auto" : "none",
          }}
        >
          <h1 className="font-brand-serif text-balance text-5xl font-normal leading-[0.88] tracking-[0.015em] md:text-6xl lg:whitespace-nowrap lg:text-[4.9rem]">
            A <span className="text-[#168b6d]">verifiable</span> agent memory kernel.
          </h1>
          <p className="font-brand-display mx-auto mt-5 max-w-4xl text-[1.9rem] font-black leading-[0.98] tracking-[-0.055em] md:text-[2.5rem]">
            <span className="block md:inline">把每一次对话，</span>
            <span className="block text-[#168b6d]">沉淀为可检索的记忆</span>
          </p>

          <time
            dateTime={siteIdentity.lastUpdated}
            className="mt-6 block font-mono text-[10px] tracking-[0.12em] text-muted-foreground"
          >
            Last updated · {siteIdentity.lastUpdated}
          </time>

          <div className="mx-auto mt-6 hidden max-w-3xl grid-cols-2 gap-4 text-left md:grid">
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
                  Coding Agent + 跨会话记忆内核
                </p>
                <CopyCode
                  className="mt-3"
                  code="curl -fsSL https://raw.githubusercontent.com/Gnosil/semantix/main/agent-skill/scripts/install.sh | sh"
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
