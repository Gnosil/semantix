import assert from "node:assert/strict";
import { readFile, access } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

const output = path.resolve(import.meta.dirname, "../out");
const en = await readFile(path.join(output, "en/index.html"), "utf8");
const zh = await readFile(path.join(output, "index.html"), "utf8");
const base = "https://semantix.ensureok.ai";

function text(html) {
  return html.replace(/<script\b[^>]*>[\s\S]*?<\/script>/g, "")
    .replace(/<style\b[^>]*>[\s\S]*?<\/style>/g, "")
    .replace(/<[^>]+>/g, " ");
}

test("both homepages export their language and reciprocal canonical alternates", () => {
  for (const [html, locale, suffix] of [[en, "en", "/en/"], [zh, "zh-CN", "/"]]) {
    assert.ok(html.includes('<html lang="' + locale + '"'));
    assert.ok(html.includes('rel="canonical" href="' + base + suffix + '"'));
    assert.ok(html.includes('hrefLang="en" href="' + base + '/en/"'));
    assert.ok(html.includes('hrefLang="zh-CN" href="' + base + '/"'));
    assert.ok(html.includes('hrefLang="x-default" href="' + base + '/"'));
    const schema = [...html.matchAll(/<script type="application\/ld\+json">(.*?)<\/script>/g)].map((m) => JSON.parse(m[1]));
    assert.ok(schema.some((item) => item['@type'] === "WebPage" && item.inLanguage === locale && item.url === base + suffix));
  }
});

test("English export translates homepage copy and retains evidence boundaries", () => {
  const visible = text(en).replaceAll("中文", "");
  assert.doesNotMatch(visible, /[\u3400-\u9fff]/);
  assert.match(visible, /Turn every conversation/);
  assert.match(visible, /production remain unverified/);
  assert.match(visible, /interfaces or experimental features/);
  assert.match(text(zh), /把每一次对话/);
  assert.match(text(zh), /跨会话复用/);
});

test("English home anchors stay in English and existing content links resolve", async () => {
  for (const id of ["features", "components", "community", "start"]) {
    assert.ok(en.includes('href="/en/#' + id + '"'));
    assert.ok(en.includes('id="' + id + '"'));
  }
  assert.ok(en.includes('href="/" hrefLang="zh-CN"'));
  assert.ok(zh.includes('href="/en/" hrefLang="en"'));
  const links = [...en.matchAll(/href="(\/[^"#?]*)(?:[?#][^"]*)?"/g)].map((m) => m[1]);
  for (const href of new Set(links)) {
    const file = path.join(output, href, path.extname(href) ? "" : "index.html");
    await assert.doesNotReject(access(file), href + " must resolve in the static export");
  }
});
