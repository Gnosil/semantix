"use client";

import Image from "next/image";
import { useEffect, useRef } from "react";

import Reveal from "@/components/Reveal";
import { siteIdentity } from "@/lib/site-identity";

const contributors = ["Gnosil", "radianceded", "jh10724-dotcom", "Allenllii"];
const marqueeContributors = Array.from({ length: 3 }, () => contributors).flat();
const repo = siteIdentity.repositoryUrl;

const communityLinks = [
  { label: "验证方法", href: `${repo}/blob/main/docs/QUICKSTART.md` },
  { label: "技术讨论", href: `${repo}/issues` },
  {
    label: "安全边界",
    href: `${repo}/blob/main/docs/Security-安全设计.md`,
  },
  { label: "贡献指南", href: `${repo}/blob/main/CONTRIBUTING.md` },
];

export default function Community() {
  const scrollCopyRef = useRef<HTMLHeadingElement>(null);

  useEffect(() => {
    const copy = scrollCopyRef.current;

    if (!copy) return;

    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      copy.dataset.visible = "true";
      return;
    }

    const observer = new IntersectionObserver(
      ([entry]) => {
        if (!entry.isIntersecting) return;
        copy.dataset.visible = "true";
        observer.disconnect();
      },
      { threshold: 0.22 },
    );

    observer.observe(copy);

    return () => observer.disconnect();
  }, []);

  return (
    <section
      id="community"
      className="scroll-mt-16 overflow-hidden bg-white text-[#111411]"
    >
      <style>{`
        @keyframes community-crew-slide {
          from { transform: translateX(0); }
          to { transform: translateX(-50%); }
        }
        @keyframes community-crew-slide-reverse {
          from { transform: translateX(-50%); }
          to { transform: translateX(0); }
        }
        .community-crew-row {
          display: flex;
          width: max-content;
          animation: community-crew-slide 38s linear infinite;
          will-change: transform;
        }
        .community-crew-row--reverse {
          animation-name: community-crew-slide-reverse;
        }
        .community-crew-track:is(:hover, :focus-within) .community-crew-row {
          animation-play-state: paused;
        }
        .community-scroll-open {
          opacity: 0;
          transform: translateY(2.75rem);
          transition: opacity 900ms cubic-bezier(0.16, 1, 0.3, 1), transform 1050ms cubic-bezier(0.16, 1, 0.3, 1);
          will-change: transform, opacity;
        }
        .community-scroll-title {
          opacity: 0;
          transform: translateY(2.25rem);
          transition: opacity 900ms 110ms cubic-bezier(0.16, 1, 0.3, 1), transform 1050ms 110ms cubic-bezier(0.16, 1, 0.3, 1);
          will-change: transform, opacity;
        }
        .community-scroll-copy[data-visible="true"] .community-scroll-open,
        .community-scroll-copy[data-visible="true"] .community-scroll-title {
          opacity: 1;
          transform: none;
        }
        @media (prefers-reduced-motion: reduce) {
          .community-crew-row {
            animation: none;
            transform: none;
          }
          .community-scroll-open,
          .community-scroll-title {
            transition: none;
            opacity: 1;
            transform: none;
          }
        }
      `}</style>
      <div className="mx-auto w-full max-w-[1600px] px-5 pb-14 pt-10 md:px-10 md:pb-16 md:pt-10 lg:px-12 lg:pb-20 lg:pt-12">
        <div>
          <div className="flex items-center justify-between gap-6 pb-4">
            <p className="font-mono text-[10px] font-semibold tracking-[0.24em] text-[#168b6d]">
              Community 社区
            </p>
          </div>

          <div className="grid gap-9 pt-9 md:grid-cols-[minmax(20rem,0.8fr)_minmax(0,1.2fr)] md:items-center md:gap-10 md:pt-11 lg:gap-16">
            <h2
              ref={scrollCopyRef}
              className="community-scroll-copy md:order-2 md:-translate-y-8 md:text-right lg:-translate-y-12"
            >
              <span className="community-scroll-open font-brand-display block text-[clamp(3.5rem,6.6vw,7rem)] font-black leading-[0.92] tracking-[-0.065em] text-[#111411]">
                共同构建，
              </span>
              <span className="community-scroll-title font-brand-display block text-[clamp(3.5rem,6.6vw,7rem)] font-black leading-[0.92] tracking-[-0.065em] text-[#168b6d]">
                共同验证。
              </span>
            </h2>

            <div className="md:order-1 md:pb-1">
              <p className="font-brand-display max-w-[34rem] text-2xl font-black leading-tight tracking-[-0.035em] text-[#111411] md:text-3xl">
                所有贡献，都应留下可复核的路径。
              </p>
              <p className="mt-4 max-w-[38rem] text-sm leading-7 text-[#111411]/62 md:text-base md:leading-8">
                Semantix 在 GitHub 公开开发。每个 Issue、PR
                与验证结果，都应留下可检查的输入、方法和结论。
              </p>
              <nav aria-label="社区参与入口" className="mt-4">
                <ul className="flex list-none flex-wrap gap-x-6 gap-y-1 p-0">
                  {communityLinks.map((link) => (
                    <li key={link.href}>
                      <a
                        href={link.href}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex min-h-10 items-center font-mono text-[10px] font-semibold tracking-[0.06em] transition-colors hover:text-[#168b6d] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#168b6d]"
                      >
                        {link.label} ↗
                      </a>
                    </li>
                  ))}
                </ul>
              </nav>
            </div>
          </div>
        </div>

        <Reveal
          delay={70}
          className="motion-reduce:translate-y-0 motion-reduce:opacity-100 motion-reduce:transition-none"
        >
          <div className="mt-7 flex flex-col gap-5 sm:flex-row sm:items-center sm:justify-between md:mt-8">
            <div>
              <p className="font-brand-display text-2xl font-black tracking-[-0.04em] md:text-3xl">
                贡献者
              </p>
              <p className="mt-1 text-xs leading-5 text-[#111411]/55">
                以 GitHub commit、PR 审阅和 Contributors 记录为准。
              </p>
            </div>
            <div className="flex flex-wrap items-center gap-5 md:-translate-x-4 md:-translate-y-8 lg:-translate-x-6 lg:-translate-y-12">
              <a
                href={`${repo}/blob/main/CONTRIBUTING.md`}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex min-h-10 items-center whitespace-nowrap bg-[#168b6d] px-4 py-2 text-sm font-bold text-[#f8f8f4] transition-[background-color,transform] hover:bg-[#116f58] active:translate-y-px focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#168b6d] motion-reduce:transform-none"
              >
                参与贡献 ↗
              </a>
              <a
                href={`${repo}/graphs/contributors`}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex min-h-10 items-center whitespace-nowrap text-sm font-semibold transition-colors hover:text-[#168b6d] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#168b6d]"
              >
                查看贡献记录 ↗
              </a>
            </div>
          </div>

          <div
            aria-label="Semantix 贡献者"
            className="community-crew-track -mx-5 mt-5 overflow-hidden py-3 [mask-image:linear-gradient(to_right,transparent,black_5%,black_95%,transparent)] [-webkit-mask-image:linear-gradient(to_right,transparent,black_5%,black_95%,transparent)] md:-mx-10 md:mt-6 lg:-mx-12"
          >
            <div className="community-crew-row">
              {[0, 1].map((copyIndex) => (
                <div
                  key={copyIndex}
                  aria-hidden={copyIndex === 1}
                  className="flex shrink-0 items-center gap-4 pr-4"
                >
                  {marqueeContributors.map((login, contributorIndex) => {
                    const isAccessible =
                      copyIndex === 0 && contributorIndex < contributors.length;

                    return (
                      <a
                        key={`${copyIndex}-${contributorIndex}-${login}`}
                        href={`https://github.com/${login}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        tabIndex={isAccessible ? undefined : -1}
                        aria-hidden={!isAccessible}
                        aria-label={
                          isAccessible ? `查看 ${login} 的 GitHub 主页` : undefined
                        }
                        className="group flex shrink-0 items-center gap-3 pr-5 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#168b6d]"
                      >
                        <Image
                          src={`https://github.com/${login}.png?size=240`}
                          alt={isAccessible ? `${login} 的 GitHub 头像` : ""}
                          width={112}
                          height={112}
                          sizes="3.5rem"
                          className="size-14 rounded-full object-cover saturate-[0.75] ring-1 ring-[#111411]/18 transition-[transform,filter] duration-300 group-hover:scale-105 group-hover:saturate-100 motion-reduce:transition-none"
                        />
                        <span className="font-mono text-[10px] tracking-[0.06em] text-[#111411]/58 transition-colors group-hover:text-[#168b6d]">
                          @{login}
                        </span>
                      </a>
                    );
                  })}
                </div>
              ))}
            </div>
          </div>

          <div
            aria-hidden="true"
            className="community-crew-track -mx-5 mt-1 overflow-hidden py-3 [mask-image:linear-gradient(to_right,transparent,black_5%,black_95%,transparent)] [-webkit-mask-image:linear-gradient(to_right,transparent,black_5%,black_95%,transparent)] md:-mx-10 lg:-mx-12"
          >
            <div className="community-crew-row community-crew-row--reverse">
              {[0, 1].map((copyIndex) => (
                <div
                  key={copyIndex}
                  className="flex shrink-0 items-center gap-4 pr-4"
                >
                  {[...marqueeContributors].reverse().map((login, contributorIndex) => (
                    <a
                      key={`${copyIndex}-${contributorIndex}-${login}`}
                      href={`https://github.com/${login}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      tabIndex={-1}
                      className="group flex shrink-0 items-center gap-3 pr-5"
                    >
                      <Image
                        src={`https://github.com/${login}.png?size=240`}
                        alt=""
                        width={112}
                        height={112}
                        sizes="3.5rem"
                        className="size-14 rounded-full object-cover saturate-[0.75] ring-1 ring-[#111411]/18 transition-[transform,filter] duration-300 group-hover:scale-105 group-hover:saturate-100 motion-reduce:transition-none"
                      />
                      <span className="font-mono text-[10px] tracking-[0.06em] text-[#111411]/58 transition-colors group-hover:text-[#168b6d]">
                        @{login}
                      </span>
                    </a>
                  ))}
                </div>
              ))}
            </div>
          </div>
        </Reveal>

        <div aria-hidden="true" className="h-2 md:h-4" />
      </div>
    </section>
  );
}
