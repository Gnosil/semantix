import { readFile } from "node:fs/promises";

// Loader hook for tsx-based tests: static asset imports (SVG logos, etc.)
// resolve to an empty string default export so component tests can import
// components whose transitive store/component chain pulls in an asset.
export async function load(url, context, nextLoad) {
  if (url.endsWith(".svg") || url.endsWith(".png") || url.endsWith(".webp")) {
    return { format: "module", source: "export default \"\";", shortCircuit: true };
  }
  // Vite resolves the navigation asset glob in production. Node component
  // tests use the same component and its vector fallback, without Vite.
  if (url.endsWith("/components/SemantixNavigationArt.tsx")) {
    const source = (await readFile(new URL(url), "utf8")).replace(
      /import\.meta\.glob<string>\([\s\S]*?\);/,
      "{} as Record<string, string>;",
    );
    return { format: "module", source, shortCircuit: true };
  }
  return nextLoad(url, context);
}
