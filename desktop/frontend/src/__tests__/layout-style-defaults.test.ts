// Run: tsx src/__tests__/layout-style-defaults.test.ts

import { JSDOM } from "jsdom";

let passed = 0;
let failed = 0;

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

console.log("\nlayout style defaults");

const dom = new JSDOM("<!doctype html><html><body></body></html>", { url: "http://localhost" });
Object.defineProperty(dom.window, "innerWidth", { configurable: true, value: 1920 });
globalThis.window = dom.window as unknown as Window & typeof globalThis;

const layout = await import("../store/layout");

eq(layout.defaultSidebarWidth(), 300, "wide classic default remains responsive");
eq(layout.useLayoutStore.getState().sidebarWidth, 300, "shared store starts from the classic sidebar default");
eq(layout.useLayoutStore.getState().workspaceFloatWidth, 420, "shared store starts the floating panel at its default width");
eq(layout.useLayoutStore.getState().workspaceFloatOpen, false, "the floating panel starts closed on every launch");

layout.applyLayoutStyleDefaults("creation");
eq(layout.useLayoutStore.getState().sidebarWidth, 236, "Creation applies its sidebar default after style hydration");
eq(layout.useLayoutStore.getState().workspaceFloatWidth, 420, "layout styles do not change the floating panel width");
eq(dom.window.localStorage.getItem("semantix.layoutPreferences.v1"), null, "applying defaults does not overwrite user preferences");

layout.applyLayoutStyleDefaults("workbench");
eq(layout.useLayoutStore.getState().sidebarWidth, 300, "switching to workbench restores its responsive sidebar default");

layout.saveSidebarWidth(286);
layout.saveWorkspaceFloatWidth(640);
layout.useLayoutStore.getState().setSidebarWidth(286);
layout.useLayoutStore.getState().setWorkspaceFloatWidth(640);
layout.applyLayoutStyleDefaults("creation");
eq(layout.useLayoutStore.getState().sidebarWidth, 286, "Creation preserves a saved sidebar width");
eq(layout.useLayoutStore.getState().workspaceFloatWidth, 640, "Creation preserves a saved floating panel width");

dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
