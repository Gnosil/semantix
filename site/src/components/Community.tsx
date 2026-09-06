import Link from "next/link";
import Image from "next/image";

import Reveal from "@/components/Reveal";
import { siteIdentity } from "@/lib/site-identity";

const contributors = [
  "Gnosil",
  "radianceded",
  "jh10724-dotcom",
  "Allenllii",
];

const repo = siteIdentity.repositoryUrl;

const participation = [
  {
    label: "验证",
    title: "复跑离线会话验证",
    body: "使用 semantix verify 按时间拆分会话并输出 TSV，再人工标注相关性。提交结果时请同时记录数据来源、运行环境和标注规则。",
    links: [
      { label: "查看验证命令", href: `${repo}/blob/main/docs/QUICKSTART.md` },
      { label: "查看 M0 决策记录", href: `${repo}/blob/main/docs/reports/m0-gate.md` },
    ],
  },
  {
    label: "讨论",
    title: "把主张写成可验收问题",
    body: "新 Issue 应说明复现输入、预期行为、实际结果和验收条件。性能或成本结论还需要基线、样本规模与测量方法。",
    links: [
      { label: "创建 Issue", href: `${repo}/issues/new` },
      { label: "查看技术讨论", href: `${repo}/issues` },
    ],
  },
  {
    label: "权衡",
    title: "先阅读失败路径与安全边界",
    body: "缓存层采用 fail-open，安全边界采用 fail-closed。注入污染、过期结果、预取浪费和错误工具调用都需要可关闭、可回滚和可定位。",
    links: [
      { label: "查看架构权衡", href: `${repo}/blob/main/docs/Agent-Infra-架构设计.md` },
      { label: "查看安全设计", href: `${repo}/blob/main/docs/Security-安全设计.md` },
    ],
  },
  {
    label: "贡献",
    title: "用小范围 PR 提交证据",
    body: "一个 PR 解决一个明确问题，并附 go vet、go test 或对应前端检查。贡献归属以 commit、PR 审阅和 Contributors 记录为准。",
    links: [
      { label: "查看 Pull Requests", href: `${repo}/pulls` },
      { label: "查看贡献记录", href: `${repo}/graphs/contributors` },
    ],
  },
];

