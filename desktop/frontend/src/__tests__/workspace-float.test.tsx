// Run: tsx src/__tests__/workspace-float.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { WorkspaceCapsules, WorkspaceFloat, formatChangedBadge, useChangedFileCount } from "../components/WorkspaceFloat";
import { LocaleProvider } from "../lib/i18n";
import type { WorkspaceFloatMode } from "../store/layout";

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
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
  globalThis.Node = dom.window.Node;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.Event = dom.window.Event;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.PointerEvent = dom.window.MouseEvent as unknown as typeof PointerEvent;
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

async function mount(node: React.ReactNode) {
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  await act(async () => {
    root.render(<LocaleProvider>{node}</LocaleProvider>);
    await wait();
  });
  return root;
}

function pressEscape(target: Element | Document) {
  const event = new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true });
  target.dispatchEvent(event);
  return event;
}

console.log("\nchanged badge");
eq(formatChangedBadge(3), "3", "small counts render verbatim");
eq(formatChangedBadge(120), "99+", "large counts cap at 99+");

console.log("\nworkspace capsules");
{
  const dom = installDom();
  const toggles: WorkspaceFloatMode[] = [];
  const root = await mount(
    <WorkspaceCapsules onToggle={(mode) => toggles.push(mode)} />,
  );
  const capsules = [...document.querySelectorAll(".workspace-capsule")] as HTMLButtonElement[];
  eq(capsules.length, 2, "two capsules render");
  eq(capsules[0]?.getAttribute("aria-label"), "Files", "first capsule is the file tree");
  eq(capsules[1]?.getAttribute("aria-label"), "Changes", "second capsule is the change list");
  eq(document.querySelector(".workspace-capsule__badge"), null, "no badge without changes");
  await act(async () => {
    capsules[1]?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await wait();
  });
  eq(toggles[0], "changed", "clicking a capsule toggles its view");
  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}
{
  const dom = installDom();
  const root = await mount(
    <WorkspaceCapsules changedCount={7} onToggle={() => {}} />,
  );
  const capsules = [...document.querySelectorAll(".workspace-capsule")] as HTMLButtonElement[];
  eq(document.querySelector(".workspace-capsule__badge")?.textContent, "7", "changed count renders as a badge");
  eq(capsules[1]?.getAttribute("aria-label"), "Changes · 7 files", "badge count is announced in the capsule label");
  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log("\nworkspace float");
{
  const dom = installDom();
  let closed = 0;
  const widths: Array<[number, boolean]> = [];
  const reported: number[] = [];
  const modeChanges: WorkspaceFloatMode[] = [];
  const root = await mount(
    <WorkspaceFloat
      open
      mode="files"
      onModeChange={(mode) => modeChanges.push(mode)}
      width={420}
      mode="files"
      width={420}
      onWidthChange={(width, commit) => widths.push([width, commit])}
      onRenderWidth={(width) => reported.push(width)}
      onClose={() => { closed += 1; }}
    >
      <div className="probe"><input className="probe-input" /></div>
    </WorkspaceFloat>,
  );
  const aside = document.querySelector(".workspace-float") as HTMLElement | null;
  ok(aside !== null && aside.getAttribute("role") === "complementary", "open panel renders as a complementary landmark");
  eq(aside?.style.getPropertyValue("--workspace-float-width"), "420px", "panel exposes its preferred width to CSS");
  eq(document.querySelectorAll(".workspace-float__tab").length, 2, "panel head carries the two view tabs");
  eq(document.querySelector(".workspace-float__tab--active")?.textContent, "Files", "active view tab is marked");
  await act(async () => {
    ([...document.querySelectorAll(".workspace-float__tab")][1] as HTMLButtonElement).dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await wait();
  });
  eq(modeChanges[0], "changed", "clicking the header tab switches the view");
  ok(document.querySelector(".workspace-float__body .probe") !== null, "children mount inside the panel body");
  ok(reported.length > 0, "rendered width is reported to the host");
  const resizer = document.querySelector(".workspace-float__resizer") as HTMLElement | null;
  eq(resizer?.getAttribute("aria-valuemin"), "320", "resizer announces the minimum width");
  eq(resizer?.getAttribute("aria-valuemax"), "860", "resizer announces the maximum width");

  await act(async () => {
    resizer?.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowLeft", bubbles: true }));
    await wait();
  });
  eq(JSON.stringify(widths.at(-1)), JSON.stringify([436, true]), "ArrowLeft widens the panel and commits");
  await act(async () => {
    resizer?.dispatchEvent(new KeyboardEvent("keydown", { key: "End", bubbles: true }));
    await wait();
  });
  eq(JSON.stringify(widths.at(-1)), JSON.stringify([860, true]), "End jumps to the maximum width");

  await act(async () => {
    pressEscape(document.querySelector(".probe-input") as Element);
    await wait();
  });
  eq(closed, 0, "Escape inside an input is left to the input");
  await act(async () => {
    pressEscape(document);
    await wait();
  });
  eq(closed, 1, "Escape outside inputs closes the panel");
  await act(async () => {
    (document.querySelector(".workspace-float__close") as HTMLButtonElement | null)?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await wait();
  });
  eq(closed, 2, "the head close button closes the panel");
  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}
{
  const dom = installDom();
  const root = await mount(
    <WorkspaceFloat open={false} mode="changed" onModeChange={() => {}} width={420} onWidthChange={() => {}} onRenderWidth={() => {}} onClose={() => {}}>
      <div className="probe" />
    </WorkspaceFloat>,
  );
  eq(document.querySelector(".workspace-float"), null, "closed panel renders nothing");
  eq(document.querySelector(".probe"), null, "children are not mounted while closed");
  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log("\nchanged file count");
{
  const dom = installDom();
  const calls: string[] = [];
  (window as unknown as { go: unknown }).go = {
    main: {
      App: {
        WorkspaceChanges: async (tabId: string) => {
          calls.push(tabId);
          return { files: tabId === "tab-a" ? [{ path: "a.ts" }, { path: "b.ts" }, { path: "c.ts" }] : [], gitAvailable: true };
        },
      },
    },
  };
  let latest = -1;
  function Probe({ tabId }: { tabId?: string }) {
    latest = useChangedFileCount(tabId, `${tabId ?? ""}\u0000/repo`);
    return null;
  }
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  await act(async () => {
    root.render(<Probe tabId="tab-a" />);
    await wait(10);
  });
  eq(latest, 3, "count follows the WorkspaceChanges file list");
  eq(calls[0], "tab-a", "count is requested for the active tab");
  await act(async () => {
    root.render(<Probe tabId={undefined} />);
    await wait(10);
  });
  eq(latest, 0, "no tab means no changes");
  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
