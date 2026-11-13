/**
 * The launch page: what Pier is, how to install the stable release and where
 * to go next. Written for someone who has never used it; the copy lives in
 * strings.ts, once per language, with the same claims in both.
 */
import { codeBlock, escapeHtml } from "./markdown.ts";
import { repository } from "./config.ts";
import type { Language } from "./language.ts";
import { docPath, pageById } from "./pages.ts";
import { strings } from "./strings.ts";

/** Anchor of the "what it does not do yet" section of the introduction, per language. */
const notYetAnchor: Record<Language, string> = {
  pt: "o-que-ainda-nao-faz",
  en: "what-it-does-not-do-yet",
};

function href(ptSlug: string, language: Language): string {
  return docPath(pageById(ptSlug), language);
}

function installBlock(tag: string, language: Language): string {
  const command = `git clone --branch ${tag} --depth 1 ${repository}.git
cd pier
./install.sh`;
  return codeBlock(command, strings[language].home.installTerminal, strings[language].code, "install");
}

function featureCards(language: Language): string {
  return strings[language].home.features.map((feature) =>
    `<a class="card" href="${href(feature.page, language)}"><h3>${escapeHtml(feature.title)}</h3><p>${
      escapeHtml(feature.text)
    }</p></a>`
  ).join("\n");
}

function stepList(language: Language): string {
  return strings[language].home.steps.map((step, index) =>
    `<li><span class="step-number">${index + 1}</span><div><h3>${escapeHtml(step.title)}</h3><p>${
      escapeHtml(step.text)
    }</p></div></li>`
  ).join("\n");
}

export function homeBody(version: string, language: Language): string {
  const text = strings[language].home;
  const safeVersion = escapeHtml(version);
  return `<main id="conteudo" class="home">
<section class="hero">
  <div class="hero-text">
    <p class="eyebrow">${escapeHtml(text.eyebrow(version))}</p>
    <h1>${text.headlineHtml}</h1>
    <p class="hero-lead">${escapeHtml(text.lead)}</p>
    <div class="hero-actions">
      <a class="button primary" href="${href("instalacao", language)}">${escapeHtml(text.installNow)}</a>
      <a class="button" href="${href("introducao", language)}">${escapeHtml(text.readDocs)}</a>
    </div>
  </div>
  <div class="hero-install">
    <p class="install-title">${escapeHtml(text.installTitle)}</p>
    ${installBlock(`v${version}`, language)}
    <p class="install-note">${text.installNoteHtml(href("instalacao", language))}</p>
  </div>
</section>

<section class="section">
  <h2>${escapeHtml(text.howItWorks)}</h2>
  <ol class="steps">${stepList(language)}</ol>
</section>

<section class="section">
  <h2>${escapeHtml(text.included)}</h2>
  <div class="cards">${featureCards(language)}</div>
</section>

<section class="section split">
  <div>
    <h2>${escapeHtml(text.automationTitle)}</h2>
    <p>${text.automationTextHtml}</p>
    <p><a href="${href("pierctl", language)}">${escapeHtml(text.automationLink)}</a></p>
  </div>
  ${codeBlock(text.automationExample, "sh", strings[language].code)}
</section>

<section class="section beta">
  <h2>${escapeHtml(text.betaTitle)}</h2>
  <p>${text.betaTextHtml(safeVersion)}</p>
  <p>${text.licenseHtml(href("comunidade", language))}</p>
  <p><a href="${href("introducao", language)}#${notYetAnchor[language]}">${
    escapeHtml(text.notYetLink)
  }</a></p>
</section>
</main>`;
}
