import { useCallback, useEffect, useRef, useState } from "react";
import type { KeyboardEvent } from "react";
import { ChevronDown } from "./SemantixIcons";
import { SemantixPermissionArt } from "./SemantixPermissionArt";
import { AnchoredPopover } from "./AnchoredPopover";
import { useT } from "../lib/i18n";
import type { ToolApprovalMode } from "../lib/types";

const options = [
  { value: "ask", label: "composer.accessAsk", description: "composer.accessAskDesc" },
  { value: "auto", label: "composer.accessAuto", description: "composer.accessAutoDesc" },
  { value: "yolo", label: "composer.accessYolo", description: "composer.accessYoloDesc" },
] as const;

export function ApprovalModePicker({ mode, disabled, shortcut, onPick }: {
  mode: ToolApprovalMode;
  disabled: boolean;
  shortcut: string;
  onPick: (mode: ToolApprovalMode) => void;
}) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const close = useCallback(() => setOpen(false), []);
  const selected = options.find(option => option.value === mode) ?? options[0];
  const menuLabel = t("composer.accessPickerTitle");

  useEffect(() => {
    if (!open) return;
    const frame = requestAnimationFrame(() => {
      menuRef.current?.querySelector<HTMLButtonElement>('[aria-checked="true"]')?.focus();
    });
    return () => cancelAnimationFrame(frame);
  }, [open]);

  const onMenuKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const buttons = Array.from(menuRef.current?.querySelectorAll<HTMLButtonElement>('[role="menuitemradio"]') ?? []);
    const index = buttons.findIndex(button => button === document.activeElement);
    if (["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
      event.preventDefault();
      const next = event.key === "Home" ? 0 : event.key === "End" ? buttons.length - 1
        : (index + (event.key === "ArrowDown" ? 1 : -1) + buttons.length) % buttons.length;
      buttons[next]?.focus();
    } else if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      close();
      triggerRef.current?.focus();
    } else if (event.key === "Tab") {
      close();
      triggerRef.current?.focus();
    }
  };

  return <>
    <button ref={triggerRef} type="button" className="composer-permissions-trigger"
      data-mode={mode} aria-haspopup="menu" aria-expanded={open}
      aria-label={`${menuLabel}: ${t(selected.label)}`} disabled={disabled}
      onClick={() => setOpen(value => !value)}
      onKeyDown={event => {
        if (event.key === "ArrowDown" || event.key === "ArrowUp") {
          event.preventDefault();
          setOpen(true);
        }
      }}>
      <SemantixPermissionArt mode={mode} size={17}/><span>{t(selected.label)}</span><ChevronDown size={10} aria-hidden="true"/>
    </button>
    <AnchoredPopover open={open} anchorRef={triggerRef} onClose={close} className="composer-permissions-menu" align="start">
      <div ref={menuRef} role="menu" aria-label={menuLabel} onKeyDown={onMenuKeyDown}>
        <header><span>{menuLabel}</span><kbd>{shortcut}</kbd></header>
        {options.map(option => <button key={option.value} type="button" role="menuitemradio"
          aria-checked={mode === option.value} disabled={disabled}
          onClick={() => { close(); onPick(option.value); }}>
          <SemantixPermissionArt mode={option.value}/>
          <span className="composer-permissions-copy"><strong>{t(option.label)}</strong><small>{t(option.description)}</small></span>
          <span className="composer-permissions-selection" aria-hidden="true"/>
        </button>)}
        <footer>{t("composer.accessBoundary")}</footer>
      </div>
    </AnchoredPopover>
  </>;
}
