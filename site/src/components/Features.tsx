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
  artAlt: string;
};

const capabilities: Capability[] = [
  {
    num: "01",
    titleEn: "Semantic Slice Library",
    title: "语义切片库",
    body: "从历史会话提取可复用语义单元：任务模板 P、上下文块 C、工具调用模式 T、高频结果 R、记忆 M。向量索引并持久化到项目/用户双库。",
    code: "type Slice struct { ID string; Type SliceType; Scope Scope; Content []byte }",
    art: "/ensureok/semantix-memory-transparent.png",
    artAlt: "多层扫描线组成的古典人物图像，象征语义切片被持续沉淀",
  },
  {
    num: "02",
    titleEn: "Semantic Cache L1·L2·L3",
    title: "三级语义缓存",
    body: "L1 字节前缀缓存；L2 把跨会话稳定的切片原样注入前缀区，让语义命中变成厂商的字节命中；L3 带验证的只读结果复用。",
    code: "L2: stable-slice injection → vendor byte cache",
    art: "/ensureok/semantix-cache-transparent.png",
    artAlt: "两张被扫描线切分的古典侧脸相互回应，象征相似语义被识别和复用",
  },
  {
    num: "03",
    titleEn: "Kernel Scheduler",
    title: "内核调度器",
    body: "按任务 intent 联合决策：工具并发度、模型 tier、缓存注入、预取预算——从你的行为模式里学出来的调度策略。",
    code: "decide(intent) → {concurrency, tier, inject, prefetch}",
    art: "/ensureok/semantix-inference-transparent.png",
    artAlt: "古典人物从迷宫中理出清晰路径，象征调度器优化推理路线",
  },
  {
    num: "04",
    titleEn: "Speculative Prefetch",
    title: "投机预取",
    body: "把 LLM 流式输出的等待时间填满：预取下一轮的切片组装、embedding 计算。waste/hit 比例自我惩罚，越用越准。",
    code: "waste/hit > 3:1 → signal-source weight ↓",
    art: "/ensureok/semantix-inference-transparent.png",
    artAlt: "古典人物从迷宫中理出清晰路径，象征提前预测和准备下一步",
  },
  {
    num: "05",
    titleEn: "Self-Evolution",
    title: "自进化引擎",
    body: "每轮回馈命中/污染/延迟/成本/成功率：在线 EWMA 调参（冻结期保护字节缓存）+ 离线重训。系统自己长出自己的最优参数。",
    code: "online EWMA + freeze-period ≥1h + offline retrain",
    art: "/ensureok/semantix-evolution-transparent.png",
    artAlt: "多臂古典人物重塑另一具雕像，象征反馈驱动的持续进化",
  },
  {
    num: "06",
    titleEn: "Single Kernel, Many Harnesses",
    title: "零侵入适配",
    body: "不改造 harness 内核：适配层接入 DeepSeek-Reasonix、Claude Code 等任意 harness，上层能力原样复用。",
    code: "adapter(harness) → kernel.emit(event)",
    art: "/ensureok/semantix-memory-transparent.png",
    artAlt: "由人物与扫描线组成的记忆图像，象征多个系统共享同一套内核能力",
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
        className="group grid w-full grid-cols-[3rem_1fr_auto] items-center gap-3 border-b border-[#101313]/20 py-5 text-left md:grid-cols-[5rem_1fr_auto] md:py-7"
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
    const selected = isOpen && active !== null ? capabilities[active] : null;
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
              <div key={selected.num} className="semantix-detail-in relative min-h-[28rem] md:min-h-[32rem]">
                <Image
                  src={selected.art}
                  alt={selected.artAlt}
                  fill
                  sizes="(max-width: 767px) 100vw, 55vw"
                  className="object-cover opacity-90 transition-transform duration-700 ease-[cubic-bezier(0.16,1,0.3,1)] hover:scale-[1.015] motion-reduce:transition-none"
                />
                <div
                  aria-hidden="true"
                  className="absolute inset-0 bg-[#2c8c75]/80 md:bg-[linear-gradient(90deg,rgba(44,140,117,0.99)_0%,rgba(44,140,117,0.94)_42%,rgba(44,140,117,0.58)_68%,rgba(44,140,117,0.06)_100%)]"
                />

                <div className="relative z-[1] flex min-h-[28rem] flex-col p-6 md:min-h-[32rem] md:max-w-[68%] md:p-9 lg:p-10">
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
      <div className="mx-auto grid max-w-[1600px] gap-14 px-5 py-24 md:px-10 md:py-32 lg:grid-cols-[0.8fr_1.2fr] lg:gap-20">
        <div className="lg:sticky lg:top-24 lg:self-start">
          <p className="font-mono text-[10px] font-medium tracking-[0.24em] text-[#168b6d]">
            Features 特性
          </p>
          <h2 className="font-brand-display mt-7 max-w-xl text-[clamp(2.7rem,5.8vw,6.5rem)] font-black leading-[0.98] tracking-[-0.055em]">
            越用越好的
            <br />
            <span className="text-[#168b6d]">能力。</span>
          </h2>
          <p className="mt-7 text-lg text-[#101313]/55">
            Autonomy you can actually audit.
          </p>
          <p className="mt-7 max-w-md text-sm leading-7 text-[#101313]/55">
            现有 harness 只做会话内的字节级前缀缓存——被动、会话绑定、调度静态。Semantix 把整个循环升级为跨会话的、主动的、自进化的闭环：每次交互都让下一次更便宜、更快。
          </p>
        </div>

        <div className="border-t border-[#101313]">
          {groups.map((group, groupIndex) => {
            const panelId = `semantix-feature-detail-${groupIndex}`;
            return (
              <div key={group.label} className={groupIndex === 0 ? "" : "mt-16"}>
                <div className="flex items-center justify-between border-b border-[#101313]/20 py-5 font-mono text-[11px] font-semibold tracking-[0.16em] text-[#168b6d] md:text-sm">
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
