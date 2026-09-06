import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const layout = await readFile(new URL("../src/app/layout.tsx", import.meta.url), "utf8");
const globals = await readFile(new URL("../src/app/globals.css", import.meta.url), "utf8");
const packageJson = JSON.parse(
  await readFile(new URL("../package.json", import.meta.url), "utf8"),
);
const licenses = await readFile(new URL("../FONT_LICENSES.md", import.meta.url), "utf8");

const fonts = [
  ["inter", "Inter Variable"],
  ["jetbrains-mono", "JetBrains Mono Variable"],
  ["noto-sans-sc", "Noto Sans SC Variable"],
];

test("site fonts are self-hosted through pinned Fontsource packages", () => {
  assert.doesNotMatch(layout, /next\/font\/google/);

  for (const [packageName, familyName] of fonts) {
    assert.equal(packageJson.dependencies[`@fontsource-variable/${packageName}`], "5.3.0");
    // Fontsource CSS is loaded via JS imports in layout.tsx (Tailwind v4's
    // LightningCSS cannot resolve bare package names in CSS @import).
    assert.match(
      layout,
      new RegExp(`import ["']@fontsource-variable/${packageName}/wght\\.css["']`),
    );
    assert.ok(globals.includes(`"${familyName}"`), `${familyName} should be used by the theme`);
    assert.ok(licenses.includes(`@fontsource-variable/${packageName}`));
  }
});

test("font source and license are recorded", () => {
  assert.match(licenses, /Fontsource font-files repository/);
  assert.match(licenses, /SIL Open Font License 1\.1/);
});

