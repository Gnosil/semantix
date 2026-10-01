import { lazy, Suspense, useEffect, useRef } from "react";
import { ArrowLeft } from "./SemantixIcons";
import { SemantixNavigationArt } from "./SemantixNavigationArt";
import { useT } from "../lib/i18n";
import type { SettingsView } from "../lib/types";
import "./CapabilityWorkspace.css";

const SkillsPage = lazy(() => import("./CapabilitiesPanel").then(m => ({ default: m.SkillsSettingsPage })));
const ToolsPage = lazy(() => import("./CapabilitiesPanel").then(m => ({ default: m.MCPServersSettingsPage })));
const BotsPage = lazy(() => import("./SettingsPanelEntry").then(m => ({ default: m.BotsWorkspacePage })));
export type CapabilityPage = "skills" | "bots" | "mcp";

export function CapabilityWorkspace({ page, activeWorkspaceKey, onBack, onUseSkill, onChanged }: {
  page: CapabilityPage;
  activeWorkspaceKey: string;
  onBack: () => void;
  onUseSkill: (command: string) => void;
  onChanged: (settings: SettingsView | null) => void;
}) {
  const t = useT();
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => { heading.current?.focus({ preventScroll: true }); }, [page]);
  const title = page === "skills" ? t("creation.sidebar.skills") : page === "bots" ? t("settings.tab.bots") : t("settings.tab.mcp");
  return <section className="chat-pane capability-workspace" aria-label={title} onKeyDown={e => {
    if (e.key === "Escape" && e.target === heading.current) { e.preventDefault(); onBack(); }
  }}>
    <header className="capability-workspace__bar">
      <button type="button" onClick={onBack}><ArrowLeft size={14} />{t("capability.backToChat")}</button>
      <span>{t("workspace.title")}</span>
    </header>
    <main className="capability-workspace__content">
      <div className="capability-workspace__intro">
        <SemantixNavigationArt name={page} size={28} />
        <div><h1 ref={heading} tabIndex={-1}>{title}</h1><p>{t(`capability.${page}Description`)}</p></div>
      </div>
      <Suspense fallback={<div className="empty">{t("caps.loading")}</div>}>
        {page === "skills" && <SkillsPage activeWorkspaceKey={activeWorkspaceKey} onUseSkill={onUseSkill} />}
        {page === "mcp" && <ToolsPage />}
        {page === "bots" && <BotsPage onChanged={onChanged} />}
      </Suspense>
    </main>
  </section>;
}
