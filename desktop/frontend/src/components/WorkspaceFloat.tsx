import { useCallback, useEffect, useRef, useState, type CSSProperties, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import { FileText, GitBranch, X } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { createRafResizeUpdater } from "../lib/resizeDrag";
import { useWorkspaceRefresh } from "../lib/workspaceRefreshStore";
import {
  WORKSPACE_FLOAT_DEFAULT_WIDTH,
  WORKSPACE_FLOAT_MAX_WIDTH,
  WORKSPACE_FLOAT_MIN_WIDTH,
  clampWorkspaceFloatWidth,
  type WorkspaceFloatMode,
} from "../store/layout";
import { Tooltip } from "./Tooltip";

// The floating workspace replaces the old right dock: two capsules float over
// the chat pane's top-right corner and toggle a slide-over panel that hosts the
// file tree or the change list. The chat column never reflows — the panel is
// absolutely positioned inside the chat pane. While it is open the capsules
// hide and the panel header carries the view tabs, so nothing competes with
// top-of-pane banners.

const KEYBOARD_RESIZE_STEP = 16;

/** Count of changed files for the 改动 capsule badge; refreshes on tab / workspace revision changes. */
export function useChangedFileCount(tabId: string | undefined, scopeKey: string): number {
  const refresh = useWorkspaceRefresh(tabId ?? "", scopeKey, Boolean(tabId));
  const { workingTree, session, gitMeta } = refresh.revisions;
  const [count, setCount] = useState(0);
  const requestRef = useRef(0);
  useEffect(() => {
    const requestId = ++requestRef.current;
    if (!tabId) {
      setCount(0);
      return;
    }
    app.WorkspaceChanges(tabId)
      .then((result) => {
        if (requestRef.current !== requestId) return;
        setCount(Array.isArray(result?.files) ? result.files.length : 0);
      })
      .catch(() => {
        if (requestRef.current === requestId) setCount(0);
      });
  }, [tabId, scopeKey, workingTree, session, gitMeta]);
  return count;
}

export function formatChangedBadge(count: number): string {
  return count > 99 ? "99+" : String(count);
}

interface WorkspaceCapsulesProps {
  changedCount: number;
  onToggle: (mode: WorkspaceFloatMode) => void;
}

export function WorkspaceCapsules({ changedCount, onToggle }: WorkspaceCapsulesProps) {
  const t = useT();
  const changedLabel = changedCount > 0 ? t("workspaceFloat.changedCount", { count: changedCount }) : t("workspace.changedTab");
  return (
    <div className="workspace-capsules" role="group" aria-label={t("workspaceFloat.capsules")}>
      <Tooltip label={t("workspace.filesTab")}>
        <button
          type="button"
          className="workspace-capsule"
          aria-label={t("workspace.filesTab")}
          onClick={() => onToggle("files")}
        >
          <FileText size={14} />
          <span className="workspace-capsule__label">{t("workspace.filesTab")}</span>
        </button>
      </Tooltip>
      <Tooltip label={changedLabel}>
        <button
          type="button"
          className="workspace-capsule"
          aria-label={changedLabel}
          onClick={() => onToggle("changed")}
        >
          <GitBranch size={14} />
          <span className="workspace-capsule__label">{t("workspace.changedTab")}</span>
          {changedCount > 0 && <span className="workspace-capsule__badge" aria-hidden="true">{formatChangedBadge(changedCount)}</span>}
        </button>
      </Tooltip>
    </div>
  );
}

interface WorkspaceFloatProps {
  open: boolean;
  mode: WorkspaceFloatMode;
  onModeChange: (mode: WorkspaceFloatMode) => void;
  /** Persisted width preference; CSS clamps the rendered width to the pane. */
  width: number;
  onWidthChange: (width: number, commit: boolean) => void;
  /** Reports the width the panel actually renders at (pane clamp applied). */
  onRenderWidth: (width: number) => void;
  onClose: () => void;
  children: ReactNode;
}

function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  const tag = target.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT";
}

function isInsideTransientLayer(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  return Boolean(target.closest(".anchored-popover, .floating-menu, .context-menu, [role='menu'], [role='dialog']"));
}

