import assert from "node:assert/strict";
import { readdir, readFile, stat } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

const blogRoot = path.resolve(import.meta.dirname, "../../blog");
const oldBlogRoot = path.resolve(import.meta.dirname, "../../docs/blog");
const allowedGroups = new Set([
  "Evaluation Guides",
  "Semantic Slices",
  "Semantic Cache",
  "Scheduling & Harness",
  "Go & Framework Independence",
]);
const expectedEditorialAngles = [
  "Worksheet",
  "Skeptic",
  "Lab Notes",
  "Architecture Review",
  "Workshop",
  "Field Note",
  "Design Memo",
  "Postmortem",
  "Whiteboard",
  "Runbook",
  "Cost Notebook",
  "Debug Diary",
  "Threat Model",
  "Decision Record",
  "Control-Loop Walkthrough",
  "Builder’s Guide",
  "Integration Recipe",
  "Code Review",
];

function parseFrontmatter(source) {
  const match = source.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n/);
  assert.ok(match, "expected YAML-style frontmatter");

  return Object.fromEntries(
    match[1].split(/\r?\n/).map((line) => {
      const separator = line.indexOf(":");
      assert.notEqual(separator, -1, `invalid frontmatter line: ${line}`);
      return [
        line.slice(0, separator).trim(),
        line.slice(separator + 1).trim().replace(/^['\"]|['\"]$/g, ""),
      ];
    }),
  );
}

test("blog corpus contains twenty standalone, valid Markdown articles", async () => {
  const files = (await readdir(blogRoot)).filter((file) => file.endsWith(".md")).sort();
  assert.equal(files.length, 20);

  const slugs = new Set();
  for (const file of files) {
    const source = await readFile(path.join(blogRoot, file), "utf8");
    const meta = parseFrontmatter(source);
    for (const key of ["title", "description", "updated", "group", "order"]) {
      assert.ok(meta[key], `${file} is missing ${key}`);
    }
    assert.ok(allowedGroups.has(meta.group), `${file} has invalid group ${meta.group}`);
    assert.match(meta.updated, /^\d{4}-\d{2}-\d{2}$/);
    assert.equal((source.match(/^# /gm) ?? []).length, 1, `${file} must contain one H1`);
    assert.ok((source.match(/^## /gm) ?? []).length >= 1, `${file} must contain an H2`);
    const slug = file.replace(/\.md$/, "");
    assert.ok(!slugs.has(slug), `duplicate slug ${slug}`);
    slugs.add(slug);
  }

  await assert.rejects(stat(oldBlogRoot), { code: "ENOENT" });
});

test("every article carries reproducible evidence and visible limitations", async () => {
  const files = (await readdir(blogRoot)).filter((file) => file.endsWith(".md"));

  for (const file of files) {
    const source = await readFile(path.join(blogRoot, file), "utf8");
    const prose = source.replace(/^---\r?\n[\s\S]*?\r?\n---\r?\n/, "");
    const wordCount = (prose.match(/\b[A-Za-z][A-Za-z’'-]*\b/g) ?? []).length;

    assert.ok(wordCount >= 275, `${file} should contain at least 275 words of substantive prose`);
    assert.match(source, /^updated: 2026-08-12$/m, `${file} should expose the editorial update date`);
    assert.match(source, /^```bash$/m, `${file} should include a reproducible command block`);
    assert.match(source, /^## Sources and limitations$/m, `${file} should expose sources and limitations`);
    assert.match(source, /https:\/\/github\.com\/Gnosil\/semantix/, `${file} should cite repository evidence`);
  }
});

test("the corpus uses multiple explicit editorial voices", async () => {
  const corpus = await Promise.all(
    (await readdir(blogRoot))
      .filter((file) => file.endsWith(".md"))
      .map((file) => readFile(path.join(blogRoot, file), "utf8")),
  );
  const titles = corpus.map((source) => parseFrontmatter(source).title);
  const representedAngles = expectedEditorialAngles.filter((angle) =>
    titles.some((title) => title.includes(angle)),
  );

  assert.ok(representedAngles.length >= 12, `expected >= 12 editorial angles, got ${representedAngles.length}`);
});
