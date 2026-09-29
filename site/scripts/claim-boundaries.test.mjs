import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

const srcRoot = path.resolve(import.meta.dirname, "../src");

test("homepage copy does not promise unverified universal gains", async () => {
  const files = [
    "components/BrandIntroOverlay.tsx",
    "components/Features.tsx",
    "lib/faq-items.ts",
    "components/SiteDocument.tsx",
    "lib/site-identity.ts",
  ];
  const source = (await Promise.all(files.map((file) => readFile(path.join(srcRoot, file), "utf8")))).join("\n");

  for (const claim of ["每次交互都让下一次更便宜、更快", "每次交互都会积累经验，让下一次响应更快、成本更低", "system自己长出自己的最优参数"]) {
    assert.ok(!source.includes(claim), `unqualified claim should not return: ${claim}`);
  }
  assert.match(source, /生产.*仍待验证|生产.*单独测量/);
  assert.match(source, /接口或实验阶段/);
});
