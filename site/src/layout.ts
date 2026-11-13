/**
 * The HTML shell shared by every page, in the page's language: header with
 * the language switch, documentation sidebar, "on this page" index, pager
 * and footer.
 */
import { type DocPage, docPath, neighbours, pageById, sections } from "./pages.ts";
import { escapeHtml, type Heading } from "./markdown.ts";
import { cliRepository, repository } from "./config.ts";
import {
  defaultLanguage,
  homePath,
  htmlLanguage,
  type Language,
  languages,
  type Localized,
  openGraphLocale,
  otherLanguage,
} from "./language.ts";
import { strings } from "./strings.ts";

export interface ShellOptions {
  language: Language;
  title: string;
  description: string;
  path: string;
  version: string;
  body: string;
  /** The same page in each language. Absent on the 404 page, which offers both languages itself. */
  alternates?: Localized;
  bodyClass?: string;
}

/** A rendered documentation body. */
export interface DocContent {
  html: string;
  headings: Heading[];
  /** True when an English page shows the Portuguese text (local development only). */
  translationPending: boolean;
}

// Loaded synchronously in <head> so the saved theme applies before paint.
const themeBootstrap = `<script src="/assets/theme.js"></script>`;

/** Plain link to the same page in the other language, written in that language. */
function languageSwitch(language: Language, alternates?: Localized): string {
  if (!alternates) return "";
  const target = otherLanguage(language);
  const text = strings[target].layout;
  const targetTag = htmlLanguage(target);
  return `<a class="language-switch" href="${
    alternates[target]
  }" hreflang="${targetTag}" lang="${targetTag}" ` +
    `aria-label="${escapeHtml(text.readInThisLanguage)}"><span class="language-name">${
      escapeHtml(text.languageName)
    }</span><span class="language-code" aria-hidden="true">${escapeHtml(text.languageCode)}</span></a>`;
}

function header(options: ShellOptions): string {
  const text = strings[options.language].layout;
  const docsHome = docPath(sections[0].pages[0], options.language);
  const docsPrefix = docsHome.slice(0, docsHome.indexOf("/docs/") + "/docs/".length);
  const current = options.path.startsWith(docsPrefix) ? ' aria-current="page"' : "";
  return `<header class="topbar">
  <div class="topbar-inner">
    <button class="menu-toggle" type="button" aria-label="${escapeHtml(text.menuOpen)}" data-open-label="${
    escapeHtml(text.menuOpen)
  }" data-close-label="${
    escapeHtml(text.menuClose)
  }" aria-expanded="false" aria-controls="docs-nav">☰</button>
    <a class="brand" aria-label="Pier" href="${
    homePath(options.language)
  }"><img src="/assets/favicon.svg" alt="" width="28" height="28"><span>Pier</span></a>
    <span class="version-badge" title="${escapeHtml(text.versionBadge)}">v${
    escapeHtml(options.version)
  }</span>
    <nav class="topnav" aria-label="${escapeHtml(text.mainNav)}">
      <a href="${docsHome}"${current}>${escapeHtml(text.navDocs)}</a>
      <a href="${docPath(pageById("instalacao"), options.language)}">${escapeHtml(text.navInstall)}</a>
      <a href="${repository}" target="_blank" rel="noopener noreferrer">GitHub</a>
      ${languageSwitch(options.language, options.alternates)}
    </nav>
    <button class="theme-toggle" type="button" aria-label="${escapeHtml(text.themeToggle)}">◐</button>
  </div>
</header>`;
}

function footer(language: Language, version: string): string {
  const text = strings[language].layout;
  return `<footer class="site-footer">
  <div class="footer-inner">
    <span>${escapeHtml(text.footerStatus(version))}</span>
    <nav aria-label="${escapeHtml(text.footerNav)}">
      <a href="${docPath(sections[0].pages[0], language)}">${escapeHtml(text.footerDocs)}</a>
      <a href="${repository}" target="_blank" rel="noopener noreferrer">${escapeHtml(text.footerCode)}</a>
      <a href="${cliRepository}" target="_blank" rel="noopener noreferrer">pierctl</a>
      <a href="${repository}/releases" target="_blank" rel="noopener noreferrer">${
    escapeHtml(text.footerReleases)
  }</a>
    </nav>
  </div>
</footer>`;
}

