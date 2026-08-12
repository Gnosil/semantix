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
    art: "/generated/component-slice-longhair-v6.png",
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
    art: "/generated/component-cache-90s-scifi-v5.png",
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
    art: "/generated/component-scheduler-profile-v9.png",
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
    art: "/generated/component-prefetch-whitehair-v6.png",
    artAlt: "白发特工穿越未来环提前截取数据胶囊",
    href: "https://github.com/Gnosil/semantix/blob/main/kernel/prefetch/prefetch.go",
    linkLabel: "查看预取接口",
  },
];

export default function Components() {
  return (
    <section
      id="components"
      className="border-t border-[#0f735c] bg-[#168b6d] p-3 text-[#168b6d] md:p-6"
    >
      <div className="mx-auto max-w-[1760px] bg-[#f7f6f1] px-5 py-14 md:px-10 md:py-16 lg:px-12">
        <Reveal>
          <header className="grid gap-6 border-b border-[#168b6d]/35 pb-8 lg:grid-cols-[0.9fr_1.1fr] lg:items-end lg:gap-16">
            <div>
              <p className="font-mono text-[10px] font-semibold tracking-[0.24em]">
                Components 内核组件
              </p>
              <h2 className="font-brand-display mt-4 text-[clamp(2.5rem,4.2vw,4.75rem)] font-black leading-[0.9] tracking-[-0.06em] text-[#101313]">
                一个内核，
                <span className="text-[#168b6d]">四个组件。</span>
              </h2>
            </div>
            <div className="max-w-2xl lg:justify-self-end">
              <p className="font-mono text-[10px] font-semibold uppercase tracking-[0.2em] text-[#168b6d]">
                One kernel, four components.
              </p>
              <p className="mt-4 text-sm leading-6 text-[#101313]/60 md:text-base md:leading-7">
                切片库负责沉淀，检索层负责复用，调度器负责编排，预取器负责填空闲——四个组件共享同一套事件与参数存储，单向依赖，互不循环。
              </p>
            </div>
          </header>
        </Reveal>

        <div className="mt-8 grid gap-5 md:grid-cols-2">
          {components.map((component, index) => (
            <Reveal key={component.titleEn} delay={index * 55}>
              <article className="flex h-full flex-col border border-[#168b6d]/35 bg-[#fbfaf6] p-5 transition-colors duration-300 hover:border-[#168b6d] md:p-6">
                <div className="flex items-center justify-between border-b border-[#168b6d]/25 pb-4 font-mono text-[10px] font-semibold tracking-[0.18em]">
                  <span>{component.num}&nbsp; / &nbsp;{component.label}</span>
                  <span className="text-[#101313]/55">{component.status}</span>
                </div>

                <div className="mt-5 grid gap-5 xl:grid-cols-[minmax(0,0.9fr)_minmax(15rem,1.1fr)] xl:items-center">
                  <div>
                    <h3 className="font-brand-display text-[clamp(1.7rem,2.2vw,2.6rem)] font-black leading-[0.96] tracking-[-0.05em] text-[#101313]">
                      {component.titleEn}
                    </h3>
                    <p className="mt-3 text-xl font-black tracking-[-0.04em] text-[#168b6d] md:text-2xl">
                      {component.title}
                    </p>
                  </div>

                  <div
                    className="relative aspect-[16/9] overflow-hidden border border-[#168b6d]/35 bg-[#168b6d]/10"
                    style={component.mirror ? { transform: "scaleX(-1)" } : undefined}
                  >
                    <Image
                      src={component.art}
                      alt={component.artAlt}
                      fill
                      sizes="(max-width: 767px) 100vw, (max-width: 1279px) 50vw, 28vw"
                      className="pane-in object-cover contrast-[1.04]"
                    />
                  </div>
                </div>

                <div className="mt-5 border-t border-[#168b6d]/25 pt-4 text-[#101313]">
                  <p className="font-mono text-[10px] font-bold tracking-[0.18em] text-[#168b6d]">职责</p>
                  <p className="mt-2 text-sm leading-6 text-[#101313]/72">{component.responsibility}</p>
                </div>

                <details className="group mt-4 border-t border-[#168b6d]/25 text-[#101313]">
                  <summary className="flex min-h-12 cursor-pointer list-none items-center justify-between py-3 font-mono text-[11px] font-bold tracking-[0.12em] text-[#168b6d] marker:content-none focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#168b6d]">
                    <span>展开信息</span>
                    <span aria-hidden="true" className="text-lg font-normal transition-transform duration-300 group-open:rotate-45">＋</span>
                  </summary>

                  <dl className="grid gap-5 border-t border-[#168b6d]/20 pb-1 pt-4 sm:grid-cols-2">
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
                    className="mb-2 mt-5 block w-fit border-b border-[#168b6d]/45 pb-1 font-mono text-[11px] font-bold tracking-[0.08em] text-[#101313] transition-colors hover:border-[#168b6d] hover:text-[#168b6d] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#168b6d]"
                  >
                    {component.linkLabel} ↗
                  </a>
                </details>
              </article>
            </Reveal>
          ))}
        </div>
      </div>
    </section>
  );
}
