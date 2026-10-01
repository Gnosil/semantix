// Run: tsx src/__tests__/topicbar-controls.test.ts

import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const testDir = dirname(fileURLToPath(import.meta.url));
const appSource = readFileSync(resolve(testDir, "../App.tsx"), "utf8");

assert.doesNotMatch(appSource, /t\("shortcuts\.cheatsheetTitle"\)|t\("topicBar\.command"\)/);

const taskSummaryControlIndex = appSource.indexOf('<Tooltip label={t("summary.session")}>');
const workspaceCapsulesIndex = appSource.indexOf('<WorkspaceCapsules');
assert.ok(taskSummaryControlIndex >= 0, "topic bar renders the localized Session summary control");
assert.ok(workspaceCapsulesIndex > taskSummaryControlIndex, "workspace capsules float below the topic bar instead of adding a toggle to it");
assert.doesNotMatch(appSource, /rightDock\.(?:expand|collapse)/, "the topic bar no longer carries a dock expand/collapse toggle");
assert.ok(!appSource.includes('aria-label="Session summary"'), "Session summary does not use a hard-coded English label");

process.stdout.write("topicbar controls: 3 contracts passed\n");
