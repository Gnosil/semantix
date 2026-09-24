"use client";

import { useState } from "react";
import Image from "next/image";
import CopyCode from "@/components/CopyCode";

type Capability = {
  num: string;
  titleEn: string;
  title: string;
  body: string;
  code: string;
  art: string;
  artClassName: string;
};

const capabilities: Capability[] = [
  {
    num: "01",
    titleEn: "Cross-Session Memory",
    title: "跨会话记忆",
    body: "会话结束后，项目约定、任务模式、工具路径和已验证结果仍会保留下来。下一次进入同一项目，不必重新建立全部背景。",
    code: "finished session → reusable project memory → next session",
    art: "/flower-ink/feature-01-wisteria-landscape.png",
    artClassName: "translate-x-[3%] -rotate-[1deg] scale-[1.05]",
  },
  {
    num: "02",
    titleEn: "Less Repeated Spend",
    title: "减少重复付费",
    body: "相似任务先复用已有上下文和安全结果，再决定是否重新调用模型。稳定的注入内容还能提高厂商前缀缓存的实际命中机会。",
    code: "semantic hit → stable bytes → fewer repeated tokens",
    art: "/flower-ink/feature-02-orchid-landscape.png",
    artClassName: "translate-x-[2%] rotate-[1deg] scale-[1.02]",
  },
  {
    num: "03",
    titleEn: "Faster Repeat Work",
    title: "减少重复工作",
    body: "Semantix 复用已经证明有效的背景与执行模式，让 Agent 少做重复查找、重复读取和重复推导，把时间留给真正变化的部分。",
    code: "known context + proven path → less setup → faster work",
    art: "/flower-ink/feature-03-trumpet-vine-landscape.png",
    artClassName: "translate-x-[3%] -rotate-[1deg] scale-[1.04]",
  },
  {
    num: "04",
    titleEn: "Useful Waiting Time",
    title: "利用等待时间",
    body: "模型生成答案时，内核可以保守地准备下一步可能需要的只读资源。预测证据不足或浪费过高时，预取会自动收缩。",
    code: "model wait → safe read-only prefetch → next context ready",
    art: "/flower-ink/feature-04-chrysanthemum-landscape.png",
    artClassName: "translate-x-[2%] rotate-[1deg] scale-[1.04]",
  },
  {
    num: "05",
    titleEn: "Bounded Learning",
    title: "越用越准确",
    body: "命中、未命中、人工修正和预取浪费都会回流到评分与阈值。更新有上下界、冻结期和回滚路径，不让一次异常放大成长期偏差。",
    code: "feedback → bounded update → better next decision",
    art: "/flower-ink/feature-05-hydrangea-landscape.png",
    artClassName: "translate-x-[2%] rotate-[1deg] scale-[1.03]",
  },
  {
    num: "06",
    titleEn: "Works With Your Agent",
    title: "多 Agent 适配",
    body: "既可以直接使用内置记忆内核的 Semantix Agent，也可以通过 Agent Skill、工具注册或 Gateway，把同一套能力接入现有工作流。",
    code: "agent skill | tool hooks | gateway → one kernel",
    art: "/flower-ink/feature-06-roses-landscape.png",
    artClassName: "translate-x-[2%] -rotate-[1deg] scale-[1.03]",
  },
];

const groups = [
  { label: "LOOP A / MEMORY", action: "沉淀  →  复用", indices: [0, 1] as const },
  { label: "LOOP B / SCHEDULING", action: "决策  →  预取", indices: [2, 3] as const },
  { label: "LOOP C / EVOLUTION", action: "反馈  →  适配", indices: [4, 5] as const },
];

