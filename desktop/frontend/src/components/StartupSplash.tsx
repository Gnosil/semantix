import { useEffect, useRef, useState } from "react";
import { SiteWordmark } from "./SiteWordmark";
import { WelcomeSuggestions } from "./WelcomeSuggestions";

const SPLASH_FLAG = "semantix.splash.shown";
const MIN_VISIBLE_MS = 600;
const FADE_OUT_MS = 180;
const MAX_HOLD_MS = 6000;

export function shouldShowStartupSplash(): boolean {
  try {
    return window.sessionStorage.getItem(SPLASH_FLAG) !== "1";
  } catch {
    return true;
  }
}

function markSplashShown(): void {
  try {
    window.sessionStorage.setItem(SPLASH_FLAG, "1");
  } catch {
    /* sessionStorage unavailable */
  }
}

export function StartupSplash({ hold, onDone }: { hold: boolean; onDone: () => void }) {
  const reducedMotion = useRef(typeof window !== "undefined" && window.matchMedia?.("(prefers-reduced-motion: reduce)").matches === true);
  const [minElapsed, setMinElapsed] = useState(reducedMotion.current);
  const [forceRelease, setForceRelease] = useState(false);
  const [leaving, setLeaving] = useState(false);
  const finishedRef = useRef(false);
  const onDoneRef = useRef(onDone);
  onDoneRef.current = onDone;

  const finish = (skipHold = false) => {
    if (finishedRef.current) return;
    if (!skipHold && (!minElapsed || hold) && !forceRelease) return;
    finishedRef.current = true;
    setLeaving(true);
    window.setTimeout(() => {
      markSplashShown();
      onDoneRef.current();
    }, reducedMotion.current ? 0 : FADE_OUT_MS);
  };

  useEffect(() => {
    const minTimer = window.setTimeout(() => setMinElapsed(true), MIN_VISIBLE_MS);
    const maxTimer = window.setTimeout(() => setForceRelease(true), MAX_HOLD_MS);
    return () => {
      window.clearTimeout(minTimer);
      window.clearTimeout(maxTimer);
    };
  }, []);

  useEffect(() => {
    finish();
  }, [minElapsed, hold, forceRelease]);

  useEffect(() => {
    const skip = (event: KeyboardEvent) => {
      if (event.key !== "Escape" && event.key !== "Enter" && event.key !== " ") return;
      finish(true);
    };
    window.addEventListener("keydown", skip);
    return () => window.removeEventListener("keydown", skip);
  }, []);

  return (
    <div className="startup-splash startup-splash--semantix" data-leaving={leaving} onClick={() => finish(true)} role="status" aria-live="polite">
      <div className="startup-splash__card">
        <SiteWordmark className="startup-splash__wordmark"/>
        <WelcomeSuggestions className="startup-splash__sub"/>
        <div className="startup-splash__dots" aria-hidden="true">
          <span />
          <span />
          <span />
        </div>
      </div>
    </div>
  );
}
