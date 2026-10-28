import { useMemo, useState, type ReactNode } from "react";
import { Search, X } from "./SemantixIcons";
import { SemantixNavigationArt } from "./SemantixNavigationArt";
import { useT, type DictKey } from "../lib/i18n";
import type { SettingsTab } from "../lib/types";

export const SETTINGS_NAV_TABS: SettingsTab[] = [
  "general", "models", "bots", "mcp", "remote", "skills", "subagents", "plugins", "memory",
  "hooks", "diagnostics", "shortcuts", "permissions", "sandbox", "network", "appearance", "storage", "updates",
];

const SETTINGS_TAB_GROUPS: { labelKey: DictKey; tabs: SettingsTab[] }[] = [
  { labelKey: "settings.navGroup.preferences", tabs: ["general", "models", "bots"] },
  { labelKey: "settings.navGroup.connections", tabs: ["mcp", "remote"] },
  { labelKey: "settings.navGroup.capabilities", tabs: ["skills", "subagents", "plugins"] },
  { labelKey: "settings.navGroup.context", tabs: ["memory"] },
  { labelKey: "settings.navGroup.automation", tabs: ["hooks", "diagnostics", "shortcuts"] },
  { labelKey: "settings.navGroup.security", tabs: ["permissions", "sandbox", "network"] },
  { labelKey: "settings.navGroup.application", tabs: ["appearance", "storage", "updates"] },
];

export type SettingsNavigationItem = {
  id: SettingsTab;
  label: string;
  meta: string;
  searchTerms?: string;
};

export function SettingsNavigation({
  items,
  activeTab,
  onSelect,
}: {
  items: SettingsNavigationItem[];
  activeTab: SettingsTab;
  onSelect: (tab: SettingsTab) => void;
}) {
  const t = useT();
  const [query, setQuery] = useState("");
  const itemById = useMemo(() => new Map(items.map((item) => [item.id, item])), [items]);
  const filteredGroups = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase();
    if (!normalized) return SETTINGS_TAB_GROUPS;
    return SETTINGS_TAB_GROUPS.map((group) => {
      const groupMatches = t(group.labelKey).toLocaleLowerCase().includes(normalized);
      const tabs = group.tabs.filter((id) => {
        if (groupMatches) return true;
        const item = itemById.get(id);
        return item ? `${item.label} ${item.meta} ${item.searchTerms ?? ""}`.toLocaleLowerCase().includes(normalized) : false;
      });
      return { ...group, tabs };
    }).filter((group) => group.tabs.length > 0);
  }, [itemById, query, t]);

  return (
    <nav className="settings-center__nav" aria-label={t("settings.title")}>
      <label className="settings-center__search">
        <Search size={15} aria-hidden="true" />
        <input
          value={query}
          onChange={(event) => setQuery(event.currentTarget.value)}
          placeholder={t("settings.searchPlaceholder")}
          aria-label={t("settings.searchPlaceholder")}
        />
        {query && (
          <button type="button" onClick={() => setQuery("")} aria-label={t("settings.searchClear")}>
            <X size={14} aria-hidden="true" />
          </button>
        )}
      </label>
      <div className="settings-center__navgroups">
        {filteredGroups.map((group) => (
          <section className="settings-center__navgroup" key={group.labelKey}>
            <div className="settings-center__navgroup-label">{t(group.labelKey)}</div>
            <div className="settings-center__navitems">
              {group.tabs.map((id) => {
                const item = itemById.get(id);
                if (!item) return null;
                return (
                  <button
                    key={id}
                    className={`settings-center__navitem${activeTab === id ? " settings-center__navitem--active" : ""}`}
                    aria-current={activeTab === id ? "page" : undefined}
                    onClick={() => onSelect(id)}
                  >
                    <span className="settings-center__navitem-main">
                      {settingsTabIcon(id)}
                      <span>{item.label}</span>
                    </span>
                  </button>
                );
              })}
            </div>
          </section>
        ))}
        {filteredGroups.length === 0 && (
          <div className="settings-center__navempty" role="status">{t("settings.searchNoResults")}</div>
        )}
      </div>
    </nav>
  );
}

function settingsTabIcon(id: SettingsTab): ReactNode {
  return <SemantixNavigationArt name={id} size={20} />;
}