export default function Community() {
  return (
    <section id="community" className="relative overflow-hidden bg-[oklch(0.976_0.005_165)] py-24">
      <div aria-hidden="true" className="pointer-events-none absolute inset-0 hidden lg:block">
        <div className="absolute -right-56 -top-24 h-[440px] w-[440px] rounded-full bg-[oklch(0.608_0.14_165/0.07)] blur-3xl" />
      </div>
      <div className="relative wrap">
        <Reveal>
          <p className="font-mono text-sm font-medium text-accent">Community 社区</p>
          <h2 className="mt-2 text-3xl font-bold tracking-tight md:text-4xl">
            从复现问题开始参与。
          </h2>
          <p className="mt-4 max-w-2xl text-muted-foreground">
            Semantix 采用 {siteIdentity.licenseName} 许可。讨论、验证与代码变更都应留下可检查的输入、方法和结果。
          </p>
        </Reveal>

        <div className="mt-12 grid gap-x-10 lg:grid-cols-2">
          {participation.map((item, index) => (
            <Reveal key={item.label} delay={(index % 2) * 70} className="min-w-0">
              <article className="border-t border-border py-7">
                <p className="font-mono text-xs font-semibold text-accent">{item.label}</p>
                <h3 className="mt-2 text-lg font-semibold">{item.title}</h3>
                <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{item.body}</p>
                <div className="mt-4 flex flex-wrap gap-x-4 gap-y-2">
                  {item.links.map((link) => (
                    <a
                      key={link.href}
                      href={link.href}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-sm font-medium text-foreground underline decoration-border underline-offset-4 transition-colors hover:text-accent hover:decoration-accent"
                    >
                      {link.label} <span aria-hidden="true">↗</span>
                    </a>
                  ))}
                </div>
              </article>
            </Reveal>
          ))}
        </div>

        <Reveal delay={120}>
          <aside className="mt-4 grid gap-3 border-y border-border py-6 md:grid-cols-[8rem_minmax(0,1fr)_auto] md:items-start md:gap-6">
            <div>
              <p className="font-mono text-xs font-semibold text-accent">验证说明</p>
              <p className="mt-1 text-xs text-muted-foreground">如何阅读报告</p>
            </div>
            <p className="max-w-3xl text-sm leading-relaxed text-muted-foreground">
              M0 报告使用合成演示会话验证流程，不是生产基准。真实会话仍待验证；数据集、样本规模、环境和标注方法公开前，
              不据此承诺普遍的成本或性能收益。
            </p>
            <a
              href={`${repo}/blob/main/docs/reports/m0-cost-comparison.md`}
              target="_blank"
              rel="noopener noreferrer"
              className="w-fit whitespace-nowrap text-sm font-medium text-foreground underline decoration-border underline-offset-4 transition-colors hover:text-accent hover:decoration-accent"
            >
              实验限制 <span aria-hidden="true">↗</span>
            </a>
          </aside>
        </Reveal>

        <Reveal delay={160}>
          <div className="mt-10 grid overflow-hidden rounded-lg border border-border bg-white lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.35fr)]">
            <div className="p-6 md:p-7">
              <p className="font-mono text-xs font-semibold text-accent">维护者</p>
              <h3 className="mt-2 text-xl font-semibold">{siteIdentity.operator.brandName}</h3>
              <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
                负责官网社区信息与官方联系渠道。上游仓库维护者负责代码审阅与合并决策。
              </p>
              <dl className="mt-5 space-y-3 border-t border-border pt-4 text-sm">
                <div className="grid grid-cols-[5rem_minmax(0,1fr)] gap-3">
                  <dt className="text-muted-foreground">法人主体</dt>
                  <dd>{siteIdentity.operator.legalName}</dd>
                </div>
                <div className="grid grid-cols-[5rem_minmax(0,1fr)] gap-3">
                  <dt className="text-muted-foreground">最后更新</dt>
                  <dd className="font-mono text-xs">{siteIdentity.lastUpdated}</dd>
                </div>
              </dl>
              <div className="mt-4 flex flex-wrap gap-x-4 gap-y-2 text-sm">
                <Link href="/about" className="font-medium underline decoration-border underline-offset-4 hover:text-accent hover:decoration-accent">
                  查看维护主体
                </Link>
                <Link
                  href="/contact"
                  className="font-medium underline decoration-border underline-offset-4 hover:text-accent hover:decoration-accent"
                >
                  联系官网维护者
                </Link>
              </div>
            </div>

            <div className="border-t border-border p-6 md:p-7 lg:border-l lg:border-t-0">
              <div className="flex items-baseline justify-between gap-4">
                <div>
                  <p className="font-mono text-xs font-semibold text-accent">贡献记录</p>
                  <h3 className="mt-2 text-xl font-semibold">贡献者</h3>
                </div>
                <a
                  href={`${repo}/graphs/contributors`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-xs font-medium text-muted-foreground underline decoration-border underline-offset-4 hover:text-accent hover:decoration-accent"
                >
                  完整记录 ↗
                </a>
              </div>
              <p className="mt-2 text-sm text-muted-foreground">
                沿用原贡献者展示；身份与贡献范围以 GitHub 提交和 PR 记录为准。
              </p>
              <div className="relative mt-5 overflow-hidden rounded-md bg-[oklch(0.976_0.005_165)] px-3 py-4 [mask-image:linear-gradient(to_right,transparent,black_8%,black_92%,transparent)]">
                <div className="crew-row">
                  {[...contributors, ...contributors].map((login, i) => (
                    <a key={`a-${i}`} href={`https://github.com/${login}`} target="_blank" rel="noopener noreferrer" title={login}>
                      <Image
                        src={`https://github.com/${login}.png`}
                        width={72}
                        height={72}
                        alt={`${login} 的头像`}
                        className="h-9 w-9 rounded-full border border-white/70 object-cover shadow-sm"
                      />
                    </a>
                  ))}
                </div>
                <div className="crew-row rev mt-3">
                  {[...contributors, ...contributors].map((login, i) => (
                    <a key={`b-${i}`} href={`https://github.com/${login}`} target="_blank" rel="noopener noreferrer" title={login}>
                      <Image
                        src={`https://github.com/${login}.png`}
                        width={72}
                        height={72}
                        alt={`${login} 的头像`}
                        className="h-9 w-9 rounded-full border border-white/70 object-cover shadow-sm"
                      />
                    </a>
                  ))}
                </div>
              </div>
            </div>
          </div>
        </Reveal>
      </div>
    </section>
  );
}
