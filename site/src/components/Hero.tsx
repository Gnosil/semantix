import Reveal from "@/components/Reveal";

export default function Hero() {
  return (
    <section className="relative overflow-hidden pt-12 pb-20 md:pt-16">
      {/* 两侧装饰：光晕 + 淡色 mono 文字 */}
      <div aria-hidden="true" className="pointer-events-none absolute inset-0 hidden lg:block">
        {/* 左光晕 */}
        <div className="absolute -left-40 top-24 h-[420px] w-[420px] rounded-full bg-[oklch(0.608_0.14_165/0.08)] blur-3xl" />
        {/* 右光晕 */}
        <div className="absolute -right-40 bottom-16 h-[380px] w-[380px] rounded-full bg-[oklch(0.524_0.12_165/0.08)] blur-3xl" />
        {/* 左装饰文字 */}
        <div className="absolute left-10 top-1/2 -translate-y-1/2 -rotate-90 origin-left whitespace-nowrap font-mono text-xs tracking-[0.35em] text-[oklch(0.45_0.05_165/0.4)]">
          extract · index · retrieve · evolve
        </div>
        {/* 右装饰文字 */}
        <div className="absolute right-10 top-1/2 -translate-y-1/2 rotate-90 origin-right whitespace-nowrap font-mono text-xs tracking-[0.35em] text-[oklch(0.45_0.05_165/0.4)]">
          L1 · L2 · L3 · semantic cache
        </div>
      </div>
      <div className="relative wrap text-center">
        {/* terminal demo */}
        <Reveal>
          <div className="mx-auto max-w-3xl text-left">
          <div className="pane-in overflow-hidden rounded-xl border border-border bg-[oklch(0.21_0.006_260)] shadow-lg">
            <div className="flex items-center gap-2 bg-[oklch(0.27_0.008_260)] px-4 py-3">
              <span className="h-3 w-3 rounded-full bg-[#F87171]" />
              <span className="h-3 w-3 rounded-full bg-[#FBBF24]" />
              <span className="h-3 w-3 rounded-full bg-[#4ADE80]" />
              <span className="ml-2 font-mono text-xs text-slate-400">
                ~/semantix — extract &amp; search
              </span>
            </div>
            <div className="px-5 py-4 font-mono text-sm leading-relaxed text-slate-300">
              <div>
                <span className="text-accent">$ semantix extract</span>{" "}
                <span className="text-slate-400">
                  -session sessions/2026-08-07.jsonl -scope project
                </span>
              </div>
              <div className="text-[#4ADE80]">
                ✓ extracted=512 slices stored=512 scope=project
              </div>
              <div className="text-slate-400">
                {"  "}P-slices 214 · C-slices 158 · T-slices 140
              </div>
              <div>&nbsp;</div>
              <div>
                <span className="text-accent">$ semantix search</span>{" "}
                <span className="text-slate-400">
                  &quot;如何缓存跨会话前缀&quot; -topk 3
                </span>
              </div>
              <div>
                #1 [P] id=9f2a score=7.42 &quot;stable slice injection → vendor byte
                cache&quot;
              </div>
              <div>
                #2 [T] id=3c81 score=6.98 &quot;grep → readFile → editFile → test&quot;
              </div>
              <div>
                #3 [C] id=b704 score=6.51 &quot;L2 freeze-period ≥1h guard&quot;
              </div>
              <div>&nbsp;</div>
              <div className="text-slate-500">
                命中 3 slices · 0.4ms · 下次会话注入前缀，直接命中字节缓存
              </div>
              <div>
                <span className="blink inline-block h-4 w-2 bg-emerald-400 align-middle">
                  ▍
                </span>
              </div>
            </div>
          </div>
          </div>
        </Reveal>
      </div>
    </section>
  );
}
