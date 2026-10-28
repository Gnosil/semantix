import { useEffect, useRef, type CSSProperties } from "react";

/** Magic UI border-beam: masked offset-path, adapted to native CSS + WAAPI.
 * Original registry source is preserved in border-beam.upstream.txt.
 */
export function BorderBeam({ active = true, duration = 5000, size = 48 }: { active?: boolean; duration?: number; size?: number }) {
  const root = useRef<HTMLSpanElement>(null);
  const light = useRef<HTMLSpanElement>(null);

  useEffect(() => {
    const element = light.current;
    const container = root.current;
    if (!active || !element || !container || typeof element.animate !== "function") return;
    if (typeof CSS !== "undefined" && !CSS.supports("offset-path", "rect(0 auto auto 0 round 4px)")) return;
    const media = window.matchMedia?.("(prefers-reduced-motion: reduce)");
    let animation: Animation | undefined;
    let visible = typeof IntersectionObserver === "undefined";
    const sync = () => {
      if (media?.matches) {
        animation?.cancel();
        animation = undefined;
        container.dataset.beamRunning = "false";
        return;
      }
      const shouldRun = visible && document.visibilityState !== "hidden";
      if (shouldRun) {
        animation ??= element.animate([{ offsetDistance: "0%" }, { offsetDistance: "100%" }], {
          duration, iterations: Infinity, easing: "linear",
        });
        animation.play();
      } else animation?.pause();
      container.dataset.beamRunning = String(shouldRun);
    };
    const observer = typeof IntersectionObserver === "undefined" ? undefined : new IntersectionObserver(entries => {
      visible = entries.some(entry => entry.isIntersecting);
      sync();
    });
    observer?.observe(container);
    media?.addEventListener("change", sync);
    document.addEventListener("visibilitychange", sync);
    sync();
    return () => {
      animation?.cancel();
      observer?.disconnect();
      media?.removeEventListener("change", sync);
      document.removeEventListener("visibilitychange", sync);
    };
  }, [active, duration]);

  if (!active) return null;
  return <span ref={root} className="semantix-border-beam" aria-hidden="true" style={{ "--beam-size": `${size}px` } as CSSProperties}>
    <span ref={light} className="semantix-border-beam__light" />
  </span>;
}
