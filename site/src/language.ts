/**
 * The two languages of the site. Portuguese is the source language and keeps
 * the original URLs; English is a mirror below /en/.
 */
export type Language = "pt" | "en";

export const languages: readonly Language[] = ["pt", "en"];

/** The fallback language announced as hreflang="x-default". */
export const defaultLanguage: Language = "en";

/** A text written once per language. */
export type Localized = Record<Language, string>;

const htmlLanguages: Localized = { pt: "pt-BR", en: "en" };
const openGraphLocales: Localized = { pt: "pt_BR", en: "en_US" };

/** The value for <html lang> and hreflang. */
export function htmlLanguage(language: Language): string {
  return htmlLanguages[language];
}

/** The value for og:locale. */
export function openGraphLocale(language: Language): string {
  return openGraphLocales[language];
}

export function otherLanguage(language: Language): Language {
  return language === "pt" ? "en" : "pt";
}

/** "/" for Portuguese, "/en/" for English. */
export function homePath(language: Language): string {
  return language === "pt" ? "/" : "/en/";
}
