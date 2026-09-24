// layout owns the desktop shell's geometry state — sidebar width, the sidebar
// collapse flag, the floating workspace panel and the terminal drawer — as a
// selectable store rather than App-local useState. Components read a single
// slice via selector (only that slice re-renders), with no prop drilling. The
// geometry constants, clamps, and the localStorage-backed load/save helpers
// live here too: they are layout-domain knowledge that belongs with the store,
// and keeping them here lets the store initialize itself from persisted state
// at module load without depending on App.
//
// The store's setters are pure (state only); callers invoke the exported save*
// helpers where a preference should survive a restart.

import type { Dispatch, SetStateAction } from "react";
import { create } from "zustand";

import { loadLayoutSize, loadOptionalLayoutSize, saveLayoutSize } from "../lib/layoutPreferences";

import { applySetState } from "./setState";

const SIDEBAR_COLLAPSED_KEY = "semantix.sidebar.collapsed";
const SIDEBAR_DEFAULT_WIDTH = 264;
export const SIDEBAR_MIN_WIDTH = 264;
export const CREATION_SIDEBAR_MIN_WIDTH = 236;
// Creation keeps the expanded rail at the narrow floor by default.
export const CREATION_SIDEBAR_DEFAULT_WIDTH = CREATION_SIDEBAR_MIN_WIDTH;
export const SIDEBAR_MAX_WIDTH = 300;
const SIDEBAR_VIEWPORT_RATIO = 0.18;

// The floating workspace panel (files / changes) slides in over the chat pane
// from the right edge. One width serves both views: it grows on demand when a
// preview or a commit diff needs the dual-pane layout and stays there until
// the user drags it back.
export const WORKSPACE_FLOAT_DEFAULT_WIDTH = 420;
export const WORKSPACE_FLOAT_MIN_WIDTH = 320;
export const WORKSPACE_FLOAT_MAX_WIDTH = 860;
export const WORKSPACE_FLOAT_WIDE_WIDTH = 660;

export function clampSidebarWidth(width: number): number {
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, Math.round(width)));
}

export function clampCreationSidebarWidth(width: number): number {
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(CREATION_SIDEBAR_MIN_WIDTH, Math.round(width)));
}

function clampStoredSidebarWidth(width: number): number {
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(CREATION_SIDEBAR_MIN_WIDTH, Math.round(width)));
}

export function clampWorkspaceFloatWidth(width: number): number {
  return Math.min(WORKSPACE_FLOAT_MAX_WIDTH, Math.max(WORKSPACE_FLOAT_MIN_WIDTH, Math.round(width)));
}

export function defaultSidebarWidth(): number {
  if (typeof window !== "undefined") {
    return clampSidebarWidth(window.innerWidth * SIDEBAR_VIEWPORT_RATIO);
  }
  return SIDEBAR_DEFAULT_WIDTH;
}

export function defaultCreationSidebarWidth(): number {
  return CREATION_SIDEBAR_DEFAULT_WIDTH;
}

function loadSidebarCollapsed(): boolean {
  if (typeof window === "undefined") return false;
  try {
    return window.localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === "1";
  } catch {
    return false;
  }
}

export function saveSidebarCollapsed(collapsed: boolean): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(SIDEBAR_COLLAPSED_KEY, collapsed ? "1" : "0");
  } catch {
    /* ignore storage failures */
  }
}

function loadSidebarWidth(): number {
  return loadLayoutSize("sidebarWidthGraphite", defaultSidebarWidth(), clampStoredSidebarWidth);
}

export function saveSidebarWidth(width: number): void {
  saveLayoutSize("sidebarWidthGraphite", width, clampStoredSidebarWidth);
}

function loadWorkspaceFloatWidth(): number {
  return loadLayoutSize("workspaceFloatWidth", WORKSPACE_FLOAT_DEFAULT_WIDTH, clampWorkspaceFloatWidth);
}

export function saveWorkspaceFloatWidth(width: number): void {
  saveLayoutSize("workspaceFloatWidth", width, clampWorkspaceFloatWidth);
}

// workspaceFloatMode selects which view the floating panel shows; the panel
// itself starts closed on every launch (only its width is a durable
// preference). (Resize drag flags, button-press animation flags, measured
// footer height, and viewport width stay as useState in App.tsx.)
export type WorkspaceFloatMode = "files" | "changed";

// terminalPanelOpen is independent from the workspace panel — the terminal is
// a bottom drawer that coexists with it. Persisted to localStorage so it
// survives restart.
const TERMINAL_PANEL_OPEN_KEY = "semantix.terminalPanel.open";
const TERMINAL_PANEL_DEFAULT_OPEN = false;

