// Run: tsx src/__tests__/context-window.test.ts

import { contextWindowPercentages, contextWindowStatus, formatCacheHitRate } from "../lib/contextWindow";
import { formatMoneyLocalized } from "../lib/money";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

function ok(condition: boolean, label: string) {
  if (condition) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\ncontext window status");

eq(
  contextWindowPercentages(1_400_000, 1_000_000),
  { raw: 140, display: 100 },
  "over-limit context preserves the raw percentage while capping the meter fill",
);
eq(
  contextWindowPercentages(1_001, 1_000),
  { raw: 101, display: 100 },
  "just-over-limit context remains visibly over 100 percent after integer formatting",
);
eq(contextWindowPercentages(0, 1_000), { raw: 0, display: 0 }, "empty context reports zero");
eq(contextWindowStatus(33, 80), { tone: "good", key: "context.windowStatusHealthy" }, "low usage stays healthy");
eq(contextWindowStatus(72, 80), { tone: "notice", key: "context.windowStatusWatch" }, "usage near compact threshold warns early");
eq(contextWindowStatus(80, 80), { tone: "warn", key: "context.windowStatusPastCompact" }, "compact threshold reached takes warning tone");
eq(contextWindowStatus(91, 80), { tone: "warn", key: "context.windowStatusNearLimit" }, "near hard limit overrides compact status");
eq(contextWindowStatus(140, 80), { tone: "warn", key: "context.windowStatusOverLimit" }, "over-limit context has a distinct status");

console.log("\ncache hit rate");

eq(formatCacheHitRate(99_950, 50), "99.95%", "cache hit rate preserves two decimal places");
eq(formatCacheHitRate(0, 10_000), "0.00%", "cache hit rate shows zero when usage data exists");
eq(formatCacheHitRate(0, 0), "-", "cache hit rate stays empty before usage data exists");

console.log("\nlocalized money");

const usdLocalized = formatMoneyLocalized(0.1759, "USD", { locale: "en" });
ok(/\$|USD|US\$/.test(usdLocalized) && usdLocalized.includes("0.1759"), "ISO USD cost renders with locale-aware currency formatting");
const cnyLocalized = formatMoneyLocalized(12.3, "CNY", { locale: "zh" });
ok(/¥|CNY|CN¥/.test(cnyLocalized) && cnyLocalized.includes("12.30"), "ISO CNY cost renders with locale-aware currency formatting");
eq(formatMoneyLocalized(0.1759, "A$", { locale: "en" }), "A$0.1759", "symbol currency remains symbol-based");
eq(formatMoneyLocalized(0, "USD", { locale: "en", empty: "dash" }), "-", "localized money preserves dash empty state");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
