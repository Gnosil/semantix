import { useCallback, useRef, useState } from "react";
import { KeyRound, LockKeyhole } from "./SemantixIcons";
import { ArrowRight } from "./SemantixIcons";
import { SiteWordmark } from "./SiteWordmark";
import { useT } from "../lib/i18n";
import { app, openExternal } from "../lib/bridge";

// Full-window first-run guide: DeepSeek stays the fastest path, while users can
// open the provider settings or defer setup without being trapped in the gate.
export function OnboardingOverlay({
  onComplete,
  onChooseProvider,
  onSkip,
}: {
  onComplete: () => void;
  onChooseProvider: () => void;
  onSkip: () => void;
}) {
  const t = useT();
  const [value, setValue] = useState("");
  const [state, setState] = useState<"idle" | "validating" | "error">("idle");
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const submit = useCallback(async () => {
    const key = value.trim();
    if (!key) {
      setError(t("onboarding.error.empty"));
      setState("error");
      inputRef.current?.focus();
      return;
    }
    setState("validating");
    setError(null);
    try {
      await app.ConnectKey(key);
      onComplete();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      if (/status\s*401|status\s*403|invalid/i.test(msg)) {
        setError(t("onboarding.error.invalid"));
      } else if (/network|unreachable|timeout|dial/i.test(msg)) {
        setError(t("onboarding.error.network"));
      } else {
        setError(msg || t("onboarding.error.unknown"));
      }
      setState("error");
      inputRef.current?.focus();
      inputRef.current?.select();
    }
  }, [t, value, onComplete]);

  return (
    <div className="onboarding onboarding--semantix" role="dialog" aria-modal="true" aria-labelledby="onboarding-title" onKeyDown={(event) => {
      if (event.key !== "Tab") return;
      const controls = event.currentTarget.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled)');
      const first = controls[0];
      const last = controls[controls.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last?.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first?.focus();
      }
    }}>
      <div className="onboarding__card">
        <div className="onboarding__identity">
          <SiteWordmark className="onboarding__wordmark"/>
          <div className="onboarding__intro">
            <p className="onboarding__eyebrow">{t("app.splashSubtitle")}</p>
            <h1 id="onboarding-title" className="onboarding__title">{t("onboarding.title")}</h1>
            <p className="onboarding__tag">{t("onboarding.tagline")}</p>
          </div>
          <div id="onboarding-privacy" className="onboarding__privacy">
            <LockKeyhole size={15} strokeWidth={1.6} aria-hidden="true" />
            <span>{t("onboarding.privacy")}</span>
          </div>
        </div>

        <form className="onboarding__setup" aria-busy={state === "validating"} onSubmit={(event) => {
          event.preventDefault();
          if (state !== "validating") void submit();
        }}>
          <div className="onboarding__setup-heading">
            <KeyRound size={18} strokeWidth={1.6} aria-hidden="true" />
            <h2>{t("onboarding.configureProvider")}</h2>
          </div>
          <label className="onboarding__label" htmlFor="onboarding-key">
            {t("onboarding.inputLabel")}
          </label>
          <input
            id="onboarding-key"
            ref={inputRef}
            className="onboarding__input"
            type="password"
            autoComplete="off"
            spellCheck={false}
            autoFocus
            aria-invalid={state === "error"}
            aria-describedby={state === "error" ? "onboarding-error onboarding-privacy" : "onboarding-privacy"}
            placeholder={t("onboarding.inputPlaceholder")}
            value={value}
            onChange={(e) => {
              setValue(e.target.value);
              if (state === "error") setState("idle");
            }}
            disabled={state === "validating"}
          />

          {state === "error" && error && (
            <div id="onboarding-error" className="onboarding__error" role="alert">
              {error}
            </div>
          )}

          <button
            type="submit"
            className="onboarding__submit"
            disabled={state === "validating"}
          >
            {state === "validating" ? (
              <>
                <span className="onboarding__spinner" />
                {t("onboarding.validating")}
              </>
            ) : (
              <>{t("onboarding.submit")}<ArrowRight size={16} aria-hidden="true" /></>
            )}
          </button>

          <button
            type="button"
            className="onboarding__provider"
            onClick={onChooseProvider}
            disabled={state === "validating"}
          >
            {t("onboarding.chooseProvider")}
          </button>

          <div className="onboarding__links">
            <button
              type="button"
              className="onboarding__link"
              onClick={() => openExternal("https://platform.deepseek.com/api_keys")}
            >
              {t("onboarding.getKey")}
            </button>
          </div>

          <button
            type="button"
            className="onboarding__skip"
            onClick={onSkip}
            disabled={state === "validating"}
          >
            {t("onboarding.skip")}
          </button>
        </form>
      </div>
    </div>
  );
}
