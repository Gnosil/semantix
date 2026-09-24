// Run: tsx src/__tests__/workspace-layout.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  WORKSPACE_FLOAT_DEFAULT_WIDTH,
  clampTerminalHeight,
  clampWorkspaceFloatWidth,
  terminalMaxHeight,
} from "../store/layout";

let passed = 0;
let failed = 0;
const testDir = dirname(fileURLToPath(import.meta.url));
const appSource = readFileSync(resolve(testDir, "../App.tsx"), "utf8");
const stylesSource = readFileSync(resolve(testDir, "../styles.css"), "utf8");
const terminalPanelSource = readFileSync(resolve(testDir, "../components/TerminalPanel.tsx"), "utf8");
const terminalRailSource = readFileSync(resolve(testDir, "../components/TerminalSessionRail.tsx"), "utf8");

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\nfloating workspace layout");

eq(clampWorkspaceFloatWidth(100), 320, "floating panel width clamps to its minimum");
eq(clampWorkspaceFloatWidth(5000), 860, "floating panel width clamps to its maximum");
eq(WORKSPACE_FLOAT_DEFAULT_WIDTH, 420, "floating panel opens at the compact default width");
eq(
  /const openWorkspaceFloat = useCallback\([\s\S]*?setWorkspaceFloatMode\(mode\);[\s\S]*?setWorkspaceFloatOpen\(true\);/.test(appSource),
  true,
  "opening a capsule selects its view and shows the floating panel",
);
eq(
  /const toggleWorkspaceCapsule = useCallback\([\s\S]*?openWorkspaceFloat\(mode\);/.test(appSource),
  true,
  "capsules open their view (the panel header owns switching once open)",
);
eq(
  /const ensureWorkspaceFloatWidth = useCallback\([\s\S]*?if \(width <= workspaceFloatWidth\) return;/.test(appSource),
  true,
  "panel width requests only ever grow the floating panel",
);
eq(
  /<section className=\{`chat-pane[\s\S]*?!workspaceFloatOpen && \([\s\S]*?<WorkspaceCapsules[\s\S]*?onToggle=\{toggleWorkspaceCapsule\}/.test(appSource),
  true,
  "capsules render inside the chat pane only while the panel is closed",
);
eq(
  /<WorkspaceFloat[\s\S]*?onModeChange=\{setWorkspaceFloatMode\}[\s\S]*?onRenderWidth=\{setWorkspaceFloatRenderWidthPx\}[\s\S]*?<WorkspacePanel[\s\S]*?panelWidth=\{workspaceFloatRenderWidthPx\}[\s\S]*?onRequestPanelWidth=\{ensureWorkspaceFloatWidth\}[\s\S]*?<\/WorkspaceFloat>[\s\S]*?<\/section>/.test(appSource),
  true,
  "the floating panel hosts the view tabs and WorkspacePanel inside the chat pane",
);
eq(
  !/workbench-dock|layout--workspace-open|workspace-panel-resizer|rightDockMode|workspacePanelMaximized/.test(appSource),
  true,
  "no docked workspace column remains in the app shell",
);
eq(
  /\.workspace-float \{[\s\S]*?position: absolute;[\s\S]*?width: min\(var\(--workspace-float-width, 420px\), calc\(100% - 16px\)\);/.test(stylesSource)
    && /\.workspace-float__tab--active \{/.test(stylesSource),
  true,
  "floating panel overlays the pane, clamps to it; its header owns the view tabs",
);
eq(
  /:root\[data-theme-style\] \.chat-pane > \.banner \{\s*padding-right: 200px;/.test(stylesSource),
  true,
  "top-of-pane banners keep their actions clear of the capsule cluster",
);
eq(
  !/\.workbench-dock|--workspace-width:|layout--workspace-open|\.workspace-panel-resizer/.test(stylesSource),
  true,
  "dock grid column, resizer and tab strip styles are gone",
);
eq(terminalMaxHeight(480), 240, "terminal maximum follows half of the current viewport height");
eq(terminalMaxHeight(180), 120, "terminal maximum never falls below the accessible minimum");
eq(clampTerminalHeight(680, 480), 240, "restored terminal height clamps after the window shrinks");
eq(clampTerminalHeight(80, 720), 120, "terminal height clamps to its minimum");
eq(
  /terminalPanelOpen[\s\S]*?terminal-drawer/.test(appSource),
  true,
  "terminal drawer is an independent panel, not a workspace dock mode",
);
eq(
  /const addTerminalOutputToComposer = useCallback\(async \(sessionId: string\) => \{[\s\S]*?app\.TerminalOutputForTab\(activeTabId, sessionId\)[\s\S]*?addWorkspaceTextToComposer\(/.test(appSource),
  true,
  "terminal output reaches chat only through the explicit add-output action",
);
eq(
  /@media \(max-width: 820px\) \{[\s\S]*?\.layout--terminal-drawer-open \.terminal-drawer[\s\S]*?display: flex !important/.test(stylesSource),
  true,
  "terminal drawer stays visible on narrow viewports",
);
eq(
  /\.layout--terminal-drawer-open \{[\s\S]*?grid-template-rows: var\(--app-chrome-height\) minmax\(0, 1fr\) var\(--terminal-height, 280px\) var\(--statusbar-height\)/.test(stylesSource),
  true,
  "terminal-drawer-open layout reserves a grid row for the status bar below the terminal drawer",
);
eq(
  /@media \(max-width: 820px\) \{[\s\S]*?\.layout--terminal-drawer-open \.terminal-drawer-resizer[\s\S]*?grid-column: 1 !important[\s\S]*?\.layout--workbench-chrome-hidden\.layout--terminal-drawer-open \.terminal-drawer[\s\S]*?grid-row: 2;[\s\S]*?\.layout--workbench-chrome-hidden\.layout--terminal-drawer-open[\s\S]*?minmax\(0, 1fr\) var\(--terminal-height, 280px\) var\(--statusbar-height\)/.test(stylesSource),
  true,
  "narrow viewport keeps the resizer and drawer in the content column above the status bar",
);
eq(
  /const terminalRenderHeight = clampTerminalHeight\(terminalHeight, viewportHeight\)/.test(appSource)
    && /"--terminal-height": `\$\{liveTerminalHeight \?\? \(terminalPanelOpen \? terminalRenderHeight : 0\)\}px`/.test(appSource),
  true,
  "terminal render height re-clamps whenever the viewport changes",
);
eq(
  /aria-hidden=\{!terminalPanelOpen\}/.test(appSource)
    && /tabIndex=\{terminalPanelOpen \? 0 : -1\}/.test(appSource)
    && /onKeyDown=\{resizeTerminalWithKeyboard\}/.test(appSource),
  true,
  "closed terminal resizer leaves the tab order and open resizer supports keyboard adjustment",
);
eq(
  /terminalPanelOpen && !sidebarCreation \? "footer--compact" : ""/.test(appSource)
    && !/\.layout\.layout--terminal-drawer-open \.footer/.test(stylesSource),
  true,
  "footer compaction applies only while the terminal is expanded outside Creation mode",
);
eq(
  /sidebarImDetailConnection \? "layout--statusbar-hidden" : ""/.test(appSource)
    && /\.layout\.layout--statusbar-hidden,[\s\S]*?--statusbar-height: 0px;/.test(stylesSource),
  true,
  "IM detail collapses the status bar row when the bar is not rendered",
);
eq(
  /\.layout--terminal-drawer-expanded \.terminal-drawer \{[\s\S]*?border-top: 1px solid var\(--border-soft\)/.test(stylesSource),
  true,
  "terminal drawer has a top border only when expanded, avoiding a collapsed artifact line",
);
eq(
  /\.sidebar--workbench \{[\s\S]*?padding: 16px 16px 10px;/.test(stylesSource)
    && /\.app--darwin \.sidebar--workbench,\s*\.sidebar--workbench \{[\s\S]*?padding: 14px 12px 10px;/.test(stylesSource),
  true,
  "workbench sidebar does not reserve the docked status bar twice",
);
eq(
  /className="topicbar__action-btn topicbar__action-btn--icon topicbar__action-btn--utility"[\s\S]*?onClick=\{toggleTerminalPanel\}/.test(appSource),
  true,
  "the topic bar keeps the terminal drawer action",
);
eq(
  /\.topicbar \{\s*position: relative;\s*z-index: var\(--z-inline-sticky\);/.test(stylesSource)
    && /\.external-opener__menu \{[\s\S]*?z-index: var\(--z-topicbar-menu\);/.test(stylesSource),
  true,
  "topic bar establishes a raised stacking context for external-opener menus",
);
eq(
  /\.composer-meta__control--approval \{[\s\S]*?margin-inline-start: 2px;/.test(stylesSource)
    && /\.composer-modebar__item:hover:not\(:disabled\) \{[\s\S]*?transform: none;/.test(stylesSource)
    && /\.composer-task-mode-trigger:hover:not\(:disabled\),[\s\S]*?\.composer-task-mode-trigger--open \{[\s\S]*?transform: none;/.test(stylesSource)
    && /\.composer-profile-trigger:hover:not\(:disabled\),[\s\S]*?\.composer-profile-trigger--open \{[\s\S]*?transform: none;/.test(stylesSource),
  true,
  "composer mode controls keep spacing and icon baselines stable on hover",
);
eq(
  /\.app--creation \.layout\.layout--creation-chrome-hidden\.layout--terminal-drawer-open \{[\s\S]*?grid-template-rows: minmax\(0, 1fr\) var\(--terminal-height, 280px\)/.test(stylesSource),
  true,
  "creation style keeps the terminal drawer below the chat pane",
);
eq(
  /sessions\.length > 0 && \([\s\S]*?<TerminalSessionRail/.test(terminalPanelSource),
  true,
  "the single terminal session keeps a visible close control",
);
eq(
  /const syncWorkspace = useTerminalStore[\s\S]*?const capabilityChanged = previous\.tabId === tabId && previous\.readOnly !== readOnly[\s\S]*?void syncWorkspace\(tabId, capabilityChanged\)/.test(terminalPanelSource),
  true,
  "terminal panel refreshes changed capability while reusing an in-flight first-open request",
);
eq(
  /readOnly=\{Boolean\(activeTab\?\.readOnly\)\}/.test(appSource)
    && /const terminalReadOnly = readOnly \|\| Boolean\(workspace\?\.readOnly\)/.test(terminalPanelSource),
  true,
  "terminal controls follow the active tab read-only boundary",
);
eq(
  /terminal-session-rail__new|onNew/.test(terminalRailSource),
  false,
  "terminal tab strip does not duplicate the header's new-session action",
);
eq(
  /className="terminal-shell-select"[\s\S]*?shellOptions\.map/.test(terminalPanelSource),
  true,
  "terminal header renders the backend-approved shell options",
);
eq(
  /createSession\(tabId, "\.", selectedShellId\)/.test(terminalPanelSource),
  true,
  "new terminal sessions use the selected shell",
);
eq(
  /createSession\(tabId, "\.", "default"\)/.test(terminalPanelSource),
  false,
  "new terminal sessions are not hard-coded to the default shell",
);
eq(
  /const TerminalPanel = lazy\(\(\) => import\("\.\/components\/TerminalPanel"\)/.test(appSource),
  true,
  "terminal and xterm load only when the terminal drawer opens",
);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
