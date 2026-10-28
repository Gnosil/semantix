import { useLayoutEffect, useRef } from "react";
import { CSS_EASE_OUT, prefersReducedMotion } from "../lib/motion";

/** Forward-only digit reel, including carries such as 9 → 0. */
export function upwardDigitSteps(from: string, to: string): string[] {
  if (!/^\d$/.test(from) || !/^\d$/.test(to) || from === to) return [to];
  const start = Number(from);
  const distance = (Number(to) - start + 10) % 10;
  return Array.from({ length: distance + 1 }, (_, index) => String((start + index) % 10));
}

export function formatRollingTokens(value: number | undefined): string {
  if (value === undefined || !Number.isFinite(value) || value < 0) return "—";
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}k`;
  return String(Math.round(value));
}

export function UsageRollingNumber({ value, counterKey }: { value: number | undefined; counterKey: string }) {
  const root = useRef<HTMLElement>(null);
  const previous = useRef<{ value: number | undefined; text: string; key: string } | null>(null);
  const text = formatRollingTokens(value);

  useLayoutEffect(() => {
    const before = previous.current;
    previous.current = { value, text, key: counterKey };
    const element = root.current;
    if (!element || !before || before.key !== counterKey || value === undefined
      || before.value === undefined || value <= before.value || before.text === text
      || !/\d/.test(before.text) || prefersReducedMotion() || document.visibilityState === "hidden") return;

    const columns = Array.from(element.querySelectorAll<HTMLElement>("[data-counter-digit]"));
    const oldDigits = before.text.replace(/\D/g, "").padStart(columns.length, "0").slice(-columns.length);
    const running: Animation[] = [];
    const tracks: HTMLElement[] = [];
    const settle = () => {
      running.forEach(animation => animation.cancel());
      tracks.forEach(track => {
        track.replaceChildren();
        track.closest<HTMLElement>(".usage-roller__digit")?.removeAttribute("data-rolling");
      });
    };
    columns.forEach((column, index) => {
      const next = column.dataset.counterDigit ?? "0";
      const steps = upwardDigitSteps(oldDigits[index], next);
      const track = column.querySelector<HTMLElement>(".usage-roller__track");
      if (steps.length < 2 || !track || typeof track.animate !== "function") return;
      track.replaceChildren(...steps.map(digit => {
        const step = document.createElement("span");
        step.className = "usage-roller__step";
        step.textContent = digit;
        return step;
      }));
      tracks.push(track);
      column.dataset.rolling = "true";
      const animation = track.animate([
        { transform: "translateY(0)" },
        { transform: `translateY(-${(steps.length - 1) * 1.12}em)` },
      ], { duration: 600, delay: Math.min(index * 35, 140), easing: CSS_EASE_OUT, fill: "both" });
      running.push(animation);
      animation.onfinish = () => {
        column.removeAttribute("data-rolling");
        track.replaceChildren();
        animation.cancel();
      };
    });
    const preference = window.matchMedia?.("(prefers-reduced-motion: reduce)");
    const motionChange = () => { if (preference?.matches) settle(); };
    const visibilityChange = () => { if (document.visibilityState === "hidden") settle(); };
    preference?.addEventListener("change", motionChange);
    document.addEventListener("visibilitychange", visibilityChange);
    return () => {
      settle();
      preference?.removeEventListener("change", motionChange);
      document.removeEventListener("visibilitychange", visibilityChange);
    };
  }, [counterKey, text, value]);

  return <strong ref={root} className="usage-rolling-number" aria-label={text}>
    <span className="usage-roller" aria-hidden="true">
      {Array.from(text).map((character, index) => /\d/.test(character) ? (
        <span key={`digit-${index}`} className="usage-roller__digit" data-counter-digit={character}>
          <span className="usage-roller__settled">{character}</span>
          <span className="usage-roller__track" />
        </span>
      ) : <span key={`symbol-${index}`} className={`usage-roller__symbol${character === "." ? " usage-roller__decimal" : ""}`}>{character}</span>)}
    </span>
  </strong>;
}
