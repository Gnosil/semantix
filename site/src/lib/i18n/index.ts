import english from "./en.json";

export type Locale = "zh-CN" | "en";
export type LocaleProps = { locale?: Locale };
export type Message = keyof typeof english;

// Source-language keys preserve the existing Chinese copy. Missing English
// translations are caught by TypeScript rather than silently falling back.
export function translate(locale: Locale, message: Message): string {
  return locale === "en" ? english[message] : message;
}

export function homePath(locale: Locale): string {
  return locale === "en" ? "/en/" : "/";
}

// Only the homepage is localized. Docs, blog, and legal pages retain their URLs.
export function homeLink(locale: Locale, href: string): string {
  return href.startsWith("/#") ? homePath(locale) + href.slice(1) : href;
}
