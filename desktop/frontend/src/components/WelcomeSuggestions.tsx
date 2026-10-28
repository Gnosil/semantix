import { useMemo } from "react";
import { useT } from "../lib/i18n";
import { HyperTextLine } from "./HyperTextLine";

const keys = ["build", "debug", "explore", "plan", "remember", "review"] as const;
export function WelcomeSuggestions({ className = "welcome__tag" }: { className?: string }) {
  const t = useT();
  const messages = useMemo(() => keys.map(key => t(`welcome.prompt.${key}`)), [t]);
  return <HyperTextLine messages={messages} className={className}/>;
}