/** hreflang links for every language plus x-default, and the Open Graph locales. */
function languageMetadata(language: Language, alternates?: Localized): string {
  const locales = [`<meta property="og:locale" content="${openGraphLocale(language)}">`];
  if (!alternates) return locales.join("\n");
  locales.push(`<meta property="og:locale:alternate" content="${openGraphLocale(otherLanguage(language))}">`);
  const links = languages.map((each) =>
    `<link rel="alternate" hreflang="${htmlLanguage(each)}" href="${alternates[each]}">`
  );
  links.push(`<link rel="alternate" hreflang="x-default" href="${alternates[defaultLanguage]}">`);
  return [...links, ...locales].join("\n");
}

export function shell(options: ShellOptions): string {
  const isHome = options.path === homePath(options.language);
  const fullTitle = isHome ? options.title : `${options.title} · Pier`;
  return `<!doctype html>
<html lang="${htmlLanguage(options.language)}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${escapeHtml(fullTitle)}</title>
<meta name="description" content="${escapeHtml(options.description)}">
<meta property="og:title" content="${escapeHtml(fullTitle)}">
<meta property="og:description" content="${escapeHtml(options.description)}">
${languageMetadata(options.language, options.alternates)}
<meta name="color-scheme" content="dark light">
<link rel="icon" href="/assets/favicon.svg" type="image/svg+xml">
<link rel="stylesheet" href="/assets/style.css">
${themeBootstrap}
</head>
<body class="${options.bodyClass ?? ""}">
<a class="skip-link" href="#conteudo">${escapeHtml(strings[options.language].layout.skipLink)}</a>
${header(options)}
${options.body}
${footer(options.language, options.version)}
<script src="/assets/site.js" defer></script>
</body>
</html>
`;
}

function sidebar(current: DocPage, language: Language): string {
  const groups = sections.map((section) => {
    const links = section.pages.map((page) => {
      const mark = page === current ? ' aria-current="page"' : "";
      return `<li><a href="${docPath(page, language)}"${mark}>${escapeHtml(page.title[language])}</a></li>`;
    }).join("");
    return `<div class="nav-group"><p class="nav-title">${
      escapeHtml(section.title[language])
    }</p><ul>${links}</ul></div>`;
  }).join("\n");
  return `<nav id="docs-nav" class="docs-nav" aria-label="${
    escapeHtml(strings[language].docs.sidebar)
  }">${groups}</nav>`;
}

function onThisPage(headings: Heading[], language: Language): string {
  const items = headings.filter((heading) => heading.depth === 2);
  if (items.length < 2) return "";
  const label = escapeHtml(strings[language].docs.onThisPage);
  const links = items.map((heading) => `<li><a href="#${heading.id}">${heading.text}</a></li>`).join("");
  return `<aside class="toc" aria-label="${label}"><p class="nav-title">${label}</p><ul>${links}</ul></aside>`;
}

function pager(page: DocPage, language: Language): string {
  const text = strings[language].docs;
  const { previous, next } = neighbours(page);
  const link = (target: DocPage | undefined, label: string, className: string) =>
    target
      ? `<a class="${className}" href="${docPath(target, language)}"><span>${escapeHtml(label)}</span>${
        escapeHtml(target.title[language])
      }</a>`
      : "<span></span>";
  return `<nav class="pager" aria-label="${escapeHtml(text.pager)}">${
    link(previous, text.previous, "pager-prev")
  }${link(next, text.next, "pager-next")}</nav>`;
}

function translationBanner(language: Language): string {
  return `<p class="translation-banner" role="note">${
    escapeHtml(strings[language].docs.translationInProgress)
  }</p>`;
}

export function docBody(page: DocPage, language: Language, content: DocContent): string {
  const body = content.translationPending
    ? `${translationBanner(language)}\n<div lang="${
      htmlLanguage(otherLanguage(language))
    }">${content.html}</div>`
    : content.html;
  return `<div class="docs-layout">
${sidebar(page, language)}
<main id="conteudo" class="doc">
  <article>
    <h1>${escapeHtml(page.title[language])}</h1>
    <p class="lead">${escapeHtml(page.description[language])}</p>
    ${body}
  </article>
  ${pager(page, language)}
</main>
${onThisPage(content.headings, language)}
</div>`;
}
