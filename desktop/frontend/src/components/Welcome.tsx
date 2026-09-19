import logoMark from "../assets/logo-mark.svg";
import { useT } from "../lib/i18n";

// Welcome is the empty-state landing: the Semantix mark, large, with a
// "Build with Semantix" byline set in Playfair Display italic. The composer
// below is the one starting point.

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
        <img src={logoMark} className="welcome__mark" alt="Semantix" draggable={false} />
      </span>
      <div className="welcome__byline">Build with Semantix</div>
    </div>
  );
}
