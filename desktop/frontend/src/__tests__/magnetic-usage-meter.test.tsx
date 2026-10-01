// Pointer movement must follow the newest sample without React commits,
// layout reads on every event, or a queue of trailing animations.
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import React, { act, Profiler } from "react";
import { createRoot } from "react-dom/client";
import { MagneticUsageMeter } from "../components/MagneticUsageMeter";

const dom = new JSDOM('<div id="root"></div>', { pretendToBeVisual: true });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
let reads = 0;
let measuredWidth = 250;
dom.window.HTMLElement.prototype.getBoundingClientRect = () => {
  reads++;
  return { left: 100, width: measuredWidth, top: 0, height: 4, right: 100 + measuredWidth, bottom: 4, x: 100, y: 0, toJSON() {} };
};
let resize: () => void = () => {};
globalThis.ResizeObserver = class {
  constructor(callback: () => void) { resize = callback; }
  observe() {}
  unobserve() {}
  disconnect() {}
} as unknown as typeof ResizeObserver;
let changed: () => void = () => {};
const preference = { matches: false, addEventListener: (_: string, listener: () => void) => { changed = listener; }, removeEventListener() {} };
window.matchMedia = () => preference as unknown as MediaQueryList;
let nextId = 0;
let time = 0;
const frames = new Map<number, FrameRequestCallback>();
globalThis.requestAnimationFrame = callback => { frames.set(++nextId, callback); return nextId; };
globalThis.cancelAnimationFrame = id => { frames.delete(id); };
const advance = () => {
  time += 16.67;
  const pending = [...frames.values()];
  frames.clear();
  pending.forEach(callback => callback(time));
};
let commits = 0;
const root = createRoot(document.getElementById("root")!);
await act(async () => { root.render(<Profiler id="meter" onRender={() => commits++}><MagneticUsageMeter percent={30} className="usage-context-meter" height={4}/></Profiler>); });
const meter = document.querySelector<HTMLDivElement>(".usage-ticks")!;
const ticks = () => [...meter.querySelectorAll<SVGRectElement>("rect")];
const move = (x: number, type = "mouse") => {
  const event = new dom.window.MouseEvent("pointermove", { clientX: 100 + x });
  Object.defineProperty(event, "pointerType", { value: type });
  meter.dispatchEvent(event);
};
const peak = () => ticks().map(tick => Number(tick.style.transform.match(/scaleY\(([^)]+)/)?.[1] ?? 1)).reduce((best, scale, index, list) => scale > list[best]! ? index : best, 0);
const baselineCommits = commits;
reads = 0;
move(20); move(70); move(110); move(190);
assert.equal(frames.size, 1, "a burst queues only one animation frame");
advance();
assert.ok(Math.abs(peak() * 5 + 1 - 190) <= 3, "the first paint uses the newest pointer position");
assert.equal(reads, 1, "geometry is measured once on entry");
move(30); advance();
assert.ok(Math.abs(peak() * 5 + 1 - 30) <= 3, "reversing direction follows in the next frame without lag");
assert.equal(reads, 1, "continuous tracking does not read layout");
assert.equal(commits, baselineCommits, "continuous tracking does not commit React renders");
assert.equal(frames.size, 0, "a stationary pointer runs no perpetual animation");
assert.equal(meter.querySelector("stop")?.getAttribute("offset"), "0.3", "hover preserves the data ratio");
meter.dispatchEvent(new dom.window.Event("pointerleave"));
for (let i = 0; i < 25; i++) advance();
assert.ok(ticks().every(tick => tick.style.transform === ""), "leave smoothly settles all ticks");
assert.equal(frames.size, 0, "the exit animation stops completely");
move(50, "touch");
assert.equal(frames.size, 0, "touch does not start decorative tracking");
move(80); advance();
preference.matches = true; changed();
assert.ok(ticks().every(tick => tick.style.transform === ""), "a reduced-motion change resets the wave");
move(130);
assert.equal(frames.size, 0, "reduced motion prevents tracking");
preference.matches = false; changed();
measuredWidth = 350;
await act(async () => { resize(); });
assert.equal(ticks().length, 70, "resizing rebuilds the full-width tick track");
move(320); advance();
assert.ok(Math.abs(peak() * 5 + 1 - 320) <= 3, "tracking uses the resized geometry");
move(100);
await act(async () => { root.unmount(); });
assert.equal(frames.size, 0, "unmount cancels a queued frame");
dom.window.close();
console.log("17 magnetic interaction checks passed");
