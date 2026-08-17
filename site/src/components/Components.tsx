import Image from "next/image";
import Reveal from "@/components/Reveal";

type ComponentCard = {
  num: string;
  label: string;
  status: string;
  titleEn: string;
  title: string;
  responsibility: string;
  io: string;
  limits: string;
  art: string;
  artAlt: string;
  href: string;
  linkLabel: string;
  mirror?: boolean;
};

const components: ComponentCard[] = [
  {
    num: "01",
    label: "SLICE",
    status: "已实现",
    titleEn: "Semantic Slice Library",
    title: "语义切片库",
    responsibility:
      "解析会话 JSONL，按 turn 边界生成 Prompt、ToolPattern 和 Result 切片，并按 scope 持久化。",
    io: "输入：Reasonix 风格会话 JSONL。输出：带内容哈希 ID、类型、scope 与来源元数据的 Slice。",
    limits:
      "默认提取器暂不生成 Context 和 Memory 切片；畸形行会跳过，Prompt 与 Result 分别限制为 4 KB 和 8 KB。",
    art: "/generated/component-slice-longhair-v6-ink.png",
    artAlt: "长发女数据特工从轨道档案中提取记忆切片",
    href: "https://github.com/Gnosil/semantix/blob/main/kernel/slice/extractor.go",
    linkLabel: "查看提取器源码",
  },
  {
    num: "02",
    label: "RETRIEVAL",
    status: "部分实现",
    titleEn: "Retrieval & Injection",
    title: "检索与跨会话注入",
    responsibility:
      "使用 BM25 按 scope 检索切片，并把命中项按稳定顺序组装成可逆、低权限的注入块。",
    io: "输入：查询文本、scope 与切片库。输出：排序后的命中项或注入块；未命中时返回空结果。",
    limits:
      "跨会话路径已有集成测试，但 L3 结果复用仍只有接口；真实用户命中率与生产 harness 挂载尚未验证。",
    art: "/generated/component-cache-90s-scifi-v5-ink.png",
    artAlt: "短发女数据特工在三层机械缓存环中匹配记忆晶体",
    href: "https://github.com/Gnosil/semantix/blob/main/kernel/inject/inject_test.go",
    linkLabel: "查看跨会话测试",
    mirror: true,
  },
  {
    num: "03",
    label: "SCHEDULE",
    status: "接口阶段",
    titleEn: "Kernel Scheduler",
    title: "内核调度器",
    responsibility:
      "定义工具轮次的联合决策契约，包括并发组、模型 tier、注入切片与预取目标。",
    io: "输入：包含 intent、工具调用、切片命中、预算和可用并发的 RoundInput。输出：RoundPlan。",
    limits:
      "当前只提供调度接口与数据结构，学习策略、预算执行及生产级调度循环仍待实现。",
    art: "/generated/component-scheduler-profile-v9-ink.png",
    artAlt: "侧脸双马尾轨道调度员在环形空间站编排数据路线",
    href: "https://github.com/Gnosil/semantix/blob/main/kernel/sched/sched.go",
    linkLabel: "查看调度接口",
  },
  {
    num: "04",
    label: "PREFETCH",
    status: "接口阶段",
    titleEn: "Speculative Prefetch",
    title: "投机预取",
    responsibility:
      "根据近期工具序列规划只读预取任务，并为每个任务记录类型、键与估算成本。",
    io: "输入：最近使用的工具名序列。输出：零个或多个只读 PrefetchTask。",
    limits:
      "目前没有 Planner 实现、转移矩阵、预算执行或 waste/hit 基线，因此不会主动运行预取。",
    art: "/generated/component-prefetch-whitehair-v6-ink.png",
    artAlt: "白发特工穿越未来环提前截取数据胶囊",
    href: "https://github.com/Gnosil/semantix/blob/main/kernel/prefetch/prefetch.go",
    linkLabel: "查看预取接口",
  },
];

