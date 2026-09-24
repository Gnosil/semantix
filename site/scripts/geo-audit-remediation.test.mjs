import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const readExport = (path) => readFile(new URL(`../out/${path}`, import.meta.url), "utf8");

const homepageHtml = await readExport("index.html");
const aboutHtml = await readExport("about/index.html");
const contactHtml = await readExport("contact/index.html");
const termsHtml = await readExport("terms/index.html");
const privacyHtml = await readExport("privacy/index.html");
const sitemapXml = await readExport("sitemap.xml");

test("visible contact links use a stable first-party page", () => {
  for (const [name, html] of [
    ["home", homepageHtml],
    ["about", aboutHtml],
    ["terms", termsHtml],
    ["privacy", privacyHtml],
  ]) {
    assert.match(html, /href="\/contact\/?"/, `${name} should link to /contact`);
    assert.doesNotMatch(html, /href="mailto:/, `${name} should not expose a rewritable mailto link`);
    assert.doesNotMatch(html, /cdn-cgi\/l\/email-protection/, `${name} should not ship a Cloudflare link`);
  }
});

test("contact export keeps the official email crawler-readable", () => {
  assert.match(
    contactHtml,
    /aria-label="junhaihuang@aiqueshi\.com"/,
    "contact email should remain available to assistive technology",
  );
  assert.doesNotMatch(contactHtml, />junhaihuang@aiqueshi\.com</, "email should be split to prevent edge rewriting");
  assert.doesNotMatch(contactHtml, /href="mailto:/, "contact page should not expose a rewritable mailto link");
  assert.match(sitemapXml, /<loc>https:\/\/semantix\.ensureok\.ai\/contact<\/loc>/);

  const jsonLdObjects = [...homepageHtml.matchAll(
    /<script type="application\/ld\+json">([\s\S]*?)<\/script>/g,
  )].map((match) => JSON.parse(match[1]));
  const organization = jsonLdObjects.find((value) => value["@type"] === "Organization");
  assert.equal(organization.email, "junhaihuang@aiqueshi.com");
  assert.equal(organization.contactPoint.url, "https://semantix.ensureok.ai/contact");
});

test("technical content exposes authorship, evidence, and limitations", async () => {
  const docsHtml = await readExport("docs/guide/index.html");
  const blogHtml = await readExport("blog/open-source-semantic-memory-comparison-guide/index.html");
  const auditedBlogHtml = await readExport("blog/turning-tool-calls-searchable-semantic-slices/index.html");
  const visibleDocsHtml = docsHtml.replaceAll("<!-- -->", "");
  const visibleBlogHtml = blogHtml.replaceAll("<!-- -->", "");

  assert.match(visibleDocsHtml, /作者 · (Gnosil|radianceded|jh10724-dotcom|Allenllii)/);
  assert.match(visibleDocsHtml, /证据与限制/);
  assert.match(visibleBlogHtml, /Maintainer attribution · (Gnosil|radianceded|jh10724-dotcom|Allenllii)/);
  assert.match(visibleBlogHtml, /Evidence and limitations/);
  assert.match(visibleBlogHtml, /View source and revision history/);
  assert.match(auditedBlogHtml, /View evidence run/);
});

test("benchmark and evidence pages expose a reproducible boundary", async () => {
  const benchmarkHtml = await readExport("benchmarks/index.html");
  const methodologyHtml = await readExport("evidence/methodology/index.html");
  assert.match(benchmarkHtml, /go test -count=1 \.\/\.\.\./);
  assert.match(benchmarkHtml, /Windows\/amd64/);
  assert.match(benchmarkHtml, /Download JSON/);
  assert.match(benchmarkHtml, /does not establish real-session relevance/);
  assert.match(benchmarkHtml, /Reproducible retrieval artifact/);
  assert.match(benchmarkHtml, /15 rows/);
  assert.match(benchmarkHtml, /grey ratio 40\.0%/);
  assert.match(benchmarkHtml, /Run record and page review by/);
  assert.match(benchmarkHtml, /Why this run is public/);
  assert.match(benchmarkHtml, /Next evaluation/);
  assert.match(methodologyHtml, /E0/);
  assert.match(methodologyHtml, /E1/);
  assert.match(methodologyHtml, /E2/);
  assert.match(methodologyHtml, /E3/);
  assert.match(methodologyHtml, /Publishing rules/);
});

test("docs and FAQ point readers to evidence instead of leaving claims unbounded", async () => {
  const docsHtml = await readExport("docs/index.html");
  const faqHtml = await readExport("docs/faq/index.html");
  const aboutHtml = await readExport("about/index.html");
  assert.match(docsHtml, /验证与来源/);
  assert.match(faqHtml, /FAQ 的回答以当前仓库代码/);
  assert.match(aboutHtml, /AboutPage/);
});

test("author profiles and evidence data are discoverable", async () => {
  const authorSlugs = ["gnosil", "radianceded", "jh10724-dotcom", "allenli1233"];
  const authorPages = await Promise.all(
    authorSlugs.map((slug) => readExport(`authors/${slug}/index.html`)),
  );
  const sitemap = await readExport("sitemap.xml");
  const evidence = await readExport("evidence/semantix-2026-08-12-windows.json");
  for (const authorHtml of authorPages) {
    assert.match(authorHtml, /ProfilePage/);
    assert.match(authorHtml, /Semantix contribution history/);
    assert.match(authorHtml, /Verified repository work/);
    assert.match(authorHtml, /How to verify this profile/);
    assert.match(authorHtml, /Inspect commit/);
  }
  assert.match(sitemap, /<loc>https:\/\/semantix\.ensureok\.ai\/benchmarks<\/loc>/);
  assert.match(sitemap, /<loc>https:\/\/semantix\.ensureok\.ai\/evidence\/semantix-2026-08-12-windows\.json<\/loc>/);
  for (const slug of authorSlugs) {
    assert.match(sitemap, new RegExp(`<loc>https://semantix\\.ensureok\\.ai/authors/${slug}</loc>`));
  }
  assert.match(evidence, /"productionBenchmark": false/);
});

test("maintainer identity graph uses verifiable Person profiles", () => {
  const jsonLdObjects = [...homepageHtml.matchAll(
    /<script type="application\/ld\+json">([\s\S]*?)<\/script>/g,
  )].map((match) => JSON.parse(match[1]));
  const maintainerGraph = jsonLdObjects.find((value) => Array.isArray(value["@graph"]));

  assert.equal(maintainerGraph["@graph"].length, 4);
  for (const person of maintainerGraph["@graph"]) {
    assert.equal(person["@type"], "Person");
    assert.deepEqual(person.sameAs, [person.url]);
    assert.match(person.url, /^https:\/\/github\.com\//);
  }
});

test("first-party navigation does not present GitHub as the canonical docs host", () => {
  assert.doesNotMatch(homepageHtml, />GitHub Docs</);
  assert.match(homepageHtml, /href="\/docs\/?"/);
  assert.match(homepageHtml, /href="\/blog\/?"/);
});