function loadTerminalPanelOpen(): boolean {
  if (typeof window === "undefined") return TERMINAL_PANEL_DEFAULT_OPEN;
  try {
    return window.localStorage.getItem(TERMINAL_PANEL_OPEN_KEY) === "1";
  } catch {
    return TERMINAL_PANEL_DEFAULT_OPEN;
  }
}

export function saveTerminalPanelOpen(open: boolean): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(TERMINAL_PANEL_OPEN_KEY, open ? "1" : "0");
  } catch {
    /* ignore storage failures */
  }
}

// Terminal height defaults and clamps for the bottom drawer.
export const TERMINAL_DEFAULT_HEIGHT = 280;
export const TERMINAL_MIN_HEIGHT = 120;
export const TERMINAL_MAX_HEIGHT_RATIO = 0.5; // max 50% of viewport height

const TERMINAL_HEIGHT_KEY = "semantix.terminalPanel.height";

function loadTerminalHeight(): number {
  if (typeof window === "undefined") return TERMINAL_DEFAULT_HEIGHT;
  try {
    const raw = window.localStorage.getItem(TERMINAL_HEIGHT_KEY);
    if (raw === null) return TERMINAL_DEFAULT_HEIGHT;
    const parsed = Number(raw);
    if (Number.isFinite(parsed) && parsed >= TERMINAL_MIN_HEIGHT) return parsed;
    return TERMINAL_DEFAULT_HEIGHT;
  } catch {
    return TERMINAL_DEFAULT_HEIGHT;
  }
}

export function saveTerminalHeight(height: number): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(TERMINAL_HEIGHT_KEY, String(Math.round(height)));
  } catch {
    /* ignore storage failures */
  }
}

export function terminalMaxHeight(viewportHeight: number): number {
  return Math.max(TERMINAL_MIN_HEIGHT, Math.floor(Math.max(0, viewportHeight) * TERMINAL_MAX_HEIGHT_RATIO));
}

export function clampTerminalHeight(height: number, viewportHeight: number): number {
  const max = terminalMaxHeight(viewportHeight);
  return Math.min(max, Math.max(TERMINAL_MIN_HEIGHT, Math.round(height)));
}

export type LayoutState = {
  sidebarCollapsed: boolean;
  sidebarWidth: number;
  workspaceFloatOpen: boolean;
  workspaceFloatMode: WorkspaceFloatMode;
  workspaceFloatWidth: number;
  terminalPanelOpen: boolean;
  terminalHeight: number;
  setSidebarCollapsed: (collapsed: boolean) => void;
  setSidebarWidth: (width: number) => void;
  setWorkspaceFloatOpen: Dispatch<SetStateAction<boolean>>;
  setWorkspaceFloatMode: Dispatch<SetStateAction<WorkspaceFloatMode>>;
  setWorkspaceFloatWidth: (width: number) => void;
  setTerminalPanelOpen: Dispatch<SetStateAction<boolean>>;
  setTerminalHeight: (height: number) => void;
};

export const useLayoutStore = create<LayoutState>((set) => ({
  sidebarCollapsed: loadSidebarCollapsed(),
  sidebarWidth: loadSidebarWidth(),
  workspaceFloatOpen: false,
  workspaceFloatMode: "files",
  workspaceFloatWidth: loadWorkspaceFloatWidth(),
  terminalPanelOpen: loadTerminalPanelOpen(),
  terminalHeight: loadTerminalHeight(),
  setSidebarCollapsed: (collapsed) => set({ sidebarCollapsed: collapsed }),
  setSidebarWidth: (width) => set({ sidebarWidth: width }),
  setWorkspaceFloatOpen: (update) => set((s) => ({ workspaceFloatOpen: applySetState(s.workspaceFloatOpen, update) })),
  setWorkspaceFloatMode: (update) => set((s) => ({ workspaceFloatMode: applySetState(s.workspaceFloatMode, update) })),
  setWorkspaceFloatWidth: (width) => set({ workspaceFloatWidth: width }),
  setTerminalPanelOpen: (update) => set((s) => ({ terminalPanelOpen: applySetState(s.terminalPanelOpen, update) })),
  setTerminalHeight: (height) => set({ terminalHeight: height }),
}));

export function applyLayoutStyleDefaults(style: "classic" | "workbench" | "creation"): void {
  const state = useLayoutStore.getState();
  if (loadOptionalLayoutSize("sidebarWidthGraphite", clampStoredSidebarWidth) === null) {
    state.setSidebarWidth(style === "creation" ? defaultCreationSidebarWidth() : defaultSidebarWidth());
  }
}