function ComponentList() {
  return (
    <div className="mt-8 border-t border-[#168b6d]/25">
      {components.map((component, index) => (
        <Reveal key={component.titleEn} delay={index * 40}>
          <article
            id={`component-${component.label.toLowerCase()}`}
            className="grid scroll-mt-20 gap-7 border-b border-[#168b6d]/25 py-10 md:grid-cols-11 md:items-stretch md:gap-8 lg:py-14"
          >
              <div
                className={`relative flex min-h-[18rem] items-stretch overflow-hidden md:col-span-6 md:min-h-[23rem] lg:min-h-[26rem] ${
                  index % 2 === 0 ? "md:order-1" : "md:order-2"
                }`}
              >
                <div
                  className="relative min-h-full w-full"
                  style={{
                    transform: component.mirror ? "scaleX(-1)" : undefined,
                    WebkitMaskImage:
                      "radial-gradient(ellipse 82% 78% at center, black 58%, rgba(0,0,0,0.82) 76%, transparent 100%)",
                    maskImage:
                      "radial-gradient(ellipse 82% 78% at center, black 58%, rgba(0,0,0,0.82) 76%, transparent 100%)",
                  }}
                >
                  <Image
                    src={component.art}
                    alt={component.artAlt}
                    fill
                    sizes="(max-width: 767px) 100vw, 40vw"
                    className="pane-in object-cover mix-blend-multiply"
                  />
                </div>
              </div>

              <div
                className={`flex flex-col justify-center text-[#101313] md:col-span-5 ${
                  index % 2 === 0 ? "md:order-2" : "md:order-1"
                }`}
              >
                <div className="grid grid-cols-[3rem_1fr] items-start gap-3 md:grid-cols-[4rem_1fr]">
                  <span className="pt-2 font-mono text-[10px] tracking-[0.16em] text-[#168b6d]">
                    {component.num}
                  </span>
                  <div className="min-w-0">
                    <h3 className="font-brand-display text-3xl font-black leading-[0.95] tracking-[-0.055em] text-[#101313] md:text-4xl lg:text-[2.65rem]">
                      {component.title}
                    </h3>
                    <p className="mt-2 font-mono text-[9px] uppercase tracking-[0.18em] text-[#168b6d] md:text-[10px]">
                      {component.titleEn}
                    </p>
                  </div>
                </div>

                <div className="mt-8 border-t border-[#168b6d]/20 pt-5">
                  <p className="font-mono text-[10px] font-bold tracking-[0.18em] text-[#168b6d]">职责</p>
                </div>
                <p className="mt-2 text-sm leading-6 text-[#101313]/72">{component.responsibility}</p>
                <dl className="mt-5 grid gap-5 sm:grid-cols-2">
                  <div>
                    <dt className="font-mono text-[10px] font-bold tracking-[0.18em] text-[#168b6d]">输入 / 输出</dt>
                    <dd className="mt-2 text-sm leading-6 text-[#101313]/72">{component.io}</dd>
                  </div>
                  <div>
                    <dt className="font-mono text-[10px] font-bold tracking-[0.18em] text-[#168b6d]">已知限制</dt>
                    <dd className="mt-2 text-sm leading-6 text-[#101313]/72">{component.limits}</dd>
                  </div>
                </dl>
                <a
                  href={component.href}
                  target="_blank"
                  rel="noreferrer"
                  className="mt-5 block w-fit border-b border-[#168b6d]/45 pb-1 font-mono text-[11px] font-bold tracking-[0.08em] text-[#101313] transition-colors hover:border-[#168b6d] hover:text-[#168b6d] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#168b6d]"
                >
                  {component.linkLabel} ↗
                </a>
              </div>
          </article>
        </Reveal>
      ))}
    </div>
  );
}

export default function Components() {
  return (
    <section
      id="components"
      className="border-t border-[#0f735c] bg-[#168b6d] p-3 text-[#168b6d] md:p-6"
    >
      <div className="mx-auto max-w-[1760px] bg-white px-5 py-14 md:px-10 md:py-16 lg:px-12">
        <Reveal>
          <header className="grid gap-6 border-b border-[#168b6d]/35 pb-8 lg:grid-cols-[1.1fr_0.9fr] lg:items-center lg:gap-16">
            <div>
              <p className="font-mono text-[10px] font-semibold tracking-[0.24em]">
                Components 内核组件
              </p>
              <h2 className="font-brand-display mt-4 text-[clamp(2.5rem,3.6vw,4.25rem)] font-black leading-[0.9] tracking-[-0.06em] text-[#101313] md:whitespace-nowrap">
                一个内核，
                <span className="text-[#168b6d]">四个组件。</span>
              </h2>
            </div>
            <div className="max-w-2xl lg:justify-self-end">
              <p className="font-mono text-[10px] font-semibold uppercase tracking-[0.2em] text-[#168b6d]">
                One kernel, four components.
              </p>
              <p className="mt-4 text-sm leading-6 text-[#101313]/60 md:text-base md:leading-7">
                切片库负责沉淀，检索层负责复用，调度器负责编排，预取器负责填空闲。四个组件共享同一套事件与参数存储，单向依赖，互不循环。
              </p>
            </div>
          </header>
        </Reveal>

        <ComponentList />

        <Reveal delay={55}>
          <div className="mt-5 border border-[#168b6d]/35 bg-[#101313] text-[#bde8d9]">
            <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/10 px-5 py-3 font-mono text-[10px] font-semibold tracking-[0.18em]">
              <span>REUSE VISUALIZATION / CLI 真实输出</span>
              <span className="text-white/35">semantix v0.3.1+（U28–U31）· 演示库实录</span>
            </div>
            <pre className="overflow-x-auto p-5 font-mono text-xs leading-6 text-[#bde8d9] md:p-6">
{`$ semantix dashboard

  💰 Cost savings
     paid        $ 0.0060
     baseline    $ 0.0141
     saved       $ 0.0080  (56.99%)
     ██████████████░░░░░░░░░░

  🎯 Cache hit rate (L3/L2)
     4 / 5 turns  (80.00%)
     L3 1 · L2 3
     ███████████████████░░░░░

  🗂 Zone distribution (library replay)
     hit  ████ 4   grey ██████ 6   miss  0

  📦 Slice library
     10 slices · 3 cross-session sessions

$ semantix search --query "fix failing go test"
1. 🟢 score=4.331011 zone=hit id=619551c54af5437a scope=project from:2026-08-14-c9d4
   fix failing go test after refactor
2. 🟢 score=3.852740 zone=hit id=73b12bb117664106 scope=project from:2026-08-13-b7c2
   fix failing go test in kernel slice extractor
3. 🟢 score=3.852740 zone=hit id=adfecdd9bff0db2d scope=project from:2026-08-12-a1f3
   fix failing go test in kernel slice store
🎯 3/3 hits in 3 sessions`}
            </pre>
            <p className="border-t border-white/10 px-5 py-3 font-mono text-[10px] leading-5 text-white/40">
              三要素：命中率（🎯）· 节省成本（💰）· 来源会话（from:）——以上为演示库真实运行输出；门禁数据以 semantix verify 报告为准
            </p>
          </div>
        </Reveal>
      </div>
    </section>
  );
}
