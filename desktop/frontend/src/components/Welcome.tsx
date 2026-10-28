import { SiteWordmark } from "./SiteWordmark";
import { WelcomeSuggestions } from "./WelcomeSuggestions";

export function Welcome({ onPrompt, variant = "default" }: { onPrompt: (text: string) => void; variant?: "default" | "creation" }) {
 void onPrompt;
 if (variant === "creation") return null;
 return <div className="welcome welcome--minimal">
  <h2 className="welcome__title"><SiteWordmark className="welcome__brand" /></h2>
  <WelcomeSuggestions/>
 </div>;
}
