/**
 * The HTML shell shared by every page: header, documentation sidebar,
 * "nesta página" index and footer.
 */
import { type DocPage, docPath, neighbours, sections } from "./pages.ts";
import { escapeHtml, type Heading } from "./markdown.ts";
import { cliRepository, repository } from "./config.ts";

export interface ShellOptions {
  title: string;
  description: string;
  path: string;
  version: string;
  body: string;
  bodyClass?: string;
}

// Loaded synchronously in <head> so the saved theme applies before paint.
const themeBootstrap = `<script src="/assets/theme.js"></script>`;

function header(version: string, path: string): string {
  const current = (prefix: string) => (path.startsWith(prefix) ? ' aria-current="page"' : "");
  return `<header class="topbar">
  <div class="topbar-inner">
    <button class="menu-toggle" type="button" aria-label="Abrir o menu" aria-expanded="false" aria-controls="docs-nav">☰</button>
    <a class="brand" href="/"><img src="/assets/favicon.svg" alt="" width="28" height="28"><span>Pier</span></a>
    <span class="version-badge" title="Versão estável atual">v${escapeHtml(version)}</span>
    <nav class="topnav" aria-label="Principal">
      <a href="/docs/introducao/"${current("/docs/")}>Documentação</a>
      <a href="/docs/instalacao/">Instalar</a>
      <a href="${repository}" target="_blank" rel="noopener noreferrer">GitHub</a>
    </nav>
    <button class="theme-toggle" type="button" aria-label="Alternar tema claro ou escuro">◐</button>
  </div>
</header>`;
}

function footer(version: string): string {
  return `<footer class="site-footer">
  <div class="footer-inner">
    <span>Pier ${escapeHtml(version)} · em beta · self-hosted</span>
    <nav aria-label="Rodapé">
      <a href="/docs/introducao/">Documentação</a>
      <a href="${repository}" target="_blank" rel="noopener noreferrer">Código</a>
      <a href="${cliRepository}" target="_blank" rel="noopener noreferrer">pierctl</a>
      <a href="${repository}/releases" target="_blank" rel="noopener noreferrer">Versões</a>
    </nav>
  </div>
</footer>`;
}

export function shell(options: ShellOptions): string {
  const fullTitle = options.path === "/" ? options.title : `${options.title} · Pier`;
  return `<!doctype html>
<html lang="pt-BR">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${escapeHtml(fullTitle)}</title>
<meta name="description" content="${escapeHtml(options.description)}">
<meta property="og:title" content="${escapeHtml(fullTitle)}">
<meta property="og:description" content="${escapeHtml(options.description)}">
<meta name="color-scheme" content="dark light">
<link rel="icon" href="/assets/favicon.svg" type="image/svg+xml">
<link rel="stylesheet" href="/assets/style.css">
${themeBootstrap}
</head>
<body class="${options.bodyClass ?? ""}">
<a class="skip-link" href="#conteudo">Pular para o conteúdo</a>
${header(options.version, options.path)}
${options.body}
${footer(options.version)}
<script src="/assets/site.js" defer></script>
</body>
</html>
`;
}

function sidebar(currentSlug: string): string {
  const groups = sections.map((section) => {
    const links = section.pages.map((page) => {
      const current = page.slug === currentSlug ? ' aria-current="page"' : "";
      return `<li><a href="${docPath(page)}"${current}>${escapeHtml(page.title)}</a></li>`;
    }).join("");
    return `<div class="nav-group"><p class="nav-title">${escapeHtml(section.title)}</p><ul>${links}</ul></div>`;
  }).join("\n");
  return `<nav id="docs-nav" class="docs-nav" aria-label="Documentação">${groups}</nav>`;
}

function onThisPage(headings: Heading[]): string {
  const items = headings.filter((heading) => heading.depth === 2);
  if (items.length < 2) return "";
  const links = items.map((heading) => `<li><a href="#${heading.id}">${heading.text}</a></li>`).join("");
  return `<aside class="toc" aria-label="Nesta página"><p class="nav-title">Nesta página</p><ul>${links}</ul></aside>`;
}

function pager(slug: string): string {
  const { previous, next } = neighbours(slug);
  const link = (page: DocPage | undefined, label: string, className: string) =>
    page
      ? `<a class="${className}" href="${docPath(page)}"><span>${label}</span>${escapeHtml(page.title)}</a>`
      : "<span></span>";
  return `<nav class="pager" aria-label="Navegação entre páginas">${link(previous, "Anterior", "pager-prev")}${
    link(next, "Próxima", "pager-next")
  }</nav>`;
}

export function docBody(page: DocPage, html: string, headings: Heading[]): string {
  return `<div class="docs-layout">
${sidebar(page.slug)}
<main id="conteudo" class="doc">
  <article>
    <h1>${escapeHtml(page.title)}</h1>
    <p class="lead">${escapeHtml(page.description)}</p>
    ${html}
  </article>
  ${pager(page.slug)}
</main>
${onThisPage(headings)}
</div>`;
}
