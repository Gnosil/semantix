import { useEffect, useRef, useState } from "react";

const IDLE_MS = 3000;
const SCRAMBLE_MS = 640;
const LATIN_GLYPHS = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";
const CJK_GLYPHS = "代码计划记忆上下文工作项目";

// Inspired by Magic UI Hyper Text. Only the visual text changes per frame;
// assistive text and the surrounding layout stay stable until the reveal ends.
export function HyperTextLine({ messages, className = "" }: {
  messages: readonly string[];
  className?: string;
}) {
  const root = useRef<HTMLParagraphElement>(null);
  const visual = useRef<HTMLSpanElement>(null);
  const advance = useRef<() => void>(() => {});
  const [settled, setSettled] = useState(messages[0] ?? "");

  useEffect(() => {
    const element = root.current;
    const text = visual.current;
    if (!element || !text || messages.length === 0) return;
    const preference = window.matchMedia("(prefers-reduced-motion: reduce)");
    let index = 0;
    let target = messages[0];
    let frame = 0;
    let idleTimer = 0;
    let busy = false;
    let lastActivity = window.performance.now();
    text.textContent = target;
    setSettled(target);

    const available = () => document.visibilityState !== "hidden"
      && !element.closest('[hidden],[inert],[aria-hidden="true"]')
      && !document.querySelector(".settings-modal-backdrop,.onboarding")
      && (!document.querySelector(".startup-splash") || !!element.closest(".startup-splash"));
    const armIdle = () => {
      if (idleTimer || document.visibilityState === "hidden") return;
      const remaining = Math.max(1, IDLE_MS - (window.performance.now() - lastActivity));
      idleTimer = window.setTimeout(() => {
        idleTimer = 0;
        if (window.performance.now() - lastActivity < IDLE_MS) { armIdle(); return; }
        if (!available()) { lastActivity = window.performance.now(); armIdle(); return; }
        next();
      }, remaining);
    };
    const complete = () => {
      window.cancelAnimationFrame(frame);
      frame = 0;
      busy = false;
      text.textContent = target;
      element.dataset.scrambling = "false";
      setSettled(target);
      lastActivity = window.performance.now();
      armIdle();
    };
    const next = () => {
      if (busy || messages.length < 2 || !available()) return;
      window.clearTimeout(idleTimer);
      idleTimer = 0;
      index = (index + 1) % messages.length;
      target = messages[index];
      if (preference.matches) { complete(); return; }
      busy = true;
      element.dataset.scrambling = "true";
      const characters = Array.from(target);
      const glyphs = Array.from(/\p{Script=Han}/u.test(target) ? CJK_GLYPHS : LATIN_GLYPHS);
      const start = window.performance.now();
      let lastPaint = -Infinity;
      const paint = (time: number) => {
        if (time - start >= SCRAMBLE_MS) { complete(); return; }
        // Keep letter flicker at 30fps, independent of display refresh rate.
        if (time - lastPaint >= 32) {
          const revealed = Math.floor(characters.length * (time - start) / SCRAMBLE_MS);
          text.textContent = characters.map((character, position) =>
            position < revealed || !/[\p{L}\p{N}]/u.test(character)
              ? character : glyphs[Math.floor(Math.random() * glyphs.length)]).join("");
          lastPaint = time;
        }
        frame = window.requestAnimationFrame(paint);
      };
      frame = window.requestAnimationFrame(paint);
    };
    const activity = () => { lastActivity = window.performance.now(); if (!busy) armIdle(); };
    const visibility = () => {
      window.clearTimeout(idleTimer);
      idleTimer = 0;
      if (busy) complete();
      if (document.visibilityState !== "hidden") activity();
    };
    const motionChanged = () => { if (preference.matches && busy) complete(); };
    advance.current = next;
    const events = ["pointermove", "pointerdown", "keydown", "wheel", "scroll"] as const;
    events.forEach(event => window.addEventListener(event, activity, { passive: true, capture: true }));
    document.addEventListener("visibilitychange", visibility);
    preference.addEventListener("change", motionChanged);
    armIdle();
    return () => {
      advance.current = () => {};
      window.clearTimeout(idleTimer);
      window.cancelAnimationFrame(frame);
      events.forEach(event => window.removeEventListener(event, activity, true));
      document.removeEventListener("visibilitychange", visibility);
      preference.removeEventListener("change", motionChanged);
    };
  }, [messages]);

  return <p ref={root} className={`semantix-hypertext ${className}`}
    onPointerEnter={event => { if (event.pointerType !== "touch") advance.current(); }}>
    {messages.map((message, index) => <span key={index} className="semantix-hypertext__reserve" aria-hidden="true">{message}</span>)}
    <span ref={visual} className="semantix-hypertext__visual" aria-hidden="true">{settled}</span>
    <span className="sr-only">{settled}</span>
  </p>;
}
