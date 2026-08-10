import Link from "next/link";

import Reveal from "@/components/Reveal";
import { siteIdentity } from "@/lib/site-identity";

type RoadmapStatus = "in-progress" | "experimental" | "planned";

type RoadmapStep = {
  phase: string;
  titleEn: string;
  title: string;
  status: RoadmapStatus;
  checkedAt: string;
  current: string;
  nextGate: string;
  evidence: Array<{ label: string; href: string }>;
};

const statusLabels: Record<RoadmapStatus, string> = {
  "in-progress": "进行中",
  experimental: "实验阶段",
  planned: "计划中",
};

const repo = siteIdentity.repositoryUrl;

const steps: RoadmapStep[] = [
  {
    phase: "P0",
    titleEn: "Observability",
    title: "可观测层",
    status: "in-progress",
    checkedAt: "2026-08-10",
    current: "事件契约、同步总线、JSONL 事件接入和离线验证命令已有实现；生产 harness 适配与统一基线报告尚未完成。",
    nextGate: "发布生产适配器端到端测试，并记录数据集、运行环境与基线结果。",
    evidence: [
      { label: "事件契约测试", href: `${repo}/blob/main/kernel/event/event_test.go` },
      { label: "离线验证命令", href: `${repo}/blob/main/cmd/semantix/verify.go` },
    ],
  },
  {
    phase: "P1",
    titleEn: "Slice Library",
    title: "语义切片库",
    status: "experimental",
    checkedAt: "2026-08-10",
    current: "Prompt、ToolPattern、Result 提取、文件存储、BM25 与内存向量索引已有测试；Context、Memory 和 ANN 持久化链路尚未完成。",
    nextGate: "补齐五类切片与项目/用户作用域测试，并发布可复现的检索质量评估。",
    evidence: [
      { label: "切片提取测试", href: `${repo}/blob/main/kernel/slice/slice_test.go` },
      { label: "向量索引实现", href: `${repo}/blob/main/kernel/embed/vecindex.go` },
    ],
  },
  {
    phase: "P2",
    titleEn: "Semantic Cache",
    title: "语义缓存",
    status: "experimental",
    checkedAt: "2026-08-10",
    current: "BM25 检索、确定性 L2 注入、预算截断和灰区过滤已有代码与测试；L3 结果复用和真实 harness 命中评估尚未完成。",
    nextGate: "在真实会话回放中同时报告命中率、污染率、延迟、成本与任务质量。",
    evidence: [
      { label: "L2 注入实现", href: `${repo}/blob/main/kernel/inject/inject.go` },
      { label: "注入测试", href: `${repo}/blob/main/kernel/inject/inject_test.go` },
    ],
  },
  {
    phase: "P3",
    titleEn: "Scheduler",
    title: "调度器",
    status: "planned",
    checkedAt: "2026-08-10",
    current: "仓库仅定义 RoundInput、RoundPlan 与 Decider 接口，没有 intent 分类、并发规划或模型 tier 决策实现。",
    nextGate: "提交 Decider 实现、单元测试和与固定策略对比的实验记录。",
    evidence: [
      { label: "调度接口", href: `${repo}/blob/main/kernel/sched/sched.go` },
    ],
  },
  {
    phase: "P4",
    titleEn: "Prefetcher",
    title: "预取器",
    status: "planned",
    checkedAt: "2026-08-10",
    current: "仓库仅定义只读 PrefetchTask 与 Prefetcher 接口，没有转移矩阵、路径预测、预算执行或 waste/hit 测量。",
    nextGate: "实现只读预取规划，并公开命中、浪费、成本和延迟的测量方法。",
    evidence: [
      { label: "预取接口", href: `${repo}/blob/main/kernel/prefetch/prefetch.go` },
    ],
  },
  {
    phase: "P5",
    titleEn: "Evolution Loop",
    title: "进化闭环",
    status: "planned",
    checkedAt: "2026-08-10",
    current: "仓库仅定义 Signal、Params 与 Engine 接口，没有在线 EWMA、冻结期控制、离线重训或消融实验实现。",
    nextGate: "实现可回滚的参数更新，并用固定数据集发布消融实验与失败案例。",
    evidence: [
      { label: "进化接口", href: `${repo}/blob/main/kernel/evolve/evolve.go` },
    ],
  },
];

