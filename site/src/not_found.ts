/**
 * The single 404 page. It is served for any missing address in either
 * language, so it offers both: the frame is in the default language and the
 * message is repeated in each language, each block marked with its own lang.
 */
import { escapeHtml } from "./markdown.ts";
import { allPages, docPath } from "./pages.ts";
import { defaultLanguage, homePath, htmlLanguage, type Language, languages } from "./language.ts";
import { strings } from "./strings.ts";

export const notFoundLanguage: Language = defaultLanguage;

/** The default language first, then the others. */
function displayOrder(): Language[] {
  return [notFoundLanguage, ...languages.filter((language) => language !== notFoundLanguage)];
}

function message(language: Language): string {
  const text = strings[language].notFound;
  return `<div lang="${htmlLanguage(language)}"><h1>${escapeHtml(text.title)}</h1>
<p>${text.textHtml(docPath(allPages[0], language), homePath(language))}</p></div>`;
}

export function notFoundTitle(): string {
  return displayOrder().map((language) => strings[language].notFound.title).join(" · ");
}

export function notFoundBody(): string {
  return `<main id="conteudo" class="home"><section class="section narrow not-found">${
    displayOrder().map(message).join("\n")
  }</section></main>`;
}
