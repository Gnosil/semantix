import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const blogSlugs = [
  "persistent-memory-open-source-evaluation-guide",
  "open-source-semantic-memory-shortlist",
  "cross-session-reuse-guide",
  "open-source-semantic-memory-comparison-guide",
];

const blogIndex = await readFile(new URL("../out/blog/index.html", import.meta.url), "utf8");
const sitemap = await readFile(new URL("../out/sitemap.xml", import.meta.url), "utf8");

test("blog index links every published article", () => {
  for (const slug of blogSlugs) {
    assert.ok(blogIndex.includes(`/blog/${slug}/`), `blog index should link ${slug}`);
  }
});

test("blog articles export with BlogPosting metadata", async () => {
  for (const slug of blogSlugs) {
    const html = await readFile(new URL(`../out/blog/${slug}/index.html`, import.meta.url), "utf8");
    assert.ok(html.includes('"@type":"BlogPosting"'), `${slug} should expose BlogPosting JSON-LD`);
    assert.ok(html.includes('Updated <!-- -->2026-08-12'), `${slug} should expose its update date`);
  }
});

test("sitemap lists the blog index and every article", () => {
  assert.ok(sitemap.includes("<loc>https://semantix.ensureok.ai/blog</loc>"));
  for (const slug of blogSlugs) {
    assert.ok(
      sitemap.includes(`<loc>https://semantix.ensureok.ai/blog/${slug}</loc>`),
      `sitemap should contain ${slug}`,
    );
  }
});
