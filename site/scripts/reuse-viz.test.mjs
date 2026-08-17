import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

const srcRoot = path.resolve(import.meta.dirname, "../src");
const repoRoot = path.resolve(import.meta.dirname, "../..");

test("homepage ships a reuse-visualization terminal mockup", async () => {
  const componentsSource = await readFile(
    path.join(srcRoot, "components/Components.tsx"),
    "utf8",
  );

  for (const marker of [
    "REUSE VISUALIZATION",
    "semantix dashboard",
    "💰 Cost savings",
    "🎯 Cache hit rate (L3/L2)",
    "🗂 Zone distribution",
    "📦 Slice library",
    "from:2026-08-",
    "3/3 hits in 3 sessions",
  ]) {
    assert.ok(componentsSource.includes(marker), `Components.tsx should show ${marker}`);
  }
  assert.match(componentsSource, /真实输出|演示库实录/);
  assert.match(componentsSource, /门禁数据以 semantix verify 报告为准/);
});

test("README documents the dashboard example and zone legend", async () => {
  const readme = await readFile(path.join(repoRoot, "README.md"), "utf8");

  assert.match(readme, /# Reuse Visualization/);
  assert.match(readme, /semantix dashboard/);
  assert.match(readme, /💰 Cost savings/);
  assert.match(readme, /🎯 Cache hit rate \(L3\/L2\)/);
  assert.match(readme, /🗂 Zone distribution/);
  assert.match(readme, /📦 Slice library/);

  // Two icon families: retrieval zones vs. the verify gate.
  assert.match(readme, /🟢 hit · 🟡 grey · ⚪ miss/);
  assert.match(readme, /✅hit · 🟡grey · ❌miss/);
  assert.match(readme, /✅ PASS · ⚠ WARN · ❌ FAIL/);
  assert.match(readme, /# ✅ PASS relevance=75\.0% \(≥70%\)/);
  assert.match(readme, /🎯 3\/3 hits in 3 sessions/);

  assert.match(readme, /CLI v2 \(U19–U27\) ✅/);
  assert.match(readme, /CLI reuse viz \(U28–U31\) ✅/);
});

test("homepage export renders the reuse dashboard", async () => {
  const home = await readFile(new URL("../out/index.html", import.meta.url), "utf8");
  const visible = home.replaceAll("<!-- -->", "");

  assert.ok(visible.includes("REUSE VISUALIZATION"), "export should contain the mockup header");
  assert.ok(visible.includes("semantix dashboard"), "export should show the dashboard command");
  assert.ok(visible.includes("Cost savings"), "export should show the cost-savings bar");
});
