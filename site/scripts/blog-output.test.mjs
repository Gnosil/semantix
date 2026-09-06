import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

const outRoot = path.resolve(import.meta.dirname, "../out");

test("static export contains the Blog index and twenty article pages", async () => {
  const indexHtml = await readFile(path.join(outRoot, "blog", "index.html"), "utf8");
  assert.match(indexHtml, /SEMANTIX BLOG/);
  assert.match(indexHtml, /全部文章/);
  assert.doesNotMatch(indexHtml, /Search articles/i);

  const entries = await readdir(path.join(outRoot, "blog"), { withFileTypes: true });
  const articleDirectories = entries.filter(
    (entry) => entry.isDirectory() && !entry.name.startsWith("__next"),
  );
  assert.equal(articleDirectories.length, 20);

  for (const entry of articleDirectories) {
    const html = await readFile(path.join(outRoot, "blog", entry.name, "index.html"), "utf8");
    assert.match(html, /返回 Blog/i, `${entry.name} should render the original back link`);
    assert.match(html, /Updated/i, `${entry.name} should render update metadata`);
    assert.doesNotMatch(html, /On this page/i, `${entry.name} should not render the documentation TOC`);
  }
});
