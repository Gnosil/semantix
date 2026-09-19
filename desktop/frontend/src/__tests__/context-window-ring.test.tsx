// Run: tsx src/__tests__/context-window-ring.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { ContextWindowRing } from "../components/ContextWindowRing";
import { LocaleProvider } from "../lib/i18n";
import type { ContextInfo } from "../lib/types";

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) ok(true, label);
  else ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

function wait(ms = 0): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

class TestResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

function installDom() {
  const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  // Pin the locale to JSDOM's en-US; Node's own navigator follows the OS language.
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
  globalThis.Node = dom.window.Node;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.Event = dom.window.Event;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  globalThis.ResizeObserver = TestResizeObserver;
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    value: () => ({
      matches: true,
      media: "(prefers-reduced-motion: reduce)",
      onchange: null,
      addEventListener() {},
      removeEventListener() {},
      addListener() {},
      removeListener() {},
      dispatchEvent: () => false,
    }),
  });
  return dom;
}

// The ring must render purely from props: any bound-method access is a regression.
function forbidBridgeAccess() {
  const trap = new Proxy({}, {
    get(_target, prop) {
      throw new Error(`ContextWindowRing must not call bound method ${String(prop)}`);
    },
  });
  (window as unknown as { go: { main: { App: unknown } } }).go = { main: { App: trap } };
}

function contextInfo(overrides: Partial<ContextInfo> = {}): ContextInfo {
  return { used: 10, window: 100, sessionTokens: 0, compactRatio: 0.8, ...overrides };
}

async function renderRing(props: Partial<Parameters<typeof ContextWindowRing>[0]> = {}) {
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  let currentProps: Parameters<typeof ContextWindowRing>[0] = {
    enabled: true,
    tabId: "tab-a",
    context: contextInfo(),
    ...props,
  };
  const paint = async (nextProps: Partial<Parameters<typeof ContextWindowRing>[0]> = {}) => {
    currentProps = { ...currentProps, ...nextProps };
    await act(async () => {
      root.render(
        <LocaleProvider>
          <ContextWindowRing {...currentProps} />
        </LocaleProvider>,
      );
      await wait();
    });
  };
  await paint();
  return { root, rerender: paint };
}

function popoverRow(label: string): Element | undefined {
  return [...document.querySelectorAll(".context-ring-popover__row")]
    .find((row) => row.querySelector(".context-ring-popover__label")?.textContent === label);
}

async function hover(button: HTMLElement) {
  await act(async () => {
    button.dispatchEvent(new MouseEvent("mouseover", { bubbles: true, relatedTarget: null }));
    await wait(220);
  });
}

console.log("\ncontext window ring");

{
  const dom = installDom();
  forbidBridgeAccess();

  const { root } = await renderRing({ enabled: false });

  eq(document.querySelector(".context-ring"), null, "disabled ring renders nothing");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  forbidBridgeAccess();

  const { root } = await renderRing({ turnCost: 0.125, currency: "$" });
  const button = document.querySelector(".context-ring") as HTMLButtonElement | null;
  if (!button) throw new Error("missing context ring button");
  await hover(button);
  eq(
    popoverRow("turn cost")?.querySelector(".context-ring-popover__value")?.textContent,
    "$0.1250",
    "turn cost falls back to the composer currency when the context has none",
  );
  eq(popoverRow("Requests"), undefined, "request row is hidden before any request landed");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  forbidBridgeAccess();

  const { root } = await renderRing({ context: contextInfo({ used: 1_001, window: 1_000 }) });
  const button = document.querySelector(".context-ring") as HTMLButtonElement | null;
  if (!button) throw new Error("missing over-limit context ring button");
  await hover(button);

  const popover = document.querySelector(".context-ring-popover");
  const fill = popover?.querySelector(".context-ring-popover__fill") as HTMLElement | null;
  eq(popover?.querySelector(".context-ring-popover__pct")?.textContent, "101%", "ring popover keeps a just-over-limit ratio visibly above 100 percent");
  eq(fill?.style.width, "100%", "ring popover fill is capped at the physical track width");
  eq(popover?.querySelectorAll(".context-ring-popover__seg").length, 0, "ring popover does not mix token composition into its capacity fill");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  forbidBridgeAccess();

  const { root, rerender } = await renderRing({
    tabId: "old-tab",
    context: contextInfo({ requestCount: 1, elapsedMs: 65_000, sessionCost: 0.5, sessionCurrency: "USD" }),
  });
  const button = document.querySelector(".context-ring") as HTMLButtonElement | null;
  if (!button) throw new Error("missing context ring button");
  await hover(button);
  eq(popoverRow("Requests")?.querySelector(".context-ring-popover__value")?.textContent, "1", "request count comes from the live context snapshot");
  ok(popoverRow("Runtime") !== undefined, "elapsed time row renders from the context snapshot");

  // A tab switch swaps the snapshot synchronously: no stale async response can
  // paint the previous tab's numbers over the new one.
  await rerender({ tabId: "new-tab", context: contextInfo({ requestCount: 2, elapsedMs: 0 }) });
  const nextButton = document.querySelector(".context-ring") as HTMLButtonElement | null;
  if (!nextButton) throw new Error("missing context ring button after tab switch");
  await hover(nextButton);
  eq(popoverRow("Requests")?.querySelector(".context-ring-popover__value")?.textContent, "2", "tab switch shows the new tab's request count immediately");
  eq(popoverRow("Runtime"), undefined, "elapsed row disappears when the new snapshot has no duration");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
