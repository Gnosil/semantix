import Link from "next/link";
import Reveal from "@/components/Reveal";
import CopyCode from "@/components/CopyCode";

const steps = [
  {
    number: "01",
    title: "安装完整 Agent",
    titleEn: "Install",
    desc: "一行安装交互式 Agent 与记忆内核，无需安装 Go。",
    code: "curl -fsSL https://raw.githubusercontent.com/Gnosil/semantix/main/agent-skill/scripts/install.sh | sh",
    href: "https://github.com/Gnosil/semantix/releases/latest",
    external: true,
    linkLabel: "发布包 ↗",
  },
  {
    number: "02",
    title: "进入项目",
    titleEn: "Open project",
    desc: "进入你的项目文件夹；该目录将成为 Agent 的工作区。",
    code: "cd ~/your-project",
    href: "https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md",
    external: true,
    linkLabel: "快速上手 ↗",
  },
  {
    number: "03",
    title: "开始对话",
    titleEn: "Start",
    desc: "运行 Semantix；首次启动会引导你配置模型与 API key。",
    code: "semantix",
    href: "/docs/guide",
    external: false,
    linkLabel: "深度文档 →",
  },
];

export default function Install() {
  return (
    <section
      id="start"
      className="install-scroll-handoff border-x-[10px] border-t-[10px] border-[#168b6d] bg-[#f8f8f4] md:border-x-[18px] md:border-t-[18px]"
    >
      <div className="install-scroll-content mx-auto max-w-[1600px] px-5 py-16 md:px-10 md:py-20 lg:px-12 lg:py-24">
        <Reveal>
          <p className="font-mono text-[10px] font-semibold uppercase tracking-[0.24em] text-[#168b6d]">
            Install 安装
          </p>
          <h2 className="font-brand-display mt-4 text-5xl font-black tracking-[-0.055em] text-[#101313] md:text-6xl lg:text-[4.5rem]">
            从源码跑起来。
          </h2>
          <p className="mt-3 max-w-2xl text-base leading-7 text-[#596269] md:text-lg">
            Clone, build, extract. Then verify the results yourself.
          </p>
        </Reveal>

        <div className="mt-12 border-y border-[#101313]/18 md:grid md:grid-cols-3 md:divide-x md:divide-[#101313]/18 lg:mt-16">
          {steps.map((step, i) => (
            <Reveal key={step.number} delay={i * 80}>
              <article className="group flex min-h-[24rem] h-full flex-col border-b border-[#101313]/18 py-8 last:border-b-0 md:border-b-0 md:px-7 md:py-9 lg:min-h-[26rem] lg:px-10 lg:py-11">
                <div className="flex items-center justify-between font-mono text-[10px] font-semibold uppercase tracking-[0.18em]">
                  <span className="text-[#168b6d]">{step.number}</span>
                  <span className="text-[#101313]/38">Step / {step.number}</span>
                </div>
                <h3 className="font-brand-display mt-10 text-[2rem] font-black tracking-[-0.045em] text-[#101313] lg:text-4xl">
                  {step.title}
                </h3>
                <p className="mt-2 font-mono text-[10px] uppercase tracking-[0.16em] text-[#168b6d]">
                  {step.titleEn}
                </p>
                <p className="mt-6 max-w-sm text-sm leading-6 text-[#596269]">{step.desc}</p>
                <CopyCode
                  className="mt-7 !rounded-none border-[#101313]/12"
                  code={step.code}
                  prompt
                  singleLine
                  tone="dark"
                />
                <div className="mt-auto pt-8 text-sm font-semibold text-[#101313]">
                  {step.external ? (
                    <a
                      href={step.href}
                      target="_blank"
                      rel="noopener"
                      className="inline-flex border-b border-[#168b6d]/45 pb-1 transition-colors hover:border-[#168b6d] hover:text-[#168b6d]"
                    >
                      {step.linkLabel}
                    </a>
                  ) : (
                    <Link
                      href={step.href}
                      className="inline-flex border-b border-[#168b6d]/45 pb-1 transition-colors hover:border-[#168b6d] hover:text-[#168b6d]"
                    >
                      {step.linkLabel}
                    </Link>
                  )}
                </div>
              </article>
            </Reveal>
          ))}
        </div>

        <Reveal delay={240}>
          <nav
            aria-label="安装后续操作"
            className="mt-11 flex flex-wrap items-center justify-center gap-y-4 text-center"
          >
            <a
              href="https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md"
              target="_blank"
              rel="noopener"
              className="group inline-flex items-center gap-3 px-5 py-2 text-sm font-bold text-[#168b6d] transition-colors hover:text-[#101313]"
            >
              <span>运行离线验证</span>
              <span
                aria-hidden="true"
                className="font-mono text-base transition-transform group-hover:translate-x-0.5 group-hover:-translate-y-0.5"
              >
                ↗
              </span>
            </a>
            <span aria-hidden="true" className="h-5 w-px bg-[#101313]/18" />
            <Link
              href="/docs/guide"
              className="px-5 py-2 text-sm font-semibold text-[#101313] transition-colors hover:text-[#168b6d]"
            >
              阅读架构文档 <span aria-hidden="true">→</span>
            </Link>
            <span aria-hidden="true" className="h-5 w-px bg-[#101313]/18" />
            <a
              href="https://github.com/Gnosil/semantix/blob/main/CONTRIBUTING.md"
              target="_blank"
              rel="noopener"
              className="px-5 py-2 text-sm font-semibold text-[#101313] transition-colors hover:text-[#168b6d]"
            >
              参与贡献 <span aria-hidden="true">→</span>
            </a>
          </nav>
        </Reveal>

        <Reveal delay={300}>
          <div className="mt-12 flex flex-col gap-4 border-t border-[#101313]/18 pt-6 text-xs text-[#596269] lg:flex-row lg:items-center lg:justify-between">
            <p className="font-mono uppercase tracking-[0.12em]">© 2026 MIT License / Semantix</p>
            <p className="flex flex-wrap items-center gap-x-2 gap-y-1">
              <span className="text-[#101313]/55">维护者</span>
              {["Gnosil", "radianceded", "Allenllii", "jh10724-dotcom"].map(
                (name) => (
                  <a
                    key={name}
                    href={`https://github.com/${name}`}
                    target="_blank"
                    rel="noopener"
                    className="border-b border-transparent text-[#168b6d] transition-colors hover:border-[#168b6d]"
                  >
                    {name}
                  </a>
                ),
              )}
              <span className="text-[#101313]/25">/</span>
              <span>技术作者 Gnosil</span>
            </p>
          </div>
        </Reveal>
      </div>
    </section>
  );
}
