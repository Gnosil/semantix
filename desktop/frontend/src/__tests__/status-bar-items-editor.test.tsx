// Run: tsx src/__tests__/status-bar-items-editor.test.tsx

import { JSDOM } from "jsdom";
import React, { useState } from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { StatusBarItemsEditor } from "../components/StatusBarItemsEditor";
import { LocaleProvider } from "../lib/i18n";
import { DEFAULT_STATUS_BAR_ITEMS, STATUS_BAR_ITEM_IDS, type StatusBarItemId } from "../lib/statusBarItems";

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

function flush() {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

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
globalThis.HTMLButtonElement = dom.window.HTMLButtonElement;
globalThis.HTMLInputElement = dom.window.HTMLInputElement;
globalThis.Event = dom.window.Event;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
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

let latestItems: StatusBarItemId[] = [];

function Harness({ initialItems = DEFAULT_STATUS_BAR_ITEMS }: { initialItems?: StatusBarItemId[] }) {
  const [items, setItems] = useState<StatusBarItemId[]>([...initialItems]);
  latestItems = items;
  return (
    <LocaleProvider>
      <StatusBarItemsEditor
        items={items}
        busy={false}
        onChange={setItems}
        itemLabel={(id) => id}
      />
    </LocaleProvider>
  );
}

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);

console.log("\nstatus bar items editor");

await act(async () => {
  root.render(<Harness />);
  await flush();
});

const expand = document.querySelector<HTMLButtonElement>('button[aria-label="Expand status bar items"]');
ok(expand instanceof HTMLButtonElement, "collapsed editor exposes an accessible expand control");
ok(document.querySelector(".status-bar-items-editor__summary-text")?.textContent === "6/16 shown", "the summary counts the full supported registry independently of defaults");

await act(async () => {
  expand?.click();
  await flush();
});

ok(document.body.textContent?.includes("Shown · 6") === true, "the focused default shows six status items");
ok(document.body.textContent?.includes("Hidden · 10") === true, "all ten optional metrics remain available in the hidden zone");
ok(document.querySelectorAll('[data-statusbar-drop-zone="hidden"] [data-statusbar-setting-item]').length === 10, "the hidden zone renders every optional metric");
ok(document.querySelector('[data-statusbar-drop-zone="hidden"] [data-statusbar-setting-item="balance"]') != null, "an optional balance metric remains configurable");
ok(document.querySelectorAll('[data-statusbar-drop-zone="hidden"]').length === 1, "hidden zone is an explicit drag target");

const modelRow = document.querySelector<HTMLElement>('[data-statusbar-setting-item="model"]');
const modelToggle = modelRow?.querySelector<HTMLInputElement>('input[type="checkbox"]');
await act(async () => {
  modelToggle?.click();
  await flush();
});

ok(latestItems.length === 5 && !latestItems.includes("model"), "clearing a visible item removes it from the persisted order");
ok(document.body.textContent?.includes("Shown · 5") === true, "visible count updates after hiding an item");
ok(document.body.textContent?.includes("Hidden · 11") === true, "hidden count includes both optional and newly hidden metrics");
ok(document.querySelector('[data-statusbar-drop-zone="hidden"] [data-statusbar-setting-item="model"]') != null, "hidden item moves into the hidden zone");

const showAll = Array.from(document.querySelectorAll<HTMLButtonElement>("button")).find((button) => button.textContent === "Show all");
await act(async () => {
  showAll?.click();
  await flush();
});
const remainingDefaultItems = DEFAULT_STATUS_BAR_ITEMS.filter((id) => id !== "model");
ok(latestItems.length === 16 && STATUS_BAR_ITEM_IDS.every((id) => latestItems.includes(id)), "show all restores all sixteen supported metrics");
ok(remainingDefaultItems.every((id, index) => latestItems[index] === id), "show all preserves the current visible order before appending hidden items");

const moveWorkspaceDown = document.querySelector<HTMLButtonElement>('button[aria-label="Move workspace down"]');
await act(async () => {
  moveWorkspaceDown?.click();
  await flush();
});
ok(latestItems[0] === "git_branch" && latestItems[1] === "workspace", "keyboard order controls update the visible order");

const restoreDefault = Array.from(document.querySelectorAll<HTMLButtonElement>("button")).find((button) => button.textContent === "Restore default");
await act(async () => {
  restoreDefault?.click();
  await flush();
});
ok(latestItems.length === 6 && latestItems.every((id, index) => id === DEFAULT_STATUS_BAR_ITEMS[index]), "restore default returns the focused six-item order");

const customItems: StatusBarItemId[] = ["balance", "turn_tps", "model", "cache"];
await act(async () => {
  root.render(<Harness key="custom-order" initialItems={customItems} />);
  await flush();
});
ok(latestItems.length === customItems.length && customItems.every((id, index) => latestItems[index] === id), "opening settings preserves a saved order containing optional metrics");
await act(async () => {
  document.querySelector<HTMLButtonElement>('button[aria-label="Expand status bar items"]')?.click();
  await flush();
});
ok(document.body.textContent?.includes("Shown · 4") === true && document.body.textContent?.includes("Hidden · 12") === true, "custom settings account for the full supported registry");
await act(async () => {
  Array.from(document.querySelectorAll<HTMLButtonElement>("button")).find((button) => button.textContent === "Show all")?.click();
  await flush();
});
ok(latestItems.length === 16 && new Set(latestItems).size === 16, "show all restores every metric once from a custom saved selection");
ok(customItems.every((id, index) => latestItems[index] === id), "show all preserves the saved custom order at the front");

await act(async () => root.unmount());
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
