"use client";

import { useState } from "react";
import Image from "next/image";
import Reveal from "@/components/Reveal";

type KernelComponent = {
  num: string;
  titleEn: string;
  title: string;
  status: string;
  responsibility: string;
  io: string;
  limits: string;
  art: string;
  artAlt: string;
  artClassName: string;
  branchClassName?: string;
  href: string;
  linkLabel: string;
};

const components: KernelComponent[] = [
  {
    num: "01",
    titleEn: "Ingest & Slice",
    title: "会话摄取与切片",
    status: "已实现",
    responsibility:
      "把 Harness 事件与会话记录规范化，提取为带类型、作用域、来源和价值信息的语义切片，并持久化到本地库。",
    io: "输入事件流或会话 JSONL，输出可追溯、可检索、可维护的 Slice。",
    limits:
      "提取遵守大小、类型和作用域约束。畸形输入会跳过，抽象结果保留到原始会话的来源链接。",
    art: "/flower-ink/horizontal-01-plum-blossom.png",
    artAlt: "梅花枝条水墨插画",
    artClassName: "translate-x-[12%] rotate-[7deg] scale-[1.08]",
    branchClassName:
      "left-[70%] top-[77%] w-[14%] rotate-[25deg]",
    href: "https://github.com/Gnosil/semantix/tree/main/kernel/ingest",
    linkLabel: "查看摄取与切片",
  },
  {
    num: "02",
    titleEn: "Retrieve, Inject & Reuse",
    title: "检索注入与复用",
    status: "已实现",
    responsibility:
      "按作用域检索相关切片，把命中项组装成字节稳定的注入块，并对满足安全条件的只读结果执行 L3 复用。",
    io: "输入查询、作用域和当前依赖状态，输出检索命中、稳定注入块或经过验证的复用结果。",
    limits:
      "L2 无法确定时返回空注入，L3 无法证明依赖有效时拒绝复用。真实用户会话上的整体收益仍需继续验证。",
    art: "/flower-ink/horizontal-02-iris.png",
    artAlt: "鸢尾花水墨插画",
    artClassName: "translate-x-[15%] rotate-[3deg] scale-[0.98]",
    branchClassName:
      "left-[67%] top-[77%] w-[15%] rotate-[22deg]",
    href: "https://github.com/Gnosil/semantix/tree/main/kernel/inject",
    linkLabel: "查看检索与复用",
  },
  {
    num: "03",
    titleEn: "Schedule & Prefetch",
    title: "调度与投机预取",
    status: "MVP 已接线",
    responsibility:
      "根据任务意图、切片命中、资源状态和历史工具模式生成 RoundPlan，并在等待期保守预取可能需要的只读资源。",
    io: "输入当前轮次与历史转移信号，输出并发分组、模型层级提示、注入计划和只读预取任务。",
    limits:
      "正确性优先于并发和预取。runner 只允许白名单内的只读动作，证据不足、负载过高或浪费过多时停止预测。",
    art: "/flower-ink/horizontal-03-spider-lily.png",
    artAlt: "彼岸花水墨插画",
    artClassName: "translate-x-[12%] rotate-[7deg] scale-[1.08]",
    href: "https://github.com/Gnosil/semantix/tree/main/kernel/prefetch",
    linkLabel: "查看调度与预取",
  },
  {
    num: "04",
    titleEn: "Trust & Evolution",
    title: "验证安全与演化",
    status: "持续验证",
    responsibility:
      "把依赖指纹、复用判定、内容净化、命中反馈和用量事件连接起来，在可审计的边界内更新评分与参数。",
    io: "输入命中、修正、依赖变化、预取浪费和成本事件，输出复用判定、审计记录与有界参数更新。",
    limits:
      "优化层失败时回退正常执行，安全验证失败时拒绝复用。更新设有上下界、冻结期、来源记录和回滚路径。",
    art: "/flower-ink/horizontal-04-hydrangea.png",
    artAlt: "绣球花水墨插画",
    artClassName: "translate-x-[16%] rotate-[-3deg] scale-x-[-0.94] scale-y-[0.94]",
    branchClassName:
      "left-[54%] top-[77%] w-[15%] rotate-[23deg]",
    href: "https://github.com/Gnosil/semantix/tree/main/kernel/evolve",
    linkLabel: "查看验证与演化",
  },
];

