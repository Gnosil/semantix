import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const home = await readFile(new URL("../out/index.html", import.meta.url), "utf8");

test("every compact code box exposes a copy control", () => {
  const copyButtons = home.match(/aria-label="复制代码"/g) ?? [];

  assert.equal(copyButtons.length, 10);
  assert.ok(
    home.includes(
      "$ curl -fsSL https://raw.githubusercontent.com/Gnosil/semantix/main/agent-skill/scripts/install.sh | sh",
    ),
  );
});
