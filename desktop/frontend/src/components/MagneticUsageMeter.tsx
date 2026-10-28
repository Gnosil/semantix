import { useEffect, useId, useRef, useState } from "react";

interface Props {
  percent: number;
  className: string;
  height?: number;
  step?: number;
}

export function MagneticUsageMeter({ percent, className, height = 6, step = 5 }: Props) {
  const root = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(1);
  const gradientId = `usage-meter-${useId()}`;
  const ratio = Number.isFinite(percent) ? Math.min(100, Math.max(0, percent)) / 100 : 0;

  useEffect(() => {
    const element = root.current;
    if (!element) return;
    const measure = () => setWidth(Math.max(1, element.getBoundingClientRect().width));
    measure();
    if (typeof ResizeObserver !== "function") {
      window.addEventListener("resize", measure);
      return () => window.removeEventListener("resize", measure);
    }
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    const element = root.current;
    if (!element) return;
    const ticks = Array.from(element.querySelectorAll<SVGRectElement>("rect"));
    if (typeof window.matchMedia !== "function") return;
    const preference = window.matchMedia("(prefers-reduced-motion: reduce)");
    let left = 0;
    let point = 0;
    let active = false;
    let strength = 0;
    let frame = 0;
    let lastTime = 0;

    // Pointer tracking stays outside React. One paint consumes the newest
    // position, with no easing on that position and no per-move layout reads.
    const paint = (time: number) => {
      frame = 0;
      const elapsed = lastTime ? Math.min(time - lastTime, 64) : 16;
      lastTime = time;
      strength = active ? 1 : strength * Math.exp(-elapsed / 42);
      if (strength < 0.01) strength = 0;
      ticks.forEach((tick, index) => {
        const distance = point - (index * step + 1);
        const influence = strength * Math.exp(-((distance / 34) ** 2));
        tick.style.transform = influence > 0.001
          ? `translateX(${distance * influence * 0.06}px) scaleY(${1 + influence * 12 / height})`
          : "";
      });
      if (!active && strength > 0) frame = requestAnimationFrame(paint);
    };
    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(paint);
    };
    const measurePosition = () => { left = element.getBoundingClientRect().left; };
    const move = (event: PointerEvent) => {
      if (preference.matches || event.pointerType === "touch") return;
      if (!active) measurePosition();
      point = Math.max(0, Math.min(width, event.clientX - left));
      active = true;
      element.dataset.magneticActive = "true";
      schedule();
    };
    const leave = () => {
      active = false;
      element.dataset.magneticActive = "false";
      lastTime = 0;
      schedule();
    };
    const reset = () => {
      cancelAnimationFrame(frame);
      frame = 0;
      active = false;
      strength = 0;
      lastTime = 0;
      element.dataset.magneticActive = "false";
      ticks.forEach(tick => { tick.style.transform = ""; });
    };
    const onScroll = () => { if (active) measurePosition(); };
    element.addEventListener("pointermove", move);
    element.addEventListener("pointerleave", leave);
    element.addEventListener("pointercancel", leave);
    window.addEventListener("scroll", onScroll, true);
    preference.addEventListener("change", reset);
    return () => {
      reset();
      element.removeEventListener("pointermove", move);
      element.removeEventListener("pointerleave", leave);
      element.removeEventListener("pointercancel", leave);
      window.removeEventListener("scroll", onScroll, true);
      preference.removeEventListener("change", reset);
    };
  }, [width, height, step]);

  return <div ref={root} className={`${className} usage-ticks`} aria-hidden="true">
    <svg width="100%" height={height} viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none">
      <defs><linearGradient id={gradientId} x1="0" x2={width} gradientUnits="userSpaceOnUse">
        <stop offset={ratio} className="usage-tick-ink"/>
        <stop offset={ratio} className="usage-tick-track"/>
      </linearGradient></defs>
      {Array.from({ length: Math.ceil(width / step) }, (_, index) => <rect
        key={index} x={index * step} y="0" width="2" height={height} fill={`url(#${gradientId})`}
      />)}
    </svg>
  </div>;
}
