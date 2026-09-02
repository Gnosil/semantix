"use client";

import Image from "next/image";

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
        @media (prefers-reduced-motion: reduce) {
          .community-crew-row {
            animation: none;
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

          <div className="pt-9 md:pt-11">
            <div className="mx-auto max-w-[42rem] text-center">
              <h2 className="font-brand-display mx-auto max-w-[42rem] text-[clamp(2rem,3.4vw,2.75rem)] font-black leading-[1.08] tracking-[-0.045em] text-[#111411]">
                所有贡献，都应留下可复核的路径。
              </h2>
              <p className="mx-auto mt-4 max-w-[38rem] text-sm leading-7 text-[#111411]/62 md:text-base md:leading-8">
                Semantix 在 GitHub 公开开发。每个 Issue、PR
                与验证结果，都应留下可检查的输入、方法和结论。
              </p>
              <nav aria-label="社区参与入口" className="mt-4">
                <ul className="flex list-none flex-wrap justify-center gap-x-6 gap-y-1 p-0">
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
          <div className="mt-12 text-center md:mt-14">
            <p className="font-brand-display text-lg font-black tracking-[-0.035em] md:text-xl">
              贡献者
            </p>
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

          <p className="mt-5 text-center text-xs leading-5 text-[#111411]/55">
            以 GitHub commit、PR 审阅和 Contributors 记录为准。
          </p>

          <div className="mt-4 flex flex-wrap items-center justify-center gap-5 md:mt-5">
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
        </Reveal>

        <div aria-hidden="true" className="h-2 md:h-4" />
      </div>
    </section>
  );
}
