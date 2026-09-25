import Link from "next/link";
import Reveal from "@/components/Reveal";
import CopyCode from "@/components/CopyCode";
import DesktopDownload from "@/components/DesktopDownload";

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
      className="scroll-mt-16 border-x-[10px] border-t-[10px] border-accent bg-white text-[#111411] md:border-x-[18px] md:border-t-[18px]"
    >
      <div className="mx-auto max-w-[1100px] px-5 py-16 md:px-10 md:py-20">
        <Reveal>
          <p className="font-mono text-[10px] font-semibold uppercase tracking-[0.24em] text-[#168b6d]">
            Install 安装
          </p>
          <h2 className="font-brand-display mt-4 text-[clamp(2rem,3.4vw,2.75rem)] font-black leading-[1.08] tracking-[-0.045em]">
            选择你的使用方式。
          </h2>
          <p className="mt-4 max-w-2xl text-sm leading-7 text-muted-foreground md:text-base">
            下载桌面版，或在终端安装 Semantix。
          </p>
        </Reveal>

        <DesktopDownload />

        <div id="terminal-install" className="mt-10 scroll-mt-24 border-t border-border pt-10">
          <Reveal>
            <h3 className="font-brand-display text-xl font-bold tracking-[-0.025em]">
              终端安装
            </h3>
            <p className="mt-2 text-sm leading-6 text-muted-foreground">
              偏好命令行？按下面三步开始。
            </p>
          </Reveal>
        <div className="mt-5 grid grid-cols-1 gap-4 md:grid-cols-3">
          {steps.map((step, i) => (
            <Reveal key={step.number} delay={i * 80} className="min-w-0">
              <article className="group flex h-full min-w-0 flex-col rounded-lg border border-border bg-white p-5">
                <div className="flex items-center justify-between font-mono text-[10px] font-semibold uppercase tracking-[0.18em]">
                  <span className="text-[#168b6d]">{step.number}</span>
                </div>
                <h4 className="font-brand-display mt-4 text-xl font-bold tracking-[-0.025em] text-[#101313]">
                  {step.title}
                </h4>
                <p className="mt-2 font-mono text-[10px] uppercase tracking-[0.16em] text-[#168b6d]">
                  {step.titleEn}
                </p>
                <p className="mt-4 max-w-sm text-sm leading-6 text-muted-foreground">{step.desc}</p>
                <CopyCode
                  className="mt-5 border-border"
                  code={step.code}
                  prompt
                  singleLine
                />
                <div className="mt-auto pt-5 text-xs font-semibold text-[#101313]">
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
      </div>
    </section>
  );
}
