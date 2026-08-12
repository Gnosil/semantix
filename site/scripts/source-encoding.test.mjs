import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

const sourceRoot = path.join(process.cwd(), "src");
const decoder = new TextDecoder("utf-8", { fatal: true });

async function sourceFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const files = await Promise.all(
    entries.map((entry) => {
      const target = path.join(directory, entry.name);
      return entry.isDirectory() ? sourceFiles(target) : [target];
    }),
  );
  return files.flat();
}

test("website source files are valid UTF-8", async () => {
  const invalid = [];

  for (const file of await sourceFiles(sourceRoot)) {
    try {
      decoder.decode(await readFile(file));
    } catch {
      invalid.push(path.relative(process.cwd(), file));
    }
  }

  assert.deepEqual(invalid, [], `Invalid UTF-8 source files: ${invalid.join(", ")}`);
});