function ComponentList() {
  const [activeIndex, setActiveIndex] = useState<number | null>(null);
  const [renderedIndex, setRenderedIndex] = useState(0);
  const active = components[renderedIndex];
  const isPanelOpen = activeIndex !== null;

  const toggleComponent = (index: number) => {
    if (activeIndex === index) {
      setActiveIndex(null);
      return;
    }

    setRenderedIndex(index);
    setActiveIndex(index);
  };

  return (
    <Reveal delay={70}>
      <div className="mt-12">
        <div
          role="group"
          aria-label="Semantix 内核组件"
          className="flex snap-x gap-3 overflow-x-auto pb-3 md:grid md:grid-cols-4 md:overflow-visible md:pb-0"
        >
          {components.map((component, index) => {
            const isActive = activeIndex === index;

            return (
              <button
                key={component.num}
                id={`kernel-component-control-${component.num}`}
                type="button"
                aria-expanded={isActive}
                aria-controls="kernel-component-panel"
                onClick={() => toggleComponent(index)}
                className={`group relative min-h-[10.5rem] min-w-[13.5rem] snap-start scroll-mt-20 overflow-hidden border px-5 py-5 text-left transition-[background-color,border-color,color,transform] duration-300 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-5px] md:min-w-0 ${
                  isActive
                    ? "-translate-y-1 border-[#168b6d] bg-[#168b6d] text-white focus-visible:outline-white"
                    : "border-[#101313]/18 bg-white text-[#101313] hover:border-[#168b6d] hover:text-[#168b6d] focus-visible:outline-[#168b6d]"
                }`}
              >
                {isActive ? (
                  <Image
                    src={component.art}
                    alt=""
                    fill
                    sizes="(max-width: 767px) 13.5rem, 25vw"
                    className={`pointer-events-none object-contain object-right p-1 brightness-0 invert opacity-35 ${component.artClassName}`}
                  />
                ) : null}
                {isActive && component.branchClassName ? (
                  <span
                    aria-hidden="true"
                    className={`pointer-events-none absolute z-[1] h-px origin-left bg-gradient-to-r from-white/35 via-white/30 to-transparent ${component.branchClassName}`}
                  />
                ) : null}
                <span className="relative z-[1] flex items-center justify-between font-mono text-[10px] font-semibold tracking-[0.16em]">
                  <span>{component.num}</span>
                  <span className="flex items-center gap-3">
                    <span className={isActive ? "text-white/70" : "text-[#101313]/38"}>
                      {component.status}
                    </span>
                    <span aria-hidden="true" className="text-base leading-none">
                      {isActive ? "−" : "+"}
                    </span>
                  </span>
                </span>
                <span className="font-brand-display relative z-[1] mt-8 block text-2xl font-black tracking-[-0.045em] md:text-[1.65rem] lg:text-3xl">
                  {component.title}
                </span>
                <span
                  className={`relative z-[1] mt-2 block font-mono text-[9px] uppercase tracking-[0.15em] ${
                    isActive ? "text-white/65" : "text-[#101313]/38 group-hover:text-[#168b6d]/70"
                  }`}
                >
                  {component.titleEn}
                </span>
              </button>
            );
          })}
        </div>

        <div
          id="kernel-component-panel"
          role="region"
          aria-labelledby={`kernel-component-control-${active.num}`}
          aria-hidden={!isPanelOpen}
          className={`grid bg-white transition-[grid-template-rows,opacity,margin] duration-500 ease-[cubic-bezier(0.16,1,0.3,1)] motion-reduce:transition-none ${
            isPanelOpen
              ? "mt-3 grid-rows-[1fr] opacity-100"
              : "mt-0 grid-rows-[0fr] opacity-0"
          }`}
        >
          <article className="min-h-0 overflow-hidden">
            <div
              key={active.num}
              className="semantix-detail-in flex max-w-5xl flex-col p-6 text-[#101313] md:p-9 lg:px-10 lg:pb-5 lg:pt-11"
            >
            <div className="flex flex-wrap items-start justify-between gap-4">
              <div>
                <p className="font-mono text-[10px] uppercase tracking-[0.16em] text-[#168b6d]">
                  {active.titleEn}
                </p>
                <h3 className="font-brand-display mt-3 text-4xl font-black tracking-[-0.05em] md:text-5xl">
                  {active.title}
                </h3>
              </div>
              <span className="border-l-2 border-[#168b6d] pl-3 font-mono text-[10px] font-semibold tracking-[0.12em] text-[#168b6d]">
                {active.status}
              </span>
            </div>

            <p className="mt-6 max-w-2xl text-sm leading-7 text-[#101313]/68 md:text-[15px]">
              {active.responsibility}
            </p>

            <dl className="mt-7 grid gap-6 border-t border-[#101313]/14 pt-6 text-sm leading-6 text-[#101313]/60 sm:grid-cols-2">
              <div>
                <dt className="font-mono text-[9px] font-semibold uppercase tracking-[0.16em] text-[#168b6d]">
                  输入与输出
                </dt>
                <dd className="mt-2">{active.io}</dd>
              </div>
              <div>
                <dt className="font-mono text-[9px] font-semibold uppercase tracking-[0.16em] text-[#168b6d]">
                  当前边界
                </dt>
                <dd className="mt-2">{active.limits}</dd>
              </div>
            </dl>

            <a
              href={active.href}
              target="_blank"
              rel="noreferrer"
              tabIndex={isPanelOpen ? undefined : -1}
              className="mt-8 w-fit border-b border-[#168b6d]/45 pb-1 text-sm font-semibold text-[#101313] transition-colors hover:border-[#168b6d] hover:text-[#168b6d] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#168b6d]"
            >
              {active.linkLabel} ↗
            </a>
            </div>
          </article>
        </div>

        <div
          aria-hidden="true"
          className={`transition-[height] duration-500 ease-[cubic-bezier(0.16,1,0.3,1)] motion-reduce:transition-none ${
            isPanelOpen ? "h-0" : "h-8 md:h-10"
          }`}
        />
      </div>
    </Reveal>
  );
}

export default function Components() {
  return (
    <section
      id="components"
      className="border-t border-[#d7ddd9] bg-white text-[#168b6d]"
    >
      <div className="mx-auto max-w-[1600px] px-5 pb-3 pt-16 md:px-10 md:pb-3 md:pt-20">
        <Reveal>
          <header className="max-w-4xl">
            <p className="font-mono text-[10px] font-semibold tracking-[0.24em]">
              Components 内核组件
            </p>
            <h2 className="font-brand-display mt-4 text-[clamp(2.5rem,3.6vw,4.25rem)] font-black leading-[0.92] tracking-[-0.06em] text-[#101313]">
              一个内核，
              <span className="text-[#168b6d]">四个组件。</span>
            </h2>
            <p className="mt-6 max-w-3xl text-sm leading-7 text-[#101313]/60 md:text-base">
              四个组件组把一次会话变成下一次可以复用的经验：先摄取与切片，再检索与复用，同时完成资源编排，最后用验证和反馈守住边界。
            </p>
          </header>
        </Reveal>

        <ComponentList />
      </div>
    </section>
  );
}