export default function Features() {
  const [active, setActive] = useState<number | null>(null);

  const capabilityButton = (index: number, panelId: string) => {
    const item = capabilities[index];
    const isActive = active === index;

    return (
      <button
        key={item.num}
        type="button"
        onClick={() => setActive(isActive ? null : index)}
        aria-expanded={isActive}
        aria-controls={panelId}
        className="group grid w-full scroll-mt-20 grid-cols-[3rem_1fr_auto] items-center gap-3 border-b border-[#101313]/20 py-5 text-left md:grid-cols-[5rem_1fr_auto] md:py-6"
      >
        <span
          className={`font-mono text-[10px] tracking-[0.16em] transition-colors ${
            isActive ? "text-[#168b6d]" : "text-[#101313]/35"
          }`}
        >
          {item.num}
        </span>
        <span className="min-w-0">
          <span
            className={`font-feature-serif block text-2xl font-semibold tracking-[-0.03em] transition-all duration-500 md:text-4xl ${
              isActive
                ? "translate-x-2 text-[#101313]"
                : "text-[#101313]/45 group-hover:translate-x-1 group-hover:text-[#101313]/75"
            }`}
          >
            {item.title}
          </span>
          <span
            className={`mt-1 block truncate font-mono text-[9px] uppercase tracking-[0.18em] transition-colors md:text-[10px] ${
              isActive ? "text-[#168b6d]" : "text-[#101313]/30"
            }`}
          >
            {item.titleEn}
          </span>
        </span>
        <span
          aria-hidden="true"
          className={`mr-1 size-3 rotate-45 border transition-all duration-500 ${
            isActive
              ? "border-[#168b6d] bg-[#168b6d]"
              : "border-[#101313]/30 bg-transparent"
          }`}
        />
      </button>
    );
  };

  const capabilityPanel = (
    groupIndex: number,
    indices: readonly [number, number],
  ) => {
    const isOpen = active !== null && indices.includes(active);
    const selected = isOpen ? capabilities[active] : null;
    const panelId = `semantix-feature-detail-${groupIndex}`;

    return (
      <div
        id={panelId}
        className={`grid transition-[grid-template-rows,opacity] duration-700 ease-[cubic-bezier(0.16,1,0.3,1)] motion-reduce:transition-none ${
          isOpen ? "grid-rows-[1fr] opacity-100" : "grid-rows-[0fr] opacity-0"
        }`}
      >
        <div className="min-h-0 overflow-hidden">
          <div className="relative overflow-hidden border-b border-[#101313] bg-[#2c8c75] text-[#f7f6f1]">
            <div className="semantix-grid pointer-events-none absolute inset-0 opacity-20" aria-hidden="true" />
            {selected ? (
              <div key={selected.num} className="semantix-detail-in relative min-h-[28rem] overflow-hidden md:min-h-[34rem]">
                <div
                  aria-hidden="true"
                  className="pointer-events-none absolute inset-0 bg-[linear-gradient(135deg,rgba(255,255,255,0.045),transparent_44%)]"
                />

                <figure
                  aria-hidden="true"
                  className="pointer-events-none absolute inset-y-0 right-0 hidden w-[58%] overflow-hidden md:block"
                >
                  <Image
                    src={selected.art}
                    alt=""
                    fill
                    sizes="58vw"
                    className={`object-cover object-right-center brightness-0 invert opacity-42 ${selected.artClassName}`}
                  />
                  <span className="absolute inset-y-0 left-0 w-[30%] bg-gradient-to-r from-[#2c8c75] via-[#2c8c75]/70 to-transparent" />
                </figure>

                <div className="relative z-[1] flex min-h-[28rem] flex-col p-6 md:min-h-[34rem] md:max-w-[64%] md:p-9 lg:p-10">
                  <div className="flex items-center justify-between font-mono text-[9px] tracking-[0.2em] text-[#bde8d9] md:text-[10px]">
                  <span>CAPABILITY DETAIL / {selected.num}</span>
                  <div className="flex gap-1.5" aria-hidden="true">
                    {indices.map((index) => (
                      <span
                        key={index}
                        className={`h-1.5 transition-all duration-500 ${
                          index === active ? "w-8 bg-[#bde8d9]" : "w-1.5 bg-white/25"
                        }`}
                      />
                    ))}
                  </div>
                  </div>

                  <div className="my-auto py-8">
                    <p className="font-mono text-[10px] uppercase tracking-[0.18em] text-white/40">
                      {selected.titleEn}
                    </p>
                    <h3 className="font-brand-display mt-4 text-4xl font-black tracking-[-0.055em] md:text-5xl">
                      {selected.title}
                    </h3>
                    <p className="mt-5 max-w-2xl text-sm leading-7 text-white/70 md:text-base">
                      {selected.body}
                    </p>
                    <CopyCode
                      className="mt-7 max-w-2xl rounded-none"
                      code={selected.code}
                      tone="dark"
                    />
                  </div>

                  <div className="flex items-center gap-3 font-mono text-[9px] tracking-[0.18em] text-white/35">
                    <span className="h-px flex-1 bg-white/15" />
                    SEMANTIX / {selected.num}
                  </div>
                </div>
              </div>
            ) : null}
          </div>
        </div>
      </div>
    );
  };

  return (
    <section
      id="features"
      className="border-t border-[#d7ddd9] bg-white text-[#101313]"
    >
      <div hidden>
        {capabilities.map((capability) => (
          <CopyCode key={capability.num} code={capability.code} />
        ))}
      </div>
      <div className="mx-auto grid max-w-[1600px] gap-14 px-5 py-20 md:px-10 md:py-24 lg:grid-cols-[0.8fr_1.2fr] lg:gap-20">
        <div className="lg:sticky lg:top-24 lg:self-start">
          <p className="font-mono text-[10px] font-medium tracking-[0.24em] text-[#168b6d]">
            Features 特性
          </p>
          <h2 className="font-brand-display mt-7 max-w-xl text-[clamp(2.7rem,5.8vw,6.5rem)] font-black leading-[0.98] tracking-[-0.055em]">
            <span className="block whitespace-nowrap">跨会话复用，</span>
            <span className="block whitespace-nowrap text-[#168b6d]">能力持续进化。</span>
          </h2>
          <p className="mt-7 text-lg text-[#101313]/55">
            Shipped capabilities, traceable evidence, and explicit limits.
          </p>
          <p className="mt-7 max-w-md text-sm leading-7 text-[#101313]/55">
            Semantix 当前已提供切片提取、BM25 与混合检索、稳定注入等路径。调度、预取和参数反馈处于接口或实验阶段；是否降低生产成本，需要用真实会话单独测量。
          </p>
        </div>

        <div className="border-t border-[#101313]">
          {groups.map((group, groupIndex) => {
            const panelId = `semantix-feature-detail-${groupIndex}`;
            return (
              <div key={group.label} className={groupIndex === 0 ? "" : "mt-6"}>
                <div className="sticky top-16 z-20 flex items-center justify-between border-b border-[#101313]/20 bg-white/95 py-4 font-mono text-[11px] font-semibold tracking-[0.16em] text-[#168b6d] backdrop-blur md:text-sm">
                  <span>{group.label}</span>
                  <span className="text-[10px] font-normal tracking-[0.12em] md:text-xs">
                    {group.action}
                  </span>
                </div>
                {capabilityButton(group.indices[0], panelId)}
                {capabilityPanel(groupIndex, group.indices)}
                {capabilityButton(group.indices[1], panelId)}
              </div>
            );
          })}
        </div>
      </div>
    </section>
  );
}
