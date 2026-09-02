"use client";

import { useEffect, useRef } from "react";

import { siteIdentity } from "@/lib/site-identity";

function FinaleCopy({
  inverse = false,
  decorative = false,
}: {
  inverse?: boolean;
  decorative?: boolean;
}) {
  const foreground = inverse ? "text-[#168b6d]" : "text-[#f8f8f4]";
  const button = inverse
    ? "bg-[#168b6d] text-white"
    : "bg-[#f8f8f4] text-[#0b654f]";
  const utilityLinks = [
    ["About", "/about"],
    ["Docs", "/docs"],
    ["Privacy", "/privacy"],
    ["Contact", "/contact"],
    ["GitHub", siteIdentity.repositoryUrl],
  ] as const;

  return (
    <div
      aria-hidden={decorative || undefined}
      className={`pointer-events-none absolute inset-0 overflow-hidden ${foreground}`}
    >
      <div className="absolute left-5 top-[4.5svh] font-mono text-[8px] font-semibold uppercase leading-[1.7] tracking-[0.25em] md:left-9 md:text-[10px]">
        Semantix / 04
        <br />
        Memory system
      </div>

      <div className="absolute right-5 top-[4.5svh] text-right font-mono text-[8px] font-semibold uppercase leading-[1.7] tracking-[0.25em] md:right-9 md:text-[10px]">
        Open source
        <br />
        MIT license / 2026
      </div>

      <h2
        id={decorative ? undefined : "bloom-finale-title"}
        className="font-brand-serif absolute inset-x-0 top-[11svh] m-0 whitespace-nowrap text-center text-[clamp(5rem,17.4vw,22rem)] font-normal uppercase leading-[0.72] tracking-[-0.075em]"
      >
        Semantix
      </h2>

      <div className="absolute inset-x-5 top-[33svh] flex flex-col items-center text-center md:inset-x-32 md:top-[35svh]">
        <p className="font-mono text-[9px] font-semibold uppercase leading-[1.75] tracking-[0.2em] md:text-[11px]">
          <span className="block">A verifiable memory kernel for agents</span>
          <span className="block">Retrieve • Validate • Evolve</span>
        </p>
        {decorative ? (
          <span
            className={`mt-5 inline-flex min-h-11 items-center justify-center px-7 py-3 font-mono text-[10px] font-bold uppercase tracking-[0.2em] md:min-h-12 md:px-10 md:text-xs ${button}`}
          >
            Enter the repository ↗
          </span>
        ) : (
          <a
            href={siteIdentity.repositoryUrl}
            target="_blank"
            rel="noopener noreferrer"
            className={`pointer-events-auto mt-5 inline-flex min-h-11 items-center justify-center px-7 py-3 font-mono text-[10px] font-bold uppercase tracking-[0.2em] transition-transform hover:-translate-y-0.5 active:translate-y-px focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-current md:min-h-12 md:px-10 md:text-xs ${button}`}
          >
            Enter the repository ↗
            <span className="sr-only"> (opens in a new tab)</span>
          </a>
        )}
      </div>

      <div className="absolute bottom-[0.65svh] left-2 z-40 font-mono text-[7px] font-semibold uppercase leading-[1.7] tracking-[0.2em] md:text-[9px]">
        Semantix © 2026
        <br />
        {siteIdentity.licenseName}
      </div>

      <div className="absolute bottom-[0.65svh] right-2 z-40 max-w-[48vw] text-right font-mono text-[7px] font-semibold leading-[1.7] tracking-[0.12em] md:text-[9px]">
        <p className="hidden md:block">
          由 {siteIdentity.operator.legalName} 运营与维护
        </p>
        <nav aria-label={decorative ? undefined : "终章导航"} className="mt-1 flex flex-wrap justify-end gap-x-3 uppercase md:gap-x-4">
          {utilityLinks.map(([label, href]) =>
            decorative ? (
              <span key={label}>{label}</span>
            ) : (
              <a
                key={label}
                href={href}
                target={href.startsWith("http") ? "_blank" : undefined}
                rel={href.startsWith("http") ? "noopener noreferrer" : undefined}
                className="pointer-events-auto underline decoration-current/30 underline-offset-4 transition-opacity hover:opacity-65"
              >
                {label}
              </a>
            ),
          )}
        </nav>
      </div>
    </div>
  );
}