export function WorkspaceFloat({ open, mode, onModeChange, width, onWidthChange, onRenderWidth, onClose, children }: WorkspaceFloatProps) {
  const t = useT();
  const panelRef = useRef<HTMLElement>(null);

  // Escape closes the panel unless the key is being consumed by an input,
  // a popover or a menu inside it.
  useEffect(() => {
    if (!open || typeof document === "undefined") return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || event.defaultPrevented) return;
      if (isEditableTarget(event.target) || isInsideTransientLayer(event.target)) return;
      event.preventDefault();
      onClose();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [onClose, open]);

  // The pane may be narrower than the preferred width; report what actually
  // rendered so the hosted panel lays out its tree/preview split correctly.
  useEffect(() => {
    if (!open) return;
    const panel = panelRef.current;
    if (!panel) return;
    const report = () => onRenderWidth(Math.round(panel.getBoundingClientRect().width) || width);
    report();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(report);
    observer.observe(panel);
    return () => observer.disconnect();
  }, [onRenderWidth, open, width]);

  const startResize = useCallback(
    (event: ReactPointerEvent<HTMLDivElement>) => {
      const panel = panelRef.current;
      if (!panel) return;
      event.preventDefault();
      const startX = event.clientX;
      const startWidth = Math.round(panel.getBoundingClientRect().width) || width;
      let next = startWidth;
      const live = createRafResizeUpdater({
        target: panel,
        separator: event.currentTarget,
        cssVar: "--workspace-float-width",
        onApply: (value) => onWidthChange(value, false),
      });
      const onMove = (moveEvent: PointerEvent) => {
        next = clampWorkspaceFloatWidth(startWidth + (startX - moveEvent.clientX));
        live.schedule(next);
      };
      const onDone = () => {
        live.flush();
        onWidthChange(next, true);
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onDone);
        window.removeEventListener("pointercancel", onDone);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onDone);
      window.addEventListener("pointercancel", onDone);
    },
    [onWidthChange, width],
  );

  const resizeWithKeyboard = useCallback(
    (event: ReactKeyboardEvent<HTMLDivElement>) => {
      let next: number | null = null;
      if (event.key === "ArrowLeft") next = width + KEYBOARD_RESIZE_STEP;
      else if (event.key === "ArrowRight") next = width - KEYBOARD_RESIZE_STEP;
      else if (event.key === "Home") next = WORKSPACE_FLOAT_MIN_WIDTH;
      else if (event.key === "End") next = WORKSPACE_FLOAT_MAX_WIDTH;
      if (next === null) return;
      event.preventDefault();
      onWidthChange(clampWorkspaceFloatWidth(next), true);
    },
    [onWidthChange, width],
  );

  if (!open) return null;

  return (
    <aside
      ref={panelRef}
      className={`workspace-float workspace-float--${mode}`}
      role="complementary"
      aria-label={t("workspaceFloat.panel")}
      style={{ "--workspace-float-width": `${width}px` } as CSSProperties}
    >
      <div
        className="workspace-float__resizer"
        role="separator"
        aria-orientation="vertical"
        aria-label={t("workspaceFloat.resize")}
        aria-valuemin={WORKSPACE_FLOAT_MIN_WIDTH}
        aria-valuemax={WORKSPACE_FLOAT_MAX_WIDTH}
        aria-valuenow={width}
        tabIndex={0}
        onPointerDown={startResize}
        onKeyDown={resizeWithKeyboard}
        onDoubleClick={() => onWidthChange(WORKSPACE_FLOAT_DEFAULT_WIDTH, true)}
      />
      <header className="workspace-float__head">
        <div className="workspace-float__tabs" role="tablist" aria-label={t("workspaceFloat.capsules")}>
          <button type="button" role="tab" aria-selected={mode === "files"} className={`workspace-float__tab${mode === "files" ? " workspace-float__tab--active" : ""}`} onClick={() => onModeChange("files")}>
            <FileText size={13} />
            {t("workspace.filesTab")}
          </button>
          <button type="button" role="tab" aria-selected={mode === "changed"} className={`workspace-float__tab${mode === "changed" ? " workspace-float__tab--active" : ""}`} onClick={() => onModeChange("changed")}>
            <GitBranch size={13} />
            {t("workspace.changedTab")}
          </button>
        </div>
        <Tooltip label={t("workspaceFloat.close")}>
          <button type="button" className="workspace-float__close" aria-label={t("workspaceFloat.close")} onClick={onClose}>
            <X size={14} />
          </button>
        </Tooltip>
      </header>
      <div className="workspace-float__body">{children}</div>
    </aside>
  );
}
