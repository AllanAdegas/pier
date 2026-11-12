// Interface language. The Portuguese text is the source: it is written in
// the components and doubles as the key of the English catalog, so a screen
// reads the same in code and on screen. The language is chosen once per page
// load; changing it reloads the page so that every text is rendered again.

import { english } from "./en";

export type Language = "pt-BR" | "en";

export const LANGUAGES: { id: Language; label: string }[] = [
  { id: "pt-BR", label: "Português" },
  { id: "en", label: "English" },
];

const STORAGE_KEY = "pier.language";

/** Picks the saved language, else the first browser language Pier speaks. */
export function detectLanguage(saved: string | null, preferred: readonly string[]): Language {
  if (saved === "pt-BR" || saved === "en") {
    return saved;
  }
  for (const tag of preferred) {
    const base = tag.toLowerCase().split("-")[0];
    if (base === "pt") return "pt-BR";
    if (base === "en") return "en";
  }
  return "en";
}

function savedLanguage(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY);
  } catch {
    return null;
  }
}

const browserLanguages = typeof navigator === "undefined" ? [] : navigator.languages ?? [navigator.language];

/** The language of this page load. */
export const language: Language = detectLanguage(savedLanguage(), browserLanguages);

/** BCP 47 tag for Intl formatters. */
export const locale = language === "en" ? "en-US" : "pt-BR";

/** Saves the choice and reloads, so the whole interface is rendered in it. */
export function setLanguage(next: Language): void {
  if (next === language) return;
  try {
    localStorage.setItem(STORAGE_KEY, next);
  } catch {
    // Without storage the choice lasts until the next reload only.
  }
  location.reload();
}

export type Params = Record<string, string | number>;

/** Replaces each {name} with its parameter; ${NAME}, quoted from compose files, stays as is. */
export function interpolate(text: string, params?: Params): string {
  if (!params) return text;
  return text.replace(/(?<!\$)\{(\w+)\}/g, (whole, name: string) => (name in params ? String(params[name]) : whole));
}

/** Translates a Portuguese source text into the current language. */
export function t(source: string, params?: Params): string {
  return translate(language, source, params);
}

/** Translation for an explicit language; `t` uses the page's. */
export function translate(target: Language, source: string, params?: Params): string {
  const text = target === "en" ? english[source] ?? source : source;
  return interpolate(text, params);
}

/**
 * Marks a text for translation without translating it yet, for tables built
 * at module load. The caller passes the value through `t` when showing it.
 */
export function msg(source: string): string {
  return source;
}
