import logoWordmark from "../assets/logo-wordmark.svg";
import { useT } from "../lib/i18n";

// Welcome is the empty-state landing: the brand logo and a single
// "Build with Semantix" byline. Headline, input hints, and example prompts
// are intentionally cleared — the composer below is the one starting point.

export function Welcome({ onPrompt, variant = "default" }: { onPrompt: (text: string) => void; variant?: "default" | "creation" }) {
  const t = useT();
  void onPrompt;
  void t;
  if (variant === "creation") {
    // Headline lives above the hero Composer in App footer (same stack).
    return null;
  }

  return (
    <div className="welcome welcome--brand">
      <span className="welcome__brand">
        <img src={logoWordmark} className="welcome__brand-logo" alt="Semantix" draggable={false} />
      </span>
      <div className="welcome__byline">Build with Semantix</div>
    </div>
  );
}