export default function BloomCurtain() {
  const sceneRef = useRef<HTMLElement>(null);
  const videoRef = useRef<HTMLVideoElement>(null);
  const curtainRef = useRef<HTMLDivElement>(null);
  const curtainContentRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const scene = sceneRef.current;
    const video = videoRef.current;
    const curtain = curtainRef.current;
    const curtainContent = curtainContentRef.current;
    if (!scene || !video || !curtain || !curtainContent) return;

    const mediaQuery = window.matchMedia("(prefers-reduced-motion: reduce)");
    let frame = 0;
    let bloomActive = false;
    let lastScrollY = window.scrollY;
    let lastRawCurtainProgress = 0;
    let reverseCurtainMode = false;

    video.defaultPlaybackRate = 1.2;
    video.playbackRate = 1.2;

    const setBloomVisibility = (active: boolean) => {
      if (active === bloomActive) return;
      bloomActive = active;
      window.dispatchEvent(
        new CustomEvent("semantix:bloom-visibility", { detail: { active } }),
      );
    };

    const showFinalFrame = () => {
      if (Number.isFinite(video.duration) && video.duration > 0) {
        video.currentTime = Math.max(video.duration - 0.04, 0);
      }
    };

    const playForward = () => {
      if (video.ended && video.duration > 0) return;
      void video.play().catch(() => undefined);
    };

    const syncToScroll = () => {
      const rect = scene.getBoundingClientRect();
      const currentScrollY = window.scrollY;
      const direction = currentScrollY - lastScrollY;
      const sceneVisible = rect.top < window.innerHeight && rect.bottom > 0;
      const travel = Math.max(scene.offsetHeight - window.innerHeight, 1);
      const rawCurtainProgress = Math.min(Math.max(-rect.top / travel, 0), 1);
      if (
        direction < -1 &&
        (rawCurtainProgress > 0.92 || lastRawCurtainProgress > 0.92)
      ) {
        reverseCurtainMode = true;
      }
      if (
        rawCurtainProgress <= 0.001 ||
        (direction > 1 && rawCurtainProgress >= 0.999)
      ) {
        reverseCurtainMode = false;
      }
      const forwardCurtainProgress = Math.min(
        Math.max((rawCurtainProgress - 0.18) / 0.82, 0),
        1,
      );
      const reverseCurtainProgress = Math.min(rawCurtainProgress / 0.7, 1);
      const curtainProgress = mediaQuery.matches
        ? rawCurtainProgress >= 0.5
          ? 1
          : 0
        : reverseCurtainMode
          ? reverseCurtainProgress
          : forwardCurtainProgress;

      curtain.style.transform = `translate3d(0, -${curtainProgress * 100}%, 0)`;
      curtainContent.style.transform = `translate3d(0, ${curtainProgress * window.innerHeight}px, 0)`;

      setBloomVisibility(rect.top <= 64);

      if (!mediaQuery.matches) {
        if (direction > 1 && sceneVisible && curtainProgress > 0.02) {
          playForward();
        }
        if (direction < -1) {
          video.pause();
          if (curtainProgress <= 0.01) {
            video.currentTime = 0;
          }
        }
      }

      lastRawCurtainProgress = rawCurtainProgress;
      lastScrollY = currentScrollY;
    };

    const scheduleSync = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(syncToScroll);
    };

    const syncMotionPreference = () => {
      if (mediaQuery.matches) {
        video.pause();
        showFinalFrame();
      } else if (video.readyState >= HTMLMediaElement.HAVE_METADATA) {
        video.currentTime = 0;
      }
      scheduleSync();
    };

    const initializeVideo = () => {
      video.pause();
      if (mediaQuery.matches) {
        showFinalFrame();
      } else {
        video.currentTime = 0;
      }
      scheduleSync();
    };

    video.pause();
    if (video.readyState >= HTMLMediaElement.HAVE_METADATA) {
      initializeVideo();
    }
    video.addEventListener("loadedmetadata", initializeVideo);
    window.addEventListener("scroll", scheduleSync, { passive: true });
    window.addEventListener("resize", scheduleSync);
    mediaQuery.addEventListener("change", syncMotionPreference);
    scheduleSync();

    return () => {
      cancelAnimationFrame(frame);
      video.pause();
      video.removeEventListener("loadedmetadata", initializeVideo);
      window.removeEventListener("scroll", scheduleSync);
      window.removeEventListener("resize", scheduleSync);
      mediaQuery.removeEventListener("change", syncMotionPreference);
      if (bloomActive) {
        window.dispatchEvent(
          new CustomEvent("semantix:bloom-visibility", {
            detail: { active: false },
          }),
        );
      }
    };
  }, []);

  return (
    <section
      ref={sceneRef}
      id="bloom-finale"
      aria-labelledby="bloom-finale-title"
      className="relative h-[200svh] bg-[#168b6d]"
    >
      <div className="sticky top-0 isolate h-[100svh] overflow-hidden border-x-[10px] border-b-[10px] border-[#168b6d] bg-[#168b6d] md:border-x-[18px] md:border-b-[18px]">
        <div className="absolute inset-0 z-20">
          <FinaleCopy />
        </div>

        <video
          ref={videoRef}
          aria-hidden="true"
          className="absolute inset-0 z-10 h-full w-full object-contain object-bottom mix-blend-screen will-change-transform"
          muted
          playsInline
          preload="auto"
          style={{
            filter:
              "grayscale(1) brightness(0.67) contrast(4.4) brightness(1.5)",
          }}
        >
          <source src="/media/semantix-bloom-4k.mp4" type="video/mp4" />
        </video>

        <div
          ref={curtainRef}
          aria-hidden="true"
          className="pointer-events-none absolute inset-0 z-30 overflow-hidden bg-[#f8f8f4] will-change-transform"
        >
          <div
            ref={curtainContentRef}
            className="absolute inset-0 will-change-transform"
          >
            <FinaleCopy inverse decorative />
          </div>
        </div>
      </div>
    </section>
  );
}