const groups = [
  { title: "当前基础", description: "已有实现或测试，但尚未达到发布级验证。", items: steps.slice(0, 3) },
  { title: "后续闭环", description: "当前只有接口，以下内容不代表已经实现。", items: steps.slice(3) },
];

export default function Roadmap() {
  return (
    <section id="roadmap" className="relative overflow-hidden py-24">
      <div aria-hidden="true" className="pointer-events-none absolute inset-0 hidden lg:block">
        <div className="absolute -right-48 bottom-10 h-[380px] w-[380px] rounded-full bg-[oklch(0.524_0.12_165/0.05)] blur-3xl" />
      </div>
      <div className="relative wrap">
        <Reveal>
          <p className="font-mono text-sm font-medium text-accent">Roadmap 路线图</p>
          <h2 className="mt-2 text-3xl font-bold tracking-tight md:text-4xl">
            区分已验证、实验中与计划中。
          </h2>
          <p className="mt-4 max-w-2xl text-muted-foreground">
            状态仅依据 main 分支的代码、测试与公开记录。目标不是交付承诺，未发布测量方法的数字不会作为成果展示。
          </p>
        </Reveal>

        <div className="mt-12 grid gap-5 lg:grid-cols-2">
          {groups.map((group, groupIndex) => (
            <Reveal key={group.title} delay={groupIndex * 80} className="min-w-0">
              <section className="h-full rounded-lg border border-border bg-white p-5 md:p-7">
                <h3 className="text-lg font-semibold">{group.title}</h3>
                <p className="mt-1 text-sm text-muted-foreground">{group.description}</p>

                <div className="mt-6">
                  {group.items.map((step) => (
                    <article key={step.phase} className="border-t border-border py-6 first:pt-5">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-mono text-xs font-semibold text-accent">{step.phase}</span>
                        <span className="rounded-md border border-accent/25 bg-accent/10 px-2 py-0.5 text-xs font-medium text-accent">
                          {statusLabels[step.status]}
                        </span>
                        <span className="font-mono text-xs text-muted-foreground">
                          核验于 {step.checkedAt}
                        </span>
                      </div>

                      <h4 className="mt-3 font-semibold">
                        {step.title} <span className="font-normal text-muted-foreground">{step.titleEn}</span>
                      </h4>
                      <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{step.current}</p>

                      <div className="mt-4 rounded-md bg-[oklch(0.976_0.005_165)] px-3 py-2.5">
                        <p className="font-mono text-[11px] font-semibold text-accent">下一状态条件</p>
                        <p className="mt-1 text-xs leading-relaxed text-muted-foreground">{step.nextGate}</p>
                      </div>

                      <div className="mt-4 flex flex-wrap gap-x-4 gap-y-2">
                        {step.evidence.map((item) => (
                          <a
                            key={item.href}
                            href={item.href}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-xs font-medium text-foreground underline decoration-border underline-offset-4 transition-colors hover:text-accent hover:decoration-accent"
                          >
                            {item.label} <span aria-hidden="true">↗</span>
                          </a>
                        ))}
                      </div>
                    </article>
                  ))}
                </div>
              </section>
            </Reveal>
          ))}
        </div>

        <Reveal delay={160}>
          <aside className="mt-8 border-l-2 border-accent pl-5">
            <h3 className="font-semibold">路线图维护规则</h3>
            <p className="mt-2 max-w-3xl text-sm leading-relaxed text-muted-foreground">
              由 {siteIdentity.operator.brandName} 维护，最后更新于 {siteIdentity.lastUpdated}。状态只在相关代码与测试进入 main 后更新；
              “已验证”还要求公开数据来源、运行环境、测量步骤和失败案例。核验日期表示最近一次事实检查，不是预计交付日期。
            </p>
            <div className="mt-3 flex flex-wrap gap-x-4 gap-y-2 text-sm">
              <Link href="/about" className="font-medium underline decoration-border underline-offset-4 hover:text-accent hover:decoration-accent">
                查看维护主体
              </Link>
              <a
                href={`${siteIdentity.repositoryUrl}/issues/36`}
                target="_blank"
                rel="noopener noreferrer"
                className="font-medium underline decoration-border underline-offset-4 hover:text-accent hover:decoration-accent"
              >
                反馈路线图状态 <span aria-hidden="true">↗</span>
              </a>
            </div>
          </aside>
        </Reveal>
      </div>
    </section>
  );
}
